package engine

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestDoctorValidatesUnselectedOperationalHandoffs(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	policyEngine.Repository.Config.Documentation.Handoffs = []policy.OperationalHandoff{{
		Name: "deployment", Description: "Deploy the repository.", Path: "docs/operations/missing.md", Situations: []string{"deployment"},
	}}
	report, err := policyEngine.DesignContext(ContextRequest{Mode: "none", Situations: []string{"authentication"}})
	if err != nil || HasFindings(report) || report.RepositoryContext == nil || len(report.RepositoryContext.Handoffs) != 0 {
		t.Fatalf("unselected handoff blocked context: report=%+v error=%v", report, err)
	}
	report, err = policyEngine.Doctor(t.Context())
	if err != nil || !slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool {
		return finding.Check == repository.OperationalHandoffCheck && finding.Subject == "deployment:docs/operations/missing.md"
	}) {
		t.Fatalf("doctor did not check the full handoff inventory: report=%+v error=%v", report, err)
	}
}
