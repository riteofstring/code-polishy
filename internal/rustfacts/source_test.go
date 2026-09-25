package rustfacts

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRustSourceFactsUseSyntaxForCommentsClosuresAndTests(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	write(t, repo.Root, "src/lib.rs", `#![doc = "crate prose"]
/* outer /* nested */ comment */
pub fn choose(value: Option<u8>) -> u8 {
    let marker = r###"// /* not comments */"###;
    let closure = |x: u8| if x > 0 && x < 5 { 1 } else { 2 };
    match value { Some(x) if x > 0 => closure(x), _ => 0 }
}
#[cfg(test)]
mod tests {
    #[test]
    fn test_it() { assert_eq!(choose(None), 0); }
    fn helper() {}
}
mod inner_tests {
    #![cfg(test)]
    fn inner_helper() {}
}
#[test]
fn direct_test() { let check = || if true { 1 } else { 0 }; }
`)
	result := ReadSources(t.Context(), repo, PlanSources(repo, []string{"src/lib.rs"}), runner.OSRunner{})
	if len(result.Findings) != 0 || len(result.Files) != 1 {
		t.Fatalf("Rust source facts failed: %+v", result)
	}
	source := result.Files[0]
	if len(source.Comments) != 1 || source.Comments[0].Line != 2 || len(source.Macros) != 1 || source.Macros[0].Name != "assert_eq" {
		t.Fatalf("comments/raw strings/macros were confused: %+v", source)
	}
	for _, expected := range []struct {
		name                   string
		complexity, parameters int
		test                   bool
	}{{"choose", 3, 1, false}, {"<closure>", 3, 1, false}, {"test_it", 1, 0, true}, {"helper", 1, 0, true}, {"inner_helper", 1, 0, true}, {"direct_test", 1, 0, true}, {"<closure>", 2, 0, true}} {
		if !slices.ContainsFunc(source.Functions, func(function SourceFunction) bool {
			return function.Name == expected.name && function.Complexity == expected.complexity && function.Parameters == expected.parameters && function.Test == expected.test
		}) {
			t.Errorf("missing scoped function measurement %+v: %+v", expected, source.Functions)
		}
	}
}

func TestRustSourceFactsPreserveConditionalAndRawAttributePaths(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	write(t, repo.Root, "src/lib.rs", "#[cfg_attr(feature = \"x\", cfg_attr(feature = \"y\", r#allow(dead_code)), rustfmt::skip)]\n#[doc(hidden)]\npub fn f() {}\n")
	result := ReadSources(t.Context(), repo, PlanSources(repo, []string{"src/lib.rs"}), runner.OSRunner{})
	if len(result.Findings) != 0 || len(result.Files) != 1 {
		t.Fatalf("Rust attribute facts failed: %+v", result)
	}
	attributes := result.Files[0].Attributes
	if len(attributes) != 2 || attributes[0].Name != "cfg_attr" || len(attributes[0].Children) != 2 || len(attributes[0].Children[0].Children) != 1 || attributes[0].Children[0].Children[0].Name != "allow" || attributes[0].Children[1].Name != "rustfmt::skip" || attributes[1].HasValue {
		t.Fatalf("conditional attribute paths were lost: %+v", attributes)
	}
}

func TestRustSourceFactsKeepValidSiblingsAndProtectNonSourceInputs(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	write(t, repo.Root, "good.rs", "pub fn café() { let emoji = \"🦀\"; /* prose */ }\n")
	write(t, repo.Root, "bad.rs", "pub fn broken( {\n")
	write(t, repo.Root, "generated.rs", "this is protected generated input\n")
	repo.Config.Scope.Generated = []string{"generated.rs"}
	plan := PlanSources(repo, []string{"good.rs", "bad.rs", "generated.rs"})
	result := ReadSources(t.Context(), repo, plan, runner.OSRunner{})
	if len(result.Files) != 1 || result.Files[0].Path != "good.rs" || len(result.Findings) != 1 || result.Findings[0].Path != "bad.rs" {
		t.Fatalf("source coverage discarded a valid sibling or inspected generated code: %+v", result)
	}
	comment := result.Files[0].Comments[0]
	if comment.Column != 34 || comment.StartByte != 37 {
		t.Fatalf("UTF-8 offsets and displayed columns were conflated: %+v", comment)
	}
}

func TestRustSourceFactsRejectFabricatedOrIncompleteResponses(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	write(t, repo.Root, "src/lib.rs", "pub fn f() {}\n")
	plan := PlanSources(repo, []string{"src/lib.rs"})
	_, output, err := (runner.OSRunner{}).RunStructured(t.Context(), repo.Root, plan.Commands[0])
	if err != nil {
		t.Fatal(err)
	}
	for name, corrupt := range map[string]func(map[string]any){
		"runtime":       func(value map[string]any) { value["parserVersion"] = "0.0.0" },
		"empty":         func(value map[string]any) { value["sources"] = []any{} },
		"foreign path":  func(value map[string]any) { wireSource(value)["path"] = "outside.rs" },
		"missing facts": func(value map[string]any) { delete(wireSource(value), "comments") },
		"partial":       func(value map[string]any) { wireSource(value)["complete"] = false },
		"coordinates": func(value map[string]any) {
			wireSource(value)["functions"].([]any)[0].(map[string]any)["column"] = 999
		},
	} {
		t.Run(name, func(t *testing.T) {
			var response map[string]any
			if err := json.Unmarshal(output.Stdout, &response); err != nil {
				t.Fatal(err)
			}
			corrupt(response)
			data, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseSources(repo, plan.Inputs, data); err == nil {
				t.Fatal("invalid source evidence was accepted")
			}
		})
	}
}

func TestRustSourceFactsFailOnCancellationAndUncontainedFiles(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	write(t, repo.Root, "src/lib.rs", "pub fn f() {}\n")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result := ReadSources(ctx, repo, PlanSources(repo, []string{"src/lib.rs"}), runner.OSRunner{})
	if len(result.Files) != 0 || len(result.Findings) != 1 || !strings.Contains(result.Findings[0].Message, "canceled") {
		t.Fatalf("canceled parser produced coverage: %+v", result)
	}
	outside := filepath.Join(t.TempDir(), "outside.rs")
	if err := os.WriteFile(outside, []byte("pub fn outside() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo.Root, "linked.rs")); err != nil {
		t.Fatal(err)
	}
	plan := PlanSources(repo, []string{"linked.rs"})
	if len(plan.Commands) != 0 || len(plan.Findings) != 1 || plan.Findings[0].Path != "linked.rs" {
		t.Fatalf("linked source reached the parser: %+v", plan)
	}
}

func wireSource(value map[string]any) map[string]any {
	return value["sources"].([]any)[0].(map[string]any)
}

func TestRustSourceFactsRespectConfiguredProviderOwnership(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	write(t, repo.Root, "owned.rs", "not valid Rust but owned by the provider\n")
	for _, capability := range []string{"format", "lint", "complexity", "architecture"} {
		repo.Config.Checks = append(repo.Config.Checks, policy.Command{Name: capability, Paths: []string{"owned.rs"}, RunOn: []string{"check"}, Provides: []string{capability}, Adapter: &policy.PackAdapter{PackName: "owner", Capability: capability}})
	}
	plan := PlanSources(repo.WithAnalysisProfile("check"), []string{"owned.rs"})
	if len(plan.Commands) != 0 || len(plan.Findings) != 0 {
		t.Fatalf("native parser claimed provider-owned source: %+v", plan)
	}
}
