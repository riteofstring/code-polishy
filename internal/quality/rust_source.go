package quality

import (
	"context"
	"fmt"
	"strconv"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/rustfacts"
)

func rustSourceFindings(ctx context.Context, repo repository.Repository, files []string, boundary runner.Runner) []policy.Finding {
	result := rustfacts.ReadSources(ctx, repo, rustfacts.PlanSources(repo, files), boundary)
	findings := result.Findings
	for _, source := range result.Files {
		if repo.NativeAnalysis(source.Path, "lint") && !repo.Config.Quality.CommentsAllowed() {
			for _, comment := range source.Comments {
				findings = append(findings, rustCommentFinding(source.Path, comment))
			}
		}
		findings = append(findings, rustAttributeFindings(repo, source.Path, source.Attributes)...)
		if repo.NativeAnalysis(source.Path, "complexity") {
			findings = append(findings, rustComplexityFindings(repo, source)...)
		}
	}
	return findings
}

func rustCommentFinding(path string, location rustfacts.SourceLocation) policy.Finding {
	return rustLocatedFinding("policy.sourceComment", path, fmt.Sprintf("%d:%d", location.Line, location.Column), "prose comments and doc attributes are forbidden; move durable context to a design document", location)
}

func rustAttributeFindings(repo repository.Repository, path string, attributes []rustfacts.SourceAttribute) []policy.Finding {
	findings := []policy.Finding{}
	for _, attribute := range attributes {
		message := ""
		switch attribute.Name {
		case "allow", "expect":
			if repo.NativeAnalysis(path, "lint") {
				message = "inline Rust lint suppression must be replaced by an exact Code Polishy exception for the underlying finding"
			}
		case "rustfmt::skip":
			if repo.NativeAnalysis(path, "format") {
				message = "Rust attributes must not exclude governed source from formatting"
			}
		case "doc":
			if attribute.HasValue && repo.NativeAnalysis(path, "lint") && !repo.Config.Quality.CommentsAllowed() {
				findings = append(findings, rustCommentFinding(path, attribute.SourceLocation))
			}
		}
		if message != "" {
			subject := fmt.Sprintf("%s:%d:%d", attribute.Name, attribute.Line, attribute.Column)
			findings = append(findings, rustLocatedFinding("policy.rustAttribute", path, subject, message, attribute.SourceLocation))
		}
		findings = append(findings, rustAttributeFindings(repo, path, attribute.Children)...)
	}
	return findings
}

func rustComplexityFindings(repo repository.Repository, source rustfacts.Source) []policy.Finding {
	limits := policy.EffectiveQuality(repo.Config.Quality).Complexity
	findings := []policy.Finding{}
	for _, function := range source.Functions {
		limit := limits.Rust
		if function.Test {
			limit = limits.RustTest
		}
		if function.Complexity < limit {
			continue
		}
		subject := fmt.Sprintf("%s:%d:%d", function.Name, function.Line, function.Column)
		message := fmt.Sprintf("function %s has written-source cyclomatic complexity %d; must be below %d", function.Name, function.Complexity, limit)
		finding := rustLocatedFinding("quality.rustComplexity", source.Path, subject, message, function.SourceLocation)
		finding.Fields = map[string]string{"complexity": strconv.Itoa(function.Complexity), "limit": strconv.Itoa(limit), "measurementScope": "unexpanded-written-source"}
		findings = append(findings, finding)
	}
	return findings
}

func rustLocatedFinding(check, path, subject, message string, location rustfacts.SourceLocation) policy.Finding {
	return policy.Finding{Check: check, Path: path, Subject: subject, Message: message, Line: location.Line, Column: location.Column, EndLine: location.EndLine, EndColumn: location.EndColumn}
}
