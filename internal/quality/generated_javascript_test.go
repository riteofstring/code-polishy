package quality

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestGeneratedJavaScriptDeadCodeUsesTheDeclaredOwnerEndToEnd(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	var err error
	repo.PolicyRoot, err = filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	repo.Config.Scope.Generated = []string{"python_pkg/generated/**"}
	repo.Config.Scope.GeneratedJavaScript = []policy.GeneratedJavaScript{{
		Paths: []string{"python_pkg/generated/**"}, SourcePackage: "frontend/package.json",
	}}
	writeQualityFile(t, repo.Root, "frontend/package.json", "{\"name\":\"frontend\",\"private\":true,\"type\":\"module\"}\n")
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"python-package\"\nversion = \"1.0.0\"\n")
	writeQualityFile(t, repo.Root, "frontend/src/index.ts", "import { used } from \"../../python_pkg/generated/client.js\";\nexport const boot = () => used();\n")
	writeQualityFile(t, repo.Root, "python_pkg/generated/client.ts", "export const used = () => 7;\nexport const unusedGenerated = () => 8;\n")
	writeQualityFile(t, repo.Root, "python_pkg/generated/orphan.ts", "export const orphan = () => 9;\n")
	findings := JavaScriptDeadCodeFindings(t.Context(), repo, []string{"python_pkg/generated/client.ts"})
	if len(findings) != 2 {
		t.Fatalf("expected the unused generated file and export: %+v", findings)
	}
	byPath := map[string]policy.Finding{}
	for _, finding := range findings {
		if finding.Check != "quality.deadCode" {
			t.Fatalf("unexpected analyzer failure: %+v", finding)
		}
		byPath[finding.Path] = finding
	}
	if _, found := byPath["python_pkg/generated/orphan.ts"]; !found || !strings.Contains(byPath["python_pkg/generated/client.ts"].Message, "unusedGenerated") {
		t.Fatalf("lost generated source reachability: %+v", findings)
	}
	for _, path := range []string{"package.json", "node_modules"} {
		if _, err := os.Lstat(filepath.Join(repo.Root, path)); !os.IsNotExist(err) {
			t.Fatalf("analyzer created %s: %v", path, err)
		}
	}
}

func TestGeneratedJavaScriptInvalidOwnerPreventsDeadCodeExecution(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Generated = []string{"python_pkg/generated/**"}
	repo.Config.Scope.GeneratedJavaScript = []policy.GeneratedJavaScript{{
		Paths: []string{"python_pkg/generated/**"}, SourcePackage: "missing/package.json",
	}}
	policyRoot, observed := fakeFileBundle(t, emptyDeadCodeResult)
	repo.PolicyRoot = policyRoot
	writeQualityFile(t, repo.Root, "python_pkg/generated/client.ts", "export {};\n")
	findings := JavaScriptDeadCodeFindings(t.Context(), repo, []string{"python_pkg/generated/client.ts"})
	if len(findings) != 1 || findings[0].Check != "policy.generatedJavaScriptOwnership" {
		t.Fatalf("missing owner did not block analysis: %+v", findings)
	}
	if _, err := os.Lstat(observed); !os.IsNotExist(err) {
		t.Fatalf("invalid ownership invoked the analyzer: %v", err)
	}
}
