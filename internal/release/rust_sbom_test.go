package release

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	cdx "github.com/CycloneDX/cyclonedx-go"
)

func addRustReleaseInventoryFixture(t *testing.T, files map[string]string, host string) {
	t.Helper()
	for _, name := range []string{"rust_checksums.txt", "cargo-nextest_checksums.txt", "cargo-nextest-license.txt"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "tools", name))
		if err != nil {
			t.Fatal(err)
		}
		files["tools/"+name] = string(data)
	}
	for _, relative := range rustReleaseNoticePaths(host, "rust") {
		files[relative] = "upstream notice\n"
	}
}

func TestReleaseSBOMBindsRustDownloadsToTheSelectedHost(t *testing.T) {
	for _, test := range []struct {
		host, target, nextestArchive string
	}{
		{"darwin-arm64", "aarch64-apple-darwin", "universal-apple-darwin.tar.gz"},
		{"darwin-x64", "x86_64-apple-darwin", "universal-apple-darwin.tar.gz"},
		{"linux-arm64", "aarch64-unknown-linux-gnu", "aarch64-unknown-linux-gnu.tar.gz"},
		{"linux-x64", "x86_64-unknown-linux-gnu", "x86_64-unknown-linux-gnu.tar.gz"},
		{"windows-x64", "x86_64-pc-windows-msvc", "x86_64-pc-windows-msvc.zip"},
	} {
		t.Run(test.host, func(t *testing.T) {
			root, manifest := installedRelease(t, map[string]string{BinaryPath: "fixture"}, nil)
			manifest.Host = test.host
			sealed, err := os.OpenRoot(root)
			if err != nil {
				t.Fatal(err)
			}
			defer sealed.Close()
			for _, name := range []string{"rust", "cargo-nextest"} {
				references, err := rustReleaseDistributions(sealed, manifest, name)
				if err != nil {
					t.Fatal(err)
				}
				wanted := []string{"https://github.com/nextest-rs/nextest/releases/download/cargo-nextest-0.9.138/cargo-nextest-0.9.138-" + test.nextestArchive}
				if name == "rust" {
					wanted = nil
					for _, component := range []string{"cargo", "clippy", "rust-std", "rustc", "rustfmt"} {
						wanted = append(wanted, "https://static.rust-lang.org/dist/"+component+"-1.97.1-"+test.target+".tar.xz")
					}
				}
				assertRustArchiveReferences(t, root, name, references, wanted)
			}
		})
	}
}

func assertRustArchiveReferences(t *testing.T, root, name string, references []cdx.ExternalReference, wanted []string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "tools", name+"_checksums.txt"))
	if err != nil || len(references) != len(wanted) {
		t.Fatalf("archive inventory = %+v, error = %v", references, err)
	}
	for index, reference := range references {
		if reference.Type != cdx.ERTypeDistribution || reference.URL != wanted[index] || reference.Hashes == nil || len(*reference.Hashes) != 1 {
			t.Fatalf("archive reference = %+v, wanted %s", reference, wanted[index])
		}
		hash := (*reference.Hashes)[0]
		row := filepath.Base(reference.URL) + " " + hash.Value + "\n"
		if hash.Algorithm != cdx.HashAlgoSHA256 || !strings.Contains(string(data), row) {
			t.Fatalf("archive identity does not come from its checked-in pin: %+v", reference)
		}
	}
}

