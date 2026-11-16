package quality

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestProviderGeneratedSemanticsAreAdvisoryWithoutWeakeningSyntaxOrAuthoredCode(t *testing.T) {
	repo := qualityRepository(t)
	repo.Config.Scope.Generated = []string{"generated/**"}
	paths := []string{"generated/vendor.js", "src/authored.js", "generated/broken.js"}
	for _, path := range paths {
		writeQualityFile(t, repo.Root, path, "export const value = 1;\n")
	}
	command := generatedLintPackCommand(t)
	findings := packQualityFindings(t.Context(), repo, repository.Selection{Files: paths}, command, generatedLintPackRunner{}, "check")
	severities := map[string]policy.FindingSeverity{}
	for _, finding := range findings {
		severities[finding.Path] = policy.NormalizeFinding(finding).Severity
		if (finding.Fields["analysisOutcome"] == "advisory-candidate") != (finding.Path == paths[0]) {
			t.Fatalf("provider lost the advisory classification: %+v", finding)
		}
	}
	if len(findings) != 3 || severities[paths[0]] != policy.FindingWarning || severities[paths[1]] != policy.FindingError || severities[paths[2]] != policy.FindingError {
		t.Fatalf("provider findings lost core ownership policy: %+v", findings)
	}
}

func generatedLintPackCommand(t *testing.T) policy.Command {
	t.Helper()
	source, store := t.TempDir(), t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(store, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				return os.Chmod(path, 0o755)
			}
			return err
		})
	})
	manifest := pack.Manifest{
		Schema: "../../schema/code-polishy-pack.schema.json", ManifestVersion: pack.ManifestVersion,
		Name: "semantic-fixture", Version: "1.0.0", ProtocolVersion: pack.ProtocolVersion, Platforms: []string{pack.CurrentPlatform()},
		Languages: []pack.Language{{ID: "typescript", SourcePatterns: []string{"**/*.js"}}},
		Commands:  []pack.Command{{Name: "lint", Argv: []string{"adapter"}, Capabilities: []string{"lint"}, Profiles: []string{"check"}, TimeoutSeconds: 30}},
		Fixtures: []pack.Fixture{
			{Name: "pass", Command: "lint", Capability: "lint", Project: "fixtures/pass", Files: []string{"main.js"}, ExpectedStatus: "pass"},
			{Name: "fail", Command: "lint", Capability: "lint", Project: "fixtures/fail", Files: []string{"main.js"}, ExpectedStatus: "findings", ExpectedRules: []string{"valid-typeof"}},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{pack.ManifestFilename: string(data), "README.md": "# Semantic lint fixture\n", "adapter": "adapter\n", "fixtures/pass/main.js": "export const value = 1;\n", "fixtures/fail/main.js": "typeof value === 'unknown';\n"} {
		writeQualityFile(t, source, path, content)
	}
	if err := os.Chmod(filepath.Join(source, "adapter"), 0o755); err != nil {
		t.Fatal(err)
	}
	identity, _, err := pack.Install(source, store)
	if err != nil {
		t.Fatal(err)
	}
	resolution := pack.Resolve([]policy.PackSelection{{Name: identity.Name, Version: identity.Version, Digest: identity.Digest}}, store)
	if len(resolution.Findings) != 0 || len(resolution.Commands) != 1 {
		t.Fatalf("pack resolution: %+v", resolution)
	}
	return resolution.Commands[0]
}

type generatedLintPackRunner struct{}

func (generatedLintPackRunner) Execute(_ context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	request := pack.Request{}
	if err := json.Unmarshal(invocation.Command.Stdin, &request); err != nil {
		return runner.Result{}, runner.Output{}, err
	}
	response := map[string]any{
		"protocolVersion": pack.ProtocolVersion, "status": "findings", "inputs": request.Context,
		"coverage": map[string]any{"analyzed": request.Files, "unsupported": []any{}},
		"facts":    map[string]any{"comments": []any{}},
		"findings": []pack.ResponseFinding{
			{Capability: "lint", Rule: "valid-typeof", Path: "generated/vendor.js", Subject: "legacy check", Message: "Invalid typeof value."},
			{Capability: "lint", Rule: "valid-typeof", Path: "src/authored.js", Subject: "authored check", Message: "Invalid typeof value."},
			{Capability: "lint", Rule: "parse-error", Path: "generated/broken.js", Subject: "syntax", Message: "Invalid syntax."},
		},
	}
	data, err := json.Marshal(response)
	return runner.Result{}, runner.Output{Stdout: data}, err
}

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
