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
	Provider        string               `json:"provider"`
	Inventory       []InventoryEntry     `json:"inventory"`
	Selection       SelectionInput       `json:"selection"`
	Scopes          []AnalysisScope      `json:"scopes"`
	DiagnosticFiles []string             `json:"diagnosticFiles"`
	WriteFiles      []string             `json:"writeFiles"`
	ProtocolVersion int                  `json:"protocolVersion"`
	Operation       string               `json:"operation"`
	Capability      string               `json:"capability"`
	ProjectRoot     string               `json:"projectRoot"`
	Files           []string             `json:"files"`
	Modules         []RequestModule      `json:"modules"`
	Mode            string               `json:"mode"`
	Profile         string               `json:"profile"`
	OutputDirectory string               `json:"outputDirectory,omitempty"`
	Context         []InputFile          `json:"context"`
	Policy          PolicyInput          `json:"policy"`
	Tools           []ToolIdentity       `json:"tools"`
	Complete        bool                 `json:"complete"`
	Pack            policy.PackSelection `json:"pack"`
}

type RequestModule struct {
	Name      string   `json:"name"`
	Paths     []string `json:"paths"`
	DependsOn []string `json:"dependsOn"`
}

type Response struct {
	ProtocolVersion int               `json:"protocolVersion"`
	Status          string            `json:"status"`
	ScopeHandles    []string          `json:"scopeHandles,omitempty"`
	Discovery       *DiscoveryResult  `json:"discovery,omitempty"`
	Evidence        []string          `json:"evidence,omitempty"`
	Findings        []ResponseFinding `json:"findings,omitempty"`
	Notes           []string          `json:"notes,omitempty"`
	Failure         string            `json:"failure,omitempty"`
	Coverage        *Coverage         `json:"coverage,omitempty"`
	Facts           *SourceFacts      `json:"facts,omitempty"`
	Inputs          []InputFile       `json:"inputs,omitempty"`
	Edits           []Edit            `json:"edits,omitempty"`
}

type SelectionInput struct {
	Paths    []string `json:"paths"`
	Deleted  []string `json:"deleted"`
	Complete bool     `json:"complete"`
}

type InventoryEntry struct {
	Path        string   `json:"path"`
	Language    string   `json:"language,omitempty"`
	Context     string   `json:"context,omitempty"`
	Owner       string   `json:"owner,omitempty"`
	Modules     []string `json:"modules"`
	Source      bool     `json:"source"`
	Metadata    bool     `json:"metadata"`
	Dependency  bool     `json:"dependency"`
	Asset       bool     `json:"asset"`
	Test        bool     `json:"test"`
	Generated   bool     `json:"generated"`
	Data        bool     `json:"data"`
	Development bool     `json:"development"`
	Control     bool     `json:"control"`
}

type AnalysisScope struct {
	Handle     string          `json:"handle"`
	Language   string          `json:"language"`
	Root       string          `json:"root"`
	Members    []string        `json:"members"`
	EntryFiles []string        `json:"entryFiles"`
	Context    []string        `json:"context"`
	Data       json.RawMessage `json:"data"`
}

type DiscoveryResult struct {
	Scopes []DiscoveredScope `json:"scopes"`
}

type DiscoveredScope struct {
	ID         string          `json:"id"`
	Language   string          `json:"language"`
	Root       string          `json:"root"`
	Members    []string        `json:"members"`
	EntryFiles []string        `json:"entryFiles"`
	Context    []string        `json:"context"`
	Selected   []string        `json:"selected"`
	Data       json.RawMessage `json:"data"`
}

type Edit struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type Result struct {
	Findings []policy.Finding
	Response Response
	Request  Request
	Digest   string
}

type ResponseFinding struct {
	Capability string `json:"capability"`
	Path       string `json:"path"`
	Line       int    `json:"line,omitempty"`
	Column     int    `json:"column,omitempty"`
	Subject    string `json:"subject"`
	Message    string `json:"message"`
	Rule       string `json:"rule"`
}

func RunAdapter(ctx context.Context, repo repository.Repository, selection repository.Selection, command policy.Command, commandRunner runner.Runner, profile string) Result {
	adapter := command.Adapter
	if adapter == nil {
		return Result{}
	}
	for _, selected := range selection.Files {
		if !packCommandSelects(repo, command, selected) || len(repo.Config.Checks) == 0 {
			continue
		}
		owner := repo.AnalysisOwner(selected, adapter.Capability, profile)
		if owner.Problem != "" {
			return failedResult(adapter, errors.New(owner.Problem))
		}
	}
	request := requestFor(repo, selection, command, profile)
	if len(request.Files) == 0 && !AdapterSelected(repo, selection, command, profile) {
		return Result{}
	}
	return runRequest(ctx, repo, command, commandRunner, request)
}

