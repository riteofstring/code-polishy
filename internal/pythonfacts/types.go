package pythonfacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const typeProjectProtocol = "python-type-project/v2"

//go:embed source_parser.py
var sourceParserSource string

//go:embed type_facts.py
var typeFactsSource string

//go:embed object_predicates.py
var objectPredicatesSource string

//go:embed type_resolver.py
var typeResolverSource string

//go:embed type_project.py
var typeProjectSource string

//go:embed framework_contracts.py
var frameworkContractsSource string

//go:embed pydantic_resolver.py
var pydanticResolverSource string

//go:embed loader_bindings.py
var loaderBindingsSource string

//go:embed module_evidence.py
var moduleEvidenceSource string

//go:embed module_imports.py
var moduleImportsSource string

type TypeModule struct {
	Path         string          `json:"path"`
	Module       string          `json:"module"`
	Package      string          `json:"package"`
	SourceSHA256 string          `json:"sourceSha256"`
	Facts        json.RawMessage `json:"facts"`
}

type TypedDictField struct {
	Path      string `json:"path"`
	TypeScope string `json:"typeScope"`
	TypeName  string `json:"typeName"`
	Key       string `json:"key"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
}

type TypedDictRead struct {
	Path   string         `json:"path"`
	Line   int            `json:"line"`
	Column int            `json:"column"`
	Field  TypedDictField `json:"field"`
}

type TypeProject struct {
	Imports  []ModuleImport           `json:"imports"`
	Reads    []TypedDictRead          `json:"reads"`
	Pydantic []ReachabilityDefinition `json:"pydantic"`
	Identity string                   `json:"identity"`
}

type typeCoverage struct {
	Path         string `json:"path"`
	SourceSHA256 string `json:"sourceSha256"`
}

type typeProjectResponse struct {
	Imports  []ModuleImport           `json:"imports"`
	Protocol string                   `json:"protocol"`
	Covered  []typeCoverage           `json:"covered"`
	Reads    []TypedDictRead          `json:"reads"`
	Pydantic []ReachabilityDefinition `json:"pydantic"`
}

type typeProjectReader struct {
	modules []TypeModule
	current *bytes.Reader
	index   int
	total   int
}

func validateSourceTypeFacts(data json.RawMessage) error {
	var facts struct {
		Scopes   []json.RawMessage `json:"scopes"`
		Bindings []json.RawMessage `json:"bindings"`
		Reads    []json.RawMessage `json:"reads"`
		Calls    []json.RawMessage `json:"calls"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&facts); err != nil {
		return fmt.Errorf("decode compact type facts: %w", err)
	}
	if len(facts.Scopes) == 0 || facts.Bindings == nil || facts.Reads == nil || facts.Calls == nil {
		return fmt.Errorf("compact type facts omit required scope, binding, read, or call collections")
	}
	return nil
}

func TypeSupportSource() string {
	return embeddedPythonModule("object_predicates", objectPredicatesSource) + embeddedPythonModule("type_facts", typeFactsSource) + embeddedPythonModule("type_resolver", typeResolverSource) + embeddedPythonModule("pydantic_resolver", pydanticResolverSource) + embeddedPythonModule("framework_contracts", frameworkContractsSource) + embeddedPythonModule("loader_bindings", loaderBindingsSource) + embeddedPythonModule("module_evidence", moduleEvidenceSource) + embeddedPythonModule("module_imports", moduleImportsSource)
}

func ParserSupportSource() string {
	return embeddedPythonModule("source_parser", sourceParserSource)
}

func SourceSupportSource() string {
	return ParserSupportSource() + TypeSupportSource() + embeddedPythonModule("source_adapter", sourceAdapterSource)
}

func embeddedPythonModule(name, source string) string {
	return "import sys,types\n_module=types.ModuleType('" + name + "')\nexec(compile(" + strconv.Quote(source) + ",'" + name + ".py','exec'),_module.__dict__)\nsys.modules['" + name + "']=_module\n"
}

