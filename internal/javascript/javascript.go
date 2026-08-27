// Package javascript is the one direct adapter between the Go policy engine and
// the sealed, policy-owned JavaScript tool bundle.
//
// Go decides policy. This package exchanges one bounded JSON request and one
// bounded JSON response with the fixed runner entry point installed inside the
// bundle, launched by the pinned Node runtime under an environment built from
// nothing. Tool-native objects never cross the boundary.
//
// There is no provider registry, capability resolver, or target-selected
// implementation: the runtime and the runner live at exactly one policy-owned
// path, and nothing here consults PATH, a target's node_modules, a user cache,
// or a global installation.
package javascript

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/release"
	"github.com/riteofstring/code-polishy/internal/runner"
)

// ProtocolVersion is the exact version the sealed runner admits. There is no
// negotiation and no compatibility range: a runner that answers with another
// version is rejected rather than interpreted.
const ProtocolVersion = 2

// Operation is the closed set of requests the sealed runner answers.
type Operation string

const (
	// OperationProvenance asks the installed bundle what it is. It is the only
	// operation that reads nothing but the bundle's own installed bytes.
	OperationProvenance Operation = "provenance"
	// OperationFormat reports which selected files the sealed central
	// formatting configuration would rewrite.
	OperationFormat Operation = "format"
	// OperationFormatWrite rewrites them. Writing is a distinct operation
	// rather than a flag, so only a caller that asked for it can get it.
	OperationFormatWrite Operation = "format-write"
	// OperationLint reports the rule violations, inline directives, and
	// undecidable files in a selection, under budgets and framework rule
	// activation this side decided.
	OperationLint Operation = "lint"
	// OperationTypeCheck reports the type and syntax diagnostics of one
	// contained TypeScript project, and which of the selected files that
	// project actually covers.
	OperationTypeCheck Operation = "typecheck"
	// OperationDeadCode reports the governed source no entry point reaches and
	// the exported symbols nothing uses, across one tree of packages.
	OperationDeadCode Operation = "deadcode"
	// OperationImports reports what the selected files import and which file
	// inside the target tree each specifier names.
	OperationImports Operation = "imports"
	// OperationPackages reports the workspace packages of one pnpm project,
	// what each of them declared, and the graph its lockfile resolved.
	OperationPackages Operation = "packages"
	// OperationWorkspace reports the settings a named pnpm workspace file
	// declares, including the native supply-chain and lifecycle-script ones.
	OperationWorkspace Operation = "workspace"
	// OperationLicenses reports the license expression every release installed
	// for one pnpm project declares in its own manifest.
	OperationLicenses Operation = "licenses"
	// OperationAudit reports the advisories the pinned pnpm's native audit
	// returns for one pnpm project. It is the only operation that contacts a
	// registry, so it runs only where Go asks for it by name.
	OperationAudit Operation = "audit"
)

const (
	// The runner enforces the same request limit, so the adapter refuses to
	// send what the runner would refuse to read.
	maximumRequestBytes = 1 << 20
	// A response carries bounded facts, never a tool's native output.
	maximumResponseBytes = 8 << 20
	// Enough of a failing runner's diagnostics to explain the failure.
	maximumStderrBytes = 64 << 10
	// The runner enforces the same path limit, so one operation can never be
	// handed an unbounded selection.
	maximumOperationPaths = 4096
	// How long a cancelled child may keep the response pipes open before this
	// process stops waiting for it.
	cleanupDelay = 5 * time.Second
	// Provenance reads three small JSON files inside the bundle.
	provenanceTimeout = 60 * time.Second
	// Formatting parses and prints every selected file once.
	formatTimeout = 10 * time.Minute
	// Linting parses every selected file once and runs the activated rules
	// over it, which costs more per file than printing it does.
	lintTimeout = 15 * time.Minute
	// Type checking builds one project's whole program, including the
	// declarations it depends on, and checks every file in it.
	typeCheckTimeout = 20 * time.Minute
	// Dead-code analysis resolves every import in a tree of packages before it
	// can say what nothing reaches, so it reads at least as much as a type
	// check does.
	deadCodeTimeout = 20 * time.Minute
	// Reading import facts parses every selected file once and resolves each
	// specifier it wrote, which costs less than checking the program those
	// files form.
	importsTimeout = 15 * time.Minute
	// Reading pnpm facts parses one lockfile and one manifest per workspace
	// package, and resolves nothing.
	packagesTimeout = 5 * time.Minute
	// Reading pnpm workspace settings parses one small settings file.
	workspaceTimeout = 60 * time.Second
	// Reading license metadata opens one small manifest per installed release,
	// so it is bounded by how many packages a project installed.
	licensesTimeout = 10 * time.Minute
	// A native audit sends one project's resolved graph to a registry and waits
	// for its advisories, so it is bounded by a network answer rather than by
	// how much source it reads.
	auditTimeout = 15 * time.Minute
)

