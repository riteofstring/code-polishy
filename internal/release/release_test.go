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

const fixtureCapabilityCatalog = `{"protocol":"release-capabilities/v1","capabilities":[{"name":"check","kind":"command","description":"Check selected source.","aliases":["source validation"],"paths":[],"modules":[],"enforcement":["explicit"],"workflows":["docs/agent-workflows.md"]}]}`

func installedRelease(t *testing.T, files, links map[string]string) (string, Manifest) {
	t.Helper()
	host, err := Host()
	if err != nil {
		t.Fatalf("resolve the host: %v", err)
	}
	directory := t.TempDir()
	files = maps.Clone(files)
	if files == nil {
		files = map[string]string{}
	}
	if _, present := files[CapabilityCatalogPath]; !present && links[CapabilityCatalogPath] == "" {
		files[CapabilityCatalogPath] = fixtureCapabilityCatalog
	}
	if _, present := files["docs/agent-workflows.md"]; !present {
		files["docs/agent-workflows.md"] = "# Agent workflows\n\nRead the locked policy before changing source.\n"
	}
	files[".tools/javascript/bundle/node_modules/.package-map.json"] = `{"packages":{".":{"url":"..","dependencies":{"fixture":"fixture@1.0.0"}},"fixture@1.0.0":{"url":"./fixture","dependencies":{"fixture":"fixture@1.0.0"}}}}`
	files["tools/javascript_bundle_inventory.txt"] = "fixture@1.0.0\tMIT\n"
	files[".tools/python/fixture/lib/python3.12/site-packages/packaging-26.3.dist-info/METADATA"] = "Metadata-Version: 2.4\nName: packaging\nVersion: 26.3\n\n"
	files[".tools/python/fixture/lib/python3.12/site-packages/vulture-2.16.dist-info/METADATA"] = "Metadata-Version: 2.4\nName: vulture\nVersion: 2.16\nRequires-Dist: packaging>=25\n\n"
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
			PNPM: "11.13.0", Packaging: "26.3", Python: "3.12.13+20260728", Ruff: "0.16.0", Shellcheck: "0.11.0", Staticcheck: "0.7.0",
			Ty: "0.0.65", Vulture: "2.16",
		},
		ContentDigest: releaseEntriesDigest(entries), EntryCount: len(entries), Entries: entries,
		CapabilityCatalogSHA256: capabilityContentSHA256([]byte(files[CapabilityCatalogPath])),
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

func writeLockFixture(t *testing.T, path string, lock Lock) {
	t.Helper()
	document := struct {
		LockVersion        int      `json:"lockVersion"`
		CodePolishyVersion string   `json:"codePolishyVersion"`
		ReleaseDigest      string   `json:"releaseDigest"`
		Features           []string `json:"features"`
	}{lock.LockVersion, lock.CodePolishyVersion, lock.ReleaseDigest, lock.Features}
	if err := os.WriteFile(path, render(t, document), 0o644); err != nil {
		t.Fatalf("write the lock fixture: %v", err)
	}
}

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

func TestWriteLockAtomicallyReplacesAnExistingLock(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	old := Lock{
		LockVersion: LockVersion, CodePolishyVersion: "9.9.8",
		ReleaseDigest: otherDigest, Features: []string{"javascript-bundle"},
	}
	want := Lock{
		LockVersion: LockVersion, CodePolishyVersion: "9.9.9",
		ReleaseDigest: exampleDigest, Features: []string{"javascript-bundle"},
	}
	writeLockFixture(t, filepath.Join(repoRoot, LockFilename), old)
	if err := WriteLock(repoRoot, want); err != nil {
		t.Fatal(err)
	}
	got, present, err := ReadLock(repoRoot)
	if err != nil || !present || !slices.Equal(got.Features, want.Features) || got.CodePolishyVersion != want.CodePolishyVersion || got.ReleaseDigest != want.ReleaseDigest {
		t.Fatalf("lock = %+v, present = %v, error = %v", got, present, err)
	}
	temporaries, err := filepath.Glob(filepath.Join(repoRoot, ".code-polishy-lock-*.tmp"))
	if err != nil || len(temporaries) != 0 {
		t.Fatalf("temporary locks = %v, error = %v", temporaries, err)
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
		"an unusable revision":         func(broken *Manifest) { broken.SourceRevision = "HEAD" },
		"an unusable host":             func(broken *Manifest) { broken.Host = "Darwin/ARM64" },
		"an unusable tool pin":         func(broken *Manifest) { broken.Tools.Node = "" },
		"an unrecorded analyzer":       func(broken *Manifest) { broken.Tools.Staticcheck = "" },
		"an unrecorded runtime":        func(broken *Manifest) { broken.Tools.Python = "" },
		"an unrecorded Python library": func(broken *Manifest) { broken.Tools.Packaging = "" },
		"an unrecorded type checker":   func(broken *Manifest) { broken.Tools.Ty = "" },
		"an unrecorded dead-code tool": func(broken *Manifest) { broken.Tools.Vulture = "" },
		"another manifest version":     func(broken *Manifest) { broken.ManifestVersion = ManifestVersion + 1 },
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

	for _, carried := range []func(*Tools){
		func(tools *Tools) { tools.Go = "1.26.7" },
		func(tools *Tools) { tools.Govulncheck = "1.3.1" },
		func(tools *Tools) { tools.Node = "24.18.1" },
		func(tools *Tools) { tools.OSVScanner = "2.4.1" },
		func(tools *Tools) { tools.PNPM = "11.13.1" },
		func(tools *Tools) { tools.Packaging = "26.4" },
		func(tools *Tools) { tools.Python = "3.12.14+20260728" },
		func(tools *Tools) { tools.Ruff = "0.16.1" },
		func(tools *Tools) { tools.Shellcheck = "0.11.1" },
		func(tools *Tools) { tools.Staticcheck = "0.7.1" },
		func(tools *Tools) { tools.Ty = "0.0.66" },
		func(tools *Tools) { tools.Vulture = "2.17" },
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
	lock := Lock{
		LockVersion: LockVersion, CodePolishyVersion: "9.9.9",
		ReleaseDigest: manifest.ReleaseDigest, Features: []string{"javascript-bundle"},
	}
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

func TestSatisfiesRefusesAReleaseThatIsNotWhatItRecords(t *testing.T) {
	t.Parallel()
	_, manifest := exampleRelease(t)
	lock := Lock{
		LockVersion: LockVersion, CodePolishyVersion: "9.9.9",
		ReleaseDigest: manifest.ReleaseDigest, Features: []string{"javascript-bundle"},
	}
	claimed := manifest
	claimed.SourceRevision = strings.Repeat("b", 40)
	if err := claimed.Satisfies(lock); err == nil {
		t.Fatal("a release under another release's digest was accepted")
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
	lock := Lock{
		LockVersion: LockVersion, CodePolishyVersion: "9.9.9",
		ReleaseDigest: manifest.ReleaseDigest, Features: []string{"javascript-bundle"},
	}
	writeLockFixture(t, lockPath, lock)
	if err := RequireLockedRelease(repoRoot, directory); err != nil {
		t.Fatalf("the release the lock names was refused: %v", err)
	}

	otherRelease := lock
	otherRelease.ReleaseDigest = otherDigest
	writeLockFixture(t, lockPath, otherRelease)
	if err := RequireLockedRelease(repoRoot, directory); err == nil {
		t.Fatalf("a release the lock does not name governed the repository")
	}

	if err := RequireLockedRelease(repoRoot, sourceCheckout); err != nil {
		t.Fatalf("the source runner was refused: %v", err)
	}
}
