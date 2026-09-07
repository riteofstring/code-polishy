package pack

import (
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestFunctionFactsUseCorePolicyInAnalysisAndConformance(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "main.ts", "export function value(n: number) { if (n > 0) return 1; if (n < 0) return -1; return 0; }\n", 0o644)
	repo, err := repository.Open(root, root, policy.Config{Quality: policy.EffectiveQuality(policy.Quality{Complexity: policy.Complexity{TypeScript: 3}})})
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Capability: "complexity", Files: []string{"main.ts"}, Pack: policy.PackSelection{Name: "metrics"}}
	if err := prepareInputs(repo, &request); err != nil {
		t.Fatal(err)
	}
	functions := []FunctionFact{{Path: "main.ts", Name: "value", Line: 1, Column: 1, Complexity: 3, Depth: 1, Parameters: 1}}
	response := Response{Status: "pass", Inputs: request.Context, Facts: &SourceFacts{Functions: &functions}}
	adapter := &policy.PackAdapter{PackName: "metrics", Capability: "complexity"}
	findings := analysisFindings(repo, adapter, response)
	if len(findings) != 1 || findings[0].Check != "quality.functioncomplexity" || findings[0].Subject != "value" {
		t.Fatalf("core did not emit exactly one policy finding: %+v", findings)
	}
	fixture := Fixture{ExpectedStatus: "findings", ExpectedRules: []string{"quality.functioncomplexity"}}
	if err := verifyFixtureResult(repo, fixture, request, response); err != nil {
		t.Fatalf("real metric defect was not credited: %v", err)
	}
	functions[0].Complexity = 2
	if findings := analysisFindings(repo, adapter, response); len(findings) != 0 {
		t.Fatalf("valid metrics failed: %+v", findings)
	}
	if err := verifyFixtureResult(repo, fixture, request, response); err == nil || !strings.Contains(err.Error(), "received pass") {
		t.Fatalf("fixture passed without detecting its defect: %v", err)
	}
	fixture.ExpectedStatus, fixture.ExpectedRules = "pass", nil
	if err := verifyFixtureResult(repo, fixture, request, response); err != nil {
		t.Fatalf("valid fixture failed: %v", err)
	}
}
