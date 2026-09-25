package quality

import (
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
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
