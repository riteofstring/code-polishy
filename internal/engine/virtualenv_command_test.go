package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestDoctorAcceptsPinnedVirtualenvCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix virtualenv interpreter links")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	executable, err := filepath.EvalSymlinks(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.Symlink(executable, filepath.Join(home, filepath.Base(executable))); err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, ".python-version", "3.12.13\n", 0o600)
	writeEngineFile(t, root, ".venv/pyvenv.cfg", "home = "+home+"\nversion_info = 3.12.13\n", 0o600)
	if err := os.MkdirAll(filepath.Join(root, ".venv", "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, filepath.Join(root, ".venv", "bin", "python")); err != nil {
		t.Fatal(err)
	}
	command := policy.Command{Name: "python-build", Argv: []string{".venv/bin/python", "-m", "build", "--no-isolation"}, Cwd: "."}
	repo := repository.Repository{Root: root}
	if finding, exists := configuredCommandFinding(repo, command); exists {
		t.Fatalf("managed interpreter rejected: %+v", finding)
	}
	writeEngineFile(t, root, ".python-version", "3.13.1\n", 0o600)
	if finding, exists := configuredCommandFinding(repo, command); !exists || finding.Check != "policy.command" {
		t.Fatalf("mismatched environment accepted: %+v", finding)
	}
}
