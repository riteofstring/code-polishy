package repository

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func (repo Repository) GeneratedJavaScriptOwner(path string) (policy.GeneratedJavaScript, bool) {
	matches := []policy.GeneratedJavaScript{}
	for _, declaration := range repo.Config.Scope.GeneratedJavaScript {
		if policy.MatchesAny(path, declaration.Paths) {
			matches = append(matches, declaration)
		}
	}
	if len(matches) != 1 {
		return policy.GeneratedJavaScript{}, false
	}
	return matches[0], true
}

func (repo Repository) JavaScriptContextPath(path string) string {
	if declaration, found := repo.GeneratedJavaScriptOwner(path); found {
		return declaration.SourcePackage
	}
	return path
}

func (repo Repository) GeneratedJavaScriptOwnershipFindings(files []string) []policy.Finding {
	fileSet := make(map[string]bool, len(files))
	for _, path := range files {
		fileSet[path] = true
	}
	findings := []policy.Finding{}
	for _, declaration := range repo.Config.Scope.GeneratedJavaScript {
		matched := generatedJavaScriptMatches(files, declaration)
		if !fileSet[declaration.SourcePackage] {
			findings = append(findings, generatedJavaScriptFinding(declaration.SourcePackage, "source package does not exist in governed source"))
		}
		if repo.IsGenerated(declaration.SourcePackage) || generatedJavaScriptMappingCount(repo.Config.Scope.GeneratedJavaScript, declaration.SourcePackage) > 0 {
			findings = append(findings, generatedJavaScriptFinding(declaration.SourcePackage, "source package cannot itself be generated output"))
		}
		if len(matched) == 0 {
			findings = append(findings, generatedJavaScriptFinding(declaration.SourcePackage, fmt.Sprintf("paths do not match any current file: %s", strings.Join(declaration.Paths, ", "))))
		}
	}
	for path := range fileSet {
		owners := generatedJavaScriptMappingCount(repo.Config.Scope.GeneratedJavaScript, path)
		if finding, found := repo.generatedJavaScriptOutputFinding(path, owners); found {
			findings = append(findings, finding)
		}
	}
	sort.Slice(findings, func(left, right int) bool {
		return findings[left].Path+"\x00"+findings[left].Message < findings[right].Path+"\x00"+findings[right].Message
	})
	return findings
}

func generatedJavaScriptMatches(files []string, declaration policy.GeneratedJavaScript) []string {
	matches := []string{}
	for _, path := range files {
		if policy.MatchesAny(path, declaration.Paths) {
			matches = append(matches, path)
		}
	}
	return matches
}

func (repo Repository) generatedJavaScriptOutputFinding(path string, owners int) (policy.Finding, bool) {
	switch {
	case owners == 0:
		return policy.Finding{}, false
	case owners > 1:
		return generatedJavaScriptFinding(path, "generated output matches more than one source-package declaration"), true
	case !repo.IsGenerated(path):
		return generatedJavaScriptFinding(path, "declared output is not in scope.generated"), true
	case repo.Language(path) != "typescript" || !generatedJavaScriptExtension(path):
		return generatedJavaScriptFinding(path, "declared output is not supported JavaScript or TypeScript source"), true
	default:
		return policy.Finding{}, false
	}
}

func generatedJavaScriptMappingCount(declarations []policy.GeneratedJavaScript, path string) int {
	count := 0
	for _, declaration := range declarations {
		if policy.MatchesAny(path, declaration.Paths) {
			count++
		}
	}
	return count
}

func generatedJavaScriptExtension(path string) bool {
	return slices.Contains([]string{".cjs", ".cts", ".js", ".jsx", ".mjs", ".mts", ".ts", ".tsx"}, strings.ToLower(filepath.Ext(path)))
}

func generatedJavaScriptFinding(path, message string) policy.Finding {
	return policy.Finding{Check: "policy.generatedJavaScriptOwnership", Path: path, Subject: "source-package", Message: message}
}
