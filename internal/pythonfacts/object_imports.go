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
	"slices"
	"strings"
)

//go:embed object_imports.py
var objectImportsSource string

//go:embed object_guards.py
var objectGuardsSource string

type SourceLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type ObjectRegistryInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Error   string `json:"error"`
}

type ObjectRegistryEvidence struct {
	Path        string `json:"path"`
	JSONPointer string `json:"jsonPointer"`
	SHA256      string `json:"sha256"`
}

type ObjectImportTarget struct {
	Module          string `json:"module"`
	Symbol          string `json:"symbol"`
	StandardLibrary bool   `json:"standardLibrary"`
}

type ObjectInputGuard struct {
	Namespace       string                 `json:"namespace"`
	StandardLibrary bool                   `json:"standardLibrary"`
	Site            SourceLocation         `json:"site"`
	EndSite         SourceLocation         `json:"endSite"`
	Predicate       ReachabilityDefinition `json:"predicate"`
	PredicateSHA256 string                 `json:"predicateSha256"`
}

type ObjectImport struct {
	Path     string                  `json:"path"`
	Callable string                  `json:"callable"`
	Site     SourceLocation          `json:"site"`
	EndSite  SourceLocation          `json:"endSite"`
	Argument string                  `json:"argument"`
	Targets  []ObjectImportTarget    `json:"targets"`
	Registry *ObjectRegistryEvidence `json:"registry"`
	Guard    *ObjectInputGuard       `json:"guard"`
	Error    string                  `json:"error"`
}

type ObjectImportProject struct {
	Imports  []ObjectImport
	Identity string
}

func ResolveObjectImports(ctx context.Context, python, root string, modules []TypeModule, registries []ObjectRegistryInput) (ObjectImportProject, error) {
	ordered := slices.Clone(modules)
	slices.SortFunc(ordered, func(left, right TypeModule) int { return strings.Compare(left.Path, right.Path) })
	if len(ordered) > maximumProjectSources || !filepath.IsAbs(python) {
		return ObjectImportProject{}, fmt.Errorf("object import project source count or interpreter is invalid")
	}
	registries = slices.Clone(registries)
	slices.SortFunc(registries, func(left, right ObjectRegistryInput) int { return strings.Compare(left.Path, right.Path) })
	header, err := json.Marshal(struct {
		Protocol   string                `json:"protocol"`
		Count      int                   `json:"count"`
		Root       string                `json:"root"`
		Registries []ObjectRegistryInput `json:"registries"`
	}{"python-object-import-project/v1", len(ordered), root, append([]ObjectRegistryInput{}, registries...)})
	if err != nil {
		return ObjectImportProject{}, err
	}
	if len(header)+1 > maximumResponseSize {
		return ObjectImportProject{}, fmt.Errorf("object import registry header exceeds its input boundary")
	}
	input := &typeProjectReader{modules: ordered, current: bytes.NewReader(append(header, '\n'))}
	digest := sha256.New()
	program := TypeSupportSource() + ReachabilitySupportSource() + embeddedPythonModule("object_guards", objectGuardsSource) + embeddedPythonModule("__main__", objectImportsSource)
	data, err := runFactProject(ctx, python, io.TeeReader(input, digest), program)
	if err != nil {
		return ObjectImportProject{}, err
	}
	imports, err := decodeObjectImportProject(data, ordered, registries)
	if err != nil {
		return ObjectImportProject{}, err
	}
	_, _ = digest.Write(data)
	return ObjectImportProject{Imports: imports, Identity: hex.EncodeToString(digest.Sum(nil))}, nil
}

func decodeObjectImportProject(data []byte, modules []TypeModule, registries []ObjectRegistryInput) ([]ObjectImport, error) {
	var wire struct {
		Protocol string         `json:"protocol"`
		Covered  []typeCoverage `json:"covered"`
		Imports  []ObjectImport `json:"imports"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("object import project output has trailing data")
	}
	if wire.Protocol != "python-object-import-project/v1" || wire.Imports == nil {
		return nil, fmt.Errorf("object import project output is incomplete")
	}
	files, err := projectCoveredFiles(wire.Covered, modules)
	if err != nil {
		return nil, err
	}
	if err := validateObjectImports(files, wire.Imports, registries); err != nil {
		return nil, err
	}
	if err := validateObjectImportCalls(modules, wire.Imports); err != nil {
		return nil, err
	}
	if err := validateObjectInputGuards(modules, wire.Imports); err != nil {
		return nil, err
	}
	return wire.Imports, nil
}

func validateObjectImports(files []string, imports []ObjectImport, registries []ObjectRegistryInput) error {
	seen := map[string]bool{}
	for _, imported := range imports {
		identity := fmt.Sprintf("%s:%d:%d:%d:%d", imported.Path, imported.Site.Line, imported.Site.Column, imported.EndSite.Line, imported.EndSite.Column)
		if seen[identity] || !slices.Contains(files, imported.Path) || !validObjectImportSpan(imported) {
			return fmt.Errorf("object import evidence has an invalid or duplicate callsite")
		}
		seen[identity] = true
		if err := validateObjectImportTargets(imported); err != nil {
			return err
		}
		if imported.Registry != nil {
			if err := validateObjectRegistry(*imported.Registry, registries); err != nil {
				return err
			}
		}
	}
	return nil
}

func validObjectImportSpan(value ObjectImport) bool {
	return value.Callable != "" && value.Site.Line > 0 && value.Site.Column > 0 && value.EndSite.Column > 0 &&
		(value.EndSite.Line > value.Site.Line || value.EndSite.Line == value.Site.Line && value.EndSite.Column >= value.Site.Column)
}

func validateObjectImportTargets(imported ObjectImport) error {
	if imported.Targets == nil {
		return fmt.Errorf("object import evidence omits its target collection")
	}
	if imported.Guard != nil {
		if imported.Error != "" || imported.Registry != nil || len(imported.Targets) != 0 {
			return fmt.Errorf("guarded object input has a competing outcome")
		}
		return nil
	}
	if (imported.Error == "") != (len(imported.Targets) > 0) {
		return fmt.Errorf("object import evidence has no exact outcome")
	}
	seen := map[ObjectImportTarget]bool{}
	for _, target := range imported.Targets {
		if target.Module == "" || target.Symbol == "" || seen[target] {
			return fmt.Errorf("object import evidence has an invalid or duplicate target")
		}
		seen[target] = true
	}
	return nil
}

func validateObjectRegistry(evidence ObjectRegistryEvidence, inputs []ObjectRegistryInput) error {
	for _, input := range inputs {
		if input.Path == evidence.Path {
			digest := sha256.Sum256([]byte(input.Content))
			if input.Error != "" || input.Content == "" || evidence.SHA256 != hex.EncodeToString(digest[:]) {
				return fmt.Errorf("object import registry evidence is stale or invalid")
			}
			return nil
		}
	}
	return fmt.Errorf("object import registry has no supplied input")
}
