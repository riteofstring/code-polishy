package pack

import (
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
	} {
		report, err := ValidateContract(options)
		if err != nil || report.Status != "passed" || len(report.Errors) != 0 {
			t.Fatalf("validation %+v = %+v: %v", options, report, err)
		}
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
