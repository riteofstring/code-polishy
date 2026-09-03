package quality

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestSourceChecksFindTextHygiene(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	writeQualityFile(t, repo.Root, "internal/sample/file.go", "package sample  ")
	findings := sourceChecks(repo, []string{"internal/sample/file.go"})
	checks := findingChecks(findings)
	if !checks["quality.trailingWhitespace"] || !checks["quality.finalNewline"] {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestSourceChecksUseSeparateTestLengthBudget(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Quality.MaxFileLines = 2
	repo.Config.Quality.MaxTestFileLines = 4
	writeQualityFile(t, repo.Root, "internal/sample/file_test.go", "package sample\n\n\n")
	if findings := sourceChecks(repo, []string{"internal/sample/file_test.go"}); len(findings) != 0 {
		t.Fatalf("test file should use test budget: %+v", findings)
	}
	writeQualityFile(t, repo.Root, "internal/sample/file.go", "package sample\n\n\n")
	findings := sourceChecks(repo, []string{"internal/sample/file.go"})
	if !findingChecks(findings)["quality.fileLength"] {
		t.Fatalf("production file should exceed budget: %+v", findings)
	}
}

func TestFileLengthCountsFinalLineWithoutNewline(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Quality.MaxFileLines = 2
	writeQualityFile(t, repo.Root, "internal/sample/file.go", "package sample\nvar value = 1\nvar other = 2")
	findings := sourceChecks(repo, []string{"internal/sample/file.go"})
	if !findingChecks(findings)["quality.fileLength"] || !findingChecks(findings)["quality.finalNewline"] {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestSourceChecksExcludeMarkdownFromFileLength(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Quality.MaxFileLines = 2
	for _, path := range []string{"docs/plan.md", "docs/notes.markdown", "docs/README.MD"} {
		writeQualityFile(t, repo.Root, path, "first  \nsecond\nthird")
		findings := sourceChecks(repo, []string{path})
		checks := findingChecks(findings)
		if checks["quality.fileLength"] {
			t.Fatalf("%s received a file-length finding: %+v", path, findings)
		}
		if !checks["quality.trailingWhitespace"] || !checks["quality.finalNewline"] {
			t.Fatalf("%s did not retain text-hygiene checks: %+v", path, findings)
		}
	}
}

func TestGoComplexityUsesProductionThreshold(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Quality.Complexity.Go = 3
	writeQualityFile(t, repo.Root, "internal/sample/branch.go", "package sample\nfunc choose(a, b bool) bool {\n if a { return true }\n if b { return true }\n return false\n}\n")
	findings := goComplexityFindings(repo, "internal/sample/branch.go")
	if len(findings) != 1 || findings[0].Subject != "choose" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCoverageRequiresAdaptersForUnknownLanguages(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "sample", Paths: []string{"internal/sample/**"}}}
	repo.Config.ModuleByName = map[string]int{"sample": 0}
	writeQualityFile(t, repo.Root, "internal/sample/lib.rs", "fn main() {}\n")
	findings := CoverageFindings(repo, []string{"internal/sample/lib.rs"})
	if len(withoutSourceCommentCoverage(findings)) != 6 || !findingChecks(findings)["policy.sourceCommentCoverage"] {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGeneratedSourceStillRequiresModuleAndToolCoverage(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Generated = []string{"generated/**"}
	repo.Config.Modules = []policy.Module{{Name: "generated", Paths: []string{"generated/**"}}}
	repo.Config.ModuleByName = map[string]int{"generated": 0}
	writeQualityFile(t, repo.Root, "generated/client.rs", "fn generated() {}\n")
	findings := CoverageFindings(repo, []string{"generated/client.rs"})
	if len(findings) != 6 {
		t.Fatalf("generated executable source cannot bypass adapter coverage: %+v", findings)
	}
}

func TestCoverageUsesModuleScopedProvider(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "sample", Paths: []string{"internal/sample/**"}}}
	repo.Config.ModuleByName = map[string]int{"sample": 0}
	repo.Config.Checks = []policy.Command{{Name: "rust", Provides: []string{"format", "lint", "typecheck", "complexity", "dead-code", "architecture"}, Modules: []string{"sample"}, RunOn: []string{"check", "gate"}}}
	writeQualityFile(t, repo.Root, "internal/sample/lib.rs", "fn main() {}\n")
	if findings := CoverageFindings(repo, []string{"internal/sample/lib.rs"}); len(withoutSourceCommentCoverage(findings)) != 0 || !findingChecks(findings)["policy.sourceCommentCoverage"] {
		t.Fatalf("provider should cover module: %+v", findings)
	}
}

func TestCoverageRequiresProvidersInLocalAndGateProfiles(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "sample", Paths: []string{"internal/sample/**"}}}
	repo.Config.ModuleByName = map[string]int{"sample": 0}
	repo.Config.Checks = []policy.Command{{Name: "rust", Provides: []string{"format", "lint", "typecheck", "complexity", "dead-code", "architecture"}, Modules: []string{"sample"}, RunOn: []string{"check"}}}
	writeQualityFile(t, repo.Root, "internal/sample/lib.rs", "fn main() {}\n")
	findings := CoverageFindings(repo, []string{"internal/sample/lib.rs"})
	findings = withoutSourceCommentCoverage(findings)
	if len(findings) != 6 || !strings.Contains(findings[0].Message, "gate") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCoverageRefusesTargetProviderForBuiltInCapability(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "web", Paths: []string{"frontend/**"}}}
	repo.Config.ModuleByName = map[string]int{"web": 0}
	repo.Config.Checks = []policy.Command{{
		Name: "frontend-lint", Provides: []string{"lint"}, Argv: []string{"pnpm", "lint"},
		Paths: []string{"frontend/**"}, RunOn: []string{"check", "gate"},
	}}
	writeQualityFile(t, repo.Root, "frontend/app.ts", "export const value = 1;\n")
	writeQualityFile(t, repo.Root, "frontend/README.md", "# frontend\n")
	findings := CoverageFindings(repo, []string{"frontend/app.ts", "frontend/README.md"})
	if len(findings) != 1 || findings[0].Check != "policy.builtInCapability" || findings[0].Subject != "frontend-lint:lint" {
		t.Fatalf("findings = %+v", findings)
	}
	if !strings.Contains(findings[0].Message, "frontend/app.ts") {
		t.Fatalf("finding should name the source it covers: %+v", findings[0])
	}
}

