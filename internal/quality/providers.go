package quality

import (
	"context"
	"fmt"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func packQualityFindings(ctx context.Context, repo repository.Repository, selection repository.Selection, command policy.Command, boundary runner.Runner, profile string) []policy.Finding {
	result := pack.RunAdapter(ctx, repo, selection, command, boundary, profile)
	findings := result.Findings
	if result.Response.Facts == nil {
		return findings
	}
	if result.Response.Facts.Comments != nil && !repo.Config.Quality.CommentsAllowed() {
		findings = append(findings, packCommentFindings(repo, *result.Response.Facts.Comments)...)
	}
	if result.Response.Facts.Functions != nil {
		findings = append(findings, packFunctionFindings(repo, *result.Response.Facts.Functions)...)
	}
	return findings
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

func packFunctionFindings(repo repository.Repository, functions []pack.FunctionFact) []policy.Finding {
	findings := []policy.Finding{}
	for _, function := range functions {
		if repo.IsGenerated(function.Path) {
			continue
		}
		quality := policy.EffectiveQuality(repo.Config.Quality)
		complexity, depth, parameters := quality.Complexity.TypeScript, quality.MaxDepth, quality.MaxParams
		if repo.IsTest(function.Path) {
			complexity, depth, parameters = quality.Complexity.TypeScriptTest, quality.MaxTestDepth, quality.MaxTestParams
		}
		switch repo.Language(function.Path) {
		case "go":
			complexity = quality.Complexity.Go
			if repo.IsTest(function.Path) {
				complexity = quality.Complexity.GoTest
			}
		case "python":
			complexity = quality.Complexity.Python
		}
		for _, metric := range []struct {
			name           string
			value, maximum int
		}{
			{"complexity", function.Complexity, complexity - 1}, {"depth", function.Depth, depth}, {"parameters", function.Parameters, parameters},
		} {
			if metric.value > metric.maximum {
				findings = append(findings, policy.Finding{Check: "quality.function" + metric.name, Path: function.Path, Line: function.Line, Column: function.Column, Subject: function.Name, Message: fmt.Sprintf("function %s has %s %d; maximum is %d", function.Name, metric.name, metric.value, metric.maximum)})
			}
		}
	}
	return findings
}
