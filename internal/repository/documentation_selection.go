package repository

import (
	"fmt"
	pathpkg "path"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type DocumentationClassification struct {
	Ordinary bool
	Reason   string
}

func (repo Repository) ClassifyDocumentationCandidate(selection Selection) DocumentationClassification {
	paths := selection.Candidate.Paths()
	if len(paths) == 0 {
		return DocumentationClassification{Reason: "no candidate paths were detected"}
	}
	productInputs := make(map[string]bool, len(repo.Config.Documentation.ProductInputs))
	for _, path := range repo.Config.Documentation.ProductInputs {
		productInputs[path] = true
	}
	for _, path := range paths {
		if !policy.IsMarkdownPath(path) {
			return DocumentationClassification{Reason: fmt.Sprintf("changed path %q is not Markdown", path)}
		}
		if documentationControlPath(path) {
			return DocumentationClassification{Reason: fmt.Sprintf("changed path %q is a documentation control input", path)}
		}
		if productInputs[path] {
			return DocumentationClassification{Reason: fmt.Sprintf("changed path %q is a declared documentation product input", path)}
		}
	}
	return DocumentationClassification{Ordinary: true, Reason: "candidate contains ordinary Markdown only"}
}

func documentationControlPath(path string) bool {
	name := pathpkg.Base(path)
	if strings.EqualFold(name, "AGENTS.md") || strings.EqualFold(name, "CLAUDE.md") || strings.EqualFold(name, "SKILL.md") {
		return true
	}
	for _, segment := range strings.Split(path, "/") {
		if strings.EqualFold(segment, "skills") || strings.EqualFold(segment, "templates") {
			return true
		}
	}
	return false
}
