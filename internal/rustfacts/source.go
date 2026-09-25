package rustfacts

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

const SourceProtocol = "rust-source/v1"

//go:embed source_parser.py
var sourceParser string

type SourceInput struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

type SourcePlan struct {
	Inputs   []SourceInput
	Commands []policy.Command
	Findings []policy.Finding
}

type Sources struct {
	Files    []Source
	Findings []policy.Finding
}

func PlanSources(repo repository.Repository, paths []string) SourcePlan {
	plan := SourcePlan{}
	for _, path := range paths {
		if !sourceAnalysisSelected(repo, path) {
			continue
		}
		data, err := repo.ReadContainedFile(path, 4<<20)
		if err != nil {
			plan.Findings = append(plan.Findings, sourceFinding(path, err.Error()))
			continue
		}
		if !utf8.Valid(data) {
			plan.Findings = append(plan.Findings, sourceFinding(path, "Rust source is not UTF-8 text"))
			continue
		}
		plan.Inputs = append(plan.Inputs, SourceInput{Path: path, Source: string(data)})
	}
	if len(plan.Inputs) == 0 {
		return plan
	}
	request, err := json.Marshal(struct {
		Protocol string        `json:"protocol"`
		Sources  []SourceInput `json:"sources"`
	}{SourceProtocol, plan.Inputs})
	if err != nil || len(request) > 8<<20 || len(plan.Inputs) > 4096 {
		plan.Findings = append(plan.Findings, sourceFinding(plan.Inputs[0].Path, "Rust syntax request exceeds 8388608 bytes or 4096 sources; select a smaller source scope"))
		return plan
	}
	plan.Commands = []policy.Command{{Name: "policy-rust-source-facts", Argv: repo.PythonCommand(sourceParser), Cwd: ".", Stdin: request, Managed: true, SealedEnvironment: true, TimeoutSeconds: 60}}
	return plan
}

func sourceAnalysisSelected(repo repository.Repository, path string) bool {
	if repo.Language(path) != "rust" || repo.IsGenerated(path) || repo.IsData(path) {
		return false
	}
	return repo.NativeAnalysis(path, "lint") || repo.NativeAnalysis(path, "complexity") || repo.NativeAnalysis(path, "format") || repo.NativeAnalysis(path, "architecture")
}

func ReadSources(ctx context.Context, repo repository.Repository, plan SourcePlan, boundary runner.Runner) Sources {
	result := Sources{Findings: slices.Clone(plan.Findings)}
	if len(plan.Commands) == 0 {
		return result
	}
	structured, ok := boundary.(runner.StructuredRunner)
	if !ok {
		result.Findings = append(result.Findings, sourceFinding(plan.Inputs[0].Path, "the runner cannot capture bounded Rust syntax facts"))
		return result
	}
	_, output, err := structured.RunStructured(ctx, repo.Root, plan.Commands[0])
	if err != nil {
		result.Findings = append(result.Findings, sourceFinding(plan.Inputs[0].Path, fmt.Sprintf("Rust syntax analysis failed: %v; %s", err, diagnosticOutput(output.Stderr))))
		return result
	}
	sources, err := ParseSources(repo, plan.Inputs, output.Stdout)
	if err != nil {
		result.Findings = append(result.Findings, sourceFinding(plan.Inputs[0].Path, "invalid Rust syntax evidence: "+err.Error()))
		return result
	}
	for _, source := range sources {
		if source.Complete {
			result.Files = append(result.Files, source)
		} else {
			result.Findings = append(result.Findings, sourceFinding(source.Path, source.Error))
		}
	}
	return result
}

func sourceFinding(path, reason string) policy.Finding {
	return policy.Finding{Check: "policy.rustSource", Path: path, Subject: "syntax", Message: reason}
}
