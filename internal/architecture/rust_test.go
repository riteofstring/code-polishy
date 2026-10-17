package architecture

import (
	"context"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRustArchitectureResolvesUseTreesAliasesAndReExports(t *testing.T) {
	t.Parallel()
	repo := rustArchitectureRepository(t)
	writeArchitectureFile(t, repo.Root, "src/lib.rs", "pub mod api;\npub mod domain;\npub use domain::Value;\n")
	writeArchitectureFile(t, repo.Root, "src/api.rs", "use crate::domain::{self as model, Value as Alias};\npub fn value() -> Alias { model::make() }\n")
	writeArchitectureFile(t, repo.Root, "src/domain.rs", "pub struct Value;\npub fn make() -> Value { Value }\n")
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/api.rs"}, runner.OSRunner{})
	if analysis.Graph == nil || !slices.ContainsFunc(analysis.Findings, func(finding policy.Finding) bool {
		return finding.Check == "architecture.moduleDependency" && finding.Path == "src/api.rs" && finding.Subject == "domain" && finding.Line == 1
	}) {
		t.Fatalf("forbidden aliased dependency was lost: %+v", analysis)
	}
	repo.Config.Modules[1].DependsOn = []string{"domain"}
	analysis = AnalyzeWithRunner(t.Context(), repo, []string{"src/api.rs"}, runner.OSRunner{})
	if analysis.Graph == nil || len(analysis.Findings) != 0 || len(analysis.Graph.Nodes) != 3 {
		t.Fatalf("valid declared Rust graph failed: %+v", analysis)
	}
	if !slices.ContainsFunc(analysis.Graph.Edges, func(edge sourcegraph.Edge) bool {
		return edge.Source == "src/lib.rs" && edge.Target == "src/domain.rs" && edge.Kind == sourcegraph.EdgeReExport
	}) {
		t.Fatalf("re-export lost its original file edge: %+v", analysis.Graph.Edges)
	}
}

func TestRustArchitectureReportsSourceCyclesWithoutDeclarationCycles(t *testing.T) {
	t.Parallel()
	repo := rustArchitectureRepository(t)
	repo.Config.Modules[1].DependsOn = []string{"domain"}
	repo.Config.Modules[2].DependsOn = []string{"api"}
	writeArchitectureFile(t, repo.Root, "src/lib.rs", "pub mod api;\npub mod domain;\n")
	writeArchitectureFile(t, repo.Root, "src/api.rs", "pub fn first() { crate::domain::second(); }\n")
	writeArchitectureFile(t, repo.Root, "src/domain.rs", "pub fn second() { crate::api::first(); }\n")
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/api.rs"}, runner.OSRunner{})
	finding := onlyCycleFinding(t, analysis.Findings, "architecture.fileCycle")
	if analysis.Graph == nil || len(finding.DependencyComponent.Members) != 2 || slices.ContainsFunc(finding.DependencyComponent.Members, func(node policy.DependencyNode) bool { return node.Path == "src/lib.rs" }) {
		t.Fatalf("module declaration became a fabricated import cycle: %+v", analysis)
	}
}

func TestRustArchitectureKeepsLiteralPathsInlineModulesAndUnrelatedCrates(t *testing.T) {
	t.Parallel()
	repo := rustArchitectureRepository(t)
	writeArchitectureFile(t, repo.Root, "src/lib.rs", "#[path = \"domain.rs\"]\npub mod renamed;\nmod inline { pub use crate::renamed::Value as Alias; }\npub fn value() -> inline::Alias { renamed::Value }\n")
	writeArchitectureFile(t, repo.Root, "src/domain.rs", "pub struct Value;\n")
	writeArchitectureFile(t, repo.Root, "other/Cargo.toml", "[package]\nname='other'\nversion='1.0.0'\n[workspace]\n")
	writeArchitectureFile(t, repo.Root, "other/src/lib.rs", "invalid Rust in an unrelated crate\n")
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{})
	if analysis.Graph == nil || len(analysis.Findings) != 0 || len(analysis.Graph.Nodes) != 2 {
		t.Fatalf("literal path/inline alias or focused scope failed: %+v", analysis)
	}
}

