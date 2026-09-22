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
const maximumPythonBytes = 4 << 20
const maximumInputs = 10000

type adapter struct {
	ruff    ruffExecutor
	facts   factExecutor
	project projectExecutor
	ty      tyExecutor
}

func newAdapter() adapter {
	executable, _ := os.Executable()
	packRoot := filepath.Dir(filepath.Dir(executable))
	return adapter{
		ruff: osRuff{executable: os.Getenv("CODE_POLISHY_TOOL_RUFF")},
		facts: osPythonFacts{
			executable: os.Getenv("CODE_POLISHY_TOOL_PYTHON"),
			script:     filepath.Join(packRoot, "lib", "facts.py"),
		},
		project: osPythonProject{
			executable: os.Getenv("CODE_POLISHY_TOOL_PYTHON"),
			script:     filepath.Join(packRoot, "lib", "project.py"),
		},
		ty: osTy{executable: os.Getenv("CODE_POLISHY_TOOL_TY"), config: filepath.Join(packRoot, "config", "ty.toml")},
	}
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
	if value.Operation == "discover" {
		return nil
	}
	if slices.Contains([]string{"lint", "complexity", "typecheck", "architecture"}, value.Capability) && value.Operation == "check" {
		return nil
	}
	if value.Capability == "format" && value.Operation == "format" {
		return nil
	}
	return errors.New("operation and capability must be discover, a supported check capability, or format/format")
}

