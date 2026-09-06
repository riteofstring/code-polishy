package pythonfacts

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestArchitectureProjectReusesOneValidatedModelWithoutChangingEvidence(t *testing.T) {
	python := typeTestInterpreter(t)
	project, err := AnalyzeProjectSources(t.Context(), python, []Input{
		{Path: "src/app.py", Source: "import model\nfrom pkgutil import resolve_name\nloaded = resolve_name('model:value')\n"},
		{Path: "src/model.py", Source: "value = object()\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	modules := typeTestModules(project.Sources)
	types, err := ResolveTypeProject(t.Context(), python, modules)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := ResolveObjectImports(t.Context(), python, ".", modules, nil)
	if err != nil {
		t.Fatal(err)
	}
	sources := []ArchitectureSourceInput{
		{Path: "src/app.py", Module: "app", Source: "import model\nfrom pkgutil import resolve_name\nloaded = resolve_name('model:value')\n"},
		{Path: "src/model.py", Module: "model", Source: "value = object()\n"},
	}
	combined, err := ResolveArchitectureProject(t.Context(), python, sources, ArchitectureProjectInput{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(combined.Types.Imports, types.Imports) || !reflect.DeepEqual(combined.Types.Reads, types.Reads) {
		t.Fatalf("combined type evidence changed: %+v", combined.Types)
	}
	if !reflect.DeepEqual(combined.Objects.Imports, objects.Imports) {
		t.Fatalf("combined object evidence changed: %+v", combined.Objects)
	}
	covered := make([]typeCoverage, 0, len(combined.Sources))
	for _, source := range combined.Sources {
		covered = append(covered, typeCoverage{Path: source.Path, SourceSHA256: source.SHA256})
	}
	altered := append([]Source{}, combined.Sources...)
	altered[0].SHA256 = strings.Repeat("0", 64)
	wire, err := json.Marshal(architectureProjectResponse{
		Protocol: architectureProjectProtocol, Covered: covered, Sources: altered,
		Reads: combined.Types.Reads, Imports: combined.Types.Imports, ObjectImports: combined.Objects.Imports,
		RuntimeChecks:  architectureRuntimeCheckResponse{Evidence: []RuntimeCheckEvidence{}, Problems: []RuntimeCheckProblem{}},
		RuntimeLoaders: []ArchitectureRuntimeLoaderResult{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeArchitectureProject(wire, sources, ArchitectureProjectInput{}); err == nil {
		t.Fatal("altered source digest acquired architecture evidence")
	}
}
