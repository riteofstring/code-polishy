package repository

import (
	"strings"
	"testing"
)

func TestPythonProjectInventoryRetainsNonExactDeclarationsWithoutAdmittingThem(t *testing.T) {
	t.Parallel()
	for _, reference := range []string{"v1.2.3", "release/stable", "ABCDEF0"} {
		t.Run(reference, func(t *testing.T) {
			manifest := "[project]\ndependencies = [\"private-tool @ git+ssh://git@example.test/team/tool.git@" + reference + "#subdirectory=python/tool\", \"requests>=2.0,<3\"]\n"
			project, err := ParsePythonProjectInventory("pyproject.toml", []byte(manifest))
			if err != nil || len(project.Requirements) != 2 {
				t.Fatalf("inventory = %+v, error = %v", project, err)
			}
			source := project.Requirements[0].Git
			if source.DeclaredRef != reference || source.Commit != "" || source.Subdirectory != "python/tool" || !strings.Contains(source.InventoryIdentity(), "@"+reference) {
				t.Fatalf("inventory changed an unresolved declaration: %+v", source)
			}
			if _, err := ParsePythonProject("pyproject.toml", []byte(manifest)); err == nil {
				t.Fatal("active project admitted a non-exact Git declaration")
			}
			if project.Requirements[1].Version != "<3,>=2.0" {
				t.Fatalf("registry range = %q", project.Requirements[1].Version)
			}
		})
	}
}

func TestPythonGitInventoryRetainsRecordedRefAndResolution(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	for _, kind := range []string{"rev", "tag", "branch"} {
		t.Run(kind, func(t *testing.T) {
			value := "https://example.test/team/tool.git?" + kind + "=Release/V1&subdirectory=python/tool#" + commit
			source, err := ParsePythonGitInventorySource(value, commit, "python/tool")
			if err != nil || source.DeclaredRef != "Release/V1" || source.RefKind != kind || source.Commit != commit || source.Subdirectory != "python/tool" {
				t.Fatalf("recorded source = %+v, error = %v", source, err)
			}
			if !strings.Contains(source.InventoryIdentity(), "@Release/V1") || !strings.Contains(source.InventoryIdentity(), "commit="+commit) {
				t.Fatalf("incomplete inventory identity: %s", source.InventoryIdentity())
			}
			if _, err := ParsePythonGitSource(value, commit, "python/tool"); err == nil {
				t.Fatal("active lock admitted a moving ref")
			}
		})
	}
	for _, kind := range []string{"tag", "branch"} {
		value := "https://example.test/team/tool.git?" + kind + "=" + commit + "#" + commit
		if _, err := ParsePythonGitSource(value, "", ""); err == nil {
			t.Fatalf("a hexadecimal %s passed as a commit revision", kind)
		}
	}
}

func TestPythonGitInventoryRejectsUnsafeOrAmbiguousSources(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	for _, value := range []string{
		"https://token@example.test/team/tool.git?tag=v1#" + commit,
		"https://example.test/team/tool.git?tag=v1&branch=main#" + commit,
		"https://example.test/team/tool.git?tag=v1&tag=v2#" + commit,
		"https://example.test/team/tool.git?tag=v1&token=secret#" + commit,
		"https://example.test/team/tool.git?tag=v1&subdirectory=../escape#" + commit,
		"https://example.test/team/tool.git?tag=v1#abcdef0",
		"https://example.test/team/tool.git?tag=main~1#" + commit,
		"https://example.test/team/tool.git?tag=../main#" + commit,
		"https://example.test/team/tool.git?rev=" + strings.Repeat("b", 40) + "#" + commit,
	} {
		if source, err := ParsePythonGitInventorySource(value, "", ""); err == nil {
			t.Fatalf("unsafe source passed: %+v", source)
		}
	}
	if _, err := ParsePythonGitInventorySource("https://example.test/team/tool.git?tag=v1#"+commit, strings.Repeat("b", 40), ""); err == nil {
		t.Fatal("contradictory supplied commit passed")
	}
	manifest := "[project]\ndependencies = [\"tool @ git+https://example.test/tool.git@v1\", \"tool @ git+https://example.test/tool.git@v2\"]\n"
	if _, err := ParsePythonProjectInventory("pyproject.toml", []byte(manifest)); err == nil {
		t.Fatal("contradictory historical tags passed")
	}
}
