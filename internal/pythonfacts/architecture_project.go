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
	"path/filepath"
)

//go:embed architecture_project.py
var architectureProjectSource string

const architectureProjectProtocol = "python-architecture-project/v1"

type ArchitectureSourceInput struct {
	Path    string `json:"path"`
	Module  string `json:"module"`
	Package string `json:"package"`
	Source  string `json:"source"`
}

type ArchitectureRuntimeLoaderInput struct {
	ID      string             `json:"id"`
	Request RuntimeLoaderInput `json:"request"`
}

type ArchitectureProjectInput struct {
	Root           string                           `json:"root"`
	Registries     []ObjectRegistryInput            `json:"registries"`
	RuntimeChecks  []RuntimeCheckInput              `json:"runtimeChecks"`
	RuntimeLoaders []ArchitectureRuntimeLoaderInput `json:"runtimeLoaders"`
}

type ArchitectureRuntimeLoaderResult struct {
	ID          string                `json:"id"`
	Error       string                `json:"error"`
	Targets     []RuntimeLoaderTarget `json:"targets"`
	RuntimeType string                `json:"runtimeType"`
}

type ArchitectureProject struct {
	Sources           []Source
	Types             TypeProject
	Objects           ObjectImportProject
	RuntimeChecks     RuntimeCheckProject
	RuntimeLoaders    []ArchitectureRuntimeLoaderResult
	FactsIdentity     string
	PartitionIdentity string
	Identity          string
}

type architectureProjectResponse struct {
	Protocol       string                            `json:"protocol"`
	Covered        []typeCoverage                    `json:"covered"`
	Sources        []Source                          `json:"sources"`
	Reads          []TypedDictRead                   `json:"reads"`
	Imports        []ModuleImport                    `json:"imports"`
	ObjectImports  []ObjectImport                    `json:"objectImports"`
	RuntimeChecks  architectureRuntimeCheckResponse  `json:"runtimeChecks"`
	RuntimeLoaders []ArchitectureRuntimeLoaderResult `json:"runtimeLoaders"`
}

type architectureRuntimeCheckResponse struct {
	Evidence []RuntimeCheckEvidence `json:"evidence"`
	Problems []RuntimeCheckProblem  `json:"problems"`
}

type architectureProjectReader struct {
	sources []ArchitectureSourceInput
	current *bytes.Reader
	index   int
	total   int
}

func ResolveArchitectureProject(ctx context.Context, python string, sources []ArchitectureSourceInput, input ArchitectureProjectInput) (ArchitectureProject, error) {
	ordered, err := normalizedArchitectureSources(sources)
	if err != nil {
		return ArchitectureProject{}, err
	}
	if !filepath.IsAbs(python) {
		return ArchitectureProject{}, fmt.Errorf("architecture project interpreter is invalid")
	}
	input.Registries = append([]ObjectRegistryInput{}, input.Registries...)
	input.RuntimeChecks = append([]RuntimeCheckInput{}, input.RuntimeChecks...)
	input.RuntimeLoaders = append([]ArchitectureRuntimeLoaderInput{}, input.RuntimeLoaders...)
	header, err := json.Marshal(struct {
		Protocol string `json:"protocol"`
		Count    int    `json:"count"`
		ArchitectureProjectInput
	}{architectureProjectProtocol, len(ordered), input})
	if err != nil {
		return ArchitectureProject{}, err
	}
	if len(header)+1 > maximumResponseSize {
		return ArchitectureProject{}, fmt.Errorf("architecture project header exceeds its request boundary")
	}
	request := &architectureProjectReader{sources: ordered, current: bytes.NewReader(append(header, '\n'))}
	digest := sha256.New()
	program := SourceSupportSource() + ReachabilitySupportSource() + embeddedPythonModule("object_guards", objectGuardsSource) + embeddedPythonModule("object_imports", objectImportsSource) + embeddedPythonModule("runtime_checks", runtimeChecksSource) + embeddedPythonModule("runtime_loader_syntax", runtimeLoaderSyntaxSource) + embeddedPythonModule("runtime_loaders", runtimeLoadersSource) + embeddedPythonModule("__main__", architectureProjectSource)
	data, err := runFactProjectWithLimit(ctx, python, io.TeeReader(request, digest), program, maximumProjectFactSize+4*maximumResponseSize)
	if err != nil {
		return ArchitectureProject{}, err
	}
	response, err := decodeArchitectureProject(data, ordered, input)
	if err != nil {
		return ArchitectureProject{}, err
	}
	_, _ = digest.Write(data)
	identity := hex.EncodeToString(digest.Sum(nil))
	factsIdentity, err := sourceProjectIdentity(response.Sources)
	if err != nil {
		return ArchitectureProject{}, err
	}
	covered, _ := json.Marshal(response.Covered)
	partitionDigest := sha256.Sum256(covered)
	return ArchitectureProject{
		Sources:           response.Sources,
		Types:             TypeProject{Imports: response.Imports, Reads: response.Reads, Identity: identity},
		Objects:           ObjectImportProject{Imports: response.ObjectImports, Identity: identity},
		RuntimeChecks:     RuntimeCheckProject{Evidence: response.RuntimeChecks.Evidence, Problems: response.RuntimeChecks.Problems, Identity: identity},
		RuntimeLoaders:    response.RuntimeLoaders,
		FactsIdentity:     factsIdentity,
		PartitionIdentity: hex.EncodeToString(partitionDigest[:]),
		Identity:          identity,
	}, nil
}

