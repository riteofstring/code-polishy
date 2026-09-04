package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/release"
)

const engineBytes = "the policy engine"
const uninstalledDigest = "3333333333333333333333333333333333333333333333333333333333333333"

const bundleFile = ".tools/javascript/bundle/runner.mjs"
const bundleLink = ".tools/javascript/bundle/node_modules/tool"

type store struct {
	prefix string
}

func newStore(t *testing.T) store {
	t.Helper()

	prefix, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the install prefix: %v", err)
	}
	return store{prefix: prefix}
}

func (installed store) releaseRoot(lock release.Lock) string {
	return filepath.Join(installed.prefix, "releases", lock.CodePolishyVersion+"-"+lock.ReleaseDigest)
}

func (installed store) install(t *testing.T, revision, engine string) release.Lock {
	t.Helper()
	host, err := release.Host()
	if err != nil {
		t.Fatalf("resolve the host: %v", err)
	}
	manifest := release.Manifest{
		ManifestVersion: release.ManifestVersion, CodePolishyVersion: "9.9.9",
		SourceRevision: revision, Host: host, Features: []string{"javascript-bundle"},
		Tools: release.Tools{
			Go: "1.26.6", Govulncheck: "1.3.0", Node: "24.18.0", OSVScanner: "2.4.0",
			PNPM: "11.13.0", Packaging: "26.3", Python: "3.12.13+20260728", Ruff: "0.16.0", Shellcheck: "0.11.0", Staticcheck: "0.7.0",
			Ty: "0.0.65", Vulture: "2.16",
		},
		Entries: []release.Entry{
			{Path: bundleLink, Symlink: "../.pnpm/tool"},
			{Path: bundleFile, SHA256: digestOf(engine + " runner")},
			{Path: release.BinaryPath, SHA256: digestOf(engine)},
		},
	}
	manifest.EntryCount = len(manifest.Entries)
	manifest.ContentDigest = release.EntriesDigest(manifest.Entries)
	manifest.ReleaseDigest = manifest.Identity()
	lock := release.Lock{
		LockVersion: release.LockVersion, CodePolishyVersion: "9.9.9",
		ReleaseDigest: manifest.ReleaseDigest, Features: []string{"javascript-bundle"},
	}
	directory := installed.releaseRoot(lock)
	writeFile(t, directory, release.BinaryPath, engine)
	writeFile(t, directory, bundleFile, engine+" runner")
	writeLink(t, directory, bundleLink, "../.pnpm/tool")
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("render the manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, release.ManifestFilename), encoded, 0o644); err != nil {
		t.Fatalf("write the manifest: %v", err)
	}
	return lock
}

