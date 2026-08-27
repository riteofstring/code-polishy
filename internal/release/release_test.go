package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const exampleDigest = "1111111111111111111111111111111111111111111111111111111111111111"
const otherDigest = "2222222222222222222222222222222222222222222222222222222222222222"
const exampleRevision = "0123456789abcdef0123456789abcdef01234567"

// installedRelease writes a release whose manifest records exactly the files
// and links it installs, under the digest its own record implies, so a test can
// change one of them and see what is refused.
func installedRelease(t *testing.T, files, links map[string]string) (string, Manifest) {
	t.Helper()
	host, err := Host()
	if err != nil {
		t.Fatalf("resolve the host: %v", err)
	}
	directory := t.TempDir()
	paths := append(slices.Collect(maps.Keys(files)), slices.Collect(maps.Keys(links))...)
	slices.Sort(paths)
	entries := []Entry{}
	for _, relative := range paths {
		installed := filepath.Join(directory, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
			t.Fatalf("create %s: %v", filepath.Dir(installed), err)
		}
		if target, isLink := links[relative]; isLink {
			if err := os.Symlink(target, installed); err != nil {
				t.Fatalf("link %s: %v", installed, err)
			}
			entries = append(entries, Entry{Path: relative, Symlink: target})
			continue
		}
		if err := os.WriteFile(installed, []byte(files[relative]), 0o755); err != nil {
			t.Fatalf("write %s: %v", installed, err)
		}
		digest := sha256.Sum256([]byte(files[relative]))
		entries = append(entries, Entry{Path: relative, SHA256: hex.EncodeToString(digest[:])})
	}
	manifest := Manifest{
		ManifestVersion: ManifestVersion, CodePolishyVersion: "9.9.9", SourceRevision: exampleRevision,
		Host: host, Features: []string{"javascript-bundle"},
		Tools: Tools{
			Go: "1.26.6", Govulncheck: "1.3.0", Node: "24.18.0", OSVScanner: "2.4.0",
			PNPM: "11.13.0", Ruff: "0.16.0", Shellcheck: "0.11.0", Staticcheck: "0.7.0",
		},
		ContentDigest: releaseEntriesDigest(entries), EntryCount: len(entries), Entries: entries,
	}
	manifest.ReleaseDigest = manifest.Identity()
	if err := os.WriteFile(filepath.Join(directory, ManifestFilename), render(t, manifest), 0o644); err != nil {
		t.Fatalf("write the manifest: %v", err)
	}
	return directory, manifest
}

func TestReleaseHostMatrixRejectsIncompleteWindowsArm64Bundle(t *testing.T) {
	for _, test := range []struct {
		os, arch, want string
		supported      bool
	}{
		{"darwin", "arm64", "darwin-arm64", true},
		{"linux", "amd64", "linux-x64", true},
		{"windows", "amd64", "windows-x64", true},
		{"windows", "arm64", "", false},
	} {
		got, err := hostFor(test.os, test.arch)
		if test.supported && (err != nil || got != test.want) {
			t.Fatalf("%s/%s host=%q err=%v", test.os, test.arch, got, err)
		}
		if !test.supported && err == nil {
			t.Fatalf("%s/%s unexpectedly supported as %q", test.os, test.arch, got)
		}
	}
}

// exampleRelease is a real release in miniature: the engine binary, a file
// beside it that the engine reads, and one of the links the sealed JavaScript
// bundle is assembled from.
func exampleRelease(t *testing.T) (string, Manifest) {
	t.Helper()
	return installedRelease(t,
		map[string]string{BinaryPath: "engine", "VERSION": "9.9.9\n", "bundle/.pnpm/tool/index.mjs": "// tool\n"},
		map[string]string{"bundle/node_modules/tool": "../.pnpm/tool"})
}

func render(t *testing.T, document any) []byte {
	t.Helper()
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("render the document: %v", err)
	}
	return encoded
}

func TestParseLockReadsTheCanonicalRendering(t *testing.T) {
	t.Parallel()
	lock := Lock{
		LockVersion: LockVersion, CodePolishyVersion: "9.9.9",
		ReleaseDigest: exampleDigest, Features: []string{"javascript-bundle"},
	}
	parsed, err := parseLock(RenderLock(lock), "")
	if err != nil {
		t.Fatalf("parse the rendered lock: %v", err)
	}
	if parsed.CodePolishyVersion != "9.9.9" || parsed.ReleaseDigest != exampleDigest ||
		!slices.Equal(parsed.Features, []string{"javascript-bundle"}) {
		t.Fatalf("parsed = %+v", parsed)
	}
}

