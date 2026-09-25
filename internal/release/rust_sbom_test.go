package release

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	cdx "github.com/CycloneDX/cyclonedx-go"
)

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
