package quality

import (
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRustSourcePolicyDistinguishesProseFromStringsAndDocMetadata(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	denied := false
	repo.Config.Quality.AllowComments = &denied
	writeQualityFile(t, repo.Root, "src/lib.rs", "#![doc = \"crate prose\"]\n/* outer /* nested */ comment */\n#[doc(hidden)]\npub fn value() -> &'static str { r###\"// not prose\"### }\n")
	findings := rustSourceFindings(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{})
	if len(findings) != 2 {
		t.Fatalf("expected the doc attribute and one actual block comment: %+v", findings)
	}
	for _, finding := range findings {
		if finding.Check != "policy.sourceComment" || finding.Path != "src/lib.rs" || finding.Line < 1 || finding.Line > 2 || finding.Column < 1 {
			t.Errorf("prose finding lost its source location: %+v", finding)
		}
	}
	allowed := true
	repo.Config.Quality.AllowComments = &allowed
	if findings := rustSourceFindings(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{}); len(findings) != 0 {
		t.Fatalf("allowed Rust documentation failed: %+v", findings)
	}
}

func TestRustSourcePolicyRejectsConditionalLintAndFormatSuppression(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	writeQualityFile(t, repo.Root, "src/lib.rs", "#[cfg_attr(feature = \"maybe\", r#allow(dead_code), rustfmt::skip, rustfmt::skip::macros(println))]\n#[expect(dead_code)]\n#[rustfmt::skip::attributes(custom)]\nfn value() {}\n")
	findings := rustSourceFindings(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{})
	for _, name := range []string{"allow", "expect", "rustfmt::skip", "rustfmt::skip::macros", "rustfmt::skip::attributes"} {
		if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
			return finding.Check == "policy.rustAttribute" && strings.HasPrefix(finding.Subject, name+":") && finding.Path == "src/lib.rs" && finding.Line > 0
		}) {
			t.Errorf("conditional suppression %s was not governed: %+v", name, findings)
		}
	}
}

func TestRustComplexityDoesNotBorrowTestLimitsForInlineProductionCode(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	repo.Config.Quality.Complexity.Rust = 3
	repo.Config.Quality.Complexity.RustTest = 5
	repo.Config.Tests.Paths = []string{"src/lib.rs"}
	writeQualityFile(t, repo.Root, "src/lib.rs", "pub fn production(a: bool, b: bool) { if a {} if b {} }\n#[test]\nfn test_body() { if true {} if true {} }\n")
	findings := rustSourceFindings(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{})
	if len(findings) != 1 || findings[0].Check != "quality.rustComplexity" || !strings.HasPrefix(findings[0].Subject, "production:") || findings[0].Fields["complexity"] != "3" || findings[0].Fields["limit"] != "3" {
		t.Fatalf("inline tests relaxed production complexity or inherited TypeScript limits: %+v", findings)
	}
}

func TestRustSourcePolicyMissingParserDoesNotPassCommentCoverage(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	denied := false
	repo.Config.Quality.AllowComments = &denied
	writeQualityFile(t, repo.Root, "src/lib.rs", "// prose\npub fn value() {}\n")
	repo.PolicyRoot = t.TempDir()
	findings := rustSourceFindings(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{})
	if len(findings) != 1 || findings[0].Check != "policy.rustSource" {
		t.Fatalf("missing parser was accepted as comment coverage: %+v", findings)
	}
}

