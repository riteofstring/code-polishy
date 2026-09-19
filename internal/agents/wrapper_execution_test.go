package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/release"
)

const wrapperTestVersion = "9.9.9"
const wrapperTestDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestPOSIXWrapperBootstrapsTheExactTaggedSourceAndDispatchesOffline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX wrapper contract")
	}
	policyRoot := repositoryPolicyRoot(t)
	sourceRoot := wrapperSourceFixture(t, true)
	repoRoot := wrapperTargetFixture(t, policyRoot)
	home := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "wrapper.log")

	stdout, stderr, err := runPOSIXWrapper(repoRoot, home, logPath, os.Getenv("PATH"), "setup", "--source", sourceRoot)
	if err != nil {
		t.Fatalf("setup failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Code Polishy 9.9.9 is ready") {
		t.Fatalf("setup output = %q", stdout)
	}
	log := readTestFile(t, logPath)
	wantPrefix := filepath.Join(home, ".local", "share", "code-polishy")
	physicalRepo, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log, "install-prefix="+wantPrefix+"\n") || !strings.Contains(log, "install-repository="+physicalRepo+"\n") {
		t.Fatalf("installer log = %q", log)
	}
	releaseRoot := filepath.Join(wantPrefix, "releases", wrapperTestVersion+"-"+wrapperTestDigest)
	if info, statErr := os.Stat(releaseRoot); statErr != nil || !info.IsDir() {
		t.Fatalf("release root: info=%v err=%v", info, statErr)
	}

	failingGit := t.TempDir()
	writeFile(t, filepath.Join(failingGit, "git"), []byte("#!/usr/bin/env bash\nexit 97\n"), 0o755)
	path := failingGit + string(os.PathListSeparator) + os.Getenv("PATH")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = runPOSIXWrapper(repoRoot, home, logPath, path, "check", "--all")
	if err != nil {
		t.Fatalf("offline dispatch failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if got := readTestFile(t, logPath); !strings.Contains(got, "--repo-root "+physicalRepo+" check --all") {
		t.Fatalf("dispatch log = %q", got)
	}
	stdout, stderr, err = runPOSIXWrapper(repoRoot, home, logPath, path, "setup", "--source", filepath.Join(t.TempDir(), "missing"))
	if err != nil || !strings.Contains(stdout, "already ready") {
		t.Fatalf("installed reuse failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
}

func TestPOSIXWrapperRejectsAVersionOneLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX wrapper contract")
	}
	policyRoot := repositoryPolicyRoot(t)
	repoRoot := wrapperTargetFixture(t, policyRoot)
	lockPath := filepath.Join(repoRoot, release.LockFilename)
	writeFile(t, lockPath, []byte(`{"lockVersion":1,"codePolishyVersion":"9.9.9","releaseDigest":"`+wrapperTestDigest+`","features":["javascript-bundle"]}`), 0o600)
	stdout, stderr, err := runPOSIXWrapper(repoRoot, t.TempDir(), filepath.Join(t.TempDir(), "wrapper.log"), os.Getenv("PATH"), "setup", "--source", t.TempDir())
	if err == nil || !strings.Contains(stderr, "unsupported lockVersion") {
		t.Fatalf("version-one setup result: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
}

func TestPOSIXWrapperInstallsThePinnedHostArchiveWithoutGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX wrapper contract")
	}
	policyRoot := repositoryPolicyRoot(t)
	archive := filepath.Join(t.TempDir(), "release.zip")
	archiveBytes := []byte("pinned release archive")
	writeFile(t, archive, archiveBytes, 0o600)
	digest := sha256.Sum256(archiveBytes)
	repoRoot := wrapperArchiveTargetFixture(t, policyRoot, hex.EncodeToString(digest[:]), int64(len(archiveBytes)))
	home := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "wrapper.log")
	commands := t.TempDir()
	writeFile(t, filepath.Join(commands, "curl"), []byte(`#!/usr/bin/env bash
set -euo pipefail
output=""
while (($#)); do
  if [[ "$1" == --output ]]; then output=$2; shift 2; else shift; fi
done
cp "${CODE_POLISHY_WRAPPER_TEST_ARCHIVE:?}" "$output"
`), 0o755)
	writeFile(t, filepath.Join(commands, "unzip"), []byte(`#!/usr/bin/env bash
set -euo pipefail
cat <<'BOOTSTRAP'
#!/usr/bin/env bash
set -euo pipefail
prefix=""
while (($#)); do
  if [[ "$1" == --prefix ]]; then prefix=$2; shift 2; else shift; fi
done
mkdir -p "$prefix/releases/9.9.9-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" "$prefix/bin"
cat >"$prefix/bin/code-polishy" <<'LAUNCHER'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${CODE_POLISHY_WRAPPER_TEST_LOG:?}"
if [[ "$*" == *" version" ]]; then printf 'code-polishy 9.9.9\n'; fi
LAUNCHER
chmod +x "$prefix/bin/code-polishy"
BOOTSTRAP
`), 0o755)
	t.Setenv("CODE_POLISHY_WRAPPER_TEST_ARCHIVE", archive)
	path := commands + string(os.PathListSeparator) + os.Getenv("PATH")
	stdout, stderr, err := runPOSIXWrapper(repoRoot, home, logPath, path, "setup")
	if err != nil || !strings.Contains(stdout, "Code Polishy 9.9.9 is ready") {
		t.Fatalf("archive setup failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.Contains(readTestFile(t, logPath), "git") {
		t.Fatal("archive setup invoked Git")
	}
}

