package repository

import (
	"fmt"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func (repo Repository) SourceContextOwner(path string) (policy.SourceContext, bool) {
	matches := []policy.SourceContext{}
	for _, declaration := range repo.Config.Scope.SourceContexts {
		if policy.MatchesAny(path, declaration.Paths) {
			matches = append(matches, declaration)
		}
	}
	if len(matches) != 1 {
		return policy.SourceContext{}, false
	}
	return matches[0], true
}

func (repo Repository) SourceContextPath(path string) string {
	if !repo.IsGenerated(path) {
		return path
	}
	if declaration, found := repo.SourceContextOwner(path); found {
		return declaration.Context
	}
	return path
}

func (repo Repository) SourceContextOwnershipFindings(files []string) []policy.Finding {
	fileSet := make(map[string]bool, len(files))
	for _, path := range files {
		fileSet[path] = true
	}
	findings := []policy.Finding{}
	for _, declaration := range repo.Config.Scope.SourceContexts {
		matched := sourceContextsMatches(files, declaration)
		if !fileSet[declaration.Context] {
			findings = append(findings, sourceContextsFinding(declaration.Context, "context path does not exist in governed source"))
		}
		if repo.IsGenerated(declaration.Context) || sourceContextsMappingCount(repo.Config.Scope.SourceContexts, declaration.Context) > 0 {
			findings = append(findings, sourceContextsFinding(declaration.Context, "context path cannot itself be generated output or another mapped source"))
		}
		if len(matched) == 0 {
			findings = append(findings, sourceContextsFinding(declaration.Context, fmt.Sprintf("paths do not match any current file: %s", strings.Join(declaration.Paths, ", "))))
		}
	}
	for path := range fileSet {
		owners := sourceContextsMappingCount(repo.Config.Scope.SourceContexts, path)
		if finding, found := repo.sourceContextsOutputFinding(path, owners); found {
			findings = append(findings, finding)
		}
	}
	sort.Slice(findings, func(left, right int) bool {
		return findings[left].Path+"\x00"+findings[left].Message < findings[right].Path+"\x00"+findings[right].Message
	})
	return findings
}

func sourceContextsMatches(files []string, declaration policy.SourceContext) []string {
	matches := []string{}
	for _, path := range files {
		if policy.MatchesAny(path, declaration.Paths) {
			matches = append(matches, path)
		}
	}
	return matches
}

func (repo Repository) sourceContextsOutputFinding(path string, owners int) (policy.Finding, bool) {
	switch {
	case owners == 0:
		return policy.Finding{}, false
	case owners > 1:
		return sourceContextsFinding(path, "generated output matches more than one source-context declaration"), true
	case !repo.IsGenerated(path):
		return sourceContextsFinding(path, "declared output is not in scope.generated"), true
	default:
		return policy.Finding{}, false
	}
}

func sourceContextsMappingCount(declarations []policy.SourceContext, path string) int {
	count := 0
	for _, declaration := range declarations {
		if policy.MatchesAny(path, declaration.Paths) {
			count++
		}
	}
	return count
}

func sourceContextsFinding(path, message string) policy.Finding {
	return policy.Finding{Check: "policy.sourceContextOwnership", Path: path, Subject: "source-context", Message: message}
}

func (repo Repository) JavaScriptLintActivation(file string) policy.JavaScriptLintScope {
	context := repo.SourceContextPath(file)
	activation := policy.JavaScriptLintScope{}
	for _, scope := range repo.Config.JavaScriptLintScopes {
		if scope.Root != "." && !strings.HasPrefix(context, scope.Root+"/") {
			continue
		}
		if activation.Root == "" || len(scope.Root) > len(activation.Root) {
			activation = scope
		}
	}
	if repo.IsGenerated(file) {
		activation.ReactHooks = false
	}
	return activation
}