func TestReleaseSBOMRetainsNativeNoticeLocationsAndIncompleteComposition(t *testing.T) {
	root, manifest := installedRelease(t, map[string]string{BinaryPath: "fixture"}, nil)
	data, err := renderReleaseSBOM(manifest, root, "release.zip", exampleDigest)
	if err != nil {
		t.Fatal(err)
	}
	document := &cdx.BOM{}
	if err := cdx.NewBOMDecoder(bytes.NewReader(data), cdx.BOMFileFormatJSON).Decode(document); err != nil {
		t.Fatal(err)
	}
	refs := []cdx.BOMReference{"pkg:generic/cargo-nextest@0.9.138", "pkg:generic/rust@1.97.1"}
	if document.Compositions == nil || len(*document.Compositions) != 1 {
		t.Fatalf("native dependency coverage omitted: %+v", document.Compositions)
	}
	composition := (*document.Compositions)[0]
	if composition.Aggregate != cdx.CompositionAggregateIncomplete || composition.Assemblies == nil || composition.Dependencies == nil || !slices.Equal(*composition.Assemblies, refs) || !slices.Equal(*composition.Dependencies, refs) {
		t.Fatalf("native dependency coverage = %+v", composition)
	}
	for _, component := range *document.Components {
		if component.Name != "rust" && component.Name != "cargo-nextest" {
			continue
		}
		assertRustNoticeEvidence(t, manifest.Host, component)
	}
}

func assertRustNoticeEvidence(t *testing.T, host string, component cdx.Component) {
	t.Helper()
	if component.Hashes != nil || component.Properties == nil || component.ExternalReferences == nil {
		t.Fatalf("native archive and component identities conflated: %+v", component)
	}
	wanted := "tools/cargo-nextest-license.txt"
	if component.Name == "rust" {
		wanted = "tools/rust-notices/" + host + "/doc/rust/COPYRIGHT.html"
	}
	if !slices.Contains(*component.Properties, cdx.Property{Name: "code-polishy:notice-path", Value: wanted}) {
		t.Fatalf("native notice omitted: %+v", component)
	}
	if component.Name == "cargo-nextest" {
		license := cdx.ExternalReference{Type: cdx.ERTypeLicense, URL: "https://github.com/nextest-rs/nextest/blob/fc97e97bbe0a3927482a694247da00c099f4269e/LICENSE-MIT"}
		if !slices.Contains(*component.ExternalReferences, license) {
			t.Fatalf("immutable Nextest notice source omitted: %+v", component)
		}
	}
}

func TestReleaseSBOMRejectsMissingOrInconsistentNativeEvidence(t *testing.T) {
	for _, test := range []struct {
		name, path, problem string
		change              func(string) string
		omit                bool
	}{
		{name: "unsealed inventory", path: "tools/rust_checksums.txt", omit: true, problem: "absent from the manifest"},
		{name: "missing host", path: "tools/rust_checksums.txt", change: func(value string) string { _, rest, _ := strings.Cut(value, "\n"); return rest }, problem: "missing archive"},
		{name: "duplicate archive", path: "tools/cargo-nextest_checksums.txt", change: func(value string) string { first, _, _ := strings.Cut(value, "\n"); return value + first + "\n" }, problem: "duplicate archive"},
		{name: "foreign version", path: "tools/cargo-nextest_checksums.txt", change: func(value string) string { return strings.ReplaceAll(value, "0.9.138", "0.9.137") }, problem: "unexpected"},
		{name: "invalid digest", path: "tools/rust_checksums.txt", change: func(string) string { return "rustc-1.97.1-x86_64-unknown-linux-gnu.tar.xz invalid\n" }, problem: "malformed"},
		{name: "empty inventory", path: "tools/rust_checksums.txt", change: func(string) string { return "" }, problem: "malformed"},
		{name: "oversized inventory", path: "tools/rust_checksums.txt", change: func(string) string { return strings.Repeat(" ", 33<<10) }, problem: "exceeds"},
		{name: "missing nextest notice", path: "tools/cargo-nextest-license.txt", omit: true, problem: "notice is absent"},
		{name: "empty nextest notice", path: "tools/cargo-nextest-license.txt", change: func(string) string { return "" }, problem: "nonempty"},
		{name: "wrong notice version", path: "tools/cargo-nextest-license.txt", change: func(value string) string { return strings.ReplaceAll(value, "0.9.138", "0.9.137") }, problem: "exact release"},
		{name: "mutable notice source", path: "tools/cargo-nextest-license.txt", change: func(value string) string {
			return strings.ReplaceAll(value, "fc97e97bbe0a3927482a694247da00c099f4269e", "main")
		}, problem: "immutable"},
		{name: "unsealed SDK copyright", path: "tools/rust-notices/HOST/doc/rust/COPYRIGHT.html", omit: true, problem: "notice is absent"},
		{name: "empty SDK license", path: "tools/rust-notices/HOST/licenses/rustc/LICENSE-MIT", change: func(string) string { return "" }, problem: "nonempty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, manifest := installedRelease(t, map[string]string{BinaryPath: "fixture"}, nil)
			relative := strings.ReplaceAll(test.path, "HOST", manifest.Host)
			if test.omit {
				manifest.Entries = slices.DeleteFunc(manifest.Entries, func(entry Entry) bool { return entry.Path == relative })
			} else {
				path := filepath.Join(root, filepath.FromSlash(relative))
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(test.change(string(data))), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := renderReleaseSBOM(manifest, root, "release.zip", exampleDigest); err == nil || !strings.Contains(err.Error(), test.problem) {
				t.Fatalf("native inventory error = %v, wanted %q", err, test.problem)
			}
		})
	}
}

