package quality

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestGoToolCommandsKeepDistinctModulePaths(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot = repo.Root
	staticcheck := repo.PolicyTool("staticcheck")
	if err := os.MkdirAll(filepath.Dir(staticcheck), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staticcheck, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	packages := map[string]map[string]bool{}
	for _, root := range []string{".", "root", "a/b", "a.b", "a-b", "Go módulos (1)!", strings.Repeat("long", 50)} {
		packages[root] = map[string]bool{"./example": true}
	}
	validName := regexp.MustCompile(`^[A-Za-z0-9._-]{1,160}$`)
	for _, capability := range []string{"lint", "dead-code"} {
		t.Run(capability, func(t *testing.T) {
			commands, findings := goPackageToolCommands(repo, packages, capability)
			if len(findings) != 0 || len(commands) != len(packages) {
				t.Fatalf("commands = %+v, findings = %+v", commands, findings)
			}
			seen := map[string]bool{}
			for _, command := range commands {
				if !validName.MatchString(command.Name) || seen[command.Name] {
					t.Errorf("invalid or duplicate command identifier %q", command.Name)
				}
				seen[command.Name] = true
				if _, found := packages[command.Cwd]; !found {
					t.Errorf("command lost its module root: %+v", command)
				}
				want := []string{staticcheck, "./example"}
				if capability == "lint" {
					want = []string{repo.GoTool("go"), "vet", "./example"}
				}
				if !slices.Equal(command.Argv, want) {
					t.Errorf("argv = %q, want %q", command.Argv, want)
				}
			}
		})
	}
}