// The rendered lock is exactly what the sealed central formatting
// configuration prints for it. A target checks this file in, and Code Polishy
// formats the JSON a target checks in, so any other spelling makes the lock a
// release just wrote a formatting finding in the repository it governs.
func TestRenderLockWritesWhatTheSealedFormatterPrints(t *testing.T) {
	t.Parallel()
	rendered := string(RenderLock(Lock{
		LockVersion: LockVersion, CodePolishyVersion: "9.9.9",
		ReleaseDigest: exampleDigest, Features: []string{"javascript-bundle"},
	}))
	formatted := "{\n" +
		"  \"lockVersion\": 1,\n" +
		"  \"codePolishyVersion\": \"9.9.9\",\n" +
		"  \"releaseDigest\": \"" + exampleDigest + "\",\n" +
		"  \"features\": [\"javascript-bundle\"]\n" +
		"}\n"
	if rendered != formatted {
		t.Fatalf("rendered = %q, and the sealed formatter prints %q", rendered, formatted)
	}
}

func TestParseLockRejectsWhatItCannotActOnExactly(t *testing.T) {
	t.Parallel()
	usable := `"lockVersion":1,"codePolishyVersion":"9.9.9","releaseDigest":"` + exampleDigest + `"`
	cases := map[string]string{
		"an unknown field":     `{` + usable + `,"features":["javascript-bundle"],"registry":"https://example.invalid"}`,
		"another lock version": `{"lockVersion":2,"codePolishyVersion":"9.9.9","releaseDigest":"` + exampleDigest + `","features":["javascript-bundle"]}`,
		"a short digest":       `{"lockVersion":1,"codePolishyVersion":"9.9.9","releaseDigest":"abc","features":["javascript-bundle"]}`,
		"no required feature":  `{` + usable + `,"features":[]}`,
		"a repeated feature":   `{` + usable + `,"features":["javascript-bundle","javascript-bundle"]}`,
		"unordered features":   `{` + usable + `,"features":["javascript-bundle","dead-code"]}`,
		"a version that is a path": `{"lockVersion":1,"codePolishyVersion":"../9.9.9","releaseDigest":"` +
			exampleDigest + `","features":["javascript-bundle"]}`,
		"a second document": `{` + usable + `,"features":["javascript-bundle"]} {}`,
	}
	for name, document := range cases {
		if _, err := parseLock([]byte(document), "lock.json"); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestReadLockSeparatesAbsentFromUnusable(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	if _, present, err := ReadLock(repoRoot); present || err != nil {
		t.Fatalf("present=%v err=%v", present, err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, LockFilename), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write the lock: %v", err)
	}
	if _, present, err := ReadLock(repoRoot); !present || err == nil {
		t.Fatalf("present=%v err=%v", present, err)
	}
}

func TestDirectoryNamesOneReleaseAndNothingElse(t *testing.T) {
	t.Parallel()
	lock := Lock{
		LockVersion: LockVersion, CodePolishyVersion: "9.9.9",
		ReleaseDigest: exampleDigest, Features: []string{"javascript-bundle"},
	}
	directory := Directory("/prefix", lock)
	if directory != filepath.Join("/prefix", "releases", "9.9.9-"+exampleDigest) {
		t.Fatalf("directory = %s", directory)
	}
}

func TestParseManifestRejectsACorruptedRecord(t *testing.T) {
	t.Parallel()
	_, manifest := exampleRelease(t)
	cases := map[string]func(*Manifest){
		"a miscounted entry list":  func(broken *Manifest) { broken.EntryCount = 7 },
		"an empty entry list":      func(broken *Manifest) { broken.Entries, broken.EntryCount = nil, 0 },
		"an escaping entry":        func(broken *Manifest) { broken.Entries[0].Path = "../outside" },
		"a file that is also link": func(broken *Manifest) { broken.Entries[0].Symlink = "elsewhere" },
		"an entry that is neither": func(broken *Manifest) { broken.Entries[0].SHA256 = "" },
		"an unusable digest":       func(broken *Manifest) { broken.Entries[0].SHA256 = "abc" },
		"a repeated entry":         func(broken *Manifest) { broken.Entries[1].Path = broken.Entries[0].Path },
		"unordered entries": func(broken *Manifest) {
			broken.Entries[0], broken.Entries[1] = broken.Entries[1], broken.Entries[0]
		},
		"no engine binary": func(broken *Manifest) {
			broken.Entries = slices.DeleteFunc(broken.Entries, func(entry Entry) bool { return entry.Path == BinaryPath })
			broken.EntryCount = len(broken.Entries)
		},
		"an unusable revision":     func(broken *Manifest) { broken.SourceRevision = "HEAD" },
		"an unusable host":         func(broken *Manifest) { broken.Host = "Darwin/ARM64" },
		"an unusable tool pin":     func(broken *Manifest) { broken.Tools.Node = "" },
		"an unrecorded analyzer":   func(broken *Manifest) { broken.Tools.Staticcheck = "" },
		"another manifest version": func(broken *Manifest) { broken.ManifestVersion = ManifestVersion + 1 },
	}
	for name, corrupt := range cases {
		broken := manifest
		broken.Entries = slices.Clone(manifest.Entries)
		corrupt(&broken)
		if _, err := parseManifest(render(t, broken), ManifestFilename); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestParseManifestRejectsAnUnknownField(t *testing.T) {
	t.Parallel()
	_, manifest := exampleRelease(t)
	document := strings.Replace(string(render(t, manifest)), `{"manifestVersion"`, `{"downloadURL":"https://example.invalid","manifestVersion"`, 1)
	if _, err := parseManifest([]byte(document), ManifestFilename); err == nil {
		t.Fatalf("a manifest field this Code Polishy does not read was accepted")
	}
}

func TestReadManifestSeparatesASourceCheckoutFromARelease(t *testing.T) {
	t.Parallel()
	if _, installed, err := ReadManifest(t.TempDir()); installed || err != nil {
		t.Fatalf("installed=%v err=%v", installed, err)
	}
	directory, manifest := exampleRelease(t)
	read, installed, err := ReadManifest(directory)
	if !installed || err != nil {
		t.Fatalf("installed=%v err=%v", installed, err)
	}
	if read.ReleaseDigest != manifest.ReleaseDigest || read.EntryCount != manifest.EntryCount {
		t.Fatalf("read = %+v", read)
	}
}

func TestIdentityNamesTheReleaseTheRecordDescribes(t *testing.T) {
	t.Parallel()
	_, manifest := exampleRelease(t)
	// What the release was built from decides which release it is; what was
	// installed on this host does not.
	for _, elsewhere := range []func(*Manifest){
		func(other *Manifest) { other.SourceRevision = strings.Repeat("a", 40) },
		func(other *Manifest) { other.CodePolishyVersion = "9.9.10" },
		func(other *Manifest) { other.Features = []string{"dead-code", "javascript-bundle"} },
	} {
		other := manifest
		elsewhere(&other)
		if other.Identity() == manifest.Identity() {
			t.Fatalf("%+v is the same release as %+v", other, manifest)
		}
	}
	// A release records the exact version of every executable it carries, and a
	// target lock names it by that record, so changing any one of them is a
	// different release rather than the same one carrying another tool.
	for _, carried := range []func(*Tools){
		func(tools *Tools) { tools.Go = "1.26.7" },
		func(tools *Tools) { tools.Govulncheck = "1.3.1" },
		func(tools *Tools) { tools.Node = "24.18.1" },
		func(tools *Tools) { tools.OSVScanner = "2.4.1" },
		func(tools *Tools) { tools.PNPM = "11.13.1" },
		func(tools *Tools) { tools.Ruff = "0.16.1" },
		func(tools *Tools) { tools.Shellcheck = "0.11.1" },
		func(tools *Tools) { tools.Staticcheck = "0.7.1" },
	} {
		other := manifest
		carried(&other.Tools)
		if other.Identity() == manifest.Identity() {
			t.Fatalf("a release carrying %+v is the release carrying %+v", other.Tools, manifest.Tools)
		}
	}
	sameRelease := manifest
	sameRelease.Host = "sunos-sparc"
	sameRelease.ContentDigest, sameRelease.Entries, sameRelease.EntryCount = exampleDigest, nil, 0
	if sameRelease.Identity() != manifest.Identity() {
		t.Fatalf("the host and the installed bytes named the release")
	}
}

func TestSatisfiesRequiresTheExactLockedRelease(t *testing.T) {
	t.Parallel()
	_, manifest := exampleRelease(t)
	lock := LockFor(manifest)
	if err := manifest.Satisfies(lock); err != nil {
		t.Fatalf("the exact release did not satisfy its lock: %v", err)
	}

	otherRelease := lock
	otherRelease.ReleaseDigest = otherDigest
	if err := manifest.Satisfies(otherRelease); err == nil || !strings.Contains(err.Error(), otherDigest) {
		t.Fatalf("another digest was accepted: %v", err)
	}

	otherVersion := lock
	otherVersion.CodePolishyVersion = "9.9.10"
	if err := manifest.Satisfies(otherVersion); err == nil {
		t.Fatalf("another version was accepted")
	}

	moreFeatures := lock
	moreFeatures.Features = []string{"dead-code", "javascript-bundle"}
	if err := manifest.Satisfies(moreFeatures); err == nil || !strings.Contains(err.Error(), "dead-code") {
		t.Fatalf("a feature the release does not provide was accepted: %v", err)
	}

	foreign := manifest
	foreign.Host = "sunos-sparc"
	if err := foreign.Satisfies(lock); err == nil || !strings.Contains(err.Error(), "sunos-sparc") {
		t.Fatalf("a release for another host was accepted: %v", err)
	}
}

// A release built from another commit, recorded and installed under the digest
// a target requires, is refused: the record still describes the release it
// really is, and that is not the one the lock names.
func TestSatisfiesRefusesAReleaseThatIsNotWhatItRecords(t *testing.T) {
	t.Parallel()
	_, manifest := exampleRelease(t)
	lock := LockFor(manifest)
	claimed := manifest
	claimed.SourceRevision = strings.Repeat("b", 40)
	err := claimed.Satisfies(lock)
	if err == nil || !strings.Contains(err.Error(), claimed.Identity()) {
		t.Fatalf("a release under another release's digest was accepted: %v", err)
	}
}

func TestLockForRequiresTheReleaseThatWroteIt(t *testing.T) {
	t.Parallel()
	_, manifest := exampleRelease(t)
	parsed, err := parseLock(RenderLock(LockFor(manifest)), "")
	if err != nil {
		t.Fatalf("the written lock does not parse: %v", err)
	}
	if err := manifest.Satisfies(parsed); err != nil {
		t.Fatalf("the written lock does not select its own release: %v", err)
	}
}

func TestRequireLockedReleaseGovernsOnlyInstalledReleases(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	sourceCheckout := t.TempDir()
	if err := RequireLockedRelease(repoRoot, sourceCheckout); err != nil {
		t.Fatalf("a source checkout without a lock was refused: %v", err)
	}

	directory, manifest := exampleRelease(t)
	if err := RequireLockedRelease(repoRoot, directory); err == nil ||
		!strings.Contains(err.Error(), LockFilename) {
		t.Fatalf("a repository with no lock ran an installed release: %v", err)
	}

	lockPath := filepath.Join(repoRoot, LockFilename)
	if err := os.WriteFile(lockPath, RenderLock(LockFor(manifest)), 0o644); err != nil {
		t.Fatalf("write the lock: %v", err)
	}
	if err := RequireLockedRelease(repoRoot, directory); err != nil {
		t.Fatalf("the release the lock names was refused: %v", err)
	}

	otherRelease := LockFor(manifest)
	otherRelease.ReleaseDigest = otherDigest
	if err := os.WriteFile(lockPath, RenderLock(otherRelease), 0o644); err != nil {
		t.Fatalf("write the lock: %v", err)
	}
	if err := RequireLockedRelease(repoRoot, directory); err == nil {
		t.Fatalf("a release the lock does not name governed the repository")
	}

	// The source runner is not a release, so no lock can name it and there is
	// nothing for it to reconcile.
	if err := RequireLockedRelease(repoRoot, sourceCheckout); err != nil {
		t.Fatalf("the source runner was refused: %v", err)
	}
}
