package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestAssetLinkIdentityPreservesContainedAssetsAndInvalidatesChanges(t *testing.T) {
	repo := assetLinkRepository(t)
	first, err := repo.InputDigest("public/images")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ContentDigest("public/images"); err == nil {
		t.Fatal("strict content reader accepted a link")
	}
	if err := os.WriteFile(filepath.Join(repo.Root, "images/picture.png"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := repo.InputDigest("public/images")
	if err != nil || first == second {
		t.Fatalf("asset bytes did not invalidate identity: %v", err)
	}
	if err := os.Rename(filepath.Join(repo.Root, "images"), filepath.Join(repo.Root, "other")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo.Root, "public/images")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../other", filepath.Join(repo.Root, "public/images")); err != nil {
		t.Fatal(err)
	}
	third, err := repo.InputDigest("public/images")
	if err != nil || second == third {
		t.Fatalf("link target did not invalidate identity: %v", err)
	}
}

func TestAssetLinkIdentityRejectsUnsafeTargetsAndWrites(t *testing.T) {
	for _, target := range []string{"../missing", "images", "../../outside"} {
		t.Run(target, func(t *testing.T) {
			repo := assetLinkRepository(t)
			path := filepath.Join(repo.Root, "public/images")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.InputDigest("public/images"); err == nil {
				t.Fatal("unsafe asset target accepted")
			}
		})
	}
	for _, name := range []string{"program.js", "package.json", "nested-link", "executable.png"} {
		t.Run(name, func(t *testing.T) {
			repo := assetLinkRepository(t)
			path := filepath.Join(repo.Root, "images", name)
			if name == "nested-link" {
				if err := os.Symlink("picture.png", path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("source"), 0o600); err != nil {
				t.Fatal(err)
			}
			if name == "executable.png" {
				if err := os.Chmod(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := repo.InputDigest("public/images"); err == nil {
				t.Fatal("non-asset target accepted")
			}
		})
	}
	repo := assetLinkRepository(t)
	if err := repo.WriteRegularFile("public/images/picture.png", []byte("overwrite")); err == nil {
		t.Fatal("write through a link was accepted")
	}
	if data, err := os.ReadFile(filepath.Join(repo.Root, "images/picture.png")); err != nil || string(data) != "original" {
		t.Fatalf("asset changed: %q %v", data, err)
	}
}

func assetLinkRepository(t *testing.T) Repository {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"images", "public"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "images/picture.png"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../images", filepath.Join(root, "public/images")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	config := policy.Config{Modules: []policy.Module{{Name: "assets", Paths: []string{"images/**", "other/**", "public/**"}}}, ModuleByName: map[string]int{"assets": 0}}
	repo, err := Open(root, root, config)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}
