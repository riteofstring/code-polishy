package engine

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestRecordedAdoptionFindingsRemainOutsideExactRequestedSelection(t *testing.T) {
	data, err := os.ReadFile("testdata/adoption-selection.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Arguments []string         `json:"arguments"`
		ExitCode  int              `json:"exitCode"`
		Findings  []policy.Finding `json:"findings"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	policyEngine := &Engine{Repository: repository.Repository{Root: t.TempDir()}}
	selected := slices.Clone(fixture.Arguments[2:])
	report := policyEngine.withSelection(policyEngine.finish(fixture.Findings, nil), repository.Selection{
		Files: selected, Requested: repository.RequestedSelection{Mode: "files", Operands: selected, Expanded: selected},
	})
	if len(selected) != 14 || len(report.Findings) != 26 || fixture.ExitCode != 1 || !HasFindings(report) {
		t.Fatalf("recorded adoption lost its selection or failures: %+v", report)
	}
	for _, finding := range report.Findings {
		if slices.Contains(selected, finding.Path) || finding.SelectionRelation != policy.SelectionContext {
			t.Fatalf("unselected adoption finding was misrepresented: %+v", finding)
		}
	}
	complete, err := policyEngine.FinalizeReport("check", report)
	if err != nil {
		t.Fatal(err)
	}
	view := ReportView(complete, DisplayOptions{Filters: ReportFilters{Relations: []policy.SelectionRelation{policy.SelectionSelected}}})
	if len(view.Findings) != 0 || view.Summary.Errors != 26 || !HasFindings(complete) {
		t.Fatalf("display filter altered the recorded evaluation: %+v", view)
	}
}
