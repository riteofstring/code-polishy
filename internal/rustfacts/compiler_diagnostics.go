package rustfacts

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func compilerFindings(repo repository.Repository, manifest string, workspace Workspace, event compilerEvent) ([]policy.Finding, error) {
	if err := validateCompilerTarget(repo, workspace, event); err != nil {
		return nil, err
	}
	diagnostic := event.Message
	if diagnostic.Message == "" || diagnostic.Level == "" {
		return nil, errors.New("cargo compiler diagnostic omitted its message or severity")
	}
	switch diagnostic.Level {
	case "note", "help", "failure-note":
		return nil, nil
	case "error", "warning", "error: internal compiler error":
	default:
		return nil, errors.New("cargo compiler diagnostic has an unsupported severity")
	}
	if diagnostic.Level == "warning" && externalExpansion(repo, workspace, diagnostic.Spans) {
		return nil, nil
	}
	code, check := "rustc", "quality.rustCompiler"
	if diagnostic.Code != nil && diagnostic.Code.Code != "" {
		code = diagnostic.Code.Code
	}
	if strings.HasPrefix(code, "clippy::") {
		check = "quality.clippy"
	}
	finding := policy.Finding{
		Check: check, Path: manifest, Subject: code, Message: diagnostic.Message,
		Fields: map[string]string{"packageId": event.PackageID, "target": event.Target.Name, "targetKinds": strings.Join(event.Target.Kinds, ","), "edition": event.Target.Edition, "hostTarget": HostTarget(), "rustVersion": repo.ToolPin("rust"), "featureSelection": "default"},
	}
	return []policy.Finding{locateCompilerFinding(repo, workspace, finding, diagnostic.Spans)}, nil
}

func locateCompilerFinding(repo repository.Repository, workspace Workspace, finding policy.Finding, spans []compilerSpan) policy.Finding {
	for _, span := range spans {
		if !span.Primary {
			continue
		}
		location, ok := compilerLocation(repo, workspace, span)
		if !ok {
			finding.Fields["reportedSource"] = span.File
			continue
		}
		if finding.Line == 0 {
			finding.Path, finding.Line, finding.Column = location.Path, location.Line, location.Column
			finding.EndLine, finding.EndColumn = location.EndLine, location.EndColumn
		} else {
			finding.Related = append(finding.Related, location)
		}
	}
	return finding
}

func compilerLocation(repo repository.Repository, workspace Workspace, span compilerSpan) (policy.FindingLocation, bool) {
	for depth := 0; depth < 64; depth++ {
		path := span.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(repo.Root, filepath.FromSlash(workspace.Root), path)
		}
		if relative, err := containedPath(repo, path); err == nil && span.Line > 0 && span.Column > 0 {
			return policy.FindingLocation{Path: relative, Line: span.Line, Column: span.Column, EndLine: span.EndLine, EndColumn: span.EndColumn}, true
		}
		if span.Expansion == nil {
			break
		}
		span = span.Expansion.Span
	}
	return policy.FindingLocation{}, false
}

func externalExpansion(repo repository.Repository, workspace Workspace, spans []compilerSpan) bool {
	primary := false
	for _, span := range spans {
		path := span.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(repo.Root, filepath.FromSlash(workspace.Root), path)
		}
		if span.Expansion == nil || repositorySource(repo, path) {
			return false
		}
		primary = primary || span.Primary
	}
	return primary
}

func repositorySource(repo repository.Repository, path string) bool {
	physical, _ := filepath.EvalSymlinks(repo.Root)
	for _, root := range []string{repo.Root, physical} {
		if root != "" && withinDirectory(root, path) {
			repo.Root = root
			return !withinDirectory(CargoHome(repo), path)
		}
	}
	return false
}