func TestCoverageRefusesRepositoryWideProviderForBuiltInCapability(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "sample", Paths: []string{"internal/sample/**"}}}
	repo.Config.ModuleByName = map[string]int{"sample": 0}
	repo.Config.Checks = []policy.Command{{
		Name: "repository-format", Provides: []string{"format"}, Argv: []string{"gofumpt", "-l"}, RunOn: []string{"check"},
	}}
	writeQualityFile(t, repo.Root, "internal/sample/file.go", "package sample\n")
	findings := CoverageFindings(repo, []string{"internal/sample/file.go"})
	if len(findings) != 1 || findings[0].Subject != "repository-format:format" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCoverageAdmitsProviderReachingSourceNoBuiltInDecides(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "sample", Paths: []string{"internal/sample/**"}}}
	repo.Config.ModuleByName = map[string]int{"sample": 0}
	repo.Config.Checks = []policy.Command{{
		Name: "rust", Provides: []string{"format", "lint", "typecheck", "complexity", "dead-code", "architecture"},
		Modules: []string{"sample"}, RunOn: []string{"check", "gate"},
	}}
	writeQualityFile(t, repo.Root, "internal/sample/lib.rs", "fn main() {}\n")
	writeQualityFile(t, repo.Root, "internal/sample/build.ts", "export const build = 1;\n")
	if findings := CoverageFindings(repo, []string{"internal/sample/lib.rs", "internal/sample/build.ts"}); len(withoutSourceCommentCoverage(findings)) != 0 || !findingChecks(findings)["policy.sourceCommentCoverage"] {
		t.Fatalf("a provider covering source no built-in decides is the target's own: %+v", findings)
	}
}

func TestCoverageRefusesTargetFormatterOnlyWhereTheSealedBundleFormats(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "sample", Paths: []string{"internal/sample/**"}}}
	repo.Config.ModuleByName = map[string]int{"sample": 0}
	repo.Config.Checks = []policy.Command{{
		Name: "docs-format", Provides: []string{"format"}, Argv: []string{"prettier", "--check"},
		Paths: []string{"docs/**"}, RunOn: []string{"check"},
	}}
	writeQualityFile(t, repo.Root, "docs/guide.md", "# guide\n")
	writeQualityFile(t, repo.Root, "internal/sample/file.go", "package sample\n")
	goOnly := []string{"docs/guide.md", "internal/sample/file.go"}
	if findings := CoverageFindings(repo, goOnly); len(findings) != 0 {
		t.Fatalf("a target without JavaScript formats its own remaining file types: %+v", findings)
	}
	writeQualityFile(t, repo.Root, "internal/sample/app.ts", "export const value = 1;\n")
	findings := CoverageFindings(repo, append(goOnly, "internal/sample/app.ts"))
	if len(findings) != 1 || findings[0].Subject != "docs-format:format" {
		t.Fatalf("the sealed bundle formats Markdown once the target bears TypeScript: %+v", findings)
	}
}

