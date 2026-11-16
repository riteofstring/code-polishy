package quality

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func packQualityFindings(ctx context.Context, repo repository.Repository, selection repository.Selection, command policy.Command, boundary runner.Runner, profile string) []policy.Finding {
	result := pack.RunAdapter(ctx, repo, selection, command, boundary, profile)
	findings := result.Findings
	if command.Adapter.Capability == "lint" {
		for index, finding := range findings {
			rule := strings.TrimPrefix(finding.Check, "pack."+command.Adapter.PackName+".")
			findings[index] = generatedJavaScriptLintFinding(repo, finding, rule)
		}
	}
	if result.Response.Facts == nil {
		return findings
	}
	if result.Response.Facts.Comments != nil && !repo.Config.Quality.CommentsAllowed() {
		findings = append(findings, packCommentFindings(repo, *result.Response.Facts.Comments)...)
	}
	return findings
}

func generatedJavaScriptLintFinding(repo repository.Repository, finding policy.Finding, rule string) policy.Finding {
	semanticRules := []string{"no-unreachable", "no-dupe-else-if", "no-duplicate-case", "no-constant-binary-expression", "no-unsafe-finally", "valid-typeof", "use-isnan"}
	if repo.IsGenerated(finding.Path) && repo.Language(finding.Path) == "typescript" && slices.Contains(semanticRules, rule) {
		finding.Severity = policy.FindingWarning
		finding.Fields = maps.Clone(finding.Fields)
		if finding.Fields == nil {
			finding.Fields = map[string]string{}
		}
		finding.Fields["analysisOutcome"] = "advisory-candidate"
		finding.Remediation.Summary = "Advisory candidate: this warning requires no cleanup, investigation, waiver, or acknowledgement. If investigating this candidate, compare the authoritative inputs with runtime behavior. Apply confirmed fixes through the producer."
	}
	return finding
}

func packCommentFindings(repo repository.Repository, comments []pack.CommentFact) []policy.Finding {
	findings := []policy.Finding{}
	for _, comment := range comments {
		if repo.IsGenerated(comment.Path) {
			continue
		}
		if repo.Language(comment.Path) == "typescript" && javascriptSourceCommentAllowed(repo, javascript.LintComment{
			Path: comment.Path, Kind: comment.Kind, Raw: comment.Raw, Complete: comment.Complete, Line: comment.Line, Column: comment.Column,
			BeforeCode: comment.BeforeCode, Preamble: comment.Preamble, ByteZero: comment.ByteZero,
		}) {
			continue
		}
		findings = append(findings, policy.Finding{Check: "policy.sourceComment", Path: comment.Path, Line: comment.Line, Column: comment.Column, Subject: fmt.Sprintf("%d:%d", comment.Line, comment.Column), Message: "prose comments and docstrings are forbidden; move durable context to a design document or use an allowed machine directive"})
	}
	return findings
}
