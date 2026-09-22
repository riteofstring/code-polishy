package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

const protocolVersion = 4
const maximumRequestBytes = 64 << 20
const maximumInputBytes = 16 << 20
const maximumShellBytes = 4 << 20
const maximumInputs = 10000

type shellCheckExecutor interface {
	check(context.Context, map[string][]byte, []shellSource) ([]responseFinding, error)
}

type adapter struct {
	checker shellCheckExecutor
}

func newAdapter() adapter {
	return adapter{checker: osShellCheck{executable: os.Getenv("CODE_POLISHY_TOOL_SHELLCHECK")}}
}

func decodeRequest(reader io.Reader) (request, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maximumRequestBytes+1))
	if err != nil {
		return request{}, err
	}
	if len(data) == 0 || len(data) > maximumRequestBytes {
		return request{}, errors.New("request is empty or exceeds 64 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	value := request{}
	if err := decoder.Decode(&value); err != nil {
		return request{}, fmt.Errorf("decode request: %w", err)
	}
	if err := requireEnd(decoder); err != nil {
		return request{}, err
	}
	if err := validateRequest(value); err != nil {
		return request{}, err
	}
	return value, nil
}

func requireEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request contains more than one JSON value")
	}
	return nil
}

func validateRequest(value request) error {
	if value.ProtocolVersion != protocolVersion {
		return fmt.Errorf("protocolVersion must be %d", protocolVersion)
	}
	if value.Operation != "discover" && value.Operation != "check" {
		return errors.New("operation must be discover or check")
	}
	if value.Operation == "check" && value.Capability != "lint" && value.Capability != "typecheck" {
		return errors.New("capability must be lint or typecheck")
	}
	return nil
}

func (adapter adapter) run(ctx context.Context, request request) response {
	if request.Operation == "discover" {
		return discover(request)
	}
	result, err := adapter.analyze(ctx, request)
	if err == nil {
		return result
	}
	return response{
		ProtocolVersion: protocolVersion,
		Status:          "operational-failure",
		ScopeHandles:    scopeHandles(request.Scopes),
		Failure:         boundedFailure(err),
	}
}

func discover(request request) response {
	members := []string{}
	for _, entry := range request.Inventory {
		if entry.Source && entry.Language == "shell" && entry.Owner == request.Provider {
			members = append(members, entry.Path)
		}
	}
	members = uniqueSorted(members)
	selected := []string{}
	for _, file := range request.Files {
		if slices.Contains(members, file) {
			selected = append(selected, file)
		}
	}
	if len(members) == 0 {
		return response{ProtocolVersion: protocolVersion, Status: "operational-failure", Failure: "discovery found no provider-owned shell source"}
	}
	return response{
		ProtocolVersion: protocolVersion,
		Status:          "pass",
		Evidence:        []string{"static shell discovery grouped the declared source inventory"},
		Discovery: &discoveryResult{Scopes: []discoveredScope{{
			ID: "shell-repository", Language: "shell", Root: ".", Members: members,
			EntryFiles: uniqueSorted(selected), Context: members, Selected: uniqueSorted(selected), Data: json.RawMessage(`{"dialects":"per-file"}`),
		}}},
		Inputs: []inputFile{},
	}
}

func (adapter adapter) analyze(ctx context.Context, request request) (response, error) {
	if request.Capability == "lint" {
		if err := validateShellCheckTool(request.Tools); err != nil {
			return response{}, err
		}
	}
	data, inputs, err := readAuthorizedSources(request)
	if err != nil {
		return response{}, err
	}
	state := analysisState{request: request, data: data, result: response{
		ProtocolVersion: protocolVersion,
		Status:          "pass",
		ScopeHandles:    scopeHandles(request.Scopes),
		Evidence:        []string{"the shell pack parsed every analyzed source with its declared dialect"},
		Coverage:        &coverage{Analyzed: []string{}, Unsupported: []unsupported{}},
		Inputs:          inputs,
		Findings:        []responseFinding{},
	}, comments: []commentFact{}, literals: []literalFact{}, sources: []shellSource{}}
	for _, file := range uniqueSorted(request.Files) {
		if err := state.add(file); err != nil {
			return response{}, err
		}
	}
	if request.Capability == "lint" {
		if err := state.lint(ctx, adapter.checker); err != nil {
			return response{}, err
		}
	}
	return state.finish(), nil
}

type analysisState struct {
	request  request
	data     map[string][]byte
	result   response
	comments []commentFact
	literals []literalFact
	sources  []shellSource
}

func (state *analysisState) add(file string) error {
	source, found := state.data[file]
	if !found {
		return fmt.Errorf("selected source %s is outside the authorized context", file)
	}
	if len(source) > maximumShellBytes || !utf8.Valid(source) {
		state.result.Coverage.Unsupported = append(state.result.Coverage.Unsupported, unsupported{Path: file, Reason: "shell source exceeds the 4 MiB parser limit or is not valid UTF-8"})
		return nil
	}
	dialect := dialectFor(file, source)
	state.sources = append(state.sources, shellSource{Path: file, Data: source, Dialect: dialect})
	parsed, err := parseSource(file, source, dialect, state.request.Inventory)
	if err != nil {
		state.addSyntaxFailure(file, err)
		return nil
	}
	if state.request.Capability == "lint" && !state.addFacts(parsed) {
		state.result.Coverage.Unsupported = append(state.result.Coverage.Unsupported, unsupported{Path: file, Reason: "shell source facts exceed the protocol collection limit"})
		return nil
	}
	state.result.Coverage.Analyzed = append(state.result.Coverage.Analyzed, file)
	return nil
}

