package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const vultureProtocol = "code-polishy-python-vulture/v1"
const vultureVersion = "2.16"

type vultureExecutor interface {
	deadCode(context.Context, string, analysisScope, []string) (vultureResult, error)
}

type osVulture struct {
	executable string
	script     string
}

type vultureRequest struct {
	Protocol    string             `json:"protocol"`
	ToolVersion string             `json:"toolVersion"`
	Files       []vultureFile      `json:"files"`
	References  []vultureReference `json:"references"`
	Backends    []vultureBackend   `json:"backends"`
}

type vultureFile struct {
	Path    string `json:"path"`
	Module  string `json:"module"`
	Package string `json:"package"`
}

type vultureReference struct {
	ID     string `json:"id"`
	Module string `json:"module"`
	Symbol string `json:"symbol"`
}

type vultureBackend struct {
	ID     string `json:"id"`
	Module string `json:"module"`
	Object string `json:"object"`
}

type vultureProblem struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type vultureResult struct {
	Facts    []deadCodeFact
	Problems []vultureProblem
}

type vultureResponseWire struct {
	Protocol    *string           `json:"protocol"`
	ToolVersion *string           `json:"toolVersion"`
	Covered     *[]string         `json:"covered"`
	DeadCode    *[]deadCodeFact   `json:"deadCode"`
	Problems    *[]vultureProblem `json:"problems"`
	Failure     *string           `json:"failure"`
}

func (runner osVulture) deadCode(ctx context.Context, workspace string, scope analysisScope, files []string) (vultureResult, error) {
	if err := runner.validate(); err != nil {
		return vultureResult{}, err
	}
	files = uniqueSorted(files)
	request, err := newVultureRequest(scope, files)
	if err != nil {
		return vultureResult{}, err
	}
	input, err := json.Marshal(request)
	if err != nil {
		return vultureResult{}, err
	}
	output, err := executeTool(ctx, runner.executable, workspace, []string{"-I", "-B", runner.script}, input)
	if err != nil {
		return vultureResult{}, err
	}
	return parseVultureResponse(output, files)
}

func newVultureRequest(scope analysisScope, files []string) (vultureRequest, error) {
	data, err := decodePythonScopeData(scope.Data)
	if err != nil {
		return vultureRequest{}, err
	}
	index, err := newPythonModuleIndex(scope, data)
	if err != nil {
		return vultureRequest{}, err
	}
	request := vultureRequest{Protocol: vultureProtocol, ToolVersion: vultureVersion, Files: []vultureFile{}, References: []vultureReference{}, Backends: []vultureBackend{}}
	for _, file := range files {
		identity, found := index.byPath[file]
		if !found {
			return vultureRequest{}, fmt.Errorf("python Vulture source %s has no module identity", file)
		}
		request.Files = append(request.Files, vultureFile{Path: file, Module: identity.module, Package: identity.packageName})
	}
	for _, entry := range data.EntryPoints {
		id := strings.Join([]string{"manifest", data.Manifest, entry.Group, entry.Name, entry.Module, entry.Symbol}, ":")
		request.References = append(request.References, vultureReference{ID: id, Module: entry.Module, Symbol: entry.Symbol})
	}
	if len(data.BackendPaths) > 0 && data.BuildBackend.Module != "" {
		id := strings.Join([]string{"manifest", data.Manifest, "build-system.build-backend", data.BuildBackend.Module, data.BuildBackend.Object}, ":")
		request.Backends = append(request.Backends, vultureBackend{ID: id, Module: data.BuildBackend.Module, Object: data.BuildBackend.Object})
	}
	return request, nil
}

func (runner osVulture) validate() error {
	if strings.TrimSpace(runner.executable) == "" || !filepath.IsAbs(runner.executable) {
		return errors.New("sealed CPython path must be absolute")
	}
	if strings.TrimSpace(runner.script) == "" || !filepath.IsAbs(runner.script) {
		return errors.New("python Vulture script path must be absolute")
	}
	info, err := os.Stat(runner.script)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("python Vulture script is unavailable")
	}
	return nil
}

