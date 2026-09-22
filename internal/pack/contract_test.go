package pack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestContractValidatorUsesPublishedDocumentsAndRuntimeSemantics(t *testing.T) {
	root := filepath.Join("..", "..")
	manifest := filepath.Join(root, "tools", "fixtures", "language-pack", ManifestFilename)
	request := filepath.Join(root, "tools", "fixtures", "language-pack", "examples", "request-v4.json")
	response := filepath.Join(root, "tools", "fixtures", "language-pack", "examples", "response-v4.json")
	for _, options := range []ContractValidationOptions{
		{Kind: "manifest", InputPath: manifest},
		{Kind: "request", InputPath: request},
		{Kind: "response", InputPath: response, RequestPath: request},
		{Kind: "request", InputPath: filepath.Join(root, "tools", "fixtures", "language-pack", "examples", "request-dead-code-v4.json")},
		{Kind: "response", InputPath: filepath.Join(root, "tools", "fixtures", "language-pack", "examples", "response-dead-code-v4.json"), RequestPath: filepath.Join(root, "tools", "fixtures", "language-pack", "examples", "request-dead-code-v4.json")},
	} {
		report, err := ValidateContract(options)
		if err != nil || report.Status != "passed" || len(report.Errors) != 0 {
			t.Fatalf("validation %+v = %+v: %v", options, report, err)
		}
	}
}

func TestContractValidatorReportsTheExactInvalidDeadCodeRange(t *testing.T) {
	root := filepath.Join("..", "..", "tools", "fixtures", "language-pack", "examples")
	report, err := ValidateContract(ContractValidationOptions{
		Kind: "response", InputPath: filepath.Join(root, "invalid", "response-dead-code-range-v4.json"),
		RequestPath: filepath.Join(root, "request-dead-code-v4.json"),
	})
	if err != nil || report.Status != "failed" {
		t.Fatalf("report = %+v: %v", report, err)
	}
	if !slices.ContainsFunc(report.Errors, func(issue ContractValidationIssue) bool {
		return issue.Document == "input" && issue.Path == "facts.deadCode[0].endLine"
	}) {
		t.Fatalf("issues = %+v", report.Errors)
	}
}

func TestContractValidatorReportsTheExactInvalidCollectionField(t *testing.T) {
	root := filepath.Join("..", "..", "tools", "fixtures", "language-pack", "examples")
	report, err := ValidateContract(ContractValidationOptions{
		Kind: "response", InputPath: filepath.Join(root, "invalid", "response-comment-kind-v4.json"),
		RequestPath: filepath.Join(root, "request-v4.json"),
	})
	if err != nil || report.Status != "failed" {
		t.Fatalf("report = %+v: %v", report, err)
	}
	if !slices.ContainsFunc(report.Errors, func(issue ContractValidationIssue) bool {
		return issue.Document == "input" && issue.Path == "facts.comments[0].kind"
	}) {
		t.Fatalf("issues = %+v", report.Errors)
	}
}

func TestContractValidatorRequiresResponseCustody(t *testing.T) {
	_, err := ValidateContract(ContractValidationOptions{Kind: "response", InputPath: "response.json"})
	if err == nil || err.Error() != "response validation requires a request path" {
		t.Fatalf("error = %v", err)
	}
}

func TestContractValidatorReportsAnUnboundPolicyDeclarationScope(t *testing.T) {
	path := filepath.Join("..", "..", "tools", "fixtures", "language-pack", "examples", "request-v4.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	request := map[string]any{}
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	policyInput := request["policy"].(map[string]any)
	policyInput["declarations"] = []any{map[string]any{"kind": "python.contract", "version": 1, "scopes": []any{"scope-missing"}, "inputs": []any{}, "data": map[string]any{"project": "pyproject.toml"}}}
	invalid, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(input, invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := ValidateContract(ContractValidationOptions{Kind: "request", InputPath: input})
	if err != nil || report.Status != "failed" {
		t.Fatalf("report = %+v: %v", report, err)
	}
	if !slices.ContainsFunc(report.Errors, func(issue ContractValidationIssue) bool {
		return issue.Phase == "semantic" && issue.Path == "policy.declarations[0].scopes[0]"
	}) {
		t.Fatalf("issues = %+v", report.Errors)
	}
}