func runRequest(ctx context.Context, repo repository.Repository, command policy.Command, commandRunner runner.Runner, request Request) Result {
	adapter := command.Adapter
	if err := verifyAdapter(command); err != nil {
		return failedResult(adapter, err)
	}
	if len(request.Files) > maximumInventoryEntries || len(request.Modules) > 1000 {
		return failedResult(adapter, errors.New("adapter request exceeds its file or module count limit"))
	}
	prepared, identities, err := toolchainCommand(repo, command, adapter.Tools)
	if err != nil {
		return failedResult(adapter, err)
	}
	request.Tools = identities
	if hasProjectDiscovery(adapter) {
		request, _, err = discoveryRequest(ctx, repo, prepared, commandRunner, request)
		if err != nil {
			return failedResult(adapter, err)
		}
		if len(request.Files) == 0 {
			return Result{}
		}
		if err := verifyAdapter(command); err != nil {
			return failedResult(adapter, fmt.Errorf("adapter changed during discovery: %w", err))
		}
		prepared.InputDerivation = DiscoveryInputDerivation
	}
	if err := prepareInputs(repo, &request, command); err != nil {
		return failedResult(adapter, err)
	}
	response, err := execute(ctx, adapter.PackRoot, prepared, commandRunner, request)
	if err != nil {
		return failedResult(adapter, err)
	}
	if err := verifyExecution(repo, command, request, response); err != nil {
		return failedResult(adapter, err)
	}
	digest, err := analysisDigest(request, response)
	if err != nil {
		return failedResult(adapter, err)
	}
	return Result{Findings: analysisFindings(repo, adapter, response), Request: request, Response: response, Digest: digest}
}

func failedResult(adapter *policy.PackAdapter, err error) Result {
	return Result{Findings: []policy.Finding{packFailure(adapter, err)}}
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
	if len(data) > 64<<20 {
		return Response{}, errors.New("adapter request exceeds 64 MiB")
	}
	command.Stdin = append(data, '\n')
	result, output, runErr := boundary.RunStructured(ctx, root, command)
	if runErr != nil {
		return Response{}, fmt.Errorf("adapter process failed (%s): %w", runner.FailureCategoryFor(ctx, result, runErr), runErr)
	}
	return parseResponse(output.Stdout, request)
}

func parseResponse(data []byte, request Request) (Response, error) {
	if len(data) == 0 || len(data) > 16<<20 {
		return Response{}, errors.New("adapter response is empty or exceeds 16 MiB")
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
	if request.Operation == "discover" {
		return validateDiscoveryResponse(response, request)
	}
	if response.Discovery != nil {
		return expected("discovery", "omitted for capability responses")
	}
	if err := validateScopeHandles(response.ScopeHandles, request.Scopes); err != nil {
		return err
	}
	if err := validateAnalysisResponse(response, request); err != nil {
		return err
	}
	if err := validateEdits(response.Edits, response.Status, request); err != nil {
		return err
	}
	return validateResponseFindings(response.Findings, request)
}

func validateScopeHandles(handles []string, scopes []AnalysisScope) error {
	expectedHandles := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		expectedHandles = append(expectedHandles, scope.Handle)
	}
	if !slices.Equal(handles, expectedHandles) {
		return expected("scopeHandles", "the ordered handles authorized by the request")
	}
	return nil
}

func validateResponseEnvelope(response Response) error {
	if response.ProtocolVersion != ProtocolVersion {
		return expected("protocolVersion", fmt.Sprintf("%d", ProtocolVersion))
	}
	if !slices.Contains([]string{"pass", "findings", "incomplete", "operational-failure"}, response.Status) {
		return expected("status", "pass, findings, incomplete, or operational-failure")
	}
	if len(response.Notes) > 32 {
		return expected("notes", "at most 32 items")
	}
	if len(response.Evidence) > 64 {
		return expected("evidence", "at most 64 items")
	}
	if len(response.Findings) > 4096 {
		return expected("findings", "at most 4096 items")
	}
	if len(response.Failure) > 4096 {
		return expected("failure", "at most 4096 bytes")
	}
	return nil
}

func validateResponseText(response Response) error {
	if err := validateResponseStrings(response.Evidence, "evidence"); err != nil {
		return err
	}
	return validateResponseStrings(response.Notes, "notes")
}

func validateResponseStrings(values []string, path string) error {
	for index, value := range values {
		if strings.TrimSpace(value) == "" || len(value) > 1024 {
			return expected(indexed(path, index), "1 to 1024 non-whitespace bytes")
		}
	}
	return nil
}

func validateResponseStatus(response Response) error {
	switch response.Status {
	case "pass":
		if len(response.Evidence) == 0 {
			return expected("evidence", "at least one item when status is pass")
		}
		if responseHasFailure(response) {
			return expected("status", "findings or operational-failure when findings or failure are present")
		}
	case "findings":
		if len(response.Findings) == 0 {
			return expected("findings", "at least one item when status is findings")
		}
		if response.Failure != "" {
			return expected("failure", "empty when status is findings")
		}
	case "incomplete":
		return validateIncompleteStatus(response)
	case "operational-failure":
		if strings.TrimSpace(response.Failure) == "" {
			return expected("failure", "1 to 4096 non-whitespace bytes when status is operational-failure")
		}
		if len(response.Findings) != 0 {
			return expected("findings", "empty when status is operational-failure")
		}
	}
	return nil
}

