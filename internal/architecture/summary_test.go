package architecture

import (
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestSummarySeparatesProductionTestsAndDeclaredConcentration(t *testing.T) {
	t.Parallel()
	config := policy.Config{
		Modules: []policy.Module{
			{Name: "api", Paths: []string{"api/**"}, DependsOn: []string{"domain"}},
			{Name: "domain", Paths: []string{"domain/**"}},
		},
		ModuleByName: map[string]int{"api": 0, "domain": 1},
		Tests: policy.Testing{Suites: []policy.TestSuite{{
			Name: "api-unit", Scope: "module", Cost: "quick", Modules: []string{"api"}, RunOn: []string{"focused", "full"},
		}}},
	}
	repo := repository.Repository{Config: config}
	summaries := Summary(repo, []string{"api/api.go", "api/api_test.go", "domain/model.go", "README.md"})
	if len(summaries) != 2 || summaries[0].Name != "api" || summaries[0].Production != 1 || summaries[0].Tests != 1 ||
		summaries[0].Outgoing != 1 || summaries[0].Incoming != 0 || summaries[0].FocusedSuites != 1 ||
		summaries[1].Name != "domain" || summaries[1].Incoming != 1 || summaries[1].Production != 1 {
		t.Fatalf("summaries = %+v", summaries)
	}
}