func TestReleaseSBOMAcceptsNextestNoticeWithNativeLineEndings(t *testing.T) {
	root, manifest := installedRelease(t, map[string]string{BinaryPath: "fixture"}, nil)
	path := filepath.Join(root, "tools", "cargo-nextest-license.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := renderReleaseSBOM(manifest, root, "release.zip", exampleDigest); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseSBOMRejectsIncompleteRustParserInventory(t *testing.T) {
	const parserMetadata = ".tools/python/fixture/lib/python3.12/site-packages/tree_sitter-0.26.0.dist-info/METADATA"
	for _, test := range []struct {
		name, path, content, problem string
		omit, append                 bool
	}{
		{name: "missing wheel", path: parserMetadata, omit: true, problem: "inventory is incomplete"},
		{name: "wrong version", path: parserMetadata, content: "Metadata-Version: 2.4\nName: tree-sitter\nVersion: 0.25.0\n\n", problem: "does not match"},
		{name: "duplicate wheel", path: ".tools/python/extra/tree_sitter-0.26.0.dist-info/METADATA", content: "Metadata-Version: 2.4\nName: tree-sitter\nVersion: 0.26.0\n\n", append: true, problem: "inventory is incomplete"},
		{name: "unknown wheel", path: ".tools/python/extra/unreviewed-1.0.dist-info/METADATA", content: "Metadata-Version: 2.4\nName: unreviewed\nVersion: 1.0\n\n", append: true, problem: "ungoverned distribution"},
		{name: "missing pin", path: "internal/rustfacts/pyproject.toml", content: "[project]\ndependencies=['tree-sitter==0.26.0']\n", problem: "dependency inventory is incomplete"},
		{name: "nonexact pin", path: "internal/rustfacts/pyproject.toml", content: "[project]\ndependencies=['tree-sitter>=0.26.0','tree-sitter-rust==0.24.2']\n", problem: "exact version"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, manifest := installedRelease(t, map[string]string{BinaryPath: "fixture"}, nil)
			if test.omit {
				manifest.Entries = slices.DeleteFunc(manifest.Entries, func(entry Entry) bool { return entry.Path == test.path })
			} else {
				path := filepath.Join(root, filepath.FromSlash(test.path))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(test.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if test.append {
				manifest.Entries = append(manifest.Entries, Entry{Path: test.path})
			}
			inventory := releaseSBOMInventory{components: map[string]cdx.Component{}, dependencies: map[string]map[string]bool{}}
			if err := inventory.addPython(root, "fixture", manifest); err == nil || !strings.Contains(err.Error(), test.problem) {
				t.Fatalf("invalid parser inventory: %v; wanted %q", err, test.problem)
			}
		})
	}
}
