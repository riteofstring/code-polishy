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

const ProtocolVersion = 3

type Operation string

const (
	OperationProvenance Operation = "provenance"

	OperationFormat Operation = "format"

	OperationFormatWrite Operation = "format-write"

	OperationParse Operation = "parse"

	OperationLint Operation = "lint"

	OperationTypeCheck Operation = "typecheck"

	OperationDeadCode Operation = "deadcode"

	OperationImports Operation = "imports"

	OperationGitLab Operation = "gitlab"

	OperationPackages Operation = "packages"

	OperationWorkspace Operation = "workspace"

	OperationLicenses Operation = "licenses"

	OperationAudit Operation = "audit"
)

const (
	maximumRequestBytes = 1 << 20

	maximumResponseBytes = 8 << 20

	maximumStderrBytes = 64 << 10

	maximumOperationPaths = 4096

	maximumGitLabGovernedPaths = 20000

	cleanupDelay = 5 * time.Second

	provenanceTimeout = 60 * time.Second

	formatTimeout = 10 * time.Minute

	lintTimeout = 15 * time.Minute

	typeCheckTimeout = 20 * time.Minute

	deadCodeTimeout = 20 * time.Minute

	importsTimeout = 15 * time.Minute

	gitLabTimeout = 60 * time.Second

	packagesTimeout = 5 * time.Minute

	workspaceTimeout = 60 * time.Second

	licensesTimeout = 10 * time.Minute

	auditTimeout = 15 * time.Minute
)

type Bundle struct {
	PolicyRoot string
}

type Provenance struct {
	BundleDigest string            `json:"bundleDigest"`
	Node         string            `json:"node"`
	Pnpm         string            `json:"pnpm"`
	Tools        map[string]string `json:"tools"`
}

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

type FormatResult struct {
	Changed     []string      `json:"changed"`
	Unsupported []Unsupported `json:"unsupported"`
}

type ParseResult struct {
	Covered     []string      `json:"covered"`
	Unsupported []Unsupported `json:"unsupported"`
}

type parseResultWire struct {
	Covered     *[]string      `json:"covered"`
	Unsupported *[]Unsupported `json:"unsupported"`
}

type Unsupported struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

func (bundle Bundle) Format(ctx context.Context, root string, paths []string) (FormatResult, error) {
	return bundle.format(ctx, OperationFormat, root, paths)
}

func (bundle Bundle) FormatWrite(ctx context.Context, root string, paths []string) (FormatResult, error) {
	return bundle.format(ctx, OperationFormatWrite, root, paths)
}

