package rustfacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type Compilation struct {
	Success      bool
	Findings     []policy.Finding
	Artifacts    []CompiledTarget
	BuildScripts []BuildScript
}

type CompiledTarget struct {
	PackageID string
	Target    Target
	Features  []string
	Fresh     bool
}

type BuildScript struct {
	PackageID   string
	OutputDir   string
	Cfgs        []string
	Environment [][2]string
	LinkedLibs  []string
	LinkedPaths []string
}

type compilerEvent struct {
	Reason      string             `json:"reason"`
	PackageID   string             `json:"package_id"`
	Target      Target             `json:"target"`
	Message     compilerDiagnostic `json:"message"`
	Features    []string           `json:"features"`
	Fresh       *bool              `json:"fresh"`
	Success     *bool              `json:"success"`
	OutputDir   string             `json:"out_dir"`
	Cfgs        []string           `json:"cfgs"`
	Environment [][2]string        `json:"env"`
	LinkedLibs  []string           `json:"linked_libs"`
	LinkedPaths []string           `json:"linked_paths"`
}

type compilerDiagnostic struct {
	Message string         `json:"message"`
	Level   string         `json:"level"`
	Code    *compilerCode  `json:"code"`
	Spans   []compilerSpan `json:"spans"`
}

type compilerCode struct {
	Code string `json:"code"`
}

type compilerSpan struct {
	File      string             `json:"file_name"`
	Line      int                `json:"line_start"`
	Column    int                `json:"column_start"`
	EndLine   int                `json:"line_end"`
	EndColumn int                `json:"column_end"`
	Primary   bool               `json:"is_primary"`
	Expansion *compilerExpansion `json:"expansion"`
}

type compilerExpansion struct {
	Span compilerSpan `json:"span"`
}

func ParseCompilation(repo repository.Repository, manifest string, workspace Workspace, data []byte) (Compilation, error) {
	report := Compilation{}
	if len(data) > 8<<20 {
		return report, errors.New("cargo compiler output exceeds 8388608 bytes")
	}
	finished := false
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event compilerEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return report, fmt.Errorf("expected one Cargo JSON event per line: %w", err)
		}
		if finished {
			return report, errors.New("cargo emitted events after build-finished")
		}
		if err := consumeCompilerEvent(repo, manifest, workspace, event, &report); err != nil {
			return report, err
		}
		finished = event.Reason == "build-finished"
	}
	if !finished {
		return report, errors.New("cargo emitted no build-finished event")
	}
	if report.Success {
		return report, validateCompiledPackages(manifest, workspace, report.Artifacts)
	}
	return report, nil
}

func consumeCompilerEvent(repo repository.Repository, manifest string, workspace Workspace, event compilerEvent, report *Compilation) error {
	switch event.Reason {
	case "compiler-message":
		findings, err := compilerFindings(repo, manifest, workspace, event)
		report.Findings = append(report.Findings, findings...)
		return err
	case "compiler-artifact":
		if err := validateCompilerTarget(repo, workspace, event); err != nil {
			return err
		}
		if event.Fresh == nil || event.Features == nil {
			return errors.New("cargo artifact omitted freshness or feature evidence")
		}
		report.Artifacts = append(report.Artifacts, CompiledTarget{PackageID: event.PackageID, Target: event.Target, Features: event.Features, Fresh: *event.Fresh})
	case "build-script-executed":
		build, err := compilerBuildScript(repo, event)
		if err != nil {
			return err
		}
		report.BuildScripts = append(report.BuildScripts, build)
	case "build-finished":
		if event.Success == nil {
			return errors.New("cargo build-finished event omitted its outcome")
		}
		report.Success = *event.Success
	default:
		return fmt.Errorf("unsupported Cargo compiler event %q", event.Reason)
	}
	return nil
}

func compilerBuildScript(repo repository.Repository, event compilerEvent) (BuildScript, error) {
	if event.PackageID == "" || !withinDirectory(TargetDirectory(repo), event.OutputDir) || event.Cfgs == nil || event.Environment == nil {
		return BuildScript{}, errors.New("cargo build-script evidence omitted identity, configuration or governed output directory")
	}
	return BuildScript{PackageID: event.PackageID, OutputDir: event.OutputDir, Cfgs: event.Cfgs, Environment: event.Environment, LinkedLibs: event.LinkedLibs, LinkedPaths: event.LinkedPaths}, nil
}

func validateCompilerTarget(repo repository.Repository, workspace Workspace, event compilerEvent) error {
	if event.PackageID == "" || event.Target.Name == "" || len(event.Target.Kinds) == 0 || !filepath.IsAbs(event.Target.Path) || !supportedEdition(event.Target.Edition) {
		return errors.New("cargo compiler event omitted package or target identity")
	}
	for _, pkg := range workspace.Packages {
		if pkg.ID != event.PackageID {
			continue
		}
		if matchesDiscoveredTarget(repo, pkg.Targets, event.Target) {
			return nil
		}
		return fmt.Errorf("compiler target %q does not match discovered package %q", event.Target.Name, pkg.Name)
	}
	return nil
}

func matchesDiscoveredTarget(repo repository.Repository, targets []Target, reported Target) bool {
	path, err := containedPath(repo, reported.Path)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(targets, func(target Target) bool {
		return reported.Name == target.Name && path == target.Path && slices.Equal(reported.Kinds, target.Kinds) && reported.Edition == target.Edition
	})
}

func validateCompiledPackages(manifest string, workspace Workspace, artifacts []CompiledTarget) error {
	selected := workspace.DefaultMembers
	if manifest != filepath.ToSlash(filepath.Join(workspace.Root, "Cargo.toml")) {
		selected = nil
		for _, pkg := range workspace.Packages {
			if pkg.Manifest == manifest {
				selected = []string{pkg.ID}
			}
		}
	}
	if len(selected) == 0 {
		return errors.New("cargo compilation selected no workspace packages")
	}
	for _, id := range selected {
		if !slices.ContainsFunc(artifacts, func(artifact CompiledTarget) bool {
			return artifact.PackageID == id && !slices.Contains(artifact.Target.Kinds, "custom-build")
		}) {
			return fmt.Errorf("cargo emitted no compiled target for selected package %q", id)
		}
	}
	return nil
}

func withinDirectory(root, path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