func TestPOSIXWrapperRejectsAnAmbiguousLockBeforeCloning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX wrapper contract")
	}
	policyRoot := repositoryPolicyRoot(t)
	repoRoot := wrapperTargetFixture(t, policyRoot)
	lockPath := filepath.Join(repoRoot, ".code-polishy.lock.json")
	writeFile(t, lockPath, []byte(`{"lockVersion":2,"codePolishyVersion":"9.9.9","codePolishyVersion":"9.9.8","releaseDigest":"`+wrapperTestDigest+`","features":["javascript-bundle"]}`), 0o600)
	stdout, stderr, err := runPOSIXWrapper(repoRoot, t.TempDir(), filepath.Join(t.TempDir(), "wrapper.log"), os.Getenv("PATH"), "setup", "--source", filepath.Join(t.TempDir(), "missing"))
	if err == nil || !strings.Contains(stderr, "exactly one codePolishyVersion") {
		t.Fatalf("ambiguous lock result: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
}

func TestWrapperTemplatesStayBootstrapSized(t *testing.T) {
	policyRoot := repositoryPolicyRoot(t)
	for _, name := range []string{posixWrapperTemplateRelativePath, powerShellWrapperTemplateRelativePath} {
		contents, err := os.ReadFile(filepath.Join(policyRoot, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if len(contents) > 20*1024 {
			t.Fatalf("%s is %d bytes", name, len(contents))
		}
	}
}

func TestPowerShellWrapperParsesOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell parser contract")
	}
	path := filepath.Join(repositoryPolicyRoot(t), filepath.FromSlash(powerShellWrapperTemplateRelativePath))
	quotedPath := strings.ReplaceAll(path, "'", "''")
	script := "[scriptblock]::Create([System.IO.File]::ReadAllText('" + quotedPath + "')) | Out-Null"
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("parse PowerShell wrapper: %v\n%s", err, output)
	}
}