func TestCoverageAdmitsTargetProviderForSourceTheSealedBundleNeverReads(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "web", Paths: []string{"frontend/**"}}}
	repo.Config.ModuleByName = map[string]int{"web": 0}
	repo.Config.Checks = []policy.Command{{
		Name: "vue-quality", Provides: []string{"format", "lint", "typecheck"}, Argv: []string{"pnpm", "vue-tsc"},
		Paths: []string{"frontend/**/*.vue"}, RunOn: []string{"check", "gate"},
	}}
	writeQualityFile(t, repo.Root, "frontend/App.vue", "<template />\n")
	writeQualityFile(t, repo.Root, "frontend/main.ts", "export const value = 1;\n")
	component := []string{"frontend/App.vue", "frontend/main.ts"}
	if findings := CoverageFindings(repo, component); len(withoutSourceCommentCoverage(findings)) != 0 || !findingChecks(findings)["policy.sourceCommentCoverage"] {
		t.Fatalf("a check covering only single-file components is the target's own: %+v", findings)
	}
	repo.Config.Checks[0].Paths = []string{"frontend/**"}
	findings := CoverageFindings(repo, component)
	findings = withoutSourceCommentCoverage(findings)
	if len(findings) != 3 {
		t.Fatalf("a check reaching the source the bundle decides is refused: %+v", findings)
	}
	for _, found := range findings {
		if found.Check != "policy.builtInCapability" || !strings.Contains(found.Message, "frontend/main.ts") {
			t.Fatalf("finding should name the source the bundle decides: %+v", found)
		}
	}
}

func TestCoverageAdmitsManagedBuiltInCommands(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "sample", Paths: []string{"internal/sample/**"}}}
	repo.Config.ModuleByName = map[string]int{"sample": 0}
	repo.Config.Checks = []policy.Command{{
		Name: "policy-managed-lint", Provides: []string{"lint"}, Modules: []string{"sample"},
		RunOn: []string{"check", "gate"}, Managed: true,
	}}
	writeQualityFile(t, repo.Root, "internal/sample/app.ts", "export const value = 1;\n")
	if findings := CoverageFindings(repo, []string{"internal/sample/app.ts"}); len(findings) != 0 {
		t.Fatalf("a managed command is Code Polishy's own module: %+v", findings)
	}
}

func TestCoverageRejectsAmbiguousCustomLanguageRules(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Languages = []policy.LanguageRule{
		{Name: "elixir", Paths: []string{"lib/**/*.ex"}},
		{Name: "beam", Paths: []string{"lib/**"}},
	}
	repo.Config.Modules = []policy.Module{{Name: "sample", Paths: []string{"lib/**"}}}
	repo.Config.ModuleByName = map[string]int{"sample": 0}
	writeQualityFile(t, repo.Root, "lib/sample.ex", "defmodule Sample do\nend\n")
	findings := CoverageFindings(repo, []string{"lib/sample.ex"})
	findings = withoutSourceCommentCoverage(findings)
	if len(findings) != 1 || findings[0].Check != "policy.languageCoverage" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCoverageRejectsUnusedCustomLanguageRule(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Languages = []policy.LanguageRule{{Name: "elixir", Paths: []string{"lib/**/*.ex"}}}
	findings := CoverageFindings(repo, nil)
	if len(findings) != 1 || findings[0].Subject != "elixir" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestRepositoryCommandWithoutTriggersAlwaysApplies(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	command := policy.Command{Name: "repository-check"}
	selection := repository.Selection{Files: []string{"internal/sample/file.go"}}
	if !commandApplies(repo, command, selection) {
		t.Fatal("repository-wide command with no triggers should run in change mode")
	}
}

func TestCommandsForProfilesMatchesDirectCommandSelection(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "sample", Paths: []string{"internal/sample/**"}}}
	repo.Config.Checks = []policy.Command{
		{Name: "gate", Provides: []string{"lint"}, Argv: []string{"true"}, Modules: []string{"sample"}, RunOn: []string{"gate"}},
		{Name: "build", Provides: []string{"build"}, Argv: []string{"true"}, Modules: []string{"sample"}, RunOn: []string{"build"}},
		{Name: "supply", Provides: []string{"lock-sync"}, Argv: []string{"true"}, RunOn: []string{"supply-chain"}},
	}
	selection := repository.Selection{Files: []string{"internal/sample/file.go"}}

	commands := CommandsForProfiles(repo, selection, "gate", "build")
	if len(commands) != 2 || commands[0].Name != "gate" || commands[1].Name != "build" {
		t.Fatalf("commands = %+v", commands)
	}
	if commands := CommandsForProfiles(repo, selection, "supply-chain"); len(commands) != 1 || commands[0].Name != "supply" {
		t.Fatalf("repository command selection = %+v", commands)
	}
}

