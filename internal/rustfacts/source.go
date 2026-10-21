package rustfacts

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/pythonfacts"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

const SourceProtocol = "rust-source/v2"

const sourceRequestPrefix = `{"protocol":"` + SourceProtocol + `","sources":[`

const maximumSourceRequest = 8 << 20

const sourceRequestOverhead = len(sourceRequestPrefix) + len("]}")

//go:embed source_parser.py
var sourceParser string

type SourceInput struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

type SourcePlan struct {
	Inputs    []SourceInput
	Commands  []policy.Command
	Findings  []policy.Finding
	batchEnds []int
}

type Sources struct {
	Files    []Source
	Findings []policy.Finding
}

func PlanSources(repo repository.Repository, paths []string) SourcePlan {
	plan := SourcePlan{}
	encoded := [][]byte{}
	requestSize := sourceRequestOverhead
	for _, path := range paths {
		if !sourceAnalysisSelected(repo, path) {
			continue
		}
		input, data, err := readSourceInput(repo, path)
		if err != nil {
			plan.Findings = append(plan.Findings, sourceFinding(path, err.Error()))
			continue
		}
		separator := 0
		if len(encoded) > 0 {
			separator = 1
		}
		if len(encoded) == 4096 || requestSize+separator+len(data) > maximumSourceRequest {
			plan.appendBatch(repo, encoded, requestSize)
			encoded = nil
			requestSize = sourceRequestOverhead
			separator = 0
		}
		plan.Inputs = append(plan.Inputs, input)
		encoded = append(encoded, data)
		requestSize += separator + len(data)
	}
	plan.appendBatch(repo, encoded, requestSize)
	return plan
}

func readSourceInput(repo repository.Repository, path string) (SourceInput, []byte, error) {
	data, err := repo.ReadContainedFile(path, 4<<20)
	if err != nil {
		return SourceInput{}, nil, err
	}
	if !utf8.Valid(data) {
		return SourceInput{}, nil, errors.New("rust source is not UTF-8 text")
	}
	input := SourceInput{Path: path, Source: string(data)}
	encoded, err := json.Marshal(input)
	if err != nil {
		return SourceInput{}, nil, err
	}
	if len(encoded)+sourceRequestOverhead > maximumSourceRequest {
		return SourceInput{}, nil, errors.New("encoded Rust source exceeds the 8388608-byte syntax request limit")
	}
	return input, encoded, nil
}

func (plan *SourcePlan) appendBatch(repo repository.Repository, encoded [][]byte, size int) {
	if len(encoded) == 0 {
		return
	}
	request := make([]byte, 0, size)
	request = append(request, sourceRequestPrefix...)
	request = append(request, bytes.Join(encoded, []byte(","))...)
	request = append(request, ']', '}')
	stdin, err := sourceCommandInput(request)
	if err != nil {
		start := len(plan.Inputs) - len(encoded)
		plan.Findings = append(plan.Findings, sourceBatchFindings(plan.Inputs[start:], err.Error())...)
		plan.Inputs = plan.Inputs[:start]
		return
	}
	name := "policy-rust-source-facts"
	if len(plan.Commands) > 0 {
		name += fmt.Sprintf("-%d", len(plan.Commands)+1)
	}
	plan.Commands = append(plan.Commands, policy.Command{Name: name, Argv: repo.PythonCommand(pythonfacts.ProgramBootstrap), Cwd: ".", Stdin: stdin, Managed: true, SealedEnvironment: true, TimeoutSeconds: 60})
	plan.batchEnds = append(plan.batchEnds, len(plan.Inputs))
}

func sourceCommandInput(request []byte) ([]byte, error) {
	input, err := pythonfacts.ProgramInput(sourceParser, bytes.NewReader(request))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(input)
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
	start := 0
	for index, command := range plan.Commands {
		end := plan.batchEnds[index]
		batch := readSourceBatch(ctx, repo, plan.Inputs[start:end], command, boundary)
		result.Files = append(result.Files, batch.Files...)
		result.Findings = append(result.Findings, batch.Findings...)
		start = end
	}
	return result
}

func readSourceBatch(ctx context.Context, repo repository.Repository, inputs []SourceInput, command policy.Command, boundary runner.Runner) Sources {
	if err := ctx.Err(); err != nil {
		return Sources{Findings: sourceBatchFindings(inputs, "Rust syntax analysis did not run: "+err.Error())}
	}
	output := sourceStream{repo: repo, inputs: inputs}
	diagnostics := sourceDiagnostics{}
	_, err := runner.RunStreamed(ctx, boundary, repo.Root, command, &output, &diagnostics)
	if err != nil {
		return Sources{Findings: sourceBatchFindings(inputs, fmt.Sprintf("Rust syntax analysis failed: %v; %s", err, diagnosticOutput(diagnostics.Bytes())))}
	}
	sources, err := output.finish()
	if err != nil {
		return Sources{Findings: sourceBatchFindings(inputs, "invalid Rust syntax evidence: "+err.Error())}
	}
	result := Sources{}
	for _, source := range sources {
		if source.Complete {
			result.Files = append(result.Files, source)
		} else {
			result.Findings = append(result.Findings, sourceFinding(source.Path, source.Error))
		}
	}
	return result
}

func sourceBatchFindings(inputs []SourceInput, message string) []policy.Finding {
	findings := make([]policy.Finding, 0, len(inputs))
	for _, input := range inputs {
		findings = append(findings, sourceFinding(input.Path, message))
	}
	return findings
}

func sourceFinding(path, reason string) policy.Finding {
	return policy.Finding{Check: "policy.rustSource", Path: path, Subject: "syntax", Message: reason}
}
