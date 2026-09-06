package quality

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestGeneratedBundlesKeepSemanticChecksAndAuthoredHookCoverage(t *testing.T) {
	repo := qualityRepository(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	repo.PolicyRoot = root
	repo.Config.Scope.Generated = []string{"generated/**"}
	repo.Config.Scope.GeneratedJavaScript = []policy.GeneratedJavaScript{{Paths: []string{"generated/**"}, SourcePackage: "web"}}
	repo.Config.JavaScriptLintScopes = []policy.JavaScriptLintScope{{Root: "web", ReactHooks: true}}
	source := `import { useEffect } from "react";
export function Component() {
  return [1].map(() => { useEffect(() => {}, []); return null; });
}
`
	writeQualityFile(t, repo.Root, "web/component.js", source)
	writeQualityFile(t, repo.Root, "generated/bundle.js", source)
	writeQualityFile(t, repo.Root, "generated/broken.js", "export const broken = ;\n")
	findings := JavaScriptLintFindings(t.Context(), repo, []string{"web/component.js", "generated/bundle.js", "generated/broken.js"})
	authored, syntax := false, false
	for _, finding := range findings {
		if finding.Path == "generated/bundle.js" {
			t.Fatalf("generated bundle treated as authored source: %+v", finding)
		}
		if finding.Path == "web/component.js" && strings.Contains(finding.Message, "useEffect") {
			authored = true
		}
		if finding.Path == "generated/broken.js" {
			syntax = true
		}
	}
	if !authored || !syntax {
		t.Fatalf("authored Hooks or generated syntax coverage missing: %+v", findings)
	}
}