func normalizedArchitectureSources(sources []ArchitectureSourceInput) ([]ArchitectureSourceInput, error) {
	plain := make([]Input, 0, len(sources))
	byPath := make(map[string]ArchitectureSourceInput, len(sources))
	for _, source := range sources {
		plain = append(plain, Input{Path: source.Path, Source: source.Source})
		byPath[source.Path] = source
	}
	ordered, err := normalizedProjectSources(plain)
	if err != nil {
		return nil, err
	}
	if len(byPath) != len(sources) {
		return nil, fmt.Errorf("architecture project contains duplicate source paths")
	}
	result := make([]ArchitectureSourceInput, 0, len(ordered))
	for _, source := range ordered {
		result = append(result, byPath[source.Path])
	}
	return result, nil
}

func (reader *architectureProjectReader) Read(target []byte) (int, error) {
	if reader.current.Len() > 0 {
		return reader.current.Read(target)
	}
	if reader.index == len(reader.sources) {
		return 0, io.EOF
	}
	data, err := json.Marshal(reader.sources[reader.index])
	if err != nil {
		return 0, err
	}
	if len(data)+1 > maximumResponseSize || len(data)+1 > maximumProjectSourceSize-reader.total {
		return 0, fmt.Errorf("architecture project exceeds its source byte boundary")
	}
	reader.total += len(data) + 1
	reader.index++
	reader.current = bytes.NewReader(append(data, '\n'))
	return reader.current.Read(target)
}

func decodeArchitectureProject(data []byte, sources []ArchitectureSourceInput, input ArchitectureProjectInput) (architectureProjectResponse, error) {
	var response architectureProjectResponse
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return response, fmt.Errorf("decode architecture project: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return response, fmt.Errorf("architecture project has trailing output")
	}
	if !completeArchitectureResponse(response) {
		return response, fmt.Errorf("architecture project output is incomplete")
	}
	modules, err := architectureSourceModules(response.Sources, sources)
	if err != nil {
		return response, err
	}
	if err := validateArchitectureEvidence(response, modules, input); err != nil {
		return response, err
	}
	return response, nil
}

func completeArchitectureResponse(response architectureProjectResponse) bool {
	return response.Protocol == architectureProjectProtocol && response.Sources != nil && response.Reads != nil && response.Imports != nil && response.ObjectImports != nil && response.RuntimeChecks.Evidence != nil && response.RuntimeChecks.Problems != nil && response.RuntimeLoaders != nil
}

func architectureSourceModules(resolved []Source, sources []ArchitectureSourceInput) ([]TypeModule, error) {
	plain := make([]Input, 0, len(sources))
	modules := make([]TypeModule, 0, len(sources))
	for index, source := range sources {
		plain = append(plain, Input{Path: source.Path, Source: source.Source})
		if index >= len(resolved) || resolved[index].Error != "" {
			return nil, fmt.Errorf("architecture project could not parse %s", source.Path)
		}
		if err := validateSourceTypeFacts(resolved[index].TypeFacts); err != nil {
			return nil, fmt.Errorf("architecture project source %s: %w", source.Path, err)
		}
		modules = append(modules, TypeModule{Path: source.Path, Module: source.Module, Package: source.Package, SourceSHA256: resolved[index].SHA256, Facts: resolved[index].TypeFacts})
	}
	if err := validateProjectSourceCoverage(plain, resolved); err != nil {
		return nil, err
	}
	return modules, nil
}

func validateArchitectureEvidence(response architectureProjectResponse, modules []TypeModule, input ArchitectureProjectInput) error {
	files, err := projectCoveredFiles(response.Covered, modules)
	if err != nil {
		return err
	}
	if err := validateModuleImports(response.Imports, modules); err != nil {
		return err
	}
	if err := validateTypedDictReads(response.Reads, modules); err != nil {
		return err
	}
	if err := validateObjectImports(files, response.ObjectImports, input.Registries); err != nil {
		return err
	}
	if err := validateObjectImportCalls(modules, response.ObjectImports); err != nil {
		return err
	}
	if err := validateObjectInputGuards(modules, response.ObjectImports); err != nil {
		return err
	}
	runtime := RuntimeCheckProject{Evidence: response.RuntimeChecks.Evidence, Problems: response.RuntimeChecks.Problems}
	if err := validateRuntimeCheckProject(modules, input.RuntimeChecks, runtime); err != nil {
		return err
	}
	if err := validateArchitectureRuntimeLoaders(modules, input.RuntimeLoaders, response.RuntimeLoaders); err != nil {
		return err
	}
	return nil
}

func validateArchitectureRuntimeLoaders(modules []TypeModule, inputs []ArchitectureRuntimeLoaderInput, results []ArchitectureRuntimeLoaderResult) error {
	if len(inputs) != len(results) {
		return fmt.Errorf("architecture project omitted a runtime loader result")
	}
	seen := map[string]bool{}
	for index, result := range results {
		if result.ID == "" || seen[result.ID] || result.ID != inputs[index].ID || result.Targets == nil || result.Error == "" && result.RuntimeType == "" {
			return fmt.Errorf("architecture project runtime loader result is incomplete or reordered")
		}
		seen[result.ID] = true
		if err := validateRuntimeLoaderTargets(modules, result.Targets); err != nil {
			return err
		}
	}
	return nil
}
