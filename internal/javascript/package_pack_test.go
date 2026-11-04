package javascript

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type packedFixture struct {
	files map[string]string
	links map[string]string
}

func packWithPackageManager(t *testing.T, node string, fixture packedFixture) string {
	t.Helper()
	source := t.TempDir()
	for path, content := range fixture.files {
		writeScript(t, filepath.Join(source, path), content)
	}
	for path, target := range fixture.links {
		if err := os.Symlink(target, filepath.Join(source, path)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	versionManifest(t, filepath.Join(source, "package.json"))
	archive := filepath.Join(t.TempDir(), "pkg.tgz")
	manager := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(node))), "pnpm", "bin", "pnpm.cjs")
	command := exec.CommandContext(t.Context(), node, manager, "pack", "--out", archive, "--config.ignore-scripts=true")
	command.Dir = source
	command.Env = []string{"HOME=" + t.TempDir(), "PATH=" + filepath.Dir(node)}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pnpm pack: %v: %s", err, output)
	}
	return archive
}

func versionManifest(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if _, found := manifest["version"]; !found {
		manifest["version"] = "1.0.0"
	}
	if data, err = json.Marshal(manifest); err != nil {
		t.Fatal(err)
	}
	writeScript(t, path, string(data))
}

func installPacked(t *testing.T, archive, target string) {
	t.Helper()
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "tar", "-xzf", archive, "-C", target, "--strip-components=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("install packed artifact: %v: %s", err, output)
	}
}

type archiveEntry struct {
	name     string
	typeflag byte
	content  string
	link     string
}

func writeArchive(t *testing.T, entries []archiveEntry) string {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "pkg.tgz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(file)
	writer := tar.NewWriter(compressed)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Typeflag: entry.typeflag, Linkname: entry.link, Mode: 0o644, Size: int64(len(entry.content))}
		if entry.typeflag != tar.TypeReg {
			header.Size = 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(entry.content)); err != nil && entry.typeflag == tar.TypeReg {
			t.Fatal(err)
		}
	}
	for _, closer := range []interface{ Close() error }{writer, compressed, file} {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return archive
}

func packedDirectoryImports(t *testing.T, bundle Bundle, root string) (ImportResult, error) {
	t.Helper()
	name := namedManifest(t, root)
	entries := []archiveEntry{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(path)
		relative, _ := filepath.Rel(root, path)
		entries = append(entries, archiveEntry{name: "package/" + filepath.ToSlash(relative), typeflag: tar.TypeReg, content: string(data)})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return bundle.PackedImports(t.Context(), writeArchive(t, entries), name)
}

func namedManifest(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if name, found := manifest["name"].(string); found {
		return name
	}
	manifest["name"] = "pkg"
	if data, err = json.Marshal(manifest); err != nil {
		t.Fatal(err)
	}
	writeScript(t, path, string(data))
	return "pkg"
}
