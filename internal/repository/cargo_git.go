package repository

import (
	"fmt"
	"net/url"
)

func ParseCargoGitDeclaration(declaration CargoDependencyDeclaration) (PythonGitSource, error) {
	source, err := parsePythonGitRepositoryURL(declaration.Git, false)
	if err != nil {
		return PythonGitSource{}, err
	}
	for _, selector := range []struct{ kind, value string }{
		{"rev", declaration.Revision}, {"branch", declaration.Branch}, {"tag", declaration.Tag},
	} {
		if selector.value == "" {
			continue
		}
		if source.DeclaredRef != "" {
			return PythonGitSource{}, fmt.Errorf("cargo Git dependency must declare at most one reference selector")
		}
		reference, err := pythonGitReference(selector.value)
		if err != nil {
			return PythonGitSource{}, err
		}
		source.DeclaredRef, source.RefKind = reference, selector.kind
		if selector.kind == "rev" && pythonFullCommit(reference) {
			source.Commit = reference
		}
	}
	return source, nil
}

func ParseCargoGitLockSource(value string) (PythonGitSource, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return PythonGitSource{}, fmt.Errorf("cargo Git lock source URL is malformed")
	}
	if parsed.RawQuery != "" {
		return ParsePythonGitInventorySource(value, "", "")
	}
	commit := parsed.Fragment
	parsed.Fragment, parsed.RawFragment = "", ""
	source, err := ParsePythonGitInventorySource(parsed.String(), commit, "")
	if err != nil {
		return PythonGitSource{}, err
	}
	source.DeclaredRef = ""
	return source, nil
}