func TestManagedCommandRetainsGeneratedExecutableFiles(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Generated = []string{"generated/**"}
	command := policy.Command{Argv: []string{"ruff", "check", "--"}, Paths: []string{"**/*.py"}, PassFiles: true}
	selection := repository.Selection{Files: []string{"src/a.py", "src/a.go", "generated/client.py"}}
	prepared, runnable := prepareCommand(repo, command, selection)
	if !runnable || !slices.Equal(prepared.Argv, []string{"ruff", "check", "--", "src/a.py", "generated/client.py"}) {
		t.Fatalf("prepared = %+v, runnable = %v", prepared, runnable)
	}
}

func TestManagedCommandUsesExactOwnedFilesRelativeToItsWorkingDirectory(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	command := policy.Command{
		Argv: []string{"ruff", "check", "--no-fix", "--"}, Cwd: "services/api", PassFiles: true,
		PassFilePaths: []string{"services/api/app/main.py", "services/api/app/main_test.py"},
	}
	selection := repository.Selection{Files: []string{"services/api/app/main.py", "services/other/app/main.py"}}
	prepared, runnable := prepareCommand(repo, command, selection)
	if !runnable || !slices.Equal(prepared.Argv, []string{"ruff", "check", "--no-fix", "--", "app/main.py"}) {
		t.Fatalf("prepared = %+v, runnable = %v", prepared, runnable)
	}

	selection = repository.Selection{Files: []string{"services/api/pyproject.toml"}}
	prepared, runnable = prepareCommand(repo, command, selection)
	want := []string{"ruff", "check", "--no-fix", "--", "app/main.py", "app/main_test.py"}
	if !runnable || !slices.Equal(prepared.Argv, want) {
		t.Fatalf("config-triggered prepared = %+v, runnable = %v", prepared, runnable)
	}
}

func TestSecurityProviderFailuresAreNonSuppressiblePolicyFindings(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Checks = []policy.Command{{
		Name: "security-scan", Provides: []string{"security"}, Argv: []string{"scanner"},
		RunOn: []string{"security"}, TimeoutSeconds: 30,
	}}
	findings := RunCommands(context.Background(), repo, repository.Selection{All: true}, failingQualityRunner{}, "security")
	if len(findings) != 1 || findings[0].Check != "policy.securityProvider" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestShellCheckReadsTheSourcesAScriptDeclares(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot = t.TempDir()
	writeQualityFile(t, repo.PolicyRoot, "tools/shellcheck.sh", "#!/usr/bin/env bash\nexit 0\n")
	if err := os.Chmod(filepath.Join(repo.PolicyRoot, "tools", "shellcheck.sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeQualityFile(t, repo.Root, "scripts/tool.sh", "#!/usr/bin/env bash\n")
	recorder := &recordingQualityRunner{}
	if findings := shellChecks(context.Background(), repo, []string{"scripts/tool.sh"}, recorder); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	root, command, found := recorder.command("shellcheck")
	if !found {
		t.Fatal("the shell checks did not run ShellCheck")
	}
	if !slices.Contains(command.Argv, "--external-sources") {
		t.Errorf("argv = %v", command.Argv)
	}
	if root != repo.Root {
		t.Errorf("root = %q, want %q", root, repo.Root)
	}
}

type recordingQualityRunner struct {
	roots    []string
	commands []policy.Command
}

func (recorder *recordingQualityRunner) Run(_ context.Context, root string, specification policy.Command) error {
	recorder.roots = append(recorder.roots, root)
	recorder.commands = append(recorder.commands, specification)
	return nil
}

func (recorder *recordingQualityRunner) command(name string) (string, policy.Command, bool) {
	for index, specification := range recorder.commands {
		if specification.Name == name {
			return recorder.roots[index], specification, true
		}
	}
	return "", policy.Command{}, false
}

type failingQualityRunner struct{}

func (failingQualityRunner) Run(context.Context, string, policy.Command) error {
	return errors.New("scanner failed")
}

func qualityRepository(t *testing.T) repository.Repository {
	t.Helper()
	root := t.TempDir()
	allowComments := false
	config := policy.Config{
		Project: policy.Project{Kind: "content"},
		Quality: policy.Quality{MaxFileLines: policy.MaxFileLines, MaxTestFileLines: policy.MaxTestFileLines, Complexity: policy.Complexity{Go: policy.MaxGoComplexity, GoTest: policy.MaxGoTestComplexity}, AllowComments: &allowComments},
	}
	return repository.Repository{Root: root, Config: config}
}

func writeQualityFile(t *testing.T, root, path, contents string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func findingChecks(findings []policy.Finding) map[string]bool {
	checks := map[string]bool{}
	for _, finding := range findings {
		checks[finding.Check] = true
	}
	return checks
}

func withoutSourceCommentCoverage(findings []policy.Finding) []policy.Finding {
	result := []policy.Finding{}
	for _, finding := range findings {
		if finding.Check != "policy.sourceCommentCoverage" {
			result = append(result, finding)
		}
	}
	return result
}
