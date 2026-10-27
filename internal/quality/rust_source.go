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

const rustFormatExclusionMessage = "Rust attributes must not exclude governed source from formatting"

func rustSourceFindings(ctx context.Context, repo repository.Repository, files []string, boundary runner.Runner) []policy.Finding {
	result := rustfacts.ReadSources(ctx, repo, rustfacts.PlanSources(repo, files), boundary)
	findings := result.Findings
	for _, source := range result.Files {
		if repo.NativeAnalysis(source.Path, "lint") && !repo.Config.Quality.CommentsAllowed() {
			findings = append(findings, rustProseCommentFindings(repo, source)...)
		}
		if repo.NativeAnalysis(source.Path, "lint") {
			findings = append(findings, rustTestEvidenceFindings(source)...)
		}
		findings = append(findings, rustAttributeFindings(repo, source.Path, source.Attributes)...)
		if repo.NativeAnalysis(source.Path, "complexity") {
			findings = append(findings, rustComplexityFindings(repo, source)...)
		}
	}
	return findings
}

func rustProseCommentFindings(repo repository.Repository, source rustfacts.Source) []policy.Finding {
	data, err := repo.Read(source.Path)
	annotations := make([]sourceAnnotation, 0, len(source.Comments))
	for _, comment := range source.Comments {
		annotation := sourceAnnotation{offset: comment.StartByte}
		if err == nil && 0 <= comment.StartByte && comment.StartByte <= comment.EndByte && comment.EndByte <= len(data) {
			annotation.text = string(data[comment.StartByte:comment.EndByte])
		}
		annotations = append(annotations, annotation)
	}
	headerEnd := sourceHeaderEnd(data, annotations)
	findings := []policy.Finding{}
	for index, comment := range source.Comments {
		if !licenseNoticeAllowed("rust", data, headerEnd, annotations[index]) {
			findings = append(findings, rustCommentFinding(source.Path, comment))
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
		switch {
		case attribute.Name == "allow", attribute.Name == "expect":
			if repo.NativeAnalysis(path, "lint") {
				message = "inline Rust lint suppression must be replaced by an exact Code Polishy exception for the underlying finding"
			}
		case rustFormatExclusion(attribute.Name):
			if repo.NativeAnalysis(path, "format") {
				message = rustFormatExclusionMessage
			}
		case attribute.Name == "doc":
			if attribute.HasValue && repo.NativeAnalysis(path, "lint") && !repo.Config.Quality.CommentsAllowed() {
				findings = append(findings, rustCommentFinding(path, attribute.SourceLocation))
			}
		}
		if message != "" {
			findings = append(findings, rustAttributeFinding(path, attribute, message))
		}
		findings = append(findings, rustAttributeFindings(repo, path, attribute.Children)...)
	}
	return findings
}

func rustAttributeFinding(path string, attribute rustfacts.SourceAttribute, message string) policy.Finding {
	subject := fmt.Sprintf("%s:%d:%d", attribute.Name, attribute.Line, attribute.Column)
	return rustLocatedFinding("policy.rustAttribute", path, subject, message, attribute.SourceLocation)
}

func rustFormatExclusionFindings(path string, attributes []rustfacts.SourceAttribute) []policy.Finding {
	findings := []policy.Finding{}
	for _, attribute := range attributes {
		if rustFormatExclusion(attribute.Name) {
			findings = append(findings, rustAttributeFinding(path, attribute, rustFormatExclusionMessage))
		}
		findings = append(findings, rustFormatExclusionFindings(path, attribute.Children)...)
	}
	return findings
}

func rustFormatExclusion(name string) bool {
	return name == "rustfmt::skip" || name == "rustfmt::skip::macros" || name == "rustfmt::skip::attributes"
}

func rustComplexityFindings(repo repository.Repository, source rustfacts.Source) []policy.Finding {
	limits := policy.EffectiveQuality(repo.Config.Quality).Complexity
	findings := []policy.Finding{}
	for _, function := range source.Functions {
		limit := limits.Rust
		if function.Context == rustfacts.TestContext {
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

func rustTestEvidenceFindings(source rustfacts.Source) []policy.Finding {
	findings := []policy.Finding{}
	for _, function := range source.Functions {
		if function.TestEntry == rustfacts.NotATest || function.Body != "empty" {
			continue
		}
		subject := fmt.Sprintf("%s:%d:%d", function.Name, function.Line, function.Column)
		findings = append(findings, rustLocatedFinding("quality.testEvidence", source.Path, subject, "test body is empty and provides no behavior evidence", function.SourceLocation))
	}
	return findings
}

func rustLocatedFinding(check, path, subject, message string, location rustfacts.SourceLocation) policy.Finding {
	return policy.Finding{Check: check, Path: path, Subject: subject, Message: message, Line: location.Line, Column: location.Column, EndLine: location.EndLine, EndColumn: location.EndColumn}
}
