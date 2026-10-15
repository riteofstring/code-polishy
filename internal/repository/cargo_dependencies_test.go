package repository

import (
	"slices"
	"testing"
)

func TestCargoManifestReadsEveryDependencyTable(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	writeFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n[dependencies]\njson={package='serde_json', version='=1.0.140', optional=true, features=['raw_value']}\n[dev-dependencies]\nasserts='=1.0.0'\n[build-dependencies]\ncc.workspace=true\n[target.'cfg(windows)'.dependencies]\nwin='=0.1.0'\n[target.'cfg(unix)'.dev-dependencies]\nunix='=0.2.0'\n[target.'cfg(unix)'.build-dependencies]\nnative={path='native'}\n[workspace.dependencies]\ncc='=1.2.0'\n[patch.crates-io]\npatched={git='https://example.test/repo', rev='fixed'}\n[replace]\n'old:1.0.0'={path='fork'}\n")
	inventory := repo.InspectCargoManifests([]string{"Cargo.toml"})
	if len(inventory.Problems) != 0 || len(inventory.Manifests) != 1 {
		t.Fatalf("dependency manifest failed: %+v", inventory)
	}
	declarations := inventory.Manifests[0].Dependencies
	if len(declarations) != 9 {
		t.Fatalf("dependency tables were omitted: %+v", declarations)
	}
	for _, table := range []string{"dependencies", "dev-dependencies", "build-dependencies", "target.cfg(windows).dependencies", "target.cfg(unix).dev-dependencies", "target.cfg(unix).build-dependencies", "workspace.dependencies", "patch.crates-io", "replace"} {
		if !slices.ContainsFunc(declarations, func(declaration CargoDependencyDeclaration) bool { return declaration.Table == table }) {
			t.Errorf("missing %s dependencies", table)
		}
	}
	if !slices.ContainsFunc(declarations, func(declaration CargoDependencyDeclaration) bool {
		return declaration.Alias == "json" && declaration.Package == "serde_json" && declaration.Version == "=1.0.140" && declaration.Optional
	}) {
		t.Fatalf("renamed/optional dependency lost its identity: %+v", declarations)
	}
	if !slices.ContainsFunc(declarations, func(declaration CargoDependencyDeclaration) bool {
		return declaration.Table == "build-dependencies" && declaration.Alias == "cc" && declaration.Workspace && declaration.Version == ""
	}) {
		t.Fatalf("workspace inheritance was replaced with a fabricated version: %+v", declarations)
	}
}

func TestCargoManifestRejectsMalformedDependencyShapes(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]string{
		"number": "3", "array": "['=1.0.0']", "version": "{version=1}", "path": "{path=['lib']}",
		"features": "{version='=1.0.0', features=[1]}", "optional": "{version='=1.0.0', optional='true'}",
		"inheritance": "{workspace=false}", "default features": "{version='=1.0.0', default-features=1}",
	} {
		t.Run(name, func(t *testing.T) {
			repo := Repository{Root: t.TempDir()}
			writeFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n[dependencies]\ncrate="+value+"\n")
			inventory := repo.InspectCargoManifests([]string{"Cargo.toml"})
			if len(inventory.Manifests) != 0 || len(inventory.Problems) != 1 {
				t.Fatalf("invalid dependency became a usable manifest: %+v", inventory)
			}
		})
	}
}
