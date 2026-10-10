package release

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReleaseHostMatrixRejectsCapabilityUpgradeJunctionEscape(t *testing.T) {
	repo, _, nextRoot, old, next := capabilityUpgradeFixture(t)
	outside := filepath.Join(t.TempDir(), "outside")
	junction := filepath.Join(repo, ".code-polishy-reports")
	command := exec.CommandContext(t.Context(), "cmd.exe", "/c", "mklink", "/J", junction, outside)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	result, err := writeReleaseLock(repo, nextRoot, next)
	if err == nil || result.Changed {
		t.Fatalf("upgrade through junction = %+v, error = %v", result, err)
	}
	assertCapabilityUpgradeLock(t, repo, old)
	if _, err := os.Lstat(outside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("upgrade created a directory outside the repository: %v", err)
	}
	if err := os.Remove(junction); err != nil {
		t.Fatal(err)
	}
	result, err = writeReleaseLock(repo, nextRoot, next)
	if err != nil || !result.Changed {
		t.Fatalf("upgrade after removing junction = %+v, error = %v", result, err)
	}
	assertCapabilityUpgradeLock(t, repo, next)
}