func TestRustArchitectureCannotPassOpaqueOrMissingModuleEvidence(t *testing.T) {
	t.Parallel()
	for name, source := range map[string]string{
		"macro":                 "unknown_macro!();\n",
		"attribute macro":       "#[generate_imports]\npub fn entry() {}\n",
		"conditional macro":     "#[cfg_attr(feature = \"extra\", generate_imports)]\npub fn entry() {}\n",
		"derive macro":          "#[derive(External)]\npub struct Value;\n",
		"absolute path":         "pub fn call() { ::hidden::call(); }\n",
		"missing":               "mod absent;\n",
		"escape":                "#[path = \"../../outside.rs\"]\nmod outside;\n",
		"conditional path":      "#[cfg_attr(feature = \"alternate\", path = \"other.rs\")]\nmod other;\n",
		"unresolved local path": "pub fn call() { crate::missing::call(); }\n",
	} {
		t.Run(name, func(t *testing.T) {
			repo := rustArchitectureRepository(t)
			writeArchitectureFile(t, repo.Root, "src/lib.rs", source)
			analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{})
			if analysis.Graph != nil || !slices.ContainsFunc(analysis.Findings, func(finding policy.Finding) bool {
				return finding.Check == "architecture.importCoverage" && finding.Path == "src/lib.rs"
			}) {
				t.Fatalf("missing Rust graph evidence passed: %+v", analysis)
			}
		})
	}
}

func TestRustArchitectureResolvesRenamedWorkspaceDependencyWithoutUnrelatedFailures(t *testing.T) {
	t.Parallel()
	repo := rustArchitectureRepository(t)
	repo.Config.Modules = []policy.Module{
		{Name: "client", Paths: []string{"client/**"}},
		{Name: "model", Paths: []string{"model/**"}},
	}
	repo.Config.ModuleByName = map[string]int{"client": 0, "model": 1}
	writeArchitectureFile(t, repo.Root, "Cargo.toml", "[workspace]\nmembers=['client','model','unused']\nresolver='3'\n")
	writeArchitectureFile(t, repo.Root, "client/Cargo.toml", "[package]\nname='client'\nversion='1.0.0'\nedition='2024'\n[dependencies]\nmodel_alias={package='shared-model',path='../model',version='=1.0.0'}\n")
	writeArchitectureFile(t, repo.Root, "client/src/lib.rs", "pub use model_alias::Value;\n")
	writeArchitectureFile(t, repo.Root, "model/Cargo.toml", "[package]\nname='shared-model'\nversion='1.0.0'\nedition='2024'\n")
	writeArchitectureFile(t, repo.Root, "model/src/lib.rs", "pub struct Value;\n")
	writeArchitectureFile(t, repo.Root, "unused/Cargo.toml", "[package]\nname='unused'\nversion='1.0.0'\nedition='2024'\n")
	writeArchitectureFile(t, repo.Root, "unused/src/lib.rs", "invalid unrelated source\n")
	selected := []string{"client/src/lib.rs"}
	analysis := AnalyzeWithRunner(t.Context(), repo, selected, runner.OSRunner{})
	if analysis.Graph == nil || len(analysis.Findings) != 1 || analysis.Findings[0].Subject != "model" {
		t.Fatalf("renamed local crate lost its dependency policy: %+v", analysis)
	}
	repo.Config.Modules[0].DependsOn = []string{"model"}
	analysis = AnalyzeWithRunner(t.Context(), repo, selected, runner.OSRunner{})
	if analysis.Graph == nil || len(analysis.Findings) != 0 || len(analysis.Graph.Nodes) != 2 || len(analysis.Graph.Inputs) != 2 {
		t.Fatalf("connected workspace graph included an unrelated member or lost evidence: %+v", analysis)
	}
}

func TestRustArchitectureCommandPlanMatchesRealExecution(t *testing.T) {
	t.Parallel()
	repo := rustArchitectureRepository(t)
	writeArchitectureFile(t, repo.Root, "src/lib.rs", "#[derive(Default, Debug)]\npub struct Value;\n")
	boundary := &rustGraphRecordingRunner{}
	selected := []string{"src/lib.rs"}
	plan := RustGraphCommands(repo, selected)
	analysis := AnalyzeWithRunner(t.Context(), repo, selected, boundary)
	if analysis.Graph == nil || len(analysis.Findings) != 0 {
		t.Fatalf("real Rust graph failed: %+v", analysis)
	}
	expected := []string{}
	for _, command := range plan {
		expected = append(expected, command.Name)
	}
	if !slices.Equal(expected, boundary.names) || len(expected) < 4 {
		t.Fatalf("execution %v differs from plan %v", boundary.names, expected)
	}
}

