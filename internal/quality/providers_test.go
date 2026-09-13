package quality

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestProviderCommentsRemainCorePolicyDecisions(t *testing.T) {
	forbidden := false
	repo := repository.Repository{Config: policy.Config{Quality: policy.EffectiveQuality(policy.Quality{AllowComments: &forbidden, Complexity: policy.Complexity{TypeScript: 3}})}}
	comments := []pack.CommentFact{{Path: "main.ts", Kind: "Line", Raw: "// prose", Complete: true, Line: 1, Column: 1, BeforeCode: true, Preamble: true, ByteZero: true}}
	if findings := packCommentFindings(repo, comments); len(findings) != 1 || findings[0].Check != "policy.sourceComment" {
		t.Fatalf("prose was accepted: %+v", findings)
	}
	comments[0].Kind, comments[0].Raw = "Shebang", "#!/usr/bin/env node"
	if findings := packCommentFindings(repo, comments); len(findings) != 0 {
		t.Fatalf("supported machine directive failed: %+v", findings)
	}
}

func TestProviderCoverageDoesNotCreditUnexaminedModuleFiles(t *testing.T) {
	command := policy.Command{Name: "pack.custom.lint", Provides: []string{"lint"}, Paths: []string{"src/covered.custom"}, Modules: []string{"app"}, RunOn: []string{"check", "gate"}, Adapter: &policy.PackAdapter{PackName: "custom", Capability: "lint"}}
	repo := repository.Repository{Config: policy.Config{Modules: []policy.Module{{Name: "app", Paths: []string{"src/**"}}}, ModuleByName: map[string]int{"app": 0}, Scope: policy.Scope{Languages: []policy.LanguageRule{{Name: "custom", Paths: []string{"**/*.custom"}}}}, Checks: []policy.Command{command}}}
	findings := analysisCoverageFindings(repo, []string{"src/covered.custom", "src/uncovered.custom"})
	if slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Path == "src/covered.custom" && finding.Subject == "lint:check"
	}) {
		t.Fatal("owned lint work was rejected")
	}
	if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Path == "src/uncovered.custom" && finding.Subject == "lint:check"
	}) {
		t.Fatalf("one-file claim credited the whole module: %+v", findings)
	}
}

func TestSelectedProvidersRemoveNativeToolPrerequisitesAndCommands(t *testing.T) {
	repo := qualityRepository(t)
	repo.PolicyRoot = t.TempDir()
	paths := []string{"src/value.go", "scripts/run.sh"}
	for _, path := range paths {
		writeQualityFile(t, repo.Root, path, "source\n")
	}
	for _, capability := range builtInCapabilities {
		repo.Config.Checks = append(repo.Config.Checks, policy.Command{Name: "pack.custom." + capability, Paths: paths, Provides: []string{capability}, RunOn: []string{"check", "gate", "format"}, Adapter: &policy.PackAdapter{PackName: "custom", Capability: capability}})
	}
	if findings := ToolFindings(repo, paths); len(findings) != 0 {
		t.Fatalf("selected providers still required native tools: %+v", findings)
	}
	if commands, findings := shellToolCommands(repo, paths); len(commands) != 0 || len(findings) != 0 {
		t.Fatalf("native shell work remained: %+v %+v", commands, findings)
	}
	if commands, findings := nativeGoToolCommands(repo, paths, paths); len(commands) != 0 || len(findings) != 0 {
		t.Fatalf("native Go work remained: %+v %+v", commands, findings)
	}
	repo.Config.Checks = nil
	if findings := ToolFindings(repo, paths); len(findings) == 0 {
		t.Fatal("native defaults lost their required tool identities")
	}
}
