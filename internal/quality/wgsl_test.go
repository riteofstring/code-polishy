package quality

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestWGSLRequiresAnalysisEvidence(t *testing.T) {
	t.Parallel()
	for _, ordinaryCommand := range []bool{false, true} {
		repo := qualityRepository(t)
		path := "shaders/main.wgsl"
		repo.Config.Modules = []policy.Module{{Name: "shaders", Paths: []string{"shaders/**"}}}
		repo.Config.ModuleByName = map[string]int{"shaders": 0}
		if ordinaryCommand {
			repo.Config.Checks = []policy.Command{{
				Name: "shader-check", Argv: []string{"cargo", "check"}, Modules: []string{"shaders"},
				Provides: []string{"format", "lint", "typecheck", "complexity", "dead-code", "architecture"}, RunOn: []string{"check", "gate"},
			}}
		}
		writeQualityFile(t, repo.Root, path, "@compute @workgroup_size(1) fn main() {}\n")
		findings := CoverageFindings(repo, []string{path})
		for _, subject := range []string{"format:check", "lint:check", "typecheck:check", "complexity:check", "dead-code:check", "architecture:check", "typecheck:gate"} {
			if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
				return finding.Check == "policy.checkCoverage" && finding.Path == path && finding.Subject == subject
			}) {
				t.Fatalf("ordinary command %t: missing %s coverage failure: %+v", ordinaryCommand, subject, findings)
			}
		}
		if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
			return finding.Check == "policy.sourceCommentCoverage" && finding.Path == path && finding.Subject == "wgsl"
		}) {
			t.Fatalf("missing WGSL comment analysis passed: %+v", findings)
		}
	}
}

func TestWGSLRequiresModuleOwnership(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	path := "shaders/main.wgsl"
	writeQualityFile(t, repo.Root, path, "@compute @workgroup_size(1) fn main() {}\n")
	findings := CoverageFindings(repo, []string{path})
	if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Check == "policy.moduleCoverage" && finding.Path == path && finding.Subject == "unowned"
	}) {
		t.Fatalf("unowned shader passed coverage: %+v", findings)
	}
}