func TestRustArchitectureTracksStandardMacroArgumentsAtOriginalLocations(t *testing.T) {
	t.Parallel()
	repo := rustArchitectureRepository(t)
	writeArchitectureFile(t, repo.Root, "src/lib.rs", "pub mod api;\npub mod domain;\n")
	writeArchitectureFile(t, repo.Root, "src/domain.rs", "pub fn value() -> u8 { 1 }\npub enum Value { Present }\n")
	writeArchitectureFile(t, repo.Root, "src/api.rs", `pub fn check() {
    let crab = "🦀";
    assert_eq!(crate::domain::value(), 1, "{crab}");
    let values = vec![dbg!(crate::domain::value()); 2];
    assert!(matches!(crate::domain::Value::Present, crate::domain::Value::Present if crate::domain::value() > 0));
    println!("{}", format!("{}", crate::domain::value()));
    let _ = stringify!(crate::absent::not_a_dependency);
    let _ = Vec::<String>::new();
    let _ = Option::<u8>::None;
    assert!(!values.is_empty());
}
`)
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/api.rs"}, runner.OSRunner{})
	if analysis.Graph == nil {
		t.Fatalf("standard macro or prelude syntax blocked Rust graph: %+v", analysis.Findings)
	}
	for _, line := range []int{3, 4, 5, 6} {
		if !slices.ContainsFunc(analysis.Findings, func(finding policy.Finding) bool {
			return finding.Check == "architecture.moduleDependency" && finding.Path == "src/api.rs" && finding.Subject == "domain" && finding.Line == line && finding.Column > 1
		}) {
			t.Errorf("macro argument on original line %d lost dependency evidence: %+v", line, analysis.Findings)
		}
	}
	repo.Config.Modules[1].DependsOn = []string{"domain"}
	analysis = AnalyzeWithRunner(t.Context(), repo, []string{"src/api.rs"}, runner.OSRunner{})
	if analysis.Graph == nil || len(analysis.Findings) != 0 {
		t.Fatalf("declared macro argument dependencies failed: %+v", analysis)
	}
}

func TestRustArchitectureRespectsPreludeAndMacroShadowing(t *testing.T) {
	t.Parallel()
	for name, source := range map[string]string{
		"local macro":         "macro_rules! assert_eq { () => { use crate::missing; }; }\npub fn f() { assert_eq!(); }\n",
		"external macro":      "use external::assert_eq;\npub fn f() { assert_eq!(1, 1); }\n",
		"macro use":           "#[macro_use]\nextern crate external;\npub fn f() { assert_eq!(1, 1); }\n",
		"qualified shadow":    "mod std {}\npub fn f() { std::assert_eq!(1, 1); }\n",
		"nested macro":        "pub fn f() { assert_eq!(unknown!(), 1); }\n",
		"no std type":         "#![no_std]\npub fn f() { let _ = Vec::<u8>::new(); }\n",
		"no std macro":        "#![no_std]\npub fn f() { let _ = vec![1]; }\n",
		"no implicit type":    "#![no_implicit_prelude]\npub fn f() { let _ = Option::<u8>::None; }\n",
		"no implicit macro":   "#![no_implicit_prelude]\npub fn f() { assert_eq!(1, 1); }\n",
		"conditional prelude": "#![cfg_attr(feature = \"bare\", no_std)]\npub fn f() {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			repo := rustArchitectureRepository(t)
			writeArchitectureFile(t, repo.Root, "src/lib.rs", source)
			analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{})
			if analysis.Graph != nil || !slices.ContainsFunc(analysis.Findings, func(finding policy.Finding) bool { return finding.Check == "architecture.importCoverage" }) {
				t.Fatalf("unresolved Rust prelude/expansion passed: %+v", analysis)
			}
		})
	}
}

func TestRustArchitectureSupportsCorePreludeAndExplicitStandardMacroImports(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"#![no_std]\npub fn f() { let _ = Option::<u8>::None; assert_eq!(1, 1); }\n",
		"use std::assert_eq;\npub fn f() { assert_eq!(1, 1); }\n",
		"use std::fmt::Debug;\n#[derive(Debug)]\npub struct Value;\n",
		"pub fn f() { std::assert_eq!(1, 1); core::assert!(true); }\n",
		"mod tests { use super::*; #[test] fn f() { assert_eq!(value(), 1); } }\npub fn value() -> u8 { 1 }\n",
	} {
		t.Run(strings.Split(source, "\n")[0], func(t *testing.T) {
			repo := rustArchitectureRepository(t)
			writeArchitectureFile(t, repo.Root, "src/lib.rs", source)
			analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{})
			if analysis.Graph == nil || len(analysis.Findings) != 0 {
				t.Fatalf("valid standard/core prelude failed: %+v", analysis)
			}
		})
	}
}

