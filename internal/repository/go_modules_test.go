package repository

import (
	"strconv"
	"strings"
	"testing"
)

func TestInspectGoModulesUsesOfficialModuleParser(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	writeFile(t, repo.Root, "go.mod", "module \"example.test/root\"\n\ngo 1.26.0\n")
	inventory := repo.InspectGoModules([]string{"go.mod"})
	if len(inventory.Problems) != 0 || len(inventory.Modules) != 1 || inventory.Modules[0].Name != "example.test/root" {
		t.Fatalf("inventory = %+v", inventory)
	}
}

func TestInspectGoModulesRejectsMalformedManifest(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	writeFile(t, repo.Root, "go.mod", "module\n")
	inventory := repo.InspectGoModules([]string{"go.mod"})
	assertGoModuleProblem(t, inventory, "go.mod", "manifest is malformed")
}

func TestInspectGoModulesRejectsInvalidModuleImportIdentities(t *testing.T) {
	t.Parallel()
	for _, declared := range []string{"../escape", "example.test/../escape", "example.test//app", "example.test/app@v1"} {
		t.Run(declared, func(t *testing.T) {
			repo := Repository{Root: t.TempDir()}
			writeFile(t, repo.Root, "go.mod", "module "+strconv.Quote(declared)+"\n")
			inventory := repo.InspectGoModules([]string{"go.mod"})
			if len(inventory.Modules) != 0 || len(inventory.Problems) != 1 {
				t.Fatalf("invalid module identity accepted: %+v", inventory)
			}
		})
	}
}

func TestInspectGoModulesRejectsDuplicateModuleIdentity(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	writeFile(t, repo.Root, "go.mod", "module example.test/shared\n")
	writeFile(t, repo.Root, "nested/go.mod", "module example.test/shared\n")
	inventory := repo.InspectGoModules([]string{"nested/go.mod", "go.mod"})
	assertGoModuleProblem(t, inventory, "go.mod", "declared by multiple manifests: go.mod, nested/go.mod")
}

func TestInspectGoModulesResolvesContainedWorkspaceUses(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	writeFile(t, repo.Root, "go.work", "go 1.26.0\n\nuse (\n\t.\n\t./nested\n)\n")
	writeFile(t, repo.Root, "go.mod", "module example.test/root\n")
	writeFile(t, repo.Root, "nested/go.mod", "module example.test/nested\n")
	inventory := repo.InspectGoModules([]string{"nested/go.mod", "go.work", "go.mod"})
	if len(inventory.Problems) != 0 || len(inventory.Modules) != 2 {
		t.Fatalf("inventory = %+v", inventory)
	}
}

func TestInspectGoModulesRejectsMalformedWorkspace(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	writeFile(t, repo.Root, "go.work", "use (\n")
	inventory := repo.InspectGoModules([]string{"go.work"})
	assertGoModuleProblem(t, inventory, "go.work", "workspace manifest is malformed")
}

func TestInspectGoModulesRejectsEscapingWorkspaceUse(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	writeFile(t, repo.Root, "workspace/go.work", "go 1.26.0\n\nuse ../../outside\n")
	inventory := repo.InspectGoModules([]string{"workspace/go.work"})
	assertGoModuleProblem(t, inventory, "workspace/go.work", "use path \"../../outside\" is invalid")
}

func TestInspectGoModulesRejectsMissingWorkspaceModule(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	writeFile(t, repo.Root, "go.work", "go 1.26.0\n\nuse ./missing\n")
	writeFile(t, repo.Root, "missing/go.mod", "module example.test/missing\n")
	inventory := repo.InspectGoModules([]string{"go.work"})
	assertGoModuleProblem(t, inventory, "go.work", "does not resolve to one parsed repository module")
}

func TestInspectGoModulesRejectsRepeatedWorkspaceUse(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	writeFile(t, repo.Root, "go.work", "go 1.26.0\n\nuse (\n\t./nested\n\t./nested\n)\n")
	writeFile(t, repo.Root, "nested/go.mod", "module example.test/nested\n")
	inventory := repo.InspectGoModules([]string{"nested/go.mod", "go.work"})
	assertGoModuleProblem(t, inventory, "go.work", "repeats module manifest \"nested/go.mod\"")
}

func TestInspectGoModulesRejectsAmbiguousWorkspaceOwnership(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	writeFile(t, repo.Root, "shared/go.mod", "module example.test/shared\n")
	writeFile(t, repo.Root, "one/go.work", "go 1.26.0\n\nuse ../shared\n")
	writeFile(t, repo.Root, "two/go.work", "go 1.26.0\n\nuse ../shared\n")
	inventory := repo.InspectGoModules([]string{"two/go.work", "shared/go.mod", "one/go.work"})
	assertGoModuleProblem(t, inventory, "two/go.work", "owned by both \"one/go.work\" and \"two/go.work\"")
}

func assertGoModuleProblem(t *testing.T, inventory GoModuleInventory, path, message string) {
	t.Helper()
	for _, problem := range inventory.Problems {
		if problem.Path == path && strings.Contains(problem.Message, message) {
			return
		}
	}
	t.Fatalf("inventory has no %s problem containing %q: %+v", path, message, inventory)
}
