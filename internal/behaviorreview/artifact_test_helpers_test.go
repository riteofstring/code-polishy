package behaviorreview

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func readBehaviorArtifact(t *testing.T, root, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(artifactPath(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func behaviorRoot(t *testing.T, repo repository.Repository) string {
	t.Helper()
	root, err := behaviorReviewRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	return root.path
}

func behaviorArtifact(t *testing.T, repo repository.Repository) *artifactHandle {
	t.Helper()
	root, err := behaviorReviewRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return root
}

func writeBehaviorFile(t *testing.T, root, name, contents string) string {
	t.Helper()
	return writeBehaviorBytes(t, root, name, []byte(contents))
}

func writeBehaviorBytes(t *testing.T, root, name string, contents []byte) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func removeBehaviorFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func gitBehavior(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}
