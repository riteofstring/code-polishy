package repository

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCargoManifestInventoryReadsPackagesAndVirtualWorkspaces(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	writeFile(t, repo.Root, "Cargo.toml", "[workspace]\nmembers = ['crates/*']\nexclude = ['crates/scratch']\ndefault-members = ['crates/app']\n[workspace.package]\nedition = '2024'\nversion = '1.0.0'\n")
	writeFile(t, repo.Root, "crates/app/Cargo.toml", "[package]\nname = 'actual-name'\nworkspace = '../..'\nedition.workspace = true\nversion.workspace = true\n[package.metadata.editor]\nmessage = '''not a table: [package]'''\n")
	inventory := repo.InspectCargoManifests([]string{"crates/app/Cargo.toml", "Cargo.toml", "Cargo.toml", "src/lib.rs"})
	if len(inventory.Problems) != 0 || len(inventory.Manifests) != 2 {
		t.Fatalf("manifest inventory = %+v", inventory)
	}
	workspace, member := inventory.Manifests[0], inventory.Manifests[1]
	if workspace.Package != nil || workspace.Workspace == nil || workspace.Root != "." ||
		!slices.Equal(workspace.Workspace.Members, []string{"crates/*"}) ||
		!slices.Equal(workspace.Workspace.Exclude, []string{"crates/scratch"}) ||
		!slices.Equal(workspace.Workspace.DefaultMembers, []string{"crates/app"}) {
		t.Fatalf("virtual workspace = %+v", workspace)
	}
	if member.Package == nil || member.Package.Name != "actual-name" || member.Package.Workspace != "../.." || member.Root != "crates/app" {
		t.Fatalf("member manifest = %+v", member)
	}
}

func TestCargoManifestInventoryReportsMalformedInputs(t *testing.T) {
	t.Parallel()
	for name, input := range map[string]string{
		"syntax":             "[package\nname = 'app'\n",
		"duplicate":          "[package]\nname = 'app'\nname = 'other'\n",
		"package shape":      "package = 'app'\n",
		"members shape":      "[workspace]\nmembers = [1]\n",
		"name shape":         "[package]\nname = []\n",
		"no package":         "[profile.release]\nopt-level = 3\n",
		"no name":            "[package]\nversion = '1.0.0'\n",
		"workspace conflict": "[package]\nname = 'app'\nworkspace = '..'\n[workspace]\n",
		"invalid UTF-8":      "[package]\nname = '\xff'\n",
	} {
		t.Run(name, func(t *testing.T) {
			repo := Repository{Root: t.TempDir()}
			writeFile(t, repo.Root, "Cargo.toml", input)
			inventory := repo.InspectCargoManifests([]string{"Cargo.toml"})
			if len(inventory.Manifests) != 0 || len(inventory.Problems) != 1 || inventory.Problems[0].Path != "Cargo.toml" || inventory.Problems[0].Message == "" {
				t.Fatalf("malformed manifest inventory = %+v", inventory)
			}
		})
	}
}

func TestCargoManifestInventoryPreservesValidSiblingAndErrorLocation(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	writeFile(t, repo.Root, "good/Cargo.toml", "[package]\nname = 'good'\n")
	writeFile(t, repo.Root, "bad/Cargo.toml", "[package]\nname = 'bad'\nname = 'duplicate'\n")
	inventory := repo.InspectCargoManifests([]string{"bad/Cargo.toml", "good/Cargo.toml"})
	if len(inventory.Manifests) != 1 || inventory.Manifests[0].Package.Name != "good" ||
		len(inventory.Problems) != 1 || inventory.Problems[0].Path != "bad/Cargo.toml" || inventory.Problems[0].Line != 3 {
		t.Fatalf("partial manifest inventory = %+v", inventory)
	}
}

func TestCargoManifestInventoryEnforcesContainedRegularInputs(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	for _, path := range []string{"../Cargo.toml", "missing/Cargo.toml"} {
		inventory := repo.InspectCargoManifests([]string{path})
		if len(inventory.Manifests) != 0 || len(inventory.Problems) != 1 || inventory.Problems[0].Path != path {
			t.Fatalf("unreadable input %q = %+v", path, inventory)
		}
	}
}

func TestCargoManifestInventoryRejectsLinkedParents(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	outside := t.TempDir()
	writeFile(t, outside, "Cargo.toml", "[package]\nname = 'outside'\n")
	if err := os.Symlink(outside, filepath.Join(repo.Root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	inventory := repo.InspectCargoManifests([]string{"linked/Cargo.toml"})
	if len(inventory.Manifests) != 0 || len(inventory.Problems) != 1 || inventory.Problems[0].Path != "linked/Cargo.toml" {
		t.Fatalf("linked parent inventory = %+v", inventory)
	}
}

func TestCargoManifestInventoryBoundsInputBeforeParsing(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	path := filepath.Join(repo.Root, "Cargo.toml")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(8<<20 + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	inventory := repo.InspectCargoManifests([]string{"Cargo.toml"})
	if len(inventory.Problems) != 1 || !strings.Contains(inventory.Problems[0].Message, "input exceeds") {
		t.Fatalf("oversized manifest = %+v", inventory)
	}
}