func ResolveTypeProject(ctx context.Context, python string, modules []TypeModule) (TypeProject, error) {
	ordered := slices.Clone(modules)
	slices.SortFunc(ordered, func(left, right TypeModule) int { return strings.Compare(left.Path, right.Path) })
	if len(ordered) > maximumProjectSources || !filepath.IsAbs(python) {
		return TypeProject{}, fmt.Errorf("python type project has invalid source count or interpreter")
	}
	request, err := newTypeProjectReader(ordered)
	if err != nil {
		return TypeProject{}, err
	}
	digest := sha256.New()
	data, err := runTypeProject(ctx, python, io.TeeReader(request, digest))
	if err != nil {
		return TypeProject{}, err
	}
	response, err := decodeTypeProject(data, ordered)
	if err != nil {
		return TypeProject{}, err
	}
	_, _ = digest.Write(data)
	return TypeProject{Imports: response.Imports, Reads: response.Reads, Pydantic: response.Pydantic, Identity: hex.EncodeToString(digest.Sum(nil))}, nil
}

func newTypeProjectReader(modules []TypeModule) (*typeProjectReader, error) {
	header, err := json.Marshal(struct {
		Protocol string `json:"protocol"`
		Count    int    `json:"count"`
	}{typeProjectProtocol, len(modules)})
	if err != nil {
		return nil, err
	}
	return &typeProjectReader{modules: modules, current: bytes.NewReader(append(header, '\n'))}, nil
}

func (reader *typeProjectReader) Read(target []byte) (int, error) {
	if reader.current.Len() > 0 {
		return reader.current.Read(target)
	}
	if reader.index == len(reader.modules) {
		return 0, io.EOF
	}
	data, err := json.Marshal(reader.modules[reader.index])
	if err != nil {
		return 0, err
	}
	if len(data)+1 > maximumResponseSize || len(data)+1 > maximumProjectFactSize-reader.total {
		return 0, fmt.Errorf("python type project exceeds its compact fact byte limit")
	}
	reader.total += len(data) + 1
	reader.index++
	reader.current = bytes.NewReader(append(data, '\n'))
	return reader.current.Read(target)
}

func runTypeProject(ctx context.Context, python string, input io.Reader) ([]byte, error) {
	return runFactProject(ctx, python, input, TypeSupportSource()+embeddedPythonModule("__main__", typeProjectSource))
}

func runFactProject(ctx context.Context, python string, input io.Reader, program string) ([]byte, error) {
	programInput, err := ProgramInput(program, input)
	if err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, maximumProjectDuration)
	defer cancel()
	command := exec.CommandContext(bounded, python, "-I", "-B", "-c", ProgramBootstrap)
	command.Dir = filepath.Dir(python)
	command.Env = []string{}
	command.Stdin = programInput
	stdout := &boundedBuffer{maximum: maximumResponseSize}
	stderr := &boundedBuffer{maximum: 64 * 1024}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		if bounded.Err() != nil {
			return nil, fmt.Errorf("python type project exceeded its time limit")
		}
		return nil, fmt.Errorf("python type project failed: %s: %w", strings.TrimSpace(string(stderr.Bytes())), err)
	}
	return stdout.Bytes(), nil
}

func decodeTypeProject(data []byte, modules []TypeModule) (typeProjectResponse, error) {
	var response typeProjectResponse
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return response, fmt.Errorf("decode python type project: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return response, fmt.Errorf("python type project has trailing output")
	}
	if response.Protocol != typeProjectProtocol || response.Reads == nil || response.Pydantic == nil || response.Imports == nil {
		return response, fmt.Errorf("python type project has incomplete source coverage")
	}
	if _, err := projectCoveredFiles(response.Covered, modules); err != nil {
		return response, err
	}
	if err := validatePydanticMembers(response.Pydantic, modules); err != nil {
		return response, err
	}
	if err := validateModuleImports(response.Imports, modules); err != nil {
		return response, err
	}
	return response, validateTypedDictReads(response.Reads, modules)
}

func validateTypedDictReads(reads []TypedDictRead, modules []TypeModule) error {
	paths := map[string]bool{}
	for _, module := range modules {
		paths[module.Path] = true
	}
	seen := map[TypedDictRead]bool{}
	for _, read := range reads {
		if !validTypedDictRead(read, paths) || seen[read] {
			return fmt.Errorf("python type project contains an invalid or duplicate literal-key read")
		}
		seen[read] = true
	}
	return nil
}

func validTypedDictRead(read TypedDictRead, paths map[string]bool) bool {
	return paths[read.Path] && paths[read.Field.Path] && read.Line > 0 && read.Column > 0 && read.Field.Line > 0 && read.Field.Column > 0 &&
		read.Field.TypeScope != "" && read.Field.TypeName != ""
}