func (installed store) installHistorical(t *testing.T, version int, revision, engine string) release.Lock {
	t.Helper()
	host, err := release.Host()
	if err != nil {
		t.Fatalf("resolve the host: %v", err)
	}
	tools := release.Tools{
		Go: "1.26.6", Govulncheck: "1.3.0", Node: "24.18.0", OSVScanner: "2.4.0",
		PNPM: "11.13.0", Ruff: "0.16.0", Shellcheck: "0.11.0", Staticcheck: "0.7.0",
	}
	toolDocument := map[string]string{
		"go": tools.Go, "govulncheck": tools.Govulncheck, "node": tools.Node,
		"osv-scanner": tools.OSVScanner, "pnpm": tools.PNPM, "ruff": tools.Ruff,
		"shellcheck": tools.Shellcheck, "staticcheck": tools.Staticcheck,
	}
	if version >= 3 {
		tools.Ty = "0.0.65"
		toolDocument["ty"] = tools.Ty
	}
	if version >= 4 {
		tools.Python = "3.12.13+20260728"
		tools.Vulture = "2.16"
		toolDocument["python"] = tools.Python
		toolDocument["vulture"] = tools.Vulture
	}
	manifest := release.Manifest{
		ManifestVersion: version, CodePolishyVersion: "9.9.9", SourceRevision: revision,
		Host: host, Features: []string{"javascript-bundle"}, Tools: tools,
		Entries: []release.Entry{
			{Path: bundleLink, Symlink: "../.pnpm/tool"},
			{Path: bundleFile, SHA256: digestOf(engine + " runner")},
			{Path: release.BinaryPath, SHA256: digestOf(engine)},
		},
	}
	manifest.EntryCount = len(manifest.Entries)
	manifest.ContentDigest = release.EntriesDigest(manifest.Entries)
	manifest.ReleaseDigest = manifest.Identity()
	lock := release.Lock{
		LockVersion: release.LockVersion, CodePolishyVersion: manifest.CodePolishyVersion,
		ReleaseDigest: manifest.ReleaseDigest, Features: manifest.Features,
	}
	directory := installed.releaseRoot(lock)
	writeFile(t, directory, release.BinaryPath, engine)
	writeFile(t, directory, bundleFile, engine+" runner")
	writeLink(t, directory, bundleLink, "../.pnpm/tool")
	document := map[string]any{
		"manifestVersion": manifest.ManifestVersion, "codePolishyVersion": manifest.CodePolishyVersion,
		"sourceRevision": manifest.SourceRevision, "host": manifest.Host, "features": manifest.Features,
		"tools": toolDocument, "releaseDigest": manifest.ReleaseDigest, "contentDigest": manifest.ContentDigest,
		"entryCount": manifest.EntryCount, "entries": manifest.Entries,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("render the historical manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, release.ManifestFilename), encoded, 0o644); err != nil {
		t.Fatalf("write the historical manifest: %v", err)
	}
	return lock
}

func writeFile(t *testing.T, directory, relative, content string) {
	t.Helper()
	installed := filepath.Join(directory, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(installed), err)
	}
	if err := os.WriteFile(installed, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", installed, err)
	}
}

func writeLink(t *testing.T, directory, relative, target string) {
	t.Helper()
	installed := filepath.Join(directory, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(installed), err)
	}
	if err := os.Symlink(target, installed); err != nil {
		t.Fatalf("link %s: %v", installed, err)
	}
}

func digestOf(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

func launcherIn(t *testing.T, installed store, repoRoot string, arguments ...string) (int, string, []string) {
	t.Helper()
	launcherPath := filepath.Join(installed.prefix, "bin", "code-polishy")
	if err := os.MkdirAll(filepath.Dir(launcherPath), 0o755); err != nil {
		t.Fatalf("create the launcher directory: %v", err)
	}
	if err := os.WriteFile(launcherPath, []byte("launcher"), 0o755); err != nil {
		t.Fatalf("write the launcher: %v", err)
	}
	t.Chdir(repoRoot)
	stderr := &bytes.Buffer{}
	launched, argv := "", []string(nil)
	status := run(launcherPath, arguments, stderr, func(binary string, arguments []string) (int, error) {
		launched, argv = binary, arguments
		return 0, nil
	})
	if status != 0 {
		return status, stderr.String(), nil
	}
	if launched != argv[0] {
		t.Fatalf("launched %q as %q", launched, argv[0])
	}
	return status, stderr.String(), argv
}

func repositoryWith(t *testing.T, lock *release.Lock) string {
	t.Helper()
	repoRoot := t.TempDir()
	if lock != nil {
		document := struct {
			LockVersion        int      `json:"lockVersion"`
			CodePolishyVersion string   `json:"codePolishyVersion"`
			ReleaseDigest      string   `json:"releaseDigest"`
			Features           []string `json:"features"`
		}{lock.LockVersion, lock.CodePolishyVersion, lock.ReleaseDigest, lock.Features}
		encoded, err := json.Marshal(document)
		if err != nil {
			t.Fatalf("encode the lock fixture: %v", err)
		}
		if err := os.WriteFile(filepath.Join(repoRoot, release.LockFilename), encoded, 0o644); err != nil {
			t.Fatalf("write the lock: %v", err)
		}
	}
	resolved, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("resolve %s: %v", repoRoot, err)
	}
	return resolved
}

