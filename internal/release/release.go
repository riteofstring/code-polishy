// Package release owns what one installed Code Polishy release is and which
// exact release a target repository requires.
//
// An installed release records itself in release-manifest.json. A target
// records the release it requires in .code-polishy.lock.json. Nothing here
// resolves a channel, a version range, a fallback, a mirror, or a download: a
// lock names one release digest, and either that exact release is installed or
// the caller is told which one to install locally.
package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

const (
	// LockFilename is the file in which a target names the release it requires.
	// It is one of the two files a target checks in, so the name is declared
	// beside the configuration's.
	LockFilename = policy.LockFilename
	// ManifestFilename is the record an installed release keeps of itself.
	ManifestFilename = "release-manifest.json"
	// LockVersion and ManifestVersion are exact. There is no compatibility
	// range and no migration: a document of another version is rejected rather
	// than interpreted.
	LockVersion     = 1
	ManifestVersion = 2
	// releasesDirectory holds every release installed under one prefix.
	releasesDirectory = "releases"
)

// BinaryPath is the release-relative engine for the current host. Windows
// release entries use the executable suffix explicitly; no suffix probing or
// fallback is permitted.
var BinaryPath = func() string {
	if runtime.GOOS == "windows" {
		return "bin/code-polishy.exe"
	}
	return "bin/code-polishy"
}()

// LauncherBinaryPath is the verified launcher carried inside a native release
// bundle before the installer atomically publishes it at the stable prefix.
var LauncherBinaryPath = func() string {
	if runtime.GOOS == "windows" {
		return "bin/code-polishy-launcher.exe"
	}
	return "bin/code-polishy-launcher"
}()

var (
	digestPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	versionPattern  = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]*$`)
	featurePattern  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	hostPattern     = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9]+$`)
)

// Lock is the complete content of a target's .code-polishy.lock.json: which
// release, and which host-independent capabilities the target requires of it.
// It carries no path, credential, URL, channel, fallback version, or
// platform-specific digest, so the same lock selects the same release on every
// supported host.
type Lock struct {
	LockVersion        int      `json:"lockVersion"`
	CodePolishyVersion string   `json:"codePolishyVersion"`
	ReleaseDigest      string   `json:"releaseDigest"`
	Features           []string `json:"features"`
}

// Manifest is what an installed release records about itself. ReleaseDigest is
// its host-independent identity; ContentDigest and Entries are the bytes
// installed on this host.
type Manifest struct {
	ManifestVersion    int      `json:"manifestVersion"`
	CodePolishyVersion string   `json:"codePolishyVersion"`
	SourceRevision     string   `json:"sourceRevision"`
	Host               string   `json:"host"`
	Features           []string `json:"features"`
	Tools              Tools    `json:"tools"`
	ReleaseDigest      string   `json:"releaseDigest"`
	ContentDigest      string   `json:"contentDigest"`
	EntryCount         int      `json:"entryCount"`
	Entries            []Entry  `json:"entries"`
}

// Tools is the exact version of every executable a release carries, keyed by
// the name the executable is installed under. The installer probes each one
// against its checked-in pin before staging a release, so these are what the
// carried tools answered rather than what their files are named.
type Tools struct {
	Go          string `json:"go"`
	Govulncheck string `json:"govulncheck"`
	Node        string `json:"node"`
	OSVScanner  string `json:"osv-scanner"`
	PNPM        string `json:"pnpm"`
	Ruff        string `json:"ruff"`
	Shellcheck  string `json:"shellcheck"`
	Staticcheck string `json:"staticcheck"`
}

// Entry is one installed file or one installed link. A link records its exact
// target because the sealed JavaScript bundle is linked together by pnpm's
// isolated linker.
type Entry struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256,omitempty"`
	Symlink string `json:"symlink,omitempty"`
}

// Host names the one platform an installed release runs on. A release carries a
// sealed Node runtime, so its installed bytes are host specific even though the
// identity a lock names is not.
func Host() (string, error) {
	return hostFor(runtime.GOOS, runtime.GOARCH)
}

func hostFor(goos, goarch string) (string, error) {
	architectures := map[string]string{"amd64": "x64", "arm64": "arm64"}
	architecture, supported := architectures[goarch]
	if !supported || (goos != "darwin" && goos != "linux" && goos != "windows") || goos == "windows" && goarch != "amd64" {
		return "", fmt.Errorf("there is no Code Polishy release for %s/%s", goos, goarch)
	}
	return goos + "-" + architecture, nil
}

