package quality

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestGeneratedLintExemptionsPrecedeResultLimits(t *testing.T) {
	repo := qualityRepository(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	repo.PolicyRoot = root
	repo.Config.Scope.Generated = []string{"generated/**"}
	var source strings.Builder
	for index := range 300 {
		fmt.Fprintf(&source, "export function validate%d(value,a,b,c,d,e) {%sreturn value;%s}\n", index, strings.Repeat("if(value){", 10), strings.Repeat("}", 10))
	}
	source.WriteString(strings.Repeat("// generated explanation\n", 1800))
	paths := []string{"generated/a.js", "generated/b.js", "generated/c.js"}
	for _, path := range paths {
		writeQualityFile(t, repo.Root, path, source.String())
	}
	if findings := JavaScriptLintFindings(t.Context(), repo, paths); len(findings) != 0 {
		t.Fatalf("exempt generated diagnostics prevented analysis: %+v", findings)
	}
	writeQualityFile(t, repo.Root, paths[0], source.String()+"export function broken() { return 1; return 2; }\n")
	writeQualityFile(t, repo.Root, "app/authored.js", "export function validate(value,a,b,c,d,e) {"+strings.Repeat("if(value){", 10)+"return value;"+strings.Repeat("}", 10)+"}\n")
	findings := JavaScriptLintFindings(t.Context(), repo, append(paths, "app/authored.js"))
	generated, authored := 0, map[string]bool{}
	for _, finding := range findings {
		if finding.Path == "app/authored.js" && finding.Check == "quality.complexity" {
			authored[finding.Subject] = true
		} else if finding.Path == paths[0] && finding.Check == "quality.lint" && finding.Subject == "no-unreachable" {
			generated++
		} else {
			t.Fatalf("unexpected diagnostic: %+v", finding)
		}
	}
	if generated != 1 || !authored["complexity"] || !authored["max-depth"] || !authored["max-params"] {
		t.Fatalf("generated semantic or authored complexity coverage lost: %+v", findings)
	}
}

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
