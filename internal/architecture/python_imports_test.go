package architecture

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestPythonModuleIndexDistinguishesConflictingRootsFromStubs(t *testing.T) {
	t.Parallel()
	index := newPythonModuleIndex(repository.PythonProject{
		Root: ".", SourceRoots: []string{".", "src"},
		Files: []string{"shared.py", "src/shared.py", "src/model.py", "src/model.pyi"},
	})
	if !index.ambiguous["shared"] || index.ambiguous["model"] {
		t.Fatalf("module ambiguity = %+v", index.ambiguous)
	}
}

func TestPythonComputedImportConfigurationIsDigestBoundAndNamespaced(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	data := []byte(`{"enabled":["app.plugins.first","app.plugins.second"]}`)
	writeArchitectureFile(t, root, "plugins.json", string(data))
	digest := sha256.Sum256(data)
	repo := repository.Repository{Root: root}
	input := policy.PythonComputedImportInput{Path: "plugins.json", JSONPointer: "/enabled", SHA256: hex.EncodeToString(digest[:])}
	targets, message := pythonComputedTargets(repo, repository.PythonProject{}, policy.PythonComputedImport{Namespace: "app.plugins", Configuration: []policy.PythonComputedImportInput{input}})
	if message != "" || strings.Join(targets, ",") != "app.plugins.first,app.plugins.second" {
		t.Fatalf("targets = %+v, message = %q", targets, message)
	}
	input.SHA256 = strings.Repeat("0", 64)
	if _, message := pythonComputedTargets(repo, repository.PythonProject{}, policy.PythonComputedImport{Namespace: "app.plugins", Configuration: []policy.PythonComputedImportInput{input}}); !strings.Contains(message, "changed") {
		t.Fatalf("message = %q", message)
	}
	escapeData := []byte(`{"enabled":"outside.module"}`)
	writeArchitectureFile(t, root, "escape.json", string(escapeData))
	escapeDigest := sha256.Sum256(escapeData)
	input = policy.PythonComputedImportInput{Path: "escape.json", JSONPointer: "/enabled", SHA256: hex.EncodeToString(escapeDigest[:])}
	if _, message := pythonComputedTargets(repo, repository.PythonProject{}, policy.PythonComputedImport{Namespace: "app.plugins", Configuration: []policy.PythonComputedImportInput{input}}); !strings.Contains(message, "escapes namespace") {
		t.Fatalf("message = %q", message)
	}
}
