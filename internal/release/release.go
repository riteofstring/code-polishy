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
	LockFilename = policy.LockFilename

	ManifestFilename = "release-manifest.json"

	LockVersion     = 1
	ManifestVersion = 6

	releasesDirectory = "releases"
)

var BinaryPath = func() string {
	if runtime.GOOS == "windows" {
		return "bin/code-polishy.exe"
	}
	return "bin/code-polishy"
}()

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

var supportedReleaseHosts = []string{
	"darwin-arm64",
	"darwin-x64",
	"linux-arm64",
	"linux-x64",
	"windows-x64",
}

type Lock struct {
	LockVersion        int      `json:"lockVersion"`
	CodePolishyVersion string   `json:"codePolishyVersion"`
	ReleaseDigest      string   `json:"releaseDigest"`
	Features           []string `json:"features"`
}

type Manifest struct {
	ManifestVersion         int      `json:"manifestVersion"`
	CodePolishyVersion      string   `json:"codePolishyVersion"`
	SourceRevision          string   `json:"sourceRevision"`
	Host                    string   `json:"host"`
	Features                []string `json:"features"`
	Tools                   Tools    `json:"tools"`
	ReleaseDigest           string   `json:"releaseDigest"`
	ContentDigest           string   `json:"contentDigest"`
	CapabilityCatalogSHA256 string   `json:"capabilityCatalogSha256,omitempty"`
	EntryCount              int      `json:"entryCount"`
	Entries                 []Entry  `json:"entries"`
}

type Tools struct {
	Go          string `json:"go"`
	Govulncheck string `json:"govulncheck"`
	Node        string `json:"node"`
	OSVScanner  string `json:"osv-scanner"`
	PNPM        string `json:"pnpm"`
	Packaging   string `json:"packaging"`
	Python      string `json:"python"`
	Ruff        string `json:"ruff"`
	Shellcheck  string `json:"shellcheck"`
	Staticcheck string `json:"staticcheck"`
	Ty          string `json:"ty"`
	Vulture     string `json:"vulture"`
}

type Entry struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256,omitempty"`
	Symlink string `json:"symlink,omitempty"`
}

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

func Directory(prefix string, lock Lock) string {
	return filepath.Join(prefix, releasesDirectory, lock.CodePolishyVersion+"-"+lock.ReleaseDigest)
}

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

func LockFor(manifest Manifest) Lock {
	return Lock{
		LockVersion:        LockVersion,
		CodePolishyVersion: manifest.CodePolishyVersion,
		ReleaseDigest:      manifest.ReleaseDigest,
		Features:           slices.Clone(manifest.Features),
	}
}

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

func ReadManifest(releaseDir string) (Manifest, bool, error) {
	return readManifest(releaseDir, parseManifest)
}

func (manifest Manifest) Identity() string {
	identity := &strings.Builder{}
	fmt.Fprintf(identity, "manifestVersion=%d\n", manifest.ManifestVersion)
	fmt.Fprintf(identity, "codePolishyVersion=%s\n", manifest.CodePolishyVersion)
	fmt.Fprintf(identity, "sourceRevision=%s\n", manifest.SourceRevision)
	if manifest.ManifestVersion >= 6 {
		fmt.Fprintf(identity, "capabilityCatalogSha256=%s\n", manifest.CapabilityCatalogSHA256)
	}
	for _, feature := range manifest.Features {
		fmt.Fprintf(identity, "feature=%s\n", feature)
	}
	fmt.Fprintf(identity, "tool.go=%s\n", manifest.Tools.Go)
	fmt.Fprintf(identity, "tool.govulncheck=%s\n", manifest.Tools.Govulncheck)
	fmt.Fprintf(identity, "tool.node=%s\n", manifest.Tools.Node)
	fmt.Fprintf(identity, "tool.osv-scanner=%s\n", manifest.Tools.OSVScanner)
	fmt.Fprintf(identity, "tool.pnpm=%s\n", manifest.Tools.PNPM)
	if manifest.ManifestVersion >= 5 {
		fmt.Fprintf(identity, "tool.packaging=%s\n", manifest.Tools.Packaging)
	}
	if manifest.ManifestVersion >= 4 {
		fmt.Fprintf(identity, "tool.python=%s\n", manifest.Tools.Python)
	}
	fmt.Fprintf(identity, "tool.ruff=%s\n", manifest.Tools.Ruff)
	fmt.Fprintf(identity, "tool.shellcheck=%s\n", manifest.Tools.Shellcheck)
	fmt.Fprintf(identity, "tool.staticcheck=%s\n", manifest.Tools.Staticcheck)
	if manifest.ManifestVersion >= 3 {
		fmt.Fprintf(identity, "tool.ty=%s\n", manifest.Tools.Ty)
	}
	if manifest.ManifestVersion >= 4 {
		fmt.Fprintf(identity, "tool.vulture=%s\n", manifest.Tools.Vulture)
	}
	digest := sha256.Sum256([]byte(identity.String()))
	return hex.EncodeToString(digest[:])
}

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
	if !hostPattern.MatchString(manifest.Host) || !slices.Contains(supportedReleaseHosts, manifest.Host) {
		return fmt.Errorf("%s records the unusable host %q", source, manifest.Host)
	}

	if err := validateManifestToolPins(manifest, source); err != nil {
		return err
	}
	if !digestPattern.MatchString(manifest.ReleaseDigest) || !digestPattern.MatchString(manifest.ContentDigest) {
		return fmt.Errorf("%s records an unusable release or content digest", source)
	}
	if err := validateManifestCapabilityIdentity(manifest, source); err != nil {
		return err
	}
	if err := validateFeatures(manifest.Features); err != nil {
		return fmt.Errorf("%s %w", source, err)
	}
	return nil
}