func responseHasFailure(response Response) bool {
	return len(response.Findings) != 0 || response.Failure != ""
}

func validateIncompleteStatus(response Response) error {
	if response.Coverage == nil || len(response.Coverage.Unsupported) == 0 {
		return expected("coverage.unsupported", "at least one item when status is incomplete")
	}
	if response.Failure != "" {
		return expected("failure", "empty when status is incomplete")
	}
	return nil
}

func verifyExecution(repo repository.Repository, command policy.Command, request Request, response Response) error {
	if err := verifyAdapter(command); err != nil {
		return fmt.Errorf("adapter changed during execution: %w", err)
	}
	if err := verifyToolchainIdentity(repo, command, request.Tools, command.Adapter.Tools); err != nil {
		return err
	}
	if err := verifyAnalysisInputs(repo, request, response); err != nil {
		return err
	}
	return applyEdits(repo, request, response)
}

func verifyToolchainIdentity(repo repository.Repository, command policy.Command, identities []ToolIdentity, requested []policy.PackTool) error {
	_, current, err := toolchainCommand(repo, command, requested)
	if err != nil {
		return err
	}
	if !slices.Equal(current, identities) {
		return errors.New("toolchain changed during provider execution")
	}
	return nil
}

func validateResponseFindings(findings []ResponseFinding, request Request) error {
	for index, finding := range findings {
		if err := validateResponseFinding(finding, request, indexed("findings", index)); err != nil {
			return err
		}
	}
	return nil
}

func validateResponseFinding(finding ResponseFinding, request Request, label string) error {
	if finding.Capability != request.Capability {
		return expected(label+".capability", request.Capability)
	}
	if strings.TrimSpace(finding.Subject) == "" || len(finding.Subject) > 1024 {
		return expected(label+".subject", "1 to 1024 non-whitespace bytes")
	}
	if strings.TrimSpace(finding.Message) == "" || len(finding.Message) > 4096 {
		return expected(label+".message", "1 to 4096 non-whitespace bytes")
	}
	if !validRule(finding.Rule) {
		return expected(label+".rule", "a 1 to 256 byte rule identifier without whitespace, backslashes, or NUL")
	}
	if finding.Line < 0 {
		return expected(label+".line", "a non-negative integer")
	}
	if finding.Column < 0 || finding.Column > 0 && finding.Line == 0 {
		return expected(label+".column", "zero without a line or a positive UTF-8 byte column with a line")
	}
	if finding.Path == "repository" {
		return nil
	}
	if err := exactRelativePath(finding.Path); err != nil {
		return expected(label+".path", "repository or an exact contained relative path")
	}
	if !slices.Contains(request.DiagnosticFiles, finding.Path) {
		return expected(label+".path", "repository or a path from diagnosticFiles")
	}
	return nil
}

func requestFor(repo repository.Repository, selection repository.Selection, command policy.Command, profile string) Request {
	files := SelectedFiles(repo, selection, command, profile)
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
	return Request{Provider: command.Name, Tools: []ToolIdentity{}, ProtocolVersion: ProtocolVersion, Operation: operation, Capability: command.Adapter.Capability, ProjectRoot: repo.Root, Files: files, Selection: SelectionInput{Paths: selectionPaths(selection), Deleted: sortedUnique(selection.Candidate.Deleted), Complete: selection.All}, Modules: modules, Mode: mode, Profile: profile, Complete: selection.All, Pack: policy.PackSelection{Name: command.Adapter.PackName, Version: command.Adapter.PackVersion, Digest: command.Adapter.PackDigest}}
}

func packCommandSelects(repo repository.Repository, command policy.Command, selected string) bool {
	if !repo.CommandOwnsPath(command, selected) {
		return false
	}
	return packCapabilitySelects(repo, command.Adapter.Capability, selected)
}

func packCapabilitySelects(repo repository.Repository, capability, selected string) bool {
	if repo.IsData(selected) && slices.Contains([]string{"format", "lint", "typecheck", "complexity", "dead-code", "architecture"}, capability) {
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

func findingsForResponse(adapter *policy.PackAdapter, response Response) []policy.Finding {
	if response.Status == "operational-failure" {
		return []policy.Finding{packFailure(adapter, errors.New(response.Failure))}
	}
	findings := make([]policy.Finding, 0, len(response.Findings))
	for _, found := range response.Findings {
		findings = append(findings, policy.Finding{Check: "pack." + adapter.PackName + "." + found.Rule, Path: found.Path, Line: found.Line, Column: found.Column, Subject: found.Subject, Message: found.Message})
	}
	if response.Coverage != nil {
		for _, unsupported := range response.Coverage.Unsupported {
			findings = append(findings, policy.Finding{Check: "policy.packCoverage", Path: unsupported.Path, Subject: adapter.Capability, Message: unsupported.Reason})
		}
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
		if entry.Path == executable && (entry.Executable || slices.ContainsFunc(command.Adapter.Tools, func(tool policy.PackTool) bool { return tool.Launcher })) {
			return nil
		}
	}
	return errors.New("adapter executable is not recorded by the installation receipt")
}
