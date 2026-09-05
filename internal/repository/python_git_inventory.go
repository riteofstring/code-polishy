package repository

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (source PythonGitSource) ValidateExactPin() error {
	if source.RefKind == "tag" || source.RefKind == "branch" {
		return fmt.Errorf("git source must use a full commit revision instead of a tag or branch")
	}
	if !pythonFullCommit(source.DeclaredRef) || !pythonFullCommit(source.Commit) {
		return fmt.Errorf("git source must declare and resolve to one full 40-character commit SHA")
	}
	if source.DeclaredRef != source.Commit {
		return fmt.Errorf("git source declared ref differs from its resolved commit")
	}
	return nil
}

func (source PythonGitSource) InventoryIdentity() string {
	identity := source.RepositoryIdentity()
	if source.DeclaredRef != "" {
		identity += "@" + source.DeclaredRef
	}
	fragments := []string{}
	if source.RefKind == "tag" || source.RefKind == "branch" {
		fragments = append(fragments, "refKind="+source.RefKind)
	}
	if source.Commit != "" && source.Commit != source.DeclaredRef {
		fragments = append(fragments, "commit="+source.Commit)
	}
	if source.Subdirectory != "" {
		fragments = append(fragments, "subdirectory="+source.Subdirectory)
	}
	if len(fragments) > 0 {
		identity += "#" + strings.Join(fragments, "&")
	}
	return identity
}

func pythonGitReference(value string) (string, error) {
	if !validPythonGitReferenceText(value) {
		return "", fmt.Errorf("git reference must be one bounded nonempty name")
	}
	if invalidPythonGitReferenceSyntax(value) {
		return "", fmt.Errorf("git reference is ambiguous or contains revision syntax")
	}
	for _, component := range strings.Split(value, "/") {
		if strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".") || strings.HasSuffix(component, ".lock") {
			return "", fmt.Errorf("git reference has an invalid path component")
		}
	}
	if pythonFullCommit(value) {
		return strings.ToLower(value), nil
	}
	return value, nil
}

func validPythonGitReferenceText(value string) bool {
	return value != "" && len(value) <= 1024 && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsSpace) < 0 && strings.IndexFunc(value, unicode.IsControl) < 0
}

func invalidPythonGitReferenceSyntax(value string) bool {
	return strings.ContainsAny(value, "~^:?*[\\@#%$"+"{}") || strings.Contains(value, "..") || strings.Contains(value, "//") ||
		strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/")
}

func pythonUVGitReference(values url.Values) (string, error) {
	references := []string{}
	for _, key := range []string{"rev", "tag", "branch"} {
		references = append(references, values[key]...)
	}
	if len(references) != 1 {
		return "", fmt.Errorf("git lock source must declare exactly one revision, tag, or branch")
	}
	return pythonGitReference(references[0])
}