func validateManifestCapabilityIdentity(manifest Manifest, source string) error {
	if manifest.ManifestVersion >= 6 && !digestPattern.MatchString(manifest.CapabilityCatalogSHA256) {
		return fmt.Errorf("%s records an unusable capability catalog digest", source)
	}
	if manifest.ManifestVersion < 6 && manifest.CapabilityCatalogSHA256 != "" {
		return fmt.Errorf("%s records capability catalog metadata outside its manifest version", source)
	}
	if manifest.ManifestVersion >= 6 && !manifestRecordsCapabilityFile(manifest, CapabilityCatalogPath, manifest.CapabilityCatalogSHA256) {
		return fmt.Errorf("%s capability catalog entry does not match the release identity", source)
	}
	return nil
}

func validateManifestToolPins(manifest Manifest, source string) error {
	pins := []struct{ tool, version string }{
		{"Go", manifest.Tools.Go}, {"govulncheck", manifest.Tools.Govulncheck},
		{"Node", manifest.Tools.Node}, {"OSV-Scanner", manifest.Tools.OSVScanner},
		{"pnpm", manifest.Tools.PNPM}, {"Ruff", manifest.Tools.Ruff},
		{"ShellCheck", manifest.Tools.Shellcheck}, {"staticcheck", manifest.Tools.Staticcheck},
	}
	if manifest.ManifestVersion >= 3 {
		pins = append(pins, struct{ tool, version string }{"ty", manifest.Tools.Ty})
	}
	if manifest.ManifestVersion >= 4 {
		pins = append(pins,
			struct{ tool, version string }{"Python", manifest.Tools.Python},
			struct{ tool, version string }{"Vulture", manifest.Tools.Vulture},
		)
	}
	if manifest.ManifestVersion >= 5 {
		pins = append(pins, struct{ tool, version string }{"packaging", manifest.Tools.Packaging})
	}
	for _, pin := range pins {
		if !versionPattern.MatchString(pin.version) {
			return fmt.Errorf("%s records the unusable %s version %q", source, pin.tool, pin.version)
		}
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

		if index > 0 && manifest.Entries[index-1].Path >= entry.Path {
			return fmt.Errorf("%s does not record its entries once each in order, at %q", source, entry.Path)
		}
	}
	binaryPath := "bin/code-polishy"
	if manifest.Host == "windows-x64" {
		binaryPath = "bin/code-polishy.exe"
	}
	if !slices.ContainsFunc(manifest.Entries, func(entry Entry) bool {
		return entry.Path == binaryPath && entry.SHA256 != ""
	}) {
		return fmt.Errorf("%s records no %s, so it is not a release that can run", source, binaryPath)
	}
	if releaseEntriesDigest(manifest.Entries) != manifest.ContentDigest {
		return fmt.Errorf("%s content digest does not match its entry list", source)
	}
	return nil
}

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

func isContainedPath(relative string) bool {
	return relative != "" && relative == path.Clean(relative) && !path.IsAbs(relative) &&
		relative != "." && relative != ".." && !strings.HasPrefix(relative, "../")
}