func TestRustArchitectureKeepsFunctionBlockAndGenericBindingsLexical(t *testing.T) {
	t.Parallel()
	repo := rustArchitectureRepository(t)
	writeArchitectureFile(t, repo.Root, "src/lib.rs", "pub mod api;\npub mod domain;\n")
	writeArchitectureFile(t, repo.Root, "src/domain.rs", "pub struct Value;\nimpl Value { pub fn value() -> u8 { 1 } }\n")
	writeArchitectureFile(t, repo.Root, "src/api.rs", `pub fn first() {
    use crate::domain::Value as Alias;
    assert_eq!({ use crate::domain::Value as Local; Local::value() }, 1);
    let _ = Alias::value();
}
pub fn second() {
    use std::string::String as Alias;
    let _ = Alias::new();
    { use crate::domain::Value as Alias; let _ = Alias::value(); }
    let _ = Alias::new();
}
pub fn generic<T: Default>() -> T { T::default() }
`)
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/api.rs"}, runner.OSRunner{})
	if analysis.Graph == nil {
		t.Fatalf("block-local bindings leaked into other scopes: %+v", analysis.Findings)
	}
	for _, finding := range analysis.Findings {
		if finding.Check != "architecture.moduleDependency" || finding.Path != "src/api.rs" || !slices.Contains([]int{2, 3, 4, 9}, finding.Line) {
			t.Errorf("lexical binding produced an incorrect dependency: %+v", finding)
		}
	}
	repo.Config.Modules[1].DependsOn = []string{"domain"}
	analysis = AnalyzeWithRunner(t.Context(), repo, []string{"src/api.rs"}, runner.OSRunner{})
	if analysis.Graph == nil || len(analysis.Findings) != 0 {
		t.Fatalf("declared lexical dependencies failed: %+v", analysis)
	}
}

func TestRustArchitectureCannotResolveModuleMembersThroughThePrelude(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"pub fn f() { let _ = crate::String::new(); }\n",
		"mod empty {}\npub fn f() { let _ = empty::Option::<u8>::None; }\n",
		"pub fn first<T: Default>() -> T { T::default() }\npub fn second() { let _ = T::default(); }\n",
	} {
		t.Run(source, func(t *testing.T) {
			repo := rustArchitectureRepository(t)
			writeArchitectureFile(t, repo.Root, "src/lib.rs", source)
			analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{})
			if analysis.Graph != nil || len(analysis.Findings) == 0 {
				t.Fatalf("unavailable member acquired an implicit binding: %+v", analysis)
			}
		})
	}
}

func rustArchitectureRepository(t *testing.T) repository.Repository {
	t.Helper()
	root := t.TempDir()
	policyRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.Repository{Root: root, PolicyRoot: policyRoot, Config: policy.Config{
		Modules: []policy.Module{
			{Name: "crate", Paths: []string{"src/lib.rs"}, DependsOn: []string{"api", "domain"}},
			{Name: "api", Paths: []string{"src/api.rs"}},
			{Name: "domain", Paths: []string{"src/domain.rs"}},
		}, ModuleByName: map[string]int{"crate": 0, "api": 1, "domain": 2},
	}}
	writeArchitectureFile(t, root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\nedition='2024'\n[workspace]\n")
	writeArchitectureFile(t, root, "src/lib.rs", "pub fn entry() {}\n")
	return repo
}

type rustGraphRecordingRunner struct {
	runner.OSRunner
	names []string
}

func (boundary *rustGraphRecordingRunner) RunStructured(ctx context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	boundary.names = append(boundary.names, command.Name)
	return boundary.OSRunner.RunStructured(ctx, root, command)
}

func (boundary *rustGraphRecordingRunner) RunWithWriters(ctx context.Context, root string, command policy.Command, stdout, stderr io.Writer) (runner.Result, error) {
	boundary.names = append(boundary.names, command.Name)
	return boundary.OSRunner.RunWithWriters(ctx, root, command, stdout, stderr)
}
