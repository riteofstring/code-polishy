package pack

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestDeadCodeFactsUseCorePolicyInAnalysisAndConformance(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "main.py", "def unused():\n    pass\n", 0o644)
	repo, err := repository.Open(root, root, policy.Config{Quality: policy.EffectiveQuality(policy.Quality{})})
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Capability: "dead-code", Files: []string{"main.py"}, DiagnosticFiles: []string{"main.py"}, WriteFiles: []string{"main.py"}, Pack: policy.PackSelection{Name: "python"}}
	if err := prepareInputs(repo, &request); err != nil {
		t.Fatal(err)
	}
	deadCode := []DeadCodeFact{{Analyzer: "vulture", Path: "main.py", Line: 1, EndLine: 2, Name: "unused", Kind: "function", Confidence: 100, Message: "unused function unused"}}
	response := Response{Status: "pass", Inputs: request.Context, Coverage: &Coverage{Analyzed: request.Files, Unsupported: []Unsupported{}}, Facts: &SourceFacts{DeadCode: &deadCode}}
	findings := analysisFindings(repo, &policy.PackAdapter{PackName: "python", Capability: "dead-code"}, response)
	if len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	finding := findings[0]
	if finding.Check != "quality.deadCode" || finding.Path != "main.py" || finding.Line != 1 || finding.Column != 0 || finding.Subject != "vulture:4cc5e825ca7b05e4253c6b72dda2ba447ddbb3b88a346060c9e524832c282d29" || finding.Message != "lines 1-2: unused function unused (function, 100% confidence)" {
		t.Fatalf("finding = %+v", finding)
	}
	if finding.Remediation.Summary != "Delete the unused definition. Do not generate reachability declarations or entry-point lists from dead-code findings." || finding.Remediation.NextCommand == nil || !slices.Equal(finding.Remediation.NextCommand.Argv, []string{"code-polishy", "check", "--all"}) {
		t.Fatalf("remediation = %+v", finding.Remediation)
	}
	fixture := Fixture{ExpectedStatus: "findings", ExpectedRules: []string{"quality.deadCode"}}
	if err := verifyFixtureResult(repo, fixture, request, response); err != nil {
		t.Fatalf("real dead-code defect was not credited: %v", err)
	}
	deadCode = nil
	if findings := analysisFindings(repo, &policy.PackAdapter{PackName: "python", Capability: "dead-code"}, response); len(findings) != 0 {
		t.Fatalf("empty dead-code facts failed: %+v", findings)
	}
	fixture.ExpectedStatus, fixture.ExpectedRules = "pass", nil
	if err := verifyFixtureResult(repo, fixture, request, response); err != nil {
		t.Fatalf("valid fixture failed: %v", err)
	}
}

func TestDeadCodeFactRangesMustExistInOriginalSource(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "main.py", "unused = 1\n", 0o644)
	repo, err := repository.Open(root, root, policy.Config{})
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Capability: "dead-code", Files: []string{"main.py"}, DiagnosticFiles: []string{"main.py"}}
	if err := prepareInputs(repo, &request); err != nil {
		t.Fatal(err)
	}
	deadCode := []DeadCodeFact{{Analyzer: "vulture", Path: "main.py", Line: 1, EndLine: 3, Name: "unused", Kind: "variable", Confidence: 60, Message: "unused variable unused"}}
	response := Response{Status: "pass", Inputs: request.Context, Coverage: &Coverage{Analyzed: request.Files, Unsupported: []Unsupported{}}, Facts: &SourceFacts{DeadCode: &deadCode}}
	if err := verifyAnalysisInputs(repo, request, response); err == nil {
		t.Fatal("invented dead-code range passed")
	}
}