// Directory names the one installed release a lock requires under an install
// prefix. Nothing is searched or ranked: the directory name is exactly the
// version and release digest the lock records.
func Directory(prefix string, lock Lock) string {
	return filepath.Join(prefix, releasesDirectory, lock.CodePolishyVersion+"-"+lock.ReleaseDigest)
}

// parseLock compiles a target lock from bytes, rejecting an unknown field, a
// second document, and every value this Code Polishy cannot act on exactly.
func parseLock(data []byte, source string) (Lock, error) {
	if source == "" {
		source = LockFilename
	}
	var lock Lock
	if err := decodeExactly(data, source, &lock); err != nil {
		return Lock{}, err
	}
	if lock.LockVersion != LockVersion {
		return Lock{}, fmt.Errorf("%s records lock version %d; this Code Polishy reads lock version %d", source, lock.LockVersion, LockVersion)
	}
	if !versionPattern.MatchString(lock.CodePolishyVersion) {
		return Lock{}, fmt.Errorf("%s records the unusable Code Polishy version %q", source, lock.CodePolishyVersion)
	}
	if !digestPattern.MatchString(lock.ReleaseDigest) {
		return Lock{}, fmt.Errorf("%s records the unusable release digest %q", source, lock.ReleaseDigest)
	}
	if err := validateFeatures(lock.Features); err != nil {
		return Lock{}, fmt.Errorf("%s %w", source, err)
	}
	return lock, nil
}

// ReadLock reads the lock a repository checked in. A repository with no lock is
// reported as such rather than as an error, because what a missing lock means
// depends on who asked.
func ReadLock(repoRoot string) (Lock, bool, error) {
	source := filepath.Join(repoRoot, LockFilename)
	data, err := os.ReadFile(source)
	if errors.Is(err, os.ErrNotExist) {
		return Lock{}, false, nil
	}
	if err != nil {
		return Lock{}, false, fmt.Errorf("read %s: %w", source, err)
	}
	lock, err := parseLock(data, source)
	return lock, true, err
}

// LockFor is the exact lock a target needs in order to require this installed
// release.
func LockFor(manifest Manifest) Lock {
	return Lock{
		LockVersion:        LockVersion,
		CodePolishyVersion: manifest.CodePolishyVersion,
		ReleaseDigest:      manifest.ReleaseDigest,
		Features:           slices.Clone(manifest.Features),
	}
}

// RenderLock writes the one canonical form of a lock. The file is small,
// checked in, and read by people, so it is rendered rather than marshalled.
//
// A target checks this file in, and Code Polishy formats the JSON a target
// checks in, so the one canonical form is the one the sealed central formatting
// configuration prints. Otherwise the lock a release just wrote is a formatting
// finding in the repository it governs, and `code-polishy format` rewrites the
// file only `code-polishy lock` may write. A lock names one short feature per
// release capability, which is why its features stay on one line.
func RenderLock(lock Lock) []byte {
	features := make([]string, 0, len(lock.Features))
	for _, feature := range lock.Features {
		features = append(features, fmt.Sprintf("%q", feature))
	}
	rendered := &strings.Builder{}
	fmt.Fprintf(rendered, "{\n  \"lockVersion\": %d,\n", lock.LockVersion)
	fmt.Fprintf(rendered, "  \"codePolishyVersion\": %q,\n", lock.CodePolishyVersion)
	fmt.Fprintf(rendered, "  \"releaseDigest\": %q,\n", lock.ReleaseDigest)
	fmt.Fprintf(rendered, "  \"features\": [%s]\n}\n", strings.Join(features, ", "))
	return []byte(rendered.String())
}