func (adapter adapter) run(ctx context.Context, request request) response {
	if request.Operation == "discover" {
		return discover(ctx, request, adapter.project)
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

func (adapter adapter) analyze(ctx context.Context, request request) (response, error) {
	data, inputs, err := readAuthorizedInputs(request)
	if err != nil {
		return response{}, err
	}
	workspace, err := materializeInputs(data)
	if err != nil {
		return response{}, err
	}
	defer os.RemoveAll(workspace)
	state := analysisState{
		request: request,
		data:    data,
		result: response{
			ProtocolVersion: protocolVersion,
			Status:          "pass",
			ScopeHandles:    scopeHandles(request.Scopes),
			Coverage:        &coverage{Analyzed: []string{}, Unsupported: []unsupported{}},
			Inputs:          inputs,
			Findings:        []responseFinding{},
		},
	}
	groups, err := state.analysisGroups()
	if err != nil {
		return response{}, err
	}
	if slices.Contains([]string{"typecheck", "architecture"}, request.Capability) {
		groups, err = state.scopeMemberGroups()
		if err != nil {
			return response{}, err
		}
	}
	groups, err = state.rejectInvalidProjects(groups)
	if err != nil {
		return response{}, err
	}
	if len(groups) == 0 {
		return state.finish(), nil
	}
	if err := validateTools(request); err != nil {
		return response{}, err
	}
	if err := state.executeCapability(ctx, adapter, workspace, groups); err != nil {
		return response{}, err
	}
	return state.finish(), nil
}

func (state *analysisState) rejectInvalidProjects(groups []analysisGroup) ([]analysisGroup, error) {
	valid := make([]analysisGroup, 0, len(groups))
	for _, group := range groups {
		data, err := decodePythonScopeData(group.scope.Data)
		if err != nil {
			return nil, err
		}
		if len(data.Problems) == 0 {
			valid = append(valid, group)
			continue
		}
		for _, file := range group.files {
			state.result.Coverage.Unsupported = append(state.result.Coverage.Unsupported, unsupported{Path: file, Reason: "Python project metadata is invalid"})
		}
		for _, problem := range data.Problems {
			state.result.Findings = append(state.result.Findings, responseFinding{
				Capability: state.request.Capability, Path: "repository", Subject: problem.Path,
				Message: problem.Message, Rule: "project.configuration",
			})
		}
	}
	return valid, nil
}

type analysisState struct {
	request request
	data    map[string][]byte
	result  response
}

type analysisGroup struct {
	scope analysisScope
	files []string
}

func (state *analysisState) executeCapability(ctx context.Context, adapter adapter, workspace string, groups []analysisGroup) error {
	switch state.request.Capability {
	case "format":
		return state.format(ctx, adapter.ruff, workspace, groups)
	case "lint":
		return state.lint(ctx, adapter.ruff, adapter.facts, workspace, groups)
	case "complexity":
		return state.complexity(ctx, adapter.ruff, adapter.facts, workspace, groups)
	case "typecheck":
		return state.typecheck(ctx, adapter.ty, workspace, groups)
	case "architecture":
		return state.architecture(ctx, adapter.ruff, adapter.facts, workspace, groups)
	default:
		return fmt.Errorf("unsupported Python capability %s", state.request.Capability)
	}
}

func (state *analysisState) analysisGroups() ([]analysisGroup, error) {
	groups := make([]analysisGroup, len(state.request.Scopes))
	for index, scope := range state.request.Scopes {
		groups[index].scope = scope
	}
	for _, file := range uniqueSorted(state.request.Files) {
		if !state.usable(file) {
			continue
		}
		index := slices.IndexFunc(state.request.Scopes, func(scope analysisScope) bool {
			return slices.Contains(scope.Members, file)
		})
		if index < 0 {
			return nil, fmt.Errorf("selected Python source %s has no authorized scope", file)
		}
		groups[index].files = append(groups[index].files, file)
	}
	return groups, nil
}

func (state *analysisState) scopeMemberGroups() ([]analysisGroup, error) {
	groups := make([]analysisGroup, len(state.request.Scopes))
	seen := map[string]bool{}
	for index, scope := range state.request.Scopes {
		groups[index].scope = scope
		for _, file := range scope.Members {
			if seen[file] {
				return nil, fmt.Errorf("python project scopes repeat member %s", file)
			}
			seen[file] = true
			if state.usable(file) {
				groups[index].files = append(groups[index].files, file)
			}
		}
	}
	return groups, nil
}

func (state *analysisState) usable(file string) bool {
	data, found := state.data[file]
	if found && len(data) <= maximumPythonBytes && utf8.Valid(data) {
		return true
	}
	state.result.Coverage.Unsupported = append(state.result.Coverage.Unsupported, unsupported{
		Path: file, Reason: "Python source exceeds the 4 MiB analyzer limit or is not valid UTF-8",
	})
	return false
}

func (state *analysisState) format(ctx context.Context, executor ruffExecutor, workspace string, groups []analysisGroup) error {
	for _, group := range groups {
		for _, file := range group.files {
			formatted, err := executor.format(ctx, workspace, group.scope, file, state.data[file])
			if err != nil {
				return err
			}
			state.result.Coverage.Analyzed = append(state.result.Coverage.Analyzed, file)
			if bytes.Equal(formatted, state.data[file]) {
				continue
			}
			if state.request.Mode == "write" {
				if !slices.Contains(state.request.WriteFiles, file) {
					return fmt.Errorf("formatted source %s is not an authorized write target", file)
				}
				state.result.Edits = append(state.result.Edits, edit{Path: file, Content: string(formatted)})
				continue
			}
			state.result.Findings = append(state.result.Findings, responseFinding{
				Capability: "format", Path: file, Subject: "ruff", Rule: "format",
				Message: "source is not formatted according to the effective Python policy",
			})
		}
	}
	state.result.Evidence = []string{"Ruff 0.16.0 formatted selected Python source through stdin without target writes or cache use"}
	return nil
}

func (state *analysisState) lint(ctx context.Context, ruff ruffExecutor, facts factExecutor, workspace string, groups []analysisGroup) error {
	comments := []commentFact{}
	for _, group := range groups {
		if len(group.files) == 0 {
			continue
		}
		findings, err := ruff.lint(ctx, workspace, group.scope, group.files, state.generatedFiles())
		if err != nil {
			return err
		}
		result, err := facts.comments(ctx, workspace, group.files)
		if err != nil {
			return err
		}
		state.result.Findings = append(state.result.Findings, findings...)
		comments = append(comments, result.Comments...)
		for _, file := range group.files {
			reason := result.Failures[file]
			if reason != "" {
				state.result.Coverage.Unsupported = append(state.result.Coverage.Unsupported, unsupported{Path: file, Reason: reason})
				continue
			}
			state.result.Coverage.Analyzed = append(state.result.Coverage.Analyzed, file)
		}
	}
	if len(state.result.Findings) > 4096 || len(comments) > 20000 {
		return errors.New("python diagnostics or comment facts exceed the protocol collection limit")
	}
	sortComments(comments)
	state.result.Facts = &sourceFacts{Comments: &comments}
	state.result.Evidence = []string{"Ruff 0.16.0 and CPython 3.12 analyzed selected Python source in an isolated materialized context"}
	return nil
}

func (state *analysisState) complexity(ctx context.Context, ruff ruffExecutor, facts factExecutor, workspace string, groups []analysisGroup) error {
	functions := []functionFact{}
	for _, group := range groups {
		if len(group.files) == 0 {
			continue
		}
		ruffFunctions, err := ruff.complexity(ctx, workspace, group.scope, group.files)
		if err != nil {
			return err
		}
		result, err := facts.functions(ctx, workspace, group.files)
		if err != nil {
			return err
		}
		merged, err := mergeFunctionFacts(result.Functions, ruffFunctions)
		if err != nil {
			return err
		}
		functions = append(functions, merged...)
		state.accountFactFiles(group.files, result.Failures)
	}
	if len(functions) > 20000 {
		return errors.New("python function facts exceed the protocol collection limit")
	}
	sortFunctions(functions)
	state.result.Facts = &sourceFacts{Functions: &functions}
	state.result.Evidence = []string{"Ruff 0.16.0 supplied McCabe measurements and CPython supplied function depth and parameter facts"}
	return nil
}

func (state *analysisState) typecheck(ctx context.Context, ty tyExecutor, workspace string, groups []analysisGroup) error {
	for _, group := range groups {
		if len(group.files) == 0 {
			continue
		}
		findings, err := ty.typecheck(ctx, workspace, group.scope, group.files)
		if err != nil {
			return err
		}
		state.result.Findings = append(state.result.Findings, findings...)
		state.result.Coverage.Analyzed = append(state.result.Coverage.Analyzed, group.files...)
	}
	if len(state.result.Findings) > 4096 {
		return errors.New("python type diagnostics exceed the protocol collection limit")
	}
	state.result.Evidence = []string{"ty 0.0.65 checked every member of the selected Python project scope"}
	return nil
}

func (state *analysisState) accountFactFiles(files []string, failures map[string]string) {
	for _, file := range files {
		if reason := failures[file]; reason != "" {
			state.result.Coverage.Unsupported = append(state.result.Coverage.Unsupported, unsupported{Path: file, Reason: reason})
			continue
		}
		state.result.Coverage.Analyzed = append(state.result.Coverage.Analyzed, file)
	}
}

func mergeFunctionFacts(functions, complexities []functionFact) ([]functionFact, error) {
	byIdentity := map[string]int{}
	for _, function := range complexities {
		identity := functionIdentity(function)
		if _, found := byIdentity[identity]; found {
			return nil, errors.New("ruff repeated a function complexity measurement")
		}
		byIdentity[identity] = function.Complexity
	}
	merged := make([]functionFact, 0, len(functions))
	for _, function := range functions {
		complexity, found := byIdentity[functionIdentity(function)]
		if !found {
			return nil, fmt.Errorf("ruff omitted complexity for function %s in %s", function.Name, function.Path)
		}
		function.Complexity = complexity
		delete(byIdentity, functionIdentity(function))
		merged = append(merged, function)
	}
	if len(byIdentity) != 0 {
		return nil, errors.New("ruff returned complexity for an unknown function")
	}
	return merged, nil
}

func functionIdentity(function functionFact) string {
	return fmt.Sprintf("%s\x00%d\x00%d\x00%s", function.Path, function.Line, function.Column, function.Name)
}

func (state *analysisState) generatedFiles() map[string]bool {
	generated := map[string]bool{}
	for _, entry := range state.request.Inventory {
		if entry.Generated {
			generated[entry.Path] = true
		}
	}
	return generated
}

func (state *analysisState) finish() response {
	state.result.Coverage.Analyzed = uniqueSorted(state.result.Coverage.Analyzed)
	sort.Slice(state.result.Coverage.Unsupported, func(left, right int) bool {
		return state.result.Coverage.Unsupported[left].Path < state.result.Coverage.Unsupported[right].Path
	})
	sortFindings(state.result.Findings)
	if len(state.result.Coverage.Unsupported) > 0 {
		state.result.Status = "incomplete"
	} else if len(state.result.Findings) > 0 {
		state.result.Status = "findings"
	}
	return state.result
}

func validateTools(request request) error {
	if slices.Contains([]string{"format", "lint", "complexity", "architecture"}, request.Capability) && (!hasTool(request.Tools, "ruff", "ruff", "0.16.0") || strings.TrimSpace(os.Getenv("CODE_POLISHY_TOOL_RUFF")) == "") {
		return errors.New("request does not bind an available Ruff 0.16.0 executable")
	}
	if slices.Contains([]string{"lint", "complexity", "architecture"}, request.Capability) && (!hasTool(request.Tools, "python", "python", "3.12.13+20260728") || strings.TrimSpace(os.Getenv("CODE_POLISHY_TOOL_PYTHON")) == "") {
		return errors.New("request does not bind an available CPython 3.12.13+20260728 executable")
	}
	if request.Capability == "typecheck" && (!hasTool(request.Tools, "ty", "ty", "0.0.65") || strings.TrimSpace(os.Getenv("CODE_POLISHY_TOOL_TY")) == "") {
		return errors.New("request does not bind an available ty 0.0.65 executable")
	}
	return nil
}

func hasTool(tools []toolIdentity, id, name, version string) bool {
	return slices.ContainsFunc(tools, func(tool toolIdentity) bool {
		return tool.ID == id && tool.Name == name && tool.Version == version && validDigest(tool.SHA256)
	})
}

func readAuthorizedInputs(request request) (map[string][]byte, []inputFile, error) {
	paths, err := authorizedInputPaths(request)
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
		contents, digest, err := readVerifiedInput(root, file, expected[file])
		if err != nil {
			return nil, nil, err
		}
		data[file] = contents
		inputs = append(inputs, inputFile{Path: file, SHA256: digest})
	}
	return data, inputs, nil
}

func authorizedInputPaths(request request) ([]string, error) {
	authorized := map[string]bool{}
	for _, file := range request.Files {
		authorized[file] = true
	}
	for _, scope := range request.Scopes {
		for _, file := range scope.Context {
			authorized[file] = true
		}
	}
	declarationPaths, err := policyDeclarationPaths(request)
	if err != nil {
		return nil, err
	}
	for _, file := range declarationPaths {
		authorized[file] = true
	}
	if len(authorized) == 0 || len(authorized) > maximumInputs {
		return nil, fmt.Errorf("authorized Python context must contain 1 to %d files", maximumInputs)
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
		return nil, "", fmt.Errorf("authorized input %s has no request identity", file)
	}
	contents, err := readInput(root, file)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(contents)
	got := hex.EncodeToString(digest[:])
	if got != want {
		return nil, "", fmt.Errorf("authorized input %s changed", file)
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

func materializeInputs(data map[string][]byte) (string, error) {
	created, err := os.MkdirTemp("", "code-polishy-python-pack-")
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(created)
	if err != nil {
		os.RemoveAll(created)
		return "", err
	}
	for _, name := range sortedKeys(data) {
		target := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			os.RemoveAll(root)
			return "", err
		}
		if err := os.WriteFile(target, data[name], 0o600); err != nil {
			os.RemoveAll(root)
			return "", err
		}
	}
	return root, nil
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
		message = "Python adapter failed"
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

func sortedKeys(values map[string][]byte) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sortFindings(findings []responseFinding) {
	sort.SliceStable(findings, func(left, right int) bool {
		first := findings[left]
		second := findings[right]
		return fmt.Sprintf("%s\x00%09d\x00%09d\x00%s", first.Path, first.Line, first.Column, first.Rule) < fmt.Sprintf("%s\x00%09d\x00%09d\x00%s", second.Path, second.Line, second.Column, second.Rule)
	})
}

func sortComments(comments []commentFact) {
	sort.SliceStable(comments, func(left, right int) bool {
		first := comments[left]
		second := comments[right]
		return fmt.Sprintf("%s\x00%09d\x00%09d", first.Path, first.Line, first.Column) < fmt.Sprintf("%s\x00%09d\x00%09d", second.Path, second.Line, second.Column)
	})
}

func sortFunctions(functions []functionFact) {
	sort.SliceStable(functions, func(left, right int) bool {
		return functionIdentity(functions[left]) < functionIdentity(functions[right])
	})
}