func parseVultureResponse(data []byte, files []string) (vultureResult, error) {
	wire, err := decodeVultureResponse(data)
	if err != nil {
		return vultureResult{}, err
	}
	expected, err := validateVultureResponseHeader(wire, files)
	if err != nil {
		return vultureResult{}, err
	}
	problems, err := validateVultureProblems(*wire.Problems)
	if err != nil {
		return vultureResult{}, err
	}
	facts, err := validateVultureFacts(*wire.DeadCode, expected)
	if err != nil {
		return vultureResult{}, err
	}
	if len(problems) > 0 && len(facts) > 0 {
		return vultureResult{}, errors.New("python Vulture response combines unresolved reachability with dead-code facts")
	}
	return vultureResult{Facts: facts, Problems: problems}, nil
}

func validateVultureResponseHeader(wire vultureResponseWire, files []string) ([]string, error) {
	if !completeVultureResponse(wire) {
		return nil, errors.New("python Vulture response omits required fields")
	}
	if *wire.Protocol != vultureProtocol || *wire.ToolVersion != vultureVersion {
		return nil, errors.New("python Vulture response has the wrong protocol or tool version")
	}
	if len(*wire.Failure) > 4096 {
		return nil, errors.New("python Vulture failure exceeds 4096 bytes")
	}
	if *wire.Failure != "" {
		return nil, fmt.Errorf("python Vulture analysis failed: %s", *wire.Failure)
	}
	expected := uniqueSorted(files)
	if !slices.Equal(*wire.Covered, expected) {
		return nil, errors.New("python Vulture coverage is not the exact project source inventory")
	}
	return expected, nil
}

func completeVultureResponse(wire vultureResponseWire) bool {
	return wire.Protocol != nil && wire.ToolVersion != nil && wire.Covered != nil && wire.DeadCode != nil && wire.Problems != nil && wire.Failure != nil
}

func validateVultureProblems(problems []vultureProblem) ([]vultureProblem, error) {
	if len(problems) > 4096 {
		return nil, errors.New("python Vulture response exceeds 4096 reachability problems")
	}
	previous := ""
	for _, problem := range problems {
		if !validVultureText(problem.ID) || !validVultureText(problem.Message) || problem.ID <= previous {
			return nil, errors.New("python Vulture response contains an invalid reachability problem")
		}
		previous = problem.ID
	}
	return problems, nil
}

func decodeVultureResponse(data []byte) (vultureResponseWire, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	wire := vultureResponseWire{}
	if err := decoder.Decode(&wire); err != nil {
		return vultureResponseWire{}, fmt.Errorf("decode python Vulture response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return vultureResponseWire{}, errors.New("python Vulture response contains more than one JSON value")
	}
	return wire, nil
}

func validateVultureFacts(facts []deadCodeFact, files []string) ([]deadCodeFact, error) {
	if len(facts) > 10000 {
		return nil, errors.New("python Vulture response exceeds 10000 dead-code facts")
	}
	allowed := map[string]bool{}
	for _, file := range files {
		allowed[file] = true
	}
	seen := map[string]bool{}
	for _, fact := range facts {
		if err := validateVultureFact(fact, allowed); err != nil {
			return nil, err
		}
		identity := deadCodeIdentity(fact)
		if seen[identity] {
			return nil, errors.New("python Vulture response repeats a dead-code fact")
		}
		seen[identity] = true
	}
	sortDeadCode(facts)
	return facts, nil
}

func validateVultureFact(fact deadCodeFact, allowed map[string]bool) error {
	validKind := slices.Contains([]string{"attribute", "class", "function", "import", "method", "property", "unreachable_code", "variable"}, fact.Kind)
	validLocation := fact.Line >= 1 && fact.EndLine >= fact.Line
	validConfidence := fact.Confidence >= 60 && fact.Confidence <= 100
	if fact.Analyzer != "vulture" || !allowed[fact.Path] || !validLocation || !validVultureText(fact.Name) || !validKind || !validConfidence || !validVultureText(fact.Message) {
		return errors.New("python Vulture response contains an invalid dead-code fact")
	}
	return nil
}

func validVultureText(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 4096 && !strings.ContainsRune(value, '\x00')
}

func deadCodeIdentity(fact deadCodeFact) string {
	return strings.Join([]string{
		fact.Path, strconv.Itoa(fact.Line), strconv.Itoa(fact.EndLine), fact.Name, fact.Kind,
		strconv.Itoa(fact.Confidence), fact.Message,
	}, "\x00")
}