func wrapperSourceFixture(t *testing.T, annotated bool) string {
	t.Helper()
	root := t.TempDir()
	for _, directory := range []string{"tools", "scripts"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(root, "VERSION"), []byte(wrapperTestVersion+"\n"), 0o600)
	writeFile(t, filepath.Join(root, "tools", "install-policy-tools.sh"), []byte("#!/usr/bin/env bash\nset -euo pipefail\n"), 0o755)
	writeFile(t, filepath.Join(root, "scripts", "install.sh"), []byte(`#!/usr/bin/env bash
set -euo pipefail
prefix=""
repository=""
while (($#)); do
  case "$1" in
    --prefix) prefix=$2; shift 2 ;;
    --require-repository) repository=$2; shift 2 ;;
    *) exit 2 ;;
  esac
done
[[ -n "$prefix" && -n "$repository" ]]
version=$(sed -n 's/.*"codePolishyVersion"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$repository/.code-polishy.lock.json")
digest=$(sed -n 's/.*"releaseDigest"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$repository/.code-polishy.lock.json")
mkdir -p "$prefix/releases/$version-$digest" "$prefix/bin"
cat >"$prefix/bin/code-polishy" <<'LAUNCHER'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${CODE_POLISHY_WRAPPER_TEST_LOG:?}"
if [[ "$*" == *" version" ]]; then
  printf 'code-polishy 9.9.9\n'
fi
LAUNCHER
chmod +x "$prefix/bin/code-polishy"
printf 'install-prefix=%s\ninstall-repository=%s\n' "$prefix" "$repository" >>"${CODE_POLISHY_WRAPPER_TEST_LOG:?}"
`), 0o755)
	runGit(t, root, "init", "--quiet")
	runGit(t, root, "config", "user.email", "wrapper-tests@example.invalid")
	runGit(t, root, "config", "user.name", "Wrapper Tests")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "--quiet", "-m", "fixture")
	if annotated {
		runGit(t, root, "tag", "-a", "v"+wrapperTestVersion, "-m", "fixture release")
	} else {
		runGit(t, root, "tag", "v"+wrapperTestVersion)
	}
	return root
}

func wrapperTargetFixture(t *testing.T, policyRoot string) string {
	t.Helper()
	return wrapperArchiveTargetFixture(t, policyRoot, wrapperTestDigest, 1)
}

func wrapperArchiveTargetFixture(t *testing.T, policyRoot, archiveSHA string, archiveSize int64) string {
	t.Helper()
	root := t.TempDir()
	template, err := os.ReadFile(filepath.Join(policyRoot, filepath.FromSlash(posixWrapperTemplateRelativePath)))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, posixWrapperTargetFilename), template, 0o755)
	archives := []release.LockedArchive{}
	for _, host := range []string{"darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "windows-x64"} {
		archives = append(archives, release.LockedArchive{
			Host: host, URL: "https://example.invalid/code-polishy-" + wrapperTestVersion + "-" + host + ".zip",
			SHA256: archiveSHA, Size: archiveSize,
		})
	}
	lock := release.Lock{
		LockVersion: release.LockVersion, CodePolishyVersion: wrapperTestVersion,
		ReleaseDigest: wrapperTestDigest, Features: []string{"javascript-bundle"},
		Publication: &release.LockPublication{
			IndexURL: "https://example.invalid/release-index.json", IndexSHA256: wrapperTestDigest, Archives: archives,
		},
	}
	writeFile(t, filepath.Join(root, release.LockFilename), release.RenderLock(lock), 0o600)
	return root
}

func repositoryPolicyRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
}

func runPOSIXWrapper(repoRoot, home, logPath, path string, arguments ...string) (string, string, error) {
	command := exec.Command(filepath.Join(repoRoot, posixWrapperTargetFilename), arguments...)
	command.Dir = repoRoot
	command.Env = wrapperTestEnvironment(home, path, logPath)
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

func wrapperTestEnvironment(home, path, logPath string) []string {
	environment := make([]string, 0, len(os.Environ())+3)
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "HOME=") || strings.HasPrefix(value, "PATH=") || strings.HasPrefix(value, "CODE_POLISHY_WRAPPER_TEST_LOG=") {
			continue
		}
		environment = append(environment, value)
	}
	return append(environment, "HOME="+home, "PATH="+path, "CODE_POLISHY_WRAPPER_TEST_LOG="+logPath)
}

func runGit(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
