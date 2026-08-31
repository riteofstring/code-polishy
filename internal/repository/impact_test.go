package repository

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestCandidateImpactRetainsDirectAndDeletedPathOwnership(t *testing.T) {
	t.Parallel()
	repo := Repository{Config: policy.Config{Modules: []policy.Module{
		{Name: "checkout", Paths: []string{"checkout/**"}},
		{Name: "authentication", Paths: []string{"auth/**"}},
	}}}

	impact := repo.CandidateImpact(CandidateDelta{
		AddedOrModified: []string{"checkout/submit.go"},
		Deleted:         []string{"auth/session.go"},
	})

	if !slices.Equal(impact.Paths, []string{"auth/session.go", "checkout/submit.go"}) {
		t.Fatalf("paths = %v", impact.Paths)
	}
	if !slices.Equal(impact.DirectModules, []string{"authentication", "checkout"}) {
		t.Fatalf("direct modules = %v", impact.DirectModules)
	}
	if !slices.Equal(impact.ImpactedModules, []string{"authentication", "checkout"}) {
		t.Fatalf("impacted modules = %v", impact.ImpactedModules)
	}
}

func TestCandidateImpactIncludesTransitiveReverseDependents(t *testing.T) {
	t.Parallel()
	repo := Repository{Config: policy.Config{Modules: []policy.Module{
		{Name: "domain", Paths: []string{"domain/**"}},
		{Name: "api", Paths: []string{"api/**"}, DependsOn: []string{"domain"}},
		{Name: "web", Paths: []string{"web/**"}, DependsOn: []string{"api"}},
	}}}

	impact := repo.CandidateImpact(CandidateDelta{AddedOrModified: []string{"domain/order.go"}})

	if !slices.Equal(impact.DirectModules, []string{"domain"}) {
		t.Fatalf("direct modules = %v", impact.DirectModules)
	}
	if !slices.Equal(impact.ImpactedModules, []string{"api", "domain", "web"}) {
		t.Fatalf("impacted modules = %v", impact.ImpactedModules)
	}
}