// parseManifest compiles what a release says about itself, under the same
// strictness a lock gets.
func parseManifest(data []byte, source string) (Manifest, error) {
	if source == "" {
		source = ManifestFilename
	}
	var manifest Manifest
	if err := decodeExactly(data, source, &manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.ManifestVersion != ManifestVersion {
		return Manifest{}, fmt.Errorf("%s records manifest version %d; this Code Polishy reads manifest version %d", source, manifest.ManifestVersion, ManifestVersion)
	}
	if err := validateManifestIdentity(manifest, source); err != nil {
		return Manifest{}, err
	}
	if err := validateEntries(manifest, source); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// ReadManifest reads what an installed release records about itself. A
// directory with no manifest is not a release; for a policy root that is this
// repository's own source checkout, that is the expected answer rather than a
// failure.
func ReadManifest(releaseDir string) (Manifest, bool, error) {
	source := filepath.Join(releaseDir, ManifestFilename)
	data, err := os.ReadFile(source)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, false, nil
	}
	if err != nil {
		return Manifest{}, false, fmt.Errorf("read %s: %w", source, err)
	}
	manifest, err := parseManifest(data, source)
	return manifest, true, err
}

// Identity is the release a manifest describes: the digest of the reviewed
// commit, version, capabilities, and exact tool pins it was built from, and of
// nothing about the host it was installed on. The installer names an installed
// release by this value and a target lock records it, so recomputing it from
// the record itself is what keeps a release from being installed, or copied,
// under the name of another one.
//
// These are exactly the lines scripts/release-manifest.sh digests when it
// writes a release. scripts/test-install.sh runs this launcher against a real
// installed release, so the two cannot drift apart unnoticed.
func (manifest Manifest) Identity() string {
	identity := &strings.Builder{}
	fmt.Fprintf(identity, "manifestVersion=%d\n", manifest.ManifestVersion)
	fmt.Fprintf(identity, "codePolishyVersion=%s\n", manifest.CodePolishyVersion)
	fmt.Fprintf(identity, "sourceRevision=%s\n", manifest.SourceRevision)
	for _, feature := range manifest.Features {
		fmt.Fprintf(identity, "feature=%s\n", feature)
	}
	fmt.Fprintf(identity, "tool.go=%s\n", manifest.Tools.Go)
	fmt.Fprintf(identity, "tool.govulncheck=%s\n", manifest.Tools.Govulncheck)
	fmt.Fprintf(identity, "tool.node=%s\n", manifest.Tools.Node)
	fmt.Fprintf(identity, "tool.osv-scanner=%s\n", manifest.Tools.OSVScanner)
	fmt.Fprintf(identity, "tool.pnpm=%s\n", manifest.Tools.PNPM)
	fmt.Fprintf(identity, "tool.ruff=%s\n", manifest.Tools.Ruff)
	fmt.Fprintf(identity, "tool.shellcheck=%s\n", manifest.Tools.Shellcheck)
	fmt.Fprintf(identity, "tool.staticcheck=%s\n", manifest.Tools.Staticcheck)
	digest := sha256.Sum256([]byte(identity.String()))
	return hex.EncodeToString(digest[:])
}

// Satisfies reports whether this installed release is the exact one a lock
// requires, on this host.
func (manifest Manifest) Satisfies(lock Lock) error {
	host, err := Host()
	if err != nil {
		return err
	}
	if manifest.Host != host {
		return fmt.Errorf("the installed Code Polishy release was built for %s, not for %s", manifest.Host, host)
	}
	if identity := manifest.Identity(); identity != manifest.ReleaseDigest {
		return fmt.Errorf(
			"the installed Code Polishy release is recorded as %s, and the release it describes is %s",
			manifest.ReleaseDigest, identity,
		)
	}
	if manifest.CodePolishyVersion != lock.CodePolishyVersion || manifest.ReleaseDigest != lock.ReleaseDigest {
		return fmt.Errorf(
			"the installed release is Code Polishy %s %s, and %s requires %s %s",
			manifest.CodePolishyVersion, manifest.ReleaseDigest,
			LockFilename, lock.CodePolishyVersion, lock.ReleaseDigest,
		)
	}
	for _, feature := range lock.Features {
		if !slices.Contains(manifest.Features, feature) {
			return fmt.Errorf("the installed Code Polishy release does not provide the required feature %q", feature)
		}
	}
	return nil
}

// RequireLockedRelease refuses to let an installed release govern a repository
// that requires another one, so bypassing the launcher does not bypass the
// lock. A policy root that is not an installed release is this repository's own
// clearly labeled source runner; no target lock can name it, so there is
// nothing to reconcile.
func RequireLockedRelease(repoRoot, policyRoot string) error {
	manifest, installed, err := ReadManifest(policyRoot)
	if err != nil {
		return err
	}
	if !installed {
		return nil
	}
	lock, present, err := ReadLock(repoRoot)
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf(
			"%s has no %s; write one with `code-polishy lock` from the release it must require",
			repoRoot, LockFilename,
		)
	}
	return manifest.Satisfies(lock)
}

func decodeExactly(data []byte, source string, document any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(document); err != nil {
		return fmt.Errorf("parse %s: %w", source, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("the document contains more than one JSON value")
		}
		return fmt.Errorf("parse %s: %w", source, err)
	}
	return nil
}