// Bundle is the sealed JavaScript tool bundle installed under one Code Polishy
// checkout.
type Bundle struct {
	// PolicyRoot is the absolute path of the Code Polishy checkout that owns the
	// installed runtime and bundle.
	PolicyRoot string
}

// Provenance is what an installed bundle reports about itself: the digest over
// its installed bytes, both pinned runtime versions, and the exact version of
// every analyzer installed beside the runner.
type Provenance struct {
	BundleDigest string            `json:"bundleDigest"`
	Node         string            `json:"node"`
	Pnpm         string            `json:"pnpm"`
	Tools        map[string]string `json:"tools"`
}

// Provenance launches the sealed bundle and reports what actually ran.
func (bundle Bundle) Provenance(ctx context.Context) (Provenance, error) {
	result, err := bundle.exchange(ctx, request{Operation: OperationProvenance}, provenanceTimeout)
	if err != nil {
		return Provenance{}, err
	}
	var reported Provenance
	if err := decodeExactly(result, &reported); err != nil {
		return Provenance{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable provenance result: %w", err)
	}
	if reported.BundleDigest == "" || reported.Node == "" || reported.Pnpm == "" || len(reported.Tools) == 0 {
		return Provenance{}, fmt.Errorf("the sealed JavaScript bundle reported incomplete provenance")
	}
	return reported, nil
}

// FormatResult is what the sealed formatter reports about a selection: the
// files its central configuration formats differently, and the files it could
// not decide at all. Go turns both into findings; neither is a policy decision.
type FormatResult struct {
	Changed     []string      `json:"changed"`
	Unsupported []Unsupported `json:"unsupported"`
}

// Unsupported names one selected file the bundle refused to decide, with the
// reason. Unsupported coverage fails closed rather than counting as clean.
type Unsupported struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Format reports which of the selected files the sealed central formatting
// configuration would rewrite, without touching any of them.
func (bundle Bundle) Format(ctx context.Context, root string, paths []string) (FormatResult, error) {
	return bundle.format(ctx, OperationFormat, root, paths)
}

// FormatWrite rewrites the selected files the sealed central formatting
// configuration formats differently, and reports which ones it rewrote.
func (bundle Bundle) FormatWrite(ctx context.Context, root string, paths []string) (FormatResult, error) {
	return bundle.format(ctx, OperationFormatWrite, root, paths)
}

func (bundle Bundle) format(ctx context.Context, operation Operation, root string, paths []string) (FormatResult, error) {
	payload, err := fileRequest(operation, root, paths)
	if err != nil {
		return FormatResult{}, err
	}
	result, err := bundle.exchange(ctx, payload, formatTimeout)
	if err != nil {
		return FormatResult{}, err
	}
	var reported FormatResult
	if err := decodeExactly(result, &reported); err != nil {
		return FormatResult{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable %s result: %w", operation, err)
	}
	return reported, nil
}

// LintLimits are the exact ESLint allowed maximums one lint operation runs
// under. Go translates its own fail-at budgets into these before asking, so the
// bundle never defaults, scales, or reinterprets a policy threshold.
type LintLimits struct {
	Complexity int `json:"complexity"`
	Depth      int `json:"depth"`
	Parameters int `json:"parameters"`
}

// LintActivation is the closed set of framework rule groups a lint operation
// may run in addition to the budgets. A target selects none of it: activation
// follows from the conditional policy modules Go already resolved.
type LintActivation struct {
	ReactHooks       bool `json:"reactHooks"`
	JSXAccessibility bool `json:"jsxAccessibility"`
}

// LintResult is what the sealed linter reports about a selection: the rule
// violations it found, the inline directives it refused to honor, and the files
// it could not decide at all.
type LintResult struct {
	Findings    []LintViolation `json:"findings"`
	Directives  []LintDirective `json:"directives"`
	Unsupported []Unsupported   `json:"unsupported"`
}

// LintViolation is one rule violation, attributed to the exact rule that
// produced it. Whether it fails a check is a Go decision.
type LintViolation struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// LintDirective is one inline configuration comment in the selection. The
// sealed configuration ignores it; reporting it is how an ignored directive
// stops looking authoritative.
type LintDirective struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

// Lint reports what the sealed lint configuration finds in the selected files
// under exactly these budgets and this activation.
func (bundle Bundle) Lint(ctx context.Context, root string, paths []string, limits LintLimits, activation LintActivation) (LintResult, error) {
	payload, err := fileRequest(OperationLint, root, paths)
	if err != nil {
		return LintResult{}, err
	}
	if limits.Complexity < 1 || limits.Depth < 1 || limits.Parameters < 1 {
		return LintResult{}, fmt.Errorf("the lint request declares budgets %+v, not positive allowed maximums", limits)
	}
	payload.Limits = &limits
	payload.Activation = &activation
	result, err := bundle.exchange(ctx, payload, lintTimeout)
	if err != nil {
		return LintResult{}, err
	}
	var reported LintResult
	if err := decodeExactly(result, &reported); err != nil {
		return LintResult{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable %s result: %w", OperationLint, err)
	}
	return reported, nil
}

// TypeCheckResult is what the sealed type checker reports about one project:
// the diagnostics its program produced, which of the selected files that
// program actually covered, and why a project could not be analyzed at all.
type TypeCheckResult struct {
	Diagnostics []TypeDiagnostic `json:"diagnostics"`
	Covered     []string         `json:"covered"`
	Unsupported []Unsupported    `json:"unsupported"`
}

// TypeDiagnostic is one type or syntax diagnostic, attributed to the exact
// TypeScript code that produced it. Whether it fails a check is a Go decision.
type TypeDiagnostic struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// TypeCheck reports what the TypeScript compiler finds in one contained
// project, and which of the selected files that project covers.
//
// The project is repository-relative JSON/JSONC data the target owns. The
// bundle reads it, never executes it, and refuses an extension chain that
// leaves the repository, a compiler plug-in, or a project reference rather than
// analyzing the project under a guess.
func (bundle Bundle) TypeCheck(ctx context.Context, root, project string, paths []string) (TypeCheckResult, error) {
	payload, err := fileRequest(OperationTypeCheck, root, paths)
	if err != nil {
		return TypeCheckResult{}, err
	}
	if !containedPath(project) {
		return TypeCheckResult{}, fmt.Errorf("the %s request names project %q, not a contained repository-relative path", OperationTypeCheck, project)
	}
	payload.Project = project
	result, err := bundle.exchange(ctx, payload, typeCheckTimeout)
	if err != nil {
		return TypeCheckResult{}, err
	}
	var reported TypeCheckResult
	if err := decodeExactly(result, &reported); err != nil {
		return TypeCheckResult{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable %s result: %w", OperationTypeCheck, err)
	}
	return reported, nil
}

// DeadCodeWorkspace is one package a dead-code analysis covers: the governed
// source it contributes, and which of those files are entry points. Both are
// Go decisions; the bundle discovers neither.
type DeadCodeWorkspace struct {
	Root    string   `json:"root"`
	Entry   []string `json:"entry"`
	Project []string `json:"project"`
}

// DeadCodeResult is what the sealed dead-code analyzer reports about one tree
// of packages: the files no entry point reaches, the exported symbols nothing
// uses, which of the selected files it actually analyzed, and the files it
// could not address at all.
type DeadCodeResult struct {
	UnusedFiles   []string       `json:"unusedFiles"`
	UnusedExports []UnusedExport `json:"unusedExports"`
	Covered       []string       `json:"covered"`
	Unsupported   []Unsupported  `json:"unsupported"`
}

// UnusedExport is one exported symbol nothing uses, attributed to the exact
// analyzer issue type that produced it. Whether it fails a check is a Go
// decision.
type UnusedExport struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Symbol string `json:"symbol"`
	Kind   string `json:"kind"`
}

// DeadCode reports what the sealed dead-code analyzer finds in one tree of
// packages rooted at directory.
//
// The analysis covers a whole tree at once because reachability is a property
// of the tree: an export used only by a sibling package is used. Go decides
// which packages the tree contains, which governed files each contributes, and
// which of those are entry points, so the bundle reads no analyzer
// configuration and loads no target configuration file to learn any of it.
func (bundle Bundle) DeadCode(ctx context.Context, root, directory string, workspaces []DeadCodeWorkspace) (DeadCodeResult, error) {
	payload, err := deadCodeRequest(root, directory, workspaces)
	if err != nil {
		return DeadCodeResult{}, err
	}
	result, err := bundle.exchange(ctx, payload, deadCodeTimeout)
	if err != nil {
		return DeadCodeResult{}, err
	}
	var reported DeadCodeResult
	if err := decodeExactly(result, &reported); err != nil {
		return DeadCodeResult{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable %s result: %w", OperationDeadCode, err)
	}
	return reported, nil
}

// ImportResult is what the sealed import reader reports about a selection: the
// imports its files declare, and the files it could not read at all. Go turns
// both into findings; neither is a policy decision.
type ImportResult struct {
	Imports     []ImportFact  `json:"imports"`
	Unsupported []Unsupported `json:"unsupported"`
}

// ImportFact is one import a file declares. Resolved is the target-relative
// file the specifier names, and is empty when the specifier names nothing
// inside the target tree: an external package, a dependency the target has not
// installed, and a build artifact all look like that, and Go decides what each
// of them means. Package is the external package the specifier reaches, and is
// empty when it reaches none: a path, a subpath import, a module the pinned
// runtime provides, and text that is no package name at all.
type ImportFact struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Specifier string `json:"specifier"`
	Resolved  string `json:"resolved"`
	Package   string `json:"package"`
}

// Imports reports what the selected files import, which file inside the target
// tree each specifier names, and which external package it reaches.
//
// Resolution belongs to the bundle because it is TypeScript's answer rather
// than a path rewrite: the extension a specifier omits, the entry a package's
// exports map selects, and the sibling package a workspace link points at are
// all ecosystem mechanics Go must not approximate. Which specifiers name a
// module the runtime itself provides belongs to it for the same reason: that
// set is the pinned Node's, not a list Go would have to keep in step with it.
func (bundle Bundle) Imports(ctx context.Context, root string, paths []string) (ImportResult, error) {
	payload, err := fileRequest(OperationImports, root, paths)
	if err != nil {
		return ImportResult{}, err
	}
	result, err := bundle.exchange(ctx, payload, importsTimeout)
	if err != nil {
		return ImportResult{}, err
	}
	var reported ImportResult
	if err := decodeExactly(result, &reported); err != nil {
		return ImportResult{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable %s result: %w", OperationImports, err)
	}
	return reported, nil
}

// PackageResult is what the sealed reader reports about one pnpm project: the
// workspace packages its lockfile covers, and the graph that lockfile resolved.
// Each resolved release also says whether this host should have materialized
// license metadata for it. A lock it could not read at all is unsupported
// coverage, never a clean graph.
type PackageResult struct {
	LockfileVersion string          `json:"lockfileVersion"`
	Importers       []Importer      `json:"importers"`
	Packages        []LockedPackage `json:"packages"`
	Unsupported     []Unsupported   `json:"unsupported"`
}

// Importer is one workspace package of a pnpm project: where it lives, the
// manifest that declares it, and every dependency either side names.
type Importer struct {
	Path         string       `json:"path"`
	Manifest     string       `json:"manifest"`
	Dependencies []Dependency `json:"dependencies"`
}

// Dependency is one declaration seen from both sides at once. Declared is the
// exact text the manifest wrote and Specifier the exact text the lock recorded,
// so Go decides drift by comparing them rather than by asking pnpm to install.
// An empty Declared means the lock resolves something no manifest asked for,
// and an empty Specifier means the manifest asks for something the lock never
// resolved.
//
// ResolvedName differs from Name when the declaration is an alias, and Link
// names the workspace package a declaration points at instead of a release.
type Dependency struct {
	Name            string `json:"name"`
	Scope           string `json:"scope"`
	Declared        string `json:"declared"`
	Specifier       string `json:"specifier"`
	ResolvedName    string `json:"resolvedName"`
	ResolvedVersion string `json:"resolvedVersion"`
	Link            string `json:"link"`
}

// LicenseMetadata is the closed host-applicability fact the sealed pnpm reader
// reports for installed license metadata. It is intentionally not a policy
// decision: supply-chain policy decides how each state affects coverage.
type LicenseMetadata string

const (
	// LicenseMetadataRequired says a normal install on this host should contain
	// the release's manifest, so absent metadata is missing coverage.
	LicenseMetadataRequired LicenseMetadata = "required"
	// LicenseMetadataPlatformExcluded says every optional lock context excludes
	// the current policy-owned Node host.
	LicenseMetadataPlatformExcluded LicenseMetadata = "platform-excluded"
	// LicenseMetadataUnknown says malformed or undecidable platform metadata
	// prevented the sealed reader from proving the release is absent by design.
	LicenseMetadataUnknown LicenseMetadata = "unknown"
)

// LockedPackage is one package the lockfile resolved, with the kind of source
// it came from and the closed host fact for its installed license metadata.
// Only "registry" records the integrity that makes installed bytes checkable;
// Go decides what every source and metadata state means.
type LockedPackage struct {
	Name            string          `json:"name"`
	Version         string          `json:"version"`
	Source          string          `json:"source"`
	LicenseMetadata LicenseMetadata `json:"licenseMetadata"`
}

// Packages reports the workspace and resolved graph of the pnpm project rooted
// at directory.
//
// The lockfile is the exact record of what a target installs, and it is YAML,
// so the bundle reads it with the parser pnpm reads it with. Go keeps every
// decision that follows: which declaration must be exact, whether the lock
// still agrees with the manifests, which sources are admissible, and how old or
// vulnerable a resolved release may be.
func (bundle Bundle) Packages(ctx context.Context, root, directory string) (PackageResult, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return PackageResult{}, fmt.Errorf("the %s target root %q is not a normal absolute path", OperationPackages, root)
	}
	if !containedPath(directory) {
		return PackageResult{}, fmt.Errorf("the %s request names directory %q, not a contained repository-relative path", OperationPackages, directory)
	}
	payload := request{Operation: OperationPackages, Root: root, Directory: directory}
	result, err := bundle.exchange(ctx, payload, packagesTimeout)
	if err != nil {
		return PackageResult{}, err
	}
	var reported PackageResult
	if err := decodeExactly(result, &reported); err != nil {
		return PackageResult{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable %s result: %w", OperationPackages, err)
	}
	for _, item := range reported.Packages {
		if !validLicenseMetadata(item.LicenseMetadata) {
			return PackageResult{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable %s result: package %q reports licenseMetadata %q, not required, platform-excluded, or unknown", OperationPackages, item.Name+"@"+item.Version, item.LicenseMetadata)
		}
	}
	return reported, nil
}

func validLicenseMetadata(metadata LicenseMetadata) bool {
	switch metadata {
	case LicenseMetadataRequired, LicenseMetadataPlatformExcluded, LicenseMetadataUnknown:
		return true
	default:
		return false
	}
}

// WorkspaceResult is what the sealed reader reports about the named pnpm
// workspace files: what each one declares, and which of them it could not read
// at all. A file missing from Files is unsupported coverage, never a file that
// declares nothing.
type WorkspaceResult struct {
	Files       []WorkspaceFile `json:"files"`
	Unsupported []Unsupported   `json:"unsupported"`
}

// WorkspaceFile is one pnpm settings file and every setting written in it.
type WorkspaceFile struct {
	Path     string             `json:"path"`
	Settings []WorkspaceSetting `json:"settings"`
}

// WorkspaceSetting is one declared pnpm setting, with every scalar it was
// written with in document order. A setting written as one scalar reports one
// value, a sequence reports each of its entries, and a setting written with no
// scalar at all reports none.
type WorkspaceSetting struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// Workspace reports what the named pnpm workspace files declare.
//
// pnpm resolves under the settings written in that file, and it is YAML, so the
// bundle reads it with the parser pnpm reads it with. Go keeps every decision
// that follows: which file may own a workspace's settings, which protections a
// pinned pnpm version must carry, and what each declared value has to be.
func (bundle Bundle) Workspace(ctx context.Context, root string, paths []string) (WorkspaceResult, error) {
	payload, err := fileRequest(OperationWorkspace, root, paths)
	if err != nil {
		return WorkspaceResult{}, err
	}
	result, err := bundle.exchange(ctx, payload, workspaceTimeout)
	if err != nil {
		return WorkspaceResult{}, err
	}
	var reported WorkspaceResult
	if err := decodeExactly(result, &reported); err != nil {
		return WorkspaceResult{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable %s result: %w", OperationWorkspace, err)
	}
	return reported, nil
}

// LicenseResult is what the sealed reader found in one pnpm project's installed
// dependency tree: the release each stored package is, and the license its own
// manifest declares. A tree it could not read is unsupported coverage, never a
// project whose dependencies declare nothing.
type LicenseResult struct {
	Packages    []InstalledPackage `json:"packages"`
	Unsupported []Unsupported      `json:"unsupported"`
}

// InstalledPackage is one installed release and the exact license text its
// manifest wrote. An empty License is a release that declared no expression at
// all, which is a policy decision rather than a reading failure: Go parses the
// expression, compares it with the configured policy, and decides both.
type InstalledPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	License string `json:"license"`
}

// Licenses reports the license every release installed for the pnpm project
// rooted at directory declares.
//
// A lockfile records what a target installs, never what those packages are
// licensed under, so the fact exists only in each installed manifest. Reading
// them is metadata only: no target code, install script, or executable
// configuration runs, and no registry is contacted.
func (bundle Bundle) Licenses(ctx context.Context, root, directory string) (LicenseResult, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return LicenseResult{}, fmt.Errorf("the %s target root %q is not a normal absolute path", OperationLicenses, root)
	}
	if !containedPath(directory) {
		return LicenseResult{}, fmt.Errorf("the %s request names directory %q, not a contained repository-relative path", OperationLicenses, directory)
	}
	payload := request{Operation: OperationLicenses, Root: root, Directory: directory}
	result, err := bundle.exchange(ctx, payload, licensesTimeout)
	if err != nil {
		return LicenseResult{}, err
	}
	var reported LicenseResult
	if err := decodeExactly(result, &reported); err != nil {
		return LicenseResult{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable %s result: %w", OperationLicenses, err)
	}
	return reported, nil
}

// AuditResult is what the pinned pnpm's native audit reported about one pnpm
// project: the advisories it returned, and the ones the reader could not use.
type AuditResult struct {
	Advisories  []Advisory    `json:"advisories"`
	Unsupported []Unsupported `json:"unsupported"`
}

// AuditInvocation describes the policy-owned process that performs the native
// audit. The supply-chain adapter owns its conversion to a governed command;
// this bundle adapter stays independent of policy decisions.
type AuditInvocation struct {
	Argv              []string
	Cwd               string
	SealedEnvironment bool
	TimeoutSeconds    int
}

// Advisory is one reported vulnerability as facts rather than as the registry's
// own object: who it is, which package it names, how the registry rated it, and
// the exact installed versions it was reported against. Go decides the
// threshold, the assessments, and how it reconciles with the OSV lane.
type Advisory struct {
	ID       string   `json:"id"`
	Aliases  []string `json:"aliases"`
	Package  string   `json:"package"`
	Severity string   `json:"severity"`
	Title    string   `json:"title"`
	Versions []string `json:"versions"`
}

// AuditCommand is the one policy-owned command that reaches the npm registry.
// It carries one closed audit request as an argument so callers can run it
// through the common command boundary and retain the response as governed
// output. The sealed environment prevents user package configuration,
// credentials, caches, or loaders from changing the audit.
func (bundle Bundle) AuditCommand(root, directory string) (AuditInvocation, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return AuditInvocation{}, fmt.Errorf("the %s target root %q is not a normal absolute path", OperationAudit, root)
	}
	if !containedPath(directory) {
		return AuditInvocation{}, fmt.Errorf("the %s request names directory %q, not a contained repository-relative path", OperationAudit, directory)
	}
	payload := request{Operation: OperationAudit, Root: root, Directory: directory}
	node, sealedRunner, err := bundle.paths()
	if err != nil {
		return AuditInvocation{}, err
	}
	payload.ProtocolVersion = ProtocolVersion
	encoded, err := json.Marshal(payload)
	if err != nil {
		return AuditInvocation{}, fmt.Errorf("encode the %s request: %w", OperationAudit, err)
	}
	if len(encoded) > maximumRequestBytes {
		return AuditInvocation{}, fmt.Errorf("the %s request exceeds the %d byte limit", OperationAudit, maximumRequestBytes)
	}
	return AuditInvocation{
		Argv: []string{node, sealedRunner, "--request-json", string(encoded)}, Cwd: ".",
		SealedEnvironment: true, TimeoutSeconds: int(auditTimeout / time.Second),
	}, nil
}

// ParseAuditOutput turns the response of AuditCommand into the bounded facts
// Go uses for policy. A response-level error remains a failed command result,
// never an empty advisory set.
func ParseAuditOutput(output []byte) (AuditResult, error) {
	if len(output) > maximumResponseBytes {
		return AuditResult{}, fmt.Errorf("the sealed JavaScript bundle %s response exceeds the %d byte limit", OperationAudit, maximumResponseBytes)
	}
	result, err := decodeResponse(OperationAudit, output, "", nil)
	if err != nil {
		return AuditResult{}, err
	}
	var reported AuditResult
	if err := decodeExactly(result, &reported); err != nil {
		return AuditResult{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable %s result: %w", OperationAudit, err)
	}
	return reported, nil
}

func deadCodeRequest(root, directory string, workspaces []DeadCodeWorkspace) (request, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return request{}, fmt.Errorf("the %s target root %q is not a normal absolute path", OperationDeadCode, root)
	}
	if !containedPath(directory) {
		return request{}, fmt.Errorf("the %s request names directory %q, not a contained repository-relative path", OperationDeadCode, directory)
	}
	if len(workspaces) == 0 {
		return request{}, fmt.Errorf("the %s request declares no packages", OperationDeadCode)
	}
	files := 0
	declared := make([]DeadCodeWorkspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.Entry == nil {
			workspace.Entry = []string{}
		}
		if err := containedWorkspace(directory, workspace); err != nil {
			return request{}, err
		}
		declared = append(declared, workspace)
		files += len(workspace.Project)
	}
	if files > maximumOperationPaths {
		return request{}, fmt.Errorf("the %s request selects %d files, more than the %d limit", OperationDeadCode, files, maximumOperationPaths)
	}
	return request{Operation: OperationDeadCode, Root: root, Directory: directory, Workspaces: declared}, nil
}

// One package of the analyzed tree: inside the tree, selecting something, and
// naming only files it contains.
func containedWorkspace(directory string, workspace DeadCodeWorkspace) error {
	if !containedPath(workspace.Root) || !containsPath(directory, workspace.Root) {
		return fmt.Errorf("the %s request declares package %q outside %q", OperationDeadCode, workspace.Root, directory)
	}
	if len(workspace.Project) == 0 {
		return fmt.Errorf("the %s package %q selects no files", OperationDeadCode, workspace.Root)
	}
	for _, path := range slices.Concat(workspace.Project, workspace.Entry) {
		if !containedPath(path) || !containsPath(workspace.Root, path) {
			return fmt.Errorf("the %s package %q selects %q, which it does not contain", OperationDeadCode, workspace.Root, path)
		}
	}
	return nil
}

// containsPath reports whether a contained relative path names something inside
// a contained relative directory.
func containsPath(directory, path string) bool {
	return directory == "." || path == directory || strings.HasPrefix(path, directory+"/")
}

// fileRequest builds the one shape a file operation may ask for: an absolute,
// normal target root and clean repository-relative paths inside it. The runner
// enforces the same rules, so a path that could name another tree is refused on
// both sides rather than resolved by either.
func fileRequest(operation Operation, root string, paths []string) (request, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return request{}, fmt.Errorf("the %s target root %q is not a normal absolute path", operation, root)
	}
	if len(paths) == 0 {
		return request{}, fmt.Errorf("the %s request selects no files", operation)
	}
	if len(paths) > maximumOperationPaths {
		return request{}, fmt.Errorf("the %s request selects %d files, more than the %d limit", operation, len(paths), maximumOperationPaths)
	}
	for _, path := range paths {
		if !containedPath(path) {
			return request{}, fmt.Errorf("the %s request selects %q, not a contained repository-relative path", operation, path)
		}
	}
	return request{Operation: operation, Root: root, Paths: paths}, nil
}

// containedPath reports whether a selected path names a file inside the target
// root and nowhere else: relative, slash separated, already clean, and never
// climbing out.
func containedPath(path string) bool {
	if path == "" || strings.Contains(path, `\`) || filepath.IsAbs(path) {
		return false
	}
	if path != filepath.ToSlash(filepath.Clean(path)) {
		return false
	}
	return path != ".." && !strings.HasPrefix(path, "../")
}

// request carries exactly the fields the requested operation admits. The
// pointers are how an operation that takes no budgets sends none: the runner
// rejects a field it did not ask for as firmly as a missing one.
type request struct {
	ProtocolVersion int                 `json:"protocolVersion"`
	Operation       Operation           `json:"operation"`
	Root            string              `json:"root,omitempty"`
	Paths           []string            `json:"paths,omitempty"`
	Limits          *LintLimits         `json:"limits,omitempty"`
	Activation      *LintActivation     `json:"activation,omitempty"`
	Project         string              `json:"project,omitempty"`
	Directory       string              `json:"directory,omitempty"`
	Workspaces      []DeadCodeWorkspace `json:"workspaces,omitempty"`
}

type response struct {
	ProtocolVersion int             `json:"protocolVersion"`
	Operation       Operation       `json:"operation"`
	Error           string          `json:"error"`
	Result          json.RawMessage `json:"result"`
}

// exchange runs one operation to completion and returns its raw result payload.
// Every bound, the closed environment, and the contained working directory are
// enforced here, so no operation can opt out of them.
func (bundle Bundle) exchange(parent context.Context, payload request, timeout time.Duration) (json.RawMessage, error) {
	operation := payload.Operation
	node, runnerPath, err := bundle.paths()
	if err != nil {
		return nil, err
	}
	payload.ProtocolVersion = ProtocolVersion
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode the %s request: %w", operation, err)
	}
	if len(encoded) > maximumRequestBytes {
		return nil, fmt.Errorf("the %s request exceeds the %d byte limit", operation, maximumRequestBytes)
	}
	scratch, err := newScratch()
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch.root)

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	// An argument array launched directly: there is no shell to evaluate, and
	// the working directory is a scratch directory this process deletes, so
	// neither the target tree nor the invoking directory can supply a module.
	stdout := &boundedWriter{limit: maximumResponseBytes}
	stderr := &boundedWriter{limit: maximumStderrBytes}
	_, runErr := runner.Run(ctx, runner.HostCommand{
		Path: node, Argv: []string{node, runnerPath}, Directory: scratch.work,
		Environment: scratch.environment(), Stdin: bytes.NewReader(encoded),
		Stdout: stdout, Stderr: stderr,
	})

	if parent.Err() != nil {
		return nil, parent.Err()
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("the sealed JavaScript bundle %s operation timed out after %s", operation, timeout)
	}
	if stdout.truncated {
		return nil, fmt.Errorf("the sealed JavaScript bundle %s response exceeds the %d byte limit", operation, maximumResponseBytes)
	}
	return decodeResponse(operation, stdout.buffer.Bytes(), stderr.buffer.String(), runErr)
}

func decodeResponse(operation Operation, stdout []byte, stderr string, runErr error) (json.RawMessage, error) {
	var decoded response
	if err := decodeExactly(stdout, &decoded); err != nil {
		if runErr != nil {
			return nil, fmt.Errorf("the sealed JavaScript bundle failed the %s operation: %w%s", operation, runErr, diagnostics(stderr))
		}
		return nil, fmt.Errorf("the sealed JavaScript bundle returned an unreadable %s response: %w", operation, err)
	}
	if decoded.ProtocolVersion != ProtocolVersion {
		return nil, fmt.Errorf("the sealed JavaScript bundle answered with protocol version %d, not %d", decoded.ProtocolVersion, ProtocolVersion)
	}
	if decoded.Error != "" {
		return nil, fmt.Errorf("the sealed JavaScript bundle rejected the %s request: %s", operation, decoded.Error)
	}
	if runErr != nil {
		return nil, fmt.Errorf("the sealed JavaScript bundle failed the %s operation: %w%s", operation, runErr, diagnostics(stderr))
	}
	if decoded.Operation != operation {
		return nil, fmt.Errorf("the sealed JavaScript bundle answered the %s request with %q", operation, decoded.Operation)
	}
	if len(decoded.Result) == 0 {
		return nil, fmt.Errorf("the sealed JavaScript bundle returned no %s result", operation)
	}
	return decoded.Result, nil
}

// decodeExactly accepts one JSON value with exactly the declared fields. An
// unknown field is a protocol disagreement, never something to ignore.
func decodeExactly(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("unexpected trailing content")
	}
	return nil
}

func diagnostics(stderr string) string {
	if stderr == "" {
		return ""
	}
	return ": " + stderr
}

// paths resolves the one installed runtime and the one installed runner. Both
// are absolute and policy owned, and a missing installation names its installer
// rather than falling back to anything ambient.
func (bundle Bundle) paths() (node, runner string, err error) {
	if !filepath.IsAbs(bundle.PolicyRoot) {
		return "", "", fmt.Errorf("the Code Polishy checkout path %q is not absolute", bundle.PolicyRoot)
	}
	// The per-host runtime directory is named by the same tuple a release
	// records, because the sealed runtime is exactly why an installed release
	// is host specific at all.
	host, err := release.Host()
	if err != nil {
		return "", "", err
	}
	root := filepath.Join(bundle.PolicyRoot, ".tools", "javascript")
	node = filepath.Join(root, host, "node", "bin", javascriptExecutable("node", runtime.GOOS))
	runner = filepath.Join(root, "bundle", "runner.mjs")
	if !isExecutableFile(node) || !isRegularFile(runner) {
		return "", "", fmt.Errorf("the sealed JavaScript tool bundle is not installed under %s; run ./tools/install-policy-tools.sh", root)
	}
	return node, runner, nil
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && executableModeSupported(runtime.GOOS, info.Mode())
}

func javascriptExecutable(name, goos string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}

func executableModeSupported(goos string, mode os.FileMode) bool {
	return goos == "windows" || mode&0o111 != 0
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// boundedWriter keeps at most limit bytes and records that more arrived, so a
// runaway child cannot grow the policy process.
type boundedWriter struct {
	limit     int
	buffer    bytes.Buffer
	truncated bool
}

func (bounded *boundedWriter) Write(data []byte) (int, error) {
	if remaining := bounded.limit - bounded.buffer.Len(); len(data) > remaining {
		bounded.truncated = true
		bounded.buffer.Write(data[:remaining])
		return len(data), nil
	}
	bounded.buffer.Write(data)
	return len(data), nil
}
