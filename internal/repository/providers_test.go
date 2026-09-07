package repository

import (
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestProviderOwnershipUsesPathsCapabilitiesAndProfiles(t *testing.T) {
	command := policy.Command{Name: "pack.compiler.lint", Paths: []string{"src/**"}, Provides: []string{"lint"}, RunOn: []string{"check"}, Adapter: &policy.PackAdapter{PackName: "compiler", Capability: "lint", Languages: []policy.LanguageRule{{Name: "typescript", Paths: []string{"**/*.ts", "**/*.component"}}}}}
	repo := Repository{Config: policy.Config{Checks: []policy.Command{command}}}
	if owner := repo.AnalysisOwner("src/main.ts", "lint", "check"); owner.Pack != "compiler" || owner.Native {
		t.Fatalf("explicit owner did not replace native lint: %+v", owner)
	}
	if !repo.AnalysisOwner("src/main.ts", "typecheck", "check").Native {
		t.Fatal("unclaimed type checking was removed")
	}
	if owner := repo.AnalysisOwner("src/main.ts", "lint", "gate"); owner.Pack != "compiler" {
		t.Fatalf("gate did not include check profile: %+v", owner)
	}
	if !repo.AnalysisOwner("other/main.ts", "lint", "check").Native {
		t.Fatal("path restriction was ignored")
	}
	if repo.CommandOwnsPath(command, "src/not-source.json") {
		t.Fatal("path restriction expanded the provider language claim")
	}
	if owner := repo.AnalysisOwner("src/main.astro", "lint", "check"); owner.Native || owner.Problem == "" {
		t.Fatalf("unknown syntax received native coverage: %+v", owner)
	}
	command.RunOn = []string{"build"}
	repo.Config.Checks = []policy.Command{command}
	if owner := repo.AnalysisOwner("src/main.ts", "lint", "check"); owner.Native || !strings.Contains(owner.Problem, "profile") {
		t.Fatalf("inactive selected provider fell back: %+v", owner)
	}
	command.RunOn = []string{"check"}
	other := command
	other.Name, other.Adapter = "pack.other.lint", &policy.PackAdapter{PackName: "other", Capability: "lint"}
	repo.Config.Checks = []policy.Command{command, other}
	if owner := repo.AnalysisOwner("src/main.ts", "lint", "check"); owner.Native || !strings.Contains(owner.Problem, "both provide") {
		t.Fatalf("ambiguous provider passed: %+v", owner)
	}
	if files := repo.NativeAnalysisFiles([]string{"src/main.ts", "other/main.ts"}, "lint", "check"); !slices.Equal(files, []string{"other/main.ts"}) {
		t.Fatalf("native route ignored conflict: %v", files)
	}
	repo.Config.UnavailablePacks = []string{"compiler"}
	if repo.AnalysisOwner("other/main.ts", "lint", "check").Native {
		t.Fatal("unresolved selected pack enabled a hidden fallback")
	}
}