func (bundle Bundle) Parse(ctx context.Context, root string, paths []string) (ParseResult, error) {
	payload, err := fileRequest(OperationParse, root, paths)
	if err != nil {
		return ParseResult{}, err
	}
	result, err := bundle.exchange(ctx, payload, formatTimeout)
	if err != nil {
		return ParseResult{}, err
	}
	reported, err := decodeParseResult(result, paths)
	if err != nil {
		return ParseResult{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable %s result: %w", OperationParse, err)
	}
	return reported, nil
}

func decodeParseResult(data []byte, requested []string) (ParseResult, error) {
	var wire parseResultWire
	if err := decodeExactly(data, &wire); err != nil {
		return ParseResult{}, err
	}
	if wire.Covered == nil || wire.Unsupported == nil {
		return ParseResult{}, fmt.Errorf("the parse result is missing required fields")
	}
	requestedPaths, err := parseRequestedPaths(requested)
	if err != nil {
		return ParseResult{}, err
	}
	reportedPaths := map[string]bool{}
	if err := addParseCoveredPaths(*wire.Covered, requestedPaths, reportedPaths); err != nil {
		return ParseResult{}, err
	}
	if err := addParseUnsupportedPaths(*wire.Unsupported, requestedPaths, reportedPaths); err != nil {
		return ParseResult{}, err
	}
	if len(reportedPaths) != len(requestedPaths) {
		return ParseResult{}, fmt.Errorf("the parse result does not account for every requested path")
	}
	return ParseResult{Covered: *wire.Covered, Unsupported: *wire.Unsupported}, nil
}

func parseRequestedPaths(requested []string) (map[string]bool, error) {
	paths := make(map[string]bool, len(requested))
	for _, path := range requested {
		if paths[path] {
			return nil, fmt.Errorf("the parse request repeats path %q", path)
		}
		paths[path] = true
	}
	return paths, nil
}

func addParseCoveredPaths(covered []string, requested, reported map[string]bool) error {
	for _, path := range covered {
		if !validParseReportedPath(path, requested, reported) {
			return fmt.Errorf("the parse result reports invalid covered path %q", path)
		}
		reported[path] = true
	}
	return nil
}

func addParseUnsupportedPaths(unsupported []Unsupported, requested, reported map[string]bool) error {
	for _, finding := range unsupported {
		if !validParseReportedPath(finding.Path, requested, reported) {
			return fmt.Errorf("the parse result reports invalid unsupported path %q", finding.Path)
		}
		if !validParseReason(finding.Reason) {
			return fmt.Errorf("the parse result reports an invalid reason for %q", finding.Path)
		}
		reported[finding.Path] = true
	}
	return nil
}

func validParseReportedPath(path string, requested, reported map[string]bool) bool {
	return containedPath(path) && requested[path] && !reported[path]
}

func validParseReason(reason string) bool {
	return strings.TrimSpace(reason) != "" && len(reason) <= 4096
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

type LintLimits struct {
	Complexity int `json:"complexity"`
	Depth      int `json:"depth"`
	Parameters int `json:"parameters"`
}

type LintActivation struct {
	ReactHooks       bool `json:"reactHooks"`
	JSXAccessibility bool `json:"jsxAccessibility"`
}

type LintResult struct {
	Findings    []LintViolation `json:"findings"`
	Comments    []LintComment   `json:"comments"`
	Unsupported []Unsupported   `json:"unsupported"`
}

type LintViolation struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

type LintComment struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Raw        string `json:"raw"`
	Complete   bool   `json:"complete"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
	BeforeCode bool   `json:"beforeCode"`
	Preamble   bool   `json:"preamble"`
	ByteZero   bool   `json:"byteZero"`
}

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
	reported, err := decodeLintResult(result)
	if err != nil {
		return LintResult{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable %s result: %w", OperationLint, err)
	}
	return reported, nil
}

type lintResultWire struct {
	Findings    *[]LintViolation   `json:"findings"`
	Comments    *[]lintCommentWire `json:"comments"`
	Unsupported *[]Unsupported     `json:"unsupported"`
}

type lintCommentWire struct {
	Path       *string `json:"path"`
	Kind       *string `json:"kind"`
	Raw        *string `json:"raw"`
	Complete   *bool   `json:"complete"`
	Line       *int    `json:"line"`
	Column     *int    `json:"column"`
	BeforeCode *bool   `json:"beforeCode"`
	Preamble   *bool   `json:"preamble"`
	ByteZero   *bool   `json:"byteZero"`
}

func decodeLintResult(data []byte) (LintResult, error) {
	var wire lintResultWire
	if err := decodeExactly(data, &wire); err != nil {
		return LintResult{}, err
	}
	if wire.Findings == nil || wire.Comments == nil || wire.Unsupported == nil {
		return LintResult{}, fmt.Errorf("the lint result is missing required fields")
	}
	comments := make([]LintComment, 0, len(*wire.Comments))
	for index, comment := range *wire.Comments {
		parsed, err := decodeLintComment(comment)
		if err != nil {
			return LintResult{}, fmt.Errorf("the lint result comment %d is invalid: %w", index, err)
		}
		comments = append(comments, parsed)
	}
	return LintResult{
		Findings:    *wire.Findings,
		Comments:    comments,
		Unsupported: *wire.Unsupported,
	}, nil
}

func decodeLintComment(wire lintCommentWire) (LintComment, error) {
	if !lintCommentFieldsPresent(wire) {
		return LintComment{}, fmt.Errorf("it is missing required fields")
	}
	if !containedPath(*wire.Path) {
		return LintComment{}, fmt.Errorf("path %q is not contained", *wire.Path)
	}
	if !lintCommentKindAllowed(*wire.Kind) {
		return LintComment{}, fmt.Errorf("kind %q is not a parser comment kind", *wire.Kind)
	}
	if *wire.Raw == "" {
		return LintComment{}, fmt.Errorf("raw bytes are empty")
	}
	if !lintCommentLocationAllowed(*wire.Line, *wire.Column) {
		return LintComment{}, fmt.Errorf("location %d:%d is not positive", *wire.Line, *wire.Column)
	}
	return lintCommentFromWire(wire), nil
}

func lintCommentFieldsPresent(wire lintCommentWire) bool {
	for _, present := range []bool{
		wire.Path != nil,
		wire.Kind != nil,
		wire.Raw != nil,
		wire.Complete != nil,
		wire.Line != nil,
		wire.Column != nil,
		wire.BeforeCode != nil,
		wire.Preamble != nil,
		wire.ByteZero != nil,
	} {
		if !present {
			return false
		}
	}
	return true
}

func lintCommentKindAllowed(kind string) bool {
	switch kind {
	case "Line", "Block", "Shebang":
		return true
	}
	return false
}

func lintCommentLocationAllowed(line, column int) bool {
	return line >= 1 && column >= 1
}

func lintCommentFromWire(wire lintCommentWire) LintComment {
	return LintComment{
		Path:       *wire.Path,
		Kind:       *wire.Kind,
		Raw:        *wire.Raw,
		Complete:   *wire.Complete,
		Line:       *wire.Line,
		Column:     *wire.Column,
		BeforeCode: *wire.BeforeCode,
		Preamble:   *wire.Preamble,
		ByteZero:   *wire.ByteZero,
	}
}

type TypeCheckResult struct {
	Diagnostics []TypeDiagnostic `json:"diagnostics"`
	Covered     []string         `json:"covered"`
	Unsupported []Unsupported    `json:"unsupported"`
}

type TypeDiagnostic struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (bundle Bundle) TypeCheck(ctx context.Context, root, project string, paths []string) (TypeCheckResult, error) {
	return bundle.TypeCheckInherited(ctx, root, project, paths, nil)
}

func (bundle Bundle) TypeCheckInherited(ctx context.Context, root, project string, paths, inherited []string) (TypeCheckResult, error) {
	payload, err := fileRequest(OperationTypeCheck, root, paths)
	if err != nil {
		return TypeCheckResult{}, err
	}
	if !containedPath(project) {
		return TypeCheckResult{}, fmt.Errorf("the %s request names project %q, not a contained repository-relative path", OperationTypeCheck, project)
	}
	payload.Project = project
	if err := validateInheritedPaths(paths, inherited); err != nil {
		return TypeCheckResult{}, err
	}
	payload.InheritedPaths = append([]string{}, inherited...)
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

func validateInheritedPaths(paths, inherited []string) error {
	seen := map[string]bool{}
	for _, path := range inherited {
		if !containedPath(path) || !slices.Contains(paths, path) || seen[path] {
			return fmt.Errorf("the %s request declares invalid inherited path %q", OperationTypeCheck, path)
		}
		seen[path] = true
	}
	return nil
}

type DeadCodeWorkspace struct {
	Root      string   `json:"root"`
	Entry     []string `json:"entry"`
	Project   []string `json:"project"`
	Inherited []string `json:"inherited,omitempty"`
}

type DeadCodeResult struct {
	UnusedFiles   []string       `json:"unusedFiles"`
	UnusedExports []UnusedExport `json:"unusedExports"`
	Covered       []string       `json:"covered"`
	Unsupported   []Unsupported  `json:"unsupported"`
}

type UnusedExport struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Symbol string `json:"symbol"`
	Kind   string `json:"kind"`
}

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

type ImportResult struct {
	Analyzed    []string      `json:"analyzed"`
	Imports     []ImportFact  `json:"imports"`
	Unsupported []Unsupported `json:"unsupported"`
}

type ImportFact struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Specifier string `json:"specifier"`
	Resolved  string `json:"resolved"`
	Package   string `json:"package"`
	Kind      string `json:"kind"`
}

func (bundle Bundle) Imports(ctx context.Context, root string, paths []string) (ImportResult, error) {
	payload, err := fileRequest(OperationImports, root, paths)
	if err != nil {
		return ImportResult{}, err
	}
	result, err := bundle.exchange(ctx, payload, importsTimeout)
	if err != nil {
		return ImportResult{}, err
	}
	reported, err := decodeImportResult(result, paths)
	if err != nil {
		return ImportResult{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable %s result: %w", OperationImports, err)
	}
	return reported, nil
}

type PackageResult struct {
	LockfileVersion string          `json:"lockfileVersion"`
	Importers       []Importer      `json:"importers"`
	Packages        []LockedPackage `json:"packages"`
	Unsupported     []Unsupported   `json:"unsupported"`
}

type Importer struct {
	Path         string       `json:"path"`
	Manifest     string       `json:"manifest"`
	Dependencies []Dependency `json:"dependencies"`
}

type Dependency struct {
	Name            string `json:"name"`
	Scope           string `json:"scope"`
	Declared        string `json:"declared"`
	Specifier       string `json:"specifier"`
	ResolvedName    string `json:"resolvedName"`
	ResolvedVersion string `json:"resolvedVersion"`
	Link            string `json:"link"`
}

type LicenseMetadata string

const (
	LicenseMetadataRequired LicenseMetadata = "required"

	LicenseMetadataPlatformExcluded LicenseMetadata = "platform-excluded"

	LicenseMetadataUnknown LicenseMetadata = "unknown"
)

type LockedPackage struct {
	Name            string          `json:"name"`
	Version         string          `json:"version"`
	Source          string          `json:"source"`
	LicenseMetadata LicenseMetadata `json:"licenseMetadata"`
}

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

type WorkspaceResult struct {
	Files       []WorkspaceFile `json:"files"`
	Unsupported []Unsupported   `json:"unsupported"`
}

type WorkspaceFile struct {
	Path     string             `json:"path"`
	Settings []WorkspaceSetting `json:"settings"`
}

type WorkspaceSetting struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

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

type LicenseResult struct {
	Packages    []InstalledPackage `json:"packages"`
	Unsupported []Unsupported      `json:"unsupported"`
}

type InstalledPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	License string `json:"license"`
}

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

type AuditResult struct {
	Advisories  []Advisory    `json:"advisories"`
	Unsupported []Unsupported `json:"unsupported"`
}

type AuditInvocation struct {
	Argv              []string
	Cwd               string
	SealedEnvironment bool
	TimeoutSeconds    int
}

type Advisory struct {
	ID       string   `json:"id"`
	Aliases  []string `json:"aliases"`
	Package  string   `json:"package"`
	Severity string   `json:"severity"`
	Title    string   `json:"title"`
	Versions []string `json:"versions"`
}

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

func containedWorkspace(directory string, workspace DeadCodeWorkspace) error {
	if !containedPath(workspace.Root) || !containsPath(directory, workspace.Root) {
		return fmt.Errorf("the %s request declares package %q outside %q", OperationDeadCode, workspace.Root, directory)
	}
	if len(workspace.Project) == 0 {
		return fmt.Errorf("the %s package %q selects no files", OperationDeadCode, workspace.Root)
	}
	if err := validateInheritedWorkspacePaths(workspace); err != nil {
		return err
	}
	return validateWorkspacePaths(directory, workspace)
}

func validateInheritedWorkspacePaths(workspace DeadCodeWorkspace) error {
	seen := map[string]bool{}
	for _, path := range workspace.Inherited {
		if !containedPath(path) || seen[path] || !slices.Contains(workspace.Project, path) {
			return fmt.Errorf("the %s package %q declares invalid inherited path %q", OperationDeadCode, workspace.Root, path)
		}
		seen[path] = true
	}
	return nil
}

func validateWorkspacePaths(directory string, workspace DeadCodeWorkspace) error {
	for _, path := range slices.Concat(workspace.Project, workspace.Entry) {
		if !containedPath(path) || !containsPath(workspace.Root, path) && !slices.Contains(workspace.Inherited, path) {
			return fmt.Errorf("the %s package %q selects %q outside %q", OperationDeadCode, workspace.Root, path, directory)
		}
	}
	return nil
}

func containsPath(directory, path string) bool {
	return directory == "." || path == directory || strings.HasPrefix(path, directory+"/")
}

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

func containedPath(path string) bool {
	if path == "" || strings.Contains(path, `\`) || filepath.IsAbs(path) {
		return false
	}
	if path != filepath.ToSlash(filepath.Clean(path)) {
		return false
	}
	return path != ".." && !strings.HasPrefix(path, "../")
}

type request struct {
	ProtocolVersion int                 `json:"protocolVersion"`
	Operation       Operation           `json:"operation"`
	Root            string              `json:"root,omitempty"`
	Paths           []string            `json:"paths,omitempty"`
	Limits          *LintLimits         `json:"limits,omitempty"`
	Activation      *LintActivation     `json:"activation,omitempty"`
	Project         string              `json:"project,omitempty"`
	InheritedPaths  []string            `json:"inheritedPaths,omitempty"`
	Directory       string              `json:"directory,omitempty"`
	Workspaces      []DeadCodeWorkspace `json:"workspaces,omitempty"`
	GovernedPaths   []string            `json:"governedPaths,omitempty"`
}

type response struct {
	ProtocolVersion int             `json:"protocolVersion"`
	Operation       Operation       `json:"operation"`
	Error           string          `json:"error"`
	Result          json.RawMessage `json:"result"`
}

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

func (bundle Bundle) paths() (node, runner string, err error) {
	if !filepath.IsAbs(bundle.PolicyRoot) {
		return "", "", fmt.Errorf("the Code Polishy checkout path %q is not absolute", bundle.PolicyRoot)
	}

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
