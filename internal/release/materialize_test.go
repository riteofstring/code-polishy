//go:build !windows

package release

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyTreeDereferencedContainsLinksAsRegularEntries(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "store", "package"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "store", "package", "index.js"), []byte("export {};\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "store", "package"), filepath.Join(source, "node_modules", "package")); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "materialized")
	if err := CopyTreeDereferenced(source, destination); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(destination, "node_modules", "package", "index.js"))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("materialized info=%v err=%v", info, err)
	}
}

func TestCopyTreeDereferencedRejectsExternalLink(t *testing.T) {
	source, external := t.TempDir(), filepath.Join(t.TempDir(), "external")
	if err := os.WriteFile(external, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(source, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := CopyTreeDereferenced(source, filepath.Join(t.TempDir(), "materialized")); err == nil {
		t.Fatal("materialized an external link")
	}
}
