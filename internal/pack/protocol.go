package pack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type Request struct {
	ProtocolVersion int             `json:"protocolVersion"`
	Operation       string          `json:"operation"`
	Capability      string          `json:"capability"`
	ProjectRoot     string          `json:"projectRoot"`
	Files           []string        `json:"files"`
	Modules         []RequestModule `json:"modules"`
	Mode            string          `json:"mode"`
	Profile         string          `json:"profile"`
	OutputDirectory string          `json:"outputDirectory,omitempty"`
}

type RequestModule struct {
	Name      string   `json:"name"`
	Paths     []string `json:"paths"`
	DependsOn []string `json:"dependsOn"`
}

type Response struct {
	ProtocolVersion int               `json:"protocolVersion"`
	Status          string            `json:"status"`
	Evidence        []string          `json:"evidence,omitempty"`
	Findings        []ResponseFinding `json:"findings,omitempty"`
	Notes           []string          `json:"notes,omitempty"`
	Failure         string            `json:"failure,omitempty"`
}

type ResponseFinding struct {
	Capability string `json:"capability"`
	Path       string `json:"path"`
	Line       int    `json:"line,omitempty"`
	Column     int    `json:"column,omitempty"`
	Subject    string `json:"subject"`
	Message    string `json:"message"`
}

func RunAdapter(ctx context.Context, repo repository.Repository, selection repository.Selection, command policy.Command, commandRunner runner.Runner, profile string) []policy.Finding {
	adapter := command.Adapter
	if adapter == nil {
		return nil
	}
	request := requestFor(repo, selection, command, profile)
	if len(request.Files) == 0 && (adapter.Capability == "format" || adapter.Capability == "complexity") {
		return nil
	}
	if err := verifyAdapter(command); err != nil {
		return []policy.Finding{packFailure(adapter, err)}
	}
	if len(request.Files) > 10000 || len(request.Modules) > 1000 {
		return []policy.Finding{packFailure(adapter, errors.New("adapter request exceeds its file or module count limit"))}
	}
	response, err := execute(ctx, adapter.PackRoot, command, commandRunner, request)
	if err != nil {
		return []policy.Finding{packFailure(adapter, err)}
	}
	if err := verifyAdapter(command); err != nil {
		return []policy.Finding{packFailure(adapter, fmt.Errorf("adapter changed during execution: %w", err))}
	}
	return findingsForResponse(adapter, response)
}

func execute(ctx context.Context, root string, command policy.Command, commandRunner runner.Runner, request Request) (Response, error) {
	boundary, ok := commandRunner.(runner.StructuredRunner)
	if !ok {
		return Response{}, errors.New("governed runner cannot capture adapter output")
	}
	data, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}
	command.Stdin = append(data, '\n')
	result, output, runErr := boundary.RunStructured(ctx, root, command)
	if runErr != nil {
		return Response{}, fmt.Errorf("adapter process failed (%s): %w", runner.FailureCategoryFor(ctx, result, runErr), runErr)
	}
	return parseResponse(output.Stdout, request)
}

func parseResponse(data []byte, request Request) (Response, error) {
	if len(data) == 0 || len(data) > 1<<20 {
		return Response{}, errors.New("adapter response is empty or exceeds 1 MiB")
	}
	response, err := decodeResponse(data)
	if err != nil {
		return Response{}, err
	}
	if err := validateResponse(response, request); err != nil {
		return Response{}, err
	}
	return response, nil
}

func decodeResponse(data []byte) (Response, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	response := Response{}
	if err := decoder.Decode(&response); err != nil {
		return Response{}, fmt.Errorf("decode adapter response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Response{}, errors.New("adapter returned more than one JSON response")
	}
	return response, nil
}

func validateResponse(response Response, request Request) error {
	if err := validateResponseEnvelope(response); err != nil {
		return err
	}
	if err := validateResponseText(response); err != nil {
		return err
	}
	if err := validateResponseStatus(response); err != nil {
		return err
	}
	return validateResponseFindings(response.Findings, request)
}

func validateResponseEnvelope(response Response) error {
	if response.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("adapter returned unsupported protocol version %d", response.ProtocolVersion)
	}
	if !slices.Contains([]string{"pass", "findings", "operational-failure"}, response.Status) {
		return fmt.Errorf("adapter returned unknown status %q", response.Status)
	}
	if len(response.Notes) > 32 {
		return errors.New("adapter returned too many notes")
	}
	if len(response.Evidence) > 64 || len(response.Findings) > 4096 || len(response.Failure) > 4096 {
		return errors.New("adapter response exceeds its evidence, finding, or failure limit")
	}
	return nil
}

func validateResponseText(response Response) error {
	if err := validateResponseStrings(response.Evidence, "evidence"); err != nil {
		return err
	}
	return validateResponseStrings(response.Notes, "note")
}

func validateResponseStrings(values []string, label string) error {
	for _, value := range values {
		if strings.TrimSpace(value) == "" || len(value) > 1024 {
			return fmt.Errorf("adapter returned empty or oversized %s", label)
		}
	}
	return nil
}