func TestLauncherRunsTheReleaseTheLockNames(t *testing.T) {
	installed := newStore(t)
	first := installed.install(t, exampleRevision(1), engineBytes+" one")
	second := installed.install(t, exampleRevision(2), engineBytes+" two")

	for _, lock := range []release.Lock{first, second} {
		repoRoot := repositoryWith(t, &lock)
		status, stderr, argv := launcherIn(t, installed, repoRoot, "check", "--all")
		if status != 0 {
			t.Fatalf("status=%d stderr=%s", status, stderr)
		}
		releaseRoot := installed.releaseRoot(lock)
		expected := filepath.Join(releaseRoot, filepath.FromSlash(release.BinaryPath))
		if argv[0] != expected {
			t.Fatalf("launched %q, wanted %q", argv[0], expected)
		}
		wanted := []string{expected, "--policy-root", releaseRoot,
			"--repo-root", repoRoot, "check", "--all"}
		if !slices.Equal(argv, wanted) {
			t.Fatalf("argv = %v, wanted %v", argv, wanted)
		}
	}
}

func TestLauncherRunsInstalledReleasesWithHistoricalManifests(t *testing.T) {
	installed := newStore(t)
	for _, version := range []int{2, 3, 4} {
		lock := installed.installHistorical(t, version, exampleRevision(version), engineBytes)
		status, stderr, argv := launcherIn(t, installed, repositoryWith(t, &lock), "version")
		if status != 0 || len(argv) == 0 {
			t.Fatalf("manifest version %d: status=%d stderr=%s argv=%v", version, status, stderr, argv)
		}
	}
}

func TestLauncherRefusesARepositoryWithNoLock(t *testing.T) {
	installed := newStore(t)
	installed.install(t, exampleRevision(1), engineBytes)
	status, stderr, _ := launcherIn(t, installed, repositoryWith(t, nil), "check")
	if status != 2 || !strings.Contains(stderr, release.LockFilename) {
		t.Fatalf("status=%d stderr=%s", status, stderr)
	}
}

func TestLauncherNamesTheReleaseAMissingInstallationNeeds(t *testing.T) {
	installed := newStore(t)
	lock := installed.install(t, exampleRevision(1), engineBytes)
	uninstalled := lock
	uninstalled.ReleaseDigest = uninstalledDigest
	status, stderr, _ := launcherIn(t, installed, repositoryWith(t, &uninstalled), "check")
	if status != 2 || !strings.Contains(stderr, uninstalledDigest) ||
		!strings.Contains(stderr, "./scripts/install.sh") {
		t.Fatalf("status=%d stderr=%s", status, stderr)
	}
}

func TestLauncherRefusesAReleaseThatChangedAfterInstallation(t *testing.T) {
	installed := newStore(t)
	lock := installed.install(t, exampleRevision(1), engineBytes)
	binary := filepath.Join(installed.releaseRoot(lock), filepath.FromSlash(release.BinaryPath))
	if err := os.WriteFile(binary, []byte("tampered"), 0o755); err != nil {
		t.Fatalf("change the installed binary: %v", err)
	}
	status, stderr, _ := launcherIn(t, installed, repositoryWith(t, &lock), "check")
	if status != 2 || !strings.Contains(stderr, "not the file the installed Code Polishy release recorded") {
		t.Fatalf("status=%d stderr=%s", status, stderr)
	}
}