func TestRustSourcePolicyRejectsEmptyTestsWithoutClaimingHelpers(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name   string
		source string
		empty  bool
	}{
		{"empty test", "#[test]\nfn case() {}\n", true},
		{"comment only", "#[test]\nfn case() { /* no behavior */ }\n", true},
		{"empty statements", "#[test]\nfn case() { ; ; }\n", true},
		{"ignored empty test", "#[test]\n#[ignore]\nfn case() {}\n", true},
		{"raw test attribute", "#[r#test]\nfn case() {}\n", true},
		{"conditional test", "#[cfg_attr(test, test)]\nfn case() {}\n", true},
		{"nested conditional test", "#[cfg_attr(feature = \"checks\", cfg_attr(test, test))]\nfn case() {}\n", true},
		{"production function", "pub fn case() {}\n", false},
		{"test helper", "#[cfg(test)]\nmod checks { fn helper() {} }\n", false},
		{"attribute text", "pub const TEXT: &str = r#\"#[test] fn case() {}\"#;\n", false},
		{"actual behavior", "fn answer() -> u8 { 42 }\n#[test]\nfn case() { assert_eq!(answer(), 42); }\n", false},
		{"called helper", "fn answer() -> u8 { 42 }\nfn helper() { assert_eq!(answer(), 42); }\n#[test]\nfn case() { helper(); }\n", false},
		{"nested helper", "fn answer() -> u8 { 42 }\n#[test]\nfn case() { fn helper() { assert_eq!(answer(), 42); } helper(); }\n", false},
		{"test closure", "fn answer() -> u8 { 42 }\n#[test]\nfn case() { let helper = || {}; helper(); assert_eq!(answer(), 42); }\n", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repo := cargoQualityRepository(t)
			allowed := true
			repo.Config.Quality.AllowComments = &allowed
			writeQualityFile(t, repo.Root, "src/lib.rs", scenario.source)
			findings := rustSourceFindings(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{})
			if !scenario.empty {
				if len(findings) != 0 {
					t.Fatalf("source was mistaken for an empty test: %+v", findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Check != "quality.testEvidence" || findings[0].Path != "src/lib.rs" || !strings.HasPrefix(findings[0].Subject, "case:") || findings[0].Line < 2 || !strings.Contains(findings[0].Message, "empty") {
				t.Fatalf("empty Rust test lacks original-location evidence: %+v", findings)
			}
		})
	}
}

func TestRustConditionalTestsRetainProductionComplexityOutsideTestOnlyScopes(t *testing.T) {
	t.Parallel()
	for _, testOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "production", true: "test only"}[testOnly], func(t *testing.T) {
			repo := cargoQualityRepository(t)
			repo.Config.Quality.Complexity.Rust = 3
			repo.Config.Quality.Complexity.RustTest = 5
			source := "#[cfg_attr(test, test)]\nfn conditionally_tested() { if true {} if true {} }\n"
			if testOnly {
				source = "#[cfg(test)]\nmod checks {\n" + source + "}\n"
			}
			writeQualityFile(t, repo.Root, "src/lib.rs", source)
			findings := rustSourceFindings(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{})
			if testOnly {
				if len(findings) != 0 {
					t.Fatalf("test-only scope inherited production limits: %+v", findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Check != "quality.rustComplexity" || !strings.HasPrefix(findings[0].Subject, "conditionally_tested:") || findings[0].Fields["limit"] != "3" {
				t.Fatalf("conditional test attribute relaxed production limits: %+v", findings)
			}
		})
	}
}

func TestCargoCheckRejectsEmptyRustTestsAndAcceptsRepairedBehavior(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	writeQualityFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\nedition='2024'\n")
	writeQualityFile(t, repo.Root, "Cargo.lock", "version=4\n[[package]]\nname='app'\nversion='1.0.0'\n")
	source := "pub fn answer() -> u8 {\n    42\n}\n\n#[test]\nfn answer_is_correct() {}\n"
	writeQualityFile(t, repo.Root, "src/lib.rs", source)
	selection := repository.Selection{Files: []string{"src/lib.rs"}}
	findings := Check(t.Context(), repo, selection, runner.OSRunner{}, "check")
	if len(findings) != 1 || findings[0].Check != "quality.testEvidence" || findings[0].Path != "src/lib.rs" || findings[0].Line != 6 {
		t.Fatalf("ordinary Cargo checks accepted an empty compiled test: %+v", findings)
	}
	repaired := strings.Replace(source, "fn answer_is_correct() {}", "fn answer_is_correct() {\n    assert_eq!(answer(), 42);\n}", 1)
	writeQualityFile(t, repo.Root, "src/lib.rs", repaired)
	if findings := Check(t.Context(), repo, selection, runner.OSRunner{}, "check"); len(findings) != 0 {
		t.Fatalf("ordinary Cargo checks rejected repaired test behavior: %+v", findings)
	}
}