func validateResponseStatus(response Response) error {
	switch response.Status {
	case "pass":
		if len(response.Evidence) == 0 || len(response.Findings) != 0 || response.Failure != "" {
			return errors.New("pass response requires evidence and no findings or failure")
		}
	case "findings":
		if len(response.Findings) == 0 || response.Failure != "" {
			return errors.New("findings response requires at least one finding and no failure")
		}
	case "operational-failure":
		if strings.TrimSpace(response.Failure) == "" || len(response.Findings) != 0 {
			return errors.New("operational-failure response requires a failure and no findings")
		}
	}
	return nil
}

func validateResponseFindings(findings []ResponseFinding, request Request) error {
	for _, finding := range findings {
		if err := validateResponseFinding(finding, request); err != nil {
			return err
		}
	}
	return nil
}

func validateResponseFinding(finding ResponseFinding, request Request) error {
	if malformedResponseFinding(finding, request.Capability) {
		return errors.New("adapter returned a malformed finding")
	}
	if finding.Path == "repository" {
		return nil
	}
	if err := exactRelativePath(finding.Path); err != nil {
		return fmt.Errorf("adapter finding path: %w", err)
	}
	if !slices.Contains(request.Files, finding.Path) {
		return fmt.Errorf("adapter finding path %q was not selected", finding.Path)
	}
	return nil
}

func malformedResponseFinding(finding ResponseFinding, capability string) bool {
	missing := finding.Capability != capability || strings.TrimSpace(finding.Subject) == "" || strings.TrimSpace(finding.Message) == ""
	oversized := len(finding.Subject) > 1024 || len(finding.Message) > 4096
	invalidLocation := finding.Line < 0 || finding.Column < 0 || finding.Column > 0 && finding.Line == 0
	return missing || oversized || invalidLocation
}

func requestFor(repo repository.Repository, selection repository.Selection, command policy.Command, profile string) Request {
	files := []string{}
	for _, selected := range selection.Files {
		if packCommandSelects(repo, command, selected) {
			files = append(files, selected)
		}
	}
	modules := make([]RequestModule, 0, len(repo.Config.Modules))
	for _, module := range repo.Config.Modules {
		modules = append(modules, RequestModule{Name: module.Name, Paths: slices.Clone(module.Paths), DependsOn: slices.Clone(module.DependsOn)})
	}
	operation := "check"
	mode := "check"
	if command.Adapter.Capability == "format" {
		operation = "format"
		if profile == "format" {
			mode = "write"
		}
	}
	return Request{ProtocolVersion: ProtocolVersion, Operation: operation, Capability: command.Adapter.Capability, ProjectRoot: repo.Root, Files: files, Modules: modules, Mode: mode, Profile: profile}
}

func packCommandSelects(repo repository.Repository, command policy.Command, selected string) bool {
	if (len(command.Paths) > 0 && !policy.MatchesAny(selected, command.Paths)) || !moduleMatches(repo, command.Modules, selected) {
		return false
	}
	return packCapabilitySelects(repo, command.Adapter.Capability, selected)
}

func packCapabilitySelects(repo repository.Repository, capability, selected string) bool {
	if repo.IsData(selected) && capability == "format" {
		return false
	}
	if !repo.IsGenerated(selected) {
		return true
	}
	if capability == "complexity" {
		return false
	}
	return capability != "format"
}

func moduleMatches(repo repository.Repository, modules []string, selected string) bool {
	if len(modules) == 0 {
		return true
	}
	for _, owner := range repo.OwnerModuleNames(selected) {
		if slices.Contains(modules, owner) {
			return true
		}
	}
	return false
}

func findingsForResponse(adapter *policy.PackAdapter, response Response) []policy.Finding {
	if response.Status == "operational-failure" {
		return []policy.Finding{packFailure(adapter, errors.New(response.Failure))}
	}
	findings := make([]policy.Finding, 0, len(response.Findings))
	for _, found := range response.Findings {
		findings = append(findings, policy.Finding{Check: "pack." + found.Capability, Path: found.Path, Line: found.Line, Column: found.Column, Subject: found.Subject, Message: found.Message})
	}
	return findings
}

func packFailure(adapter *policy.PackAdapter, err error) policy.Finding {
	return policy.Finding{Check: "policy.packOperation", Path: policy.ConfigFilename, Subject: adapter.PackName + ":" + adapter.Capability, Message: err.Error()}
}

func verifyAdapter(command policy.Command) error {
	receipt, err := VerifyInstalled(command.Adapter.PackRoot)
	if err != nil {
		return err
	}
	if receipt.Name != command.Adapter.PackName || receipt.Version != command.Adapter.PackVersion || receipt.Digest != command.Adapter.PackDigest {
		return errors.New("installed adapter identity changed")
	}
	executable := path.Clean(strings.ReplaceAll(command.Argv[0], "\\", "/"))
	for _, entry := range receipt.Files {
		if entry.Path == executable && entry.Executable {
			return nil
		}
	}
	return errors.New("adapter executable is not recorded by the installation receipt")
}