func validateFeatures(features []string) error {
	if len(features) == 0 {
		return errors.New("names no required feature")
	}
	for index, feature := range features {
		if !featurePattern.MatchString(feature) {
			return fmt.Errorf("names the unusable feature %q", feature)
		}
		if index > 0 && features[index-1] >= feature {
			return fmt.Errorf("does not name its features once each in order, at %q", feature)
		}
	}
	return nil
}

func validateManifestIdentity(manifest Manifest, source string) error {
	if !versionPattern.MatchString(manifest.CodePolishyVersion) {
		return fmt.Errorf("%s records the unusable Code Polishy version %q", source, manifest.CodePolishyVersion)
	}
	if !revisionPattern.MatchString(manifest.SourceRevision) {
		return fmt.Errorf("%s records the unusable source revision %q", source, manifest.SourceRevision)
	}
	if !hostPattern.MatchString(manifest.Host) {
		return fmt.Errorf("%s records the unusable host %q", source, manifest.Host)
	}
	// The pins are part of which release this is, so an unusable one is a
	// corrupted record rather than a release whose identity happens to differ.
	for _, pin := range []struct{ tool, version string }{
		{"Go", manifest.Tools.Go}, {"govulncheck", manifest.Tools.Govulncheck},
		{"Node", manifest.Tools.Node}, {"OSV-Scanner", manifest.Tools.OSVScanner},
		{"pnpm", manifest.Tools.PNPM}, {"Ruff", manifest.Tools.Ruff},
		{"ShellCheck", manifest.Tools.Shellcheck}, {"staticcheck", manifest.Tools.Staticcheck},
	} {
		if !versionPattern.MatchString(pin.version) {
			return fmt.Errorf("%s records the unusable %s version %q", source, pin.tool, pin.version)
		}
	}
	if !digestPattern.MatchString(manifest.ReleaseDigest) || !digestPattern.MatchString(manifest.ContentDigest) {
		return fmt.Errorf("%s records an unusable release or content digest", source)
	}
	if err := validateFeatures(manifest.Features); err != nil {
		return fmt.Errorf("%s %w", source, err)
	}
	return nil
}

func validateEntries(manifest Manifest, source string) error {
	if manifest.EntryCount != len(manifest.Entries) {
		return fmt.Errorf("%s records %d installed entries and lists %d", source, manifest.EntryCount, len(manifest.Entries))
	}
	if len(manifest.Entries) == 0 {
		return fmt.Errorf("%s lists no installed entry", source)
	}
	for index, entry := range manifest.Entries {
		if err := validateEntry(entry, source); err != nil {
			return err
		}
		// Each installed path once, in the order the release was written, so
		// what an entry says about a path is the only thing it says.
		if index > 0 && manifest.Entries[index-1].Path >= entry.Path {
			return fmt.Errorf("%s does not record its entries once each in order, at %q", source, entry.Path)
		}
	}
	if !slices.ContainsFunc(manifest.Entries, func(entry Entry) bool {
		return entry.Path == BinaryPath && entry.SHA256 != ""
	}) {
		return fmt.Errorf("%s records no %s, so it is not a release that can run", source, BinaryPath)
	}
	if releaseEntriesDigest(manifest.Entries) != manifest.ContentDigest {
		return fmt.Errorf("%s content digest does not match its entry list", source)
	}
	return nil
}

// validateEntry keeps one recorded entry naming exactly one file or exactly one
// link inside the release.
func validateEntry(entry Entry, source string) error {
	if !isContainedPath(entry.Path) {
		return fmt.Errorf("%s records the unusable entry path %q", source, entry.Path)
	}
	if (entry.SHA256 == "") == (entry.Symlink == "") {
		return fmt.Errorf("%s records %q as neither exactly one file nor exactly one link", source, entry.Path)
	}
	if entry.SHA256 != "" && !digestPattern.MatchString(entry.SHA256) {
		return fmt.Errorf("%s records the unusable digest of %q", source, entry.Path)
	}
	return nil
}

// isContainedPath keeps a manifest entry naming something inside the release.
// The manifest is the release's own record, so an entry that climbs out of it
// is a corrupted manifest rather than a path to follow.
func isContainedPath(relative string) bool {
	return relative != "" && relative == path.Clean(relative) && !path.IsAbs(relative) &&
		relative != "." && relative != ".." && !strings.HasPrefix(relative, "../")
}