func TestLauncherRefusesAReleaseCarryingBytesItDoesNotRecord(t *testing.T) {
	installed := newStore(t)
	lock := installed.install(t, exampleRevision(1), engineBytes)
	added := filepath.Join(installed.releaseRoot(lock),
		filepath.FromSlash(".tools/javascript/bundle/added.mjs"))
	if err := os.WriteFile(added, []byte("// added\n"), 0o644); err != nil {
		t.Fatalf("add a file to the release: %v", err)
	}
	status, stderr, _ := launcherIn(t, installed, repositoryWith(t, &lock), "check")
	if status != 2 || !strings.Contains(stderr, "does not record it") {
		t.Fatalf("status=%d stderr=%s", status, stderr)
	}
}

func TestLauncherRefusesAReleaseThatIsNotTheOneItRecords(t *testing.T) {
	installed := newStore(t)
	lock := installed.install(t, exampleRevision(1), engineBytes)
	manifestPath := filepath.Join(installed.releaseRoot(lock), release.ManifestFilename)
	recorded, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read the manifest: %v", err)
	}
	substituted := strings.Replace(string(recorded), exampleRevision(1), exampleRevision(2), 1)
	if err := os.WriteFile(manifestPath, []byte(substituted), 0o644); err != nil {
		t.Fatalf("write the manifest: %v", err)
	}
	status, stderr, _ := launcherIn(t, installed, repositoryWith(t, &lock), "check")
	if status != 2 || !strings.Contains(stderr, "the release it describes is") {
		t.Fatalf("status=%d stderr=%s", status, stderr)
	}
}

func TestLauncherRefusesAReleaseThatDoesNotProvideARequiredFeature(t *testing.T) {
	installed := newStore(t)
	lock := installed.install(t, exampleRevision(1), engineBytes)
	lock.Features = []string{"dead-code", "javascript-bundle"}
	status, stderr, _ := launcherIn(t, installed, repositoryWith(t, &lock), "check")
	if status != 2 || !strings.Contains(stderr, "dead-code") {
		t.Fatalf("status=%d stderr=%s", status, stderr)
	}
}

func TestLauncherRefusesToBeAimedAtAnotherPolicyRoot(t *testing.T) {
	installed := newStore(t)
	lock := installed.install(t, exampleRevision(1), engineBytes)
	repoRoot := repositoryWith(t, &lock)
	for _, aimed := range [][]string{{"--policy-root", "/elsewhere", "check"}, {"--policy-root=/elsewhere", "check"}} {
		status, stderr, _ := launcherIn(t, installed, repoRoot, aimed...)
		if status != 2 || !strings.Contains(stderr, "--policy-root") {
			t.Fatalf("status=%d stderr=%s", status, stderr)
		}
	}
}

func TestLauncherReadsTheLockOfTheSelectedRepository(t *testing.T) {
	installed := newStore(t)
	lock := installed.install(t, exampleRevision(1), engineBytes)
	target := repositoryWith(t, &lock)
	elsewhere := repositoryWith(t, nil)

	status, stderr, argv := launcherIn(t, installed, elsewhere, "--repo-root", target, "--config=policy.json", "check")
	if status != 0 {
		t.Fatalf("status=%d stderr=%s", status, stderr)
	}
	wanted := []string{argv[0], "--policy-root", installed.releaseRoot(lock),
		"--repo-root", target, "--config", "policy.json", "check"}
	if !slices.Equal(argv, wanted) {
		t.Fatalf("argv = %v, wanted %v", argv, wanted)
	}
}

func TestLauncherRefusesARepoRootItCannotResolve(t *testing.T) {
	installed := newStore(t)
	lock := installed.install(t, exampleRevision(1), engineBytes)
	repoRoot := repositoryWith(t, &lock)
	status, stderr, _ := launcherIn(t, installed, repoRoot, "--repo-root", filepath.Join(repoRoot, "absent"), "check")
	if status != 2 || !strings.Contains(stderr, "--repo-root") {
		t.Fatalf("status=%d stderr=%s", status, stderr)
	}
}

func exampleRevision(seed int) string {
	return strings.Repeat(string(rune('a'+seed)), 40)
}
