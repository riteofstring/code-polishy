package rustfacts

import "testing"

func TestRustSourceEditionRetainsTargetAndNestedPackageIdentity(t *testing.T) {
	t.Parallel()
	workspaces := []Workspace{{Packages: []Package{
		{Manifest: "Cargo.toml", Edition: "2018", Targets: []Target{{Path: "src/lib.rs", Edition: "2018"}}},
		{Manifest: "x/Cargo.toml", Edition: "2024", Targets: []Target{{Path: "shared/main.rs", Edition: "2024"}}},
	}}}
	for path, want := range map[string]string{"src/lib.rs": "2018", "src/child.rs": "2018", "x/src/child.rs": "2024", "shared/main.rs": "2024"} {
		got, err := SourceEdition(workspaces, path)
		if err != nil || got != want {
			t.Fatalf("%s edition = %q, %v, want %q", path, got, err, want)
		}
	}
}

func TestRustSourceEditionRejectsAmbiguousOrAbsentOwnership(t *testing.T) {
	t.Parallel()
	for name, workspaces := range map[string][]Workspace{
		"absent":        {},
		"mixed targets": {{Packages: []Package{{Manifest: "Cargo.toml", Edition: "2024", Targets: []Target{{Path: "src/bin/tool.rs", Edition: "2018"}}}}}},
		"shared target": {{Packages: []Package{{Manifest: "Cargo.toml", Edition: "2024", Targets: []Target{{Path: "src/lib.rs", Edition: "2024"}, {Path: "src/lib.rs", Edition: "2018"}}}}}},
	} {
		t.Run(name, func(t *testing.T) {
			if edition, err := SourceEdition(workspaces, "src/lib.rs"); err == nil {
				t.Fatalf("ambiguous source received edition %q", edition)
			}
		})
	}
}
