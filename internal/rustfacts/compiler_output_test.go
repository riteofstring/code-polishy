package rustfacts

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestCargoCompilerOutputRequiresCompleteTargetEvidence(t *testing.T) {
	t.Parallel()
	repo, workspace, artifact := compilerOutputFixture(t)
	for name, output := range map[string]string{
		"empty": "", "no finish": artifact, "no targets": `{"reason":"build-finished","success":true}`,
		"missing outcome":   artifact + "\n" + `{"reason":"build-finished"}`,
		"duplicate outcome": artifact + "\n" + `{"reason":"build-finished","success":true}` + "\n" + `{"reason":"build-finished","success":true}`,
		"arbitrary prose":   "Finished successfully\n", "unknown event": `{"reason":"ignored-check"}`,
		"foreign package": strings.ReplaceAll(artifact, "opaque::member#identity", "unknown-package") + "\n" + `{"reason":"build-finished","success":true}`,
		"wrong target":    strings.ReplaceAll(artifact, `"name":"app"`, `"name":"unselected"`) + "\n" + `{"reason":"build-finished","success":true}`,
		"oversized":       strings.Repeat(" ", 8<<20+1),
	} {
		t.Run(name, func(t *testing.T) {
			if report, err := ParseCompilation(repo, "Cargo.toml", workspace, []byte(output)); err == nil {
				t.Fatalf("incomplete evidence passed: %+v", report)
			}
		})
	}
	valid := artifact + "\n" + `{"reason":"build-finished","success":true,"future_field":3}`
	report, err := ParseCompilation(repo, "Cargo.toml", workspace, []byte(valid))
	if err != nil || !report.Success || len(report.Artifacts) != 1 || !report.Artifacts[0].Fresh {
		t.Fatalf("valid cached target evidence rejected: %+v, %v", report, err)
	}
}

func TestCargoCompilerDiagnosticsRetainMacroAndUnavailableLocations(t *testing.T) {
	t.Parallel()
	repo, workspace, _ := compilerOutputFixture(t)
	target := workspace.Packages[0].Targets[0]
	target.Path = filepath.Join(repo.Root, target.Path)
	for name, span := range map[string]compilerSpan{
		"macro":    {File: "/unavailable/macro.rs", Primary: true, Expansion: &compilerExpansion{Span: compilerSpan{File: "src/lib.rs", Line: 1, Column: 8}}},
		"external": {File: "/unavailable/dependency.rs", Line: 5, Column: 3, Primary: true},
	} {
		t.Run(name, func(t *testing.T) {
			event := compilerEvent{Reason: "compiler-message", PackageID: workspace.Packages[0].ID, Target: target, Message: compilerDiagnostic{Message: "seeded type error", Level: "error", Code: &compilerCode{Code: "E0308"}, Spans: []compilerSpan{span}}}
			findings, err := compilerFindings(repo, "Cargo.toml", workspace, event)
			if err != nil || len(findings) != 1 {
				t.Fatalf("diagnostic was discarded: %+v, %v", findings, err)
			}
			finding := findings[0]
			if name == "macro" && (finding.Path != "src/lib.rs" || finding.Line != 1 || finding.Column != 8) {
				t.Fatalf("macro invocation was not used: %+v", finding)
			}
			if name == "external" && (finding.Path != "Cargo.toml" || finding.Line != 0 || finding.Fields["reportedSource"] != span.File) {
				t.Fatalf("unavailable location was invented: %+v", finding)
			}
		})
	}
}

func TestCargoCompilerOutputRetainsDiagnosticsBeforeTruncation(t *testing.T) {
	t.Parallel()
	repo, workspace, _ := compilerOutputFixture(t)
	target := workspace.Packages[0].Targets[0]
	target.Path = filepath.Join(repo.Root, target.Path)
	event := compilerEvent{Reason: "compiler-message", PackageID: workspace.Packages[0].ID, Target: target, Message: compilerDiagnostic{Message: "actual error", Level: "error", Code: &compilerCode{Code: "E0308"}}}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	report, err := ParseCompilation(repo, "Cargo.toml", workspace, append(data, []byte("\n{\"reason\":")...))
	if err == nil || len(report.Findings) != 1 || report.Findings[0].Subject != "E0308" || report.Success {
		t.Fatalf("partial compiler output: %+v, %v", report, err)
	}
}

func compilerOutputFixture(t *testing.T) (repository.Repository, Workspace, string) {
	t.Helper()
	repo, wire := metadataFixture(t)
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := ParseWorkspace(repo, "Cargo.toml", data)
	if err != nil {
		t.Fatal(err)
	}
	fresh := true
	event := compilerEvent{Reason: "compiler-artifact", PackageID: workspace.Packages[0].ID, Target: wire.Packages[0].Targets[0], Features: []string{}, Fresh: &fresh}
	data, err = json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return repo, workspace, string(data)
}
