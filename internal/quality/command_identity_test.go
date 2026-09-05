package quality

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestShellChecksKeepExactPathsAndValidDistinctGateNames(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	paths := []string{"Open Learn Portugal.sh", "tools/a-b.sh", "tools/a/b.sh", "tools/ação.sh", "tools/" + strings.Repeat("long", 50) + ".sh"}
	for _, path := range paths {
		writeQualityFile(t, repo.Root, path, "#!/usr/bin/env bash\nexit 0\n")
	}
	commands := CheckCommands(repo, repository.Selection{Files: paths, All: true}, "check")
	validName := regexp.MustCompile(`^[a-zA-Z0-9._-]{1,160}$`)
	names := map[string]bool{}
	checked := map[string]bool{}
	for _, command := range commands {
		if !strings.HasPrefix(command.Name, "shell-syntax-") {
			continue
		}
		if !validName.MatchString(command.Name) || names[command.Name] {
			t.Fatalf("invalid or duplicate gate name: %q", command.Name)
		}
		names[command.Name] = true
		checked[command.Argv[2]] = true
		process := exec.Command(command.Argv[0], command.Argv[1:]...)
		process.Dir = repo.Root
		if output, err := process.CombinedOutput(); err != nil {
			t.Fatalf("shell syntax check failed: %s: %v", output, err)
		}
	}
	for _, path := range paths {
		if !checked[path] {
			t.Errorf("shell file was not checked: %q", path)
		}
	}
}