func (state *analysisState) addSyntaxFailure(file string, err error) {
	state.result.Findings = append(state.result.Findings, syntaxFinding(state.request.Capability, file, err))
	if state.request.Capability == "lint" {
		state.result.Coverage.Unsupported = append(state.result.Coverage.Unsupported, unsupported{Path: file, Reason: "source-comment and portability facts require valid shell syntax"})
		return
	}
	state.result.Coverage.Analyzed = append(state.result.Coverage.Analyzed, file)
}

func (state *analysisState) addFacts(parsed parsedSource) bool {
	if len(state.comments)+len(parsed.Comments) > 20000 || len(state.literals)+len(parsed.Literals) > 20000 {
		return false
	}
	state.comments = append(state.comments, parsed.Comments...)
	state.literals = append(state.literals, parsed.Literals...)
	return true
}

func (state *analysisState) lint(ctx context.Context, checker shellCheckExecutor) error {
	findings, err := checker.check(ctx, state.data, state.sources)
	if err != nil {
		return err
	}
	state.result.Findings = append(state.result.Findings, findings...)
	if len(state.result.Findings) > 4096 {
		return errors.New("shell diagnostics exceed the protocol collection limit")
	}
	state.result.Facts = &sourceFacts{Comments: &state.comments, Literals: &state.literals}
	state.result.Evidence = append(state.result.Evidence, "ShellCheck 0.11.0 analyzed selected source in an isolated materialized context")
	return nil
}

func (state *analysisState) finish() response {
	sortFindings(state.result.Findings)
	if len(state.result.Coverage.Unsupported) > 0 {
		state.result.Status = "incomplete"
	} else if len(state.result.Findings) > 0 {
		state.result.Status = "findings"
	}
	return state.result
}

func validateShellCheckTool(tools []toolIdentity) error {
	if !slices.ContainsFunc(tools, func(tool toolIdentity) bool {
		return tool.ID == "shellcheck" && tool.Name == "shellcheck" && tool.Version == "0.11.0" && validDigest(tool.SHA256)
	}) {
		return errors.New("request does not bind shellcheck 0.11.0")
	}
	if strings.TrimSpace(os.Getenv("CODE_POLISHY_TOOL_SHELLCHECK")) == "" {
		return errors.New("CODE_POLISHY_TOOL_SHELLCHECK is unavailable")
	}
	return nil
}

func readAuthorizedSources(request request) (map[string][]byte, []inputFile, error) {
	paths, err := authorizedSourcePaths(request)
	if err != nil {
		return nil, nil, err
	}
	expected, err := contextIdentities(request.Context)
	if err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(request.ProjectRoot)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	data := make(map[string][]byte, len(paths))
	inputs := make([]inputFile, 0, len(paths))
	for _, file := range paths {
		contents, got, err := readVerifiedInput(root, file, expected[file])
		if err != nil {
			return nil, nil, err
		}
		data[file] = contents
		inputs = append(inputs, inputFile{Path: file, SHA256: got})
	}
	return data, inputs, nil
}

func authorizedSourcePaths(request request) ([]string, error) {
	authorized := map[string]bool{}
	for _, file := range request.Files {
		authorized[file] = true
	}
	for _, scope := range request.Scopes {
		for _, file := range scope.Context {
			authorized[file] = true
		}
	}
	if len(authorized) == 0 || len(authorized) > maximumInputs {
		return nil, fmt.Errorf("authorized shell context must contain 1 to %d files", maximumInputs)
	}
	paths := make([]string, 0, len(authorized))
	for file := range authorized {
		paths = append(paths, file)
	}
	sort.Strings(paths)
	return paths, nil
}

func contextIdentities(context []inputFile) (map[string]string, error) {
	expected := map[string]string{}
	for _, input := range context {
		if _, found := expected[input.Path]; found {
			return nil, fmt.Errorf("context repeats %s", input.Path)
		}
		expected[input.Path] = input.SHA256
	}
	return expected, nil
}

func readVerifiedInput(root *os.Root, file, want string) ([]byte, string, error) {
	if err := exactPath(file); err != nil {
		return nil, "", err
	}
	if !validDigest(want) {
		return nil, "", fmt.Errorf("authorized source %s has no request identity", file)
	}
	contents, err := readInput(root, file)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(contents)
	got := hex.EncodeToString(digest[:])
	if got != want {
		return nil, "", fmt.Errorf("authorized source %s changed", file)
	}
	return contents, got, nil
}

func readInput(root *os.Root, name string) ([]byte, error) {
	file, err := root.Open(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maximumInputBytes {
		return nil, fmt.Errorf("input %s must be a regular file of at most 16 MiB", name)
	}
	contents, err := io.ReadAll(io.LimitReader(file, maximumInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maximumInputBytes {
		return nil, fmt.Errorf("input %s exceeds 16 MiB", name)
	}
	return contents, nil
}

func exactPath(value string) error {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || path.Clean(value) != value || value == "." || value == ".." || strings.HasPrefix(value, "../") {
		return fmt.Errorf("%q is not an exact contained relative path", value)
	}
	return nil
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func scopeHandles(scopes []analysisScope) []string {
	result := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		result = append(result, scope.Handle)
	}
	return result
}

func boundedFailure(err error) string {
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "shell adapter failed"
	}
	if len(message) > 4096 {
		message = message[:4096]
	}
	return message
}

func uniqueSorted(values []string) []string {
	result := slices.Clone(values)
	sort.Strings(result)
	return slices.Compact(result)
}

func sortFindings(findings []responseFinding) {
	sort.SliceStable(findings, func(left, right int) bool {
		first := findings[left]
		second := findings[right]
		return fmt.Sprintf("%s\x00%09d\x00%09d\x00%s", first.Path, first.Line, first.Column, first.Rule) < fmt.Sprintf("%s\x00%09d\x00%09d\x00%s", second.Path, second.Line, second.Column, second.Rule)
	})
}
