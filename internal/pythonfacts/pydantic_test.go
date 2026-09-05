package pythonfacts

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestPydanticMembersResolveAcrossBoundedProjectPartitions(t *testing.T) {
	python := typeTestInterpreter(t)
	inputs := make([]Input, 1001)
	for index := range inputs {
		inputs[index] = Input{Path: fmt.Sprintf("src/project/module_%04d.py", index), Source: "# " + strings.Repeat("x", 8500) + "\n"}
	}
	inputs[0].Source += "from .module_0999 import ModelBase, check_field\nclass Child(ModelBase):\n    child_field: str\n    @check_field('child_field')\n    def normalize(cls, value):\n        return value.strip()\n"
	inputs[999].Source += "from .module_1000 import Parent as ModelBase\nfrom pydantic import field_validator as check_field\n"
	inputs[1000].Source += "from pydantic import BaseModel\nclass Parent(BaseModel):\n    parent_field: str\n"
	project, err := AnalyzeProjectSources(t.Context(), python, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Partitions) < 2 || project.Partitions[0].LastPath >= inputs[999].Path {
		t.Fatalf("fixture does not cross the request boundary: %+v", project.Partitions)
	}
	modules := typeTestModules(project.Sources)
	first, err := ResolveTypeProject(t.Context(), python, modules)
	if err != nil {
		t.Fatal(err)
	}
	want := []ReachabilityDefinition{
		{Path: inputs[0].Path, Line: 4, End: 4, Name: "child_field"},
		{Path: inputs[0].Path, Line: 5, End: 7, Name: "normalize"},
		{Path: inputs[1000].Path, Line: 4, End: 4, Name: "parent_field"},
	}
	if !reflect.DeepEqual(first.Pydantic, want) {
		t.Fatalf("cross-partition model members = %+v, want %+v", first.Pydantic, want)
	}
	slices.Reverse(modules)
	second, err := ResolveTypeProject(t.Context(), python, modules)
	if err != nil || first.Identity != second.Identity || !reflect.DeepEqual(first.Pydantic, second.Pydantic) {
		t.Fatalf("source order changed inferred model members: %+v, error = %v", second, err)
	}
}

func TestPydanticInferenceKeepsExactFieldsAndDecorators(t *testing.T) {
	result, err := resolveTypeTestSources(t, map[string]string{
		"src/contracts.py": "from pydantic import BaseModel as Model, Field as F, PrivateAttr as P, computed_field as computed\n",
		"src/source.py": `from contracts import Model, F, P, computed
from typing import Annotated, ClassVar
class Direct(Model):
    direct_field: str
    annotated_field: Annotated[
        str,
        F(min_length=1),
    ]
    model_config = dict(
        extra="forbid",
    )
    _cache = P(default=None)
    declared = F(default=0)
    class_value: ClassVar[str]
    wrapped_class_value: Annotated[ClassVar[int], "metadata"]
    ordinary_value = 1
    @computed
    @property
    def computed_value(self):
        return self.direct_field
    def ordinary_method(self):
        return self.direct_field
class Inherited(Direct):
    inherited_field: int
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, member := range result.Pydantic {
		names = append(names, member.Name)
	}
	want := []string{"direct_field", "annotated_field", "model_config", "_cache", "declared", "computed_value", "inherited_field"}
	if !slices.Equal(names, want) {
		t.Fatalf("inferred model members = %+v, want %v", result.Pydantic, want)
	}
}

func TestPydanticInferenceDoesNotPreserveUnprovenModels(t *testing.T) {
	cases := map[string]map[string]string{
		"late base import":  {"src/source.py": "class Model(BaseModel):\n    field: str\nfrom pydantic import BaseModel\n"},
		"late alias target": {"src/source.py": "ModelBase = BaseModel\nfrom pydantic import BaseModel\nclass Model(ModelBase):\n    field: str\n"},
		"lookalike":         {"src/source.py": "class BaseModel: pass\nclass Model(BaseModel):\n    field: str\n"},
		"wildcard":          {"src/source.py": "from pydantic import *\nclass Model(BaseModel):\n    field: str\n"},
		"rebound import":    {"src/source.py": "from pydantic import BaseModel\nBaseModel = unknown\nclass Model(BaseModel):\n    field: str\n"},
		"unresolved alias":  {"src/source.py": "ModelBase = unknown\nclass Model(ModelBase):\n    field: str\n"},
		"conditional base":  {"src/source.py": "from pydantic import BaseModel\nif condition:\n    class Base(BaseModel): pass\nclass Model(Base):\n    field: str\n"},
		"local pydantic package": {
			"src/pydantic/__init__.py": "class BaseModel: pass\n",
			"src/source.py":            "from pydantic import BaseModel\nclass Model(BaseModel):\n    field: str\n",
		},
		"shadowed reexport": {
			"src/contracts.py": "from pydantic import BaseModel as ModelBase\nModelBase = unknown\n",
			"src/source.py":    "from contracts import ModelBase\nclass Model(ModelBase):\n    field: str\n",
		},
	}
	for name, sources := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := resolveTypeTestSources(t, sources)
			if err != nil || len(result.Pydantic) != 0 {
				t.Fatalf("unproven model acquired reachability: %+v, error = %v", result.Pydantic, err)
			}
		})
	}
}

func TestPydanticResponseCannotReplaceExactSourceMembers(t *testing.T) {
	python := typeTestInterpreter(t)
	project, err := AnalyzeProjectSources(t.Context(), python, []Input{{Path: "src/model.py", Source: "from pydantic import BaseModel\nclass Model(BaseModel):\n    field: str\n"}})
	if err != nil {
		t.Fatal(err)
	}
	modules := typeTestModules(project.Sources)
	for name, change := range map[string]func(*typeProjectResponse){
		"missing members": func(response *typeProjectResponse) { response.Pydantic = nil },
		"wrong field":     func(response *typeProjectResponse) { response.Pydantic[0].Name = "unrelated" },
		"wrong span":      func(response *typeProjectResponse) { response.Pydantic[0].End = 4 },
		"wrong path":      func(response *typeProjectResponse) { response.Pydantic[0].Path = "src/unrelated.py" },
		"duplicate field": func(response *typeProjectResponse) {
			response.Pydantic = append(response.Pydantic, response.Pydantic[0])
		},
	} {
		t.Run(name, func(t *testing.T) {
			response := typeProjectResponse{Imports: []ModuleImport{}, Protocol: typeProjectProtocol, Covered: []typeCoverage{{Path: modules[0].Path, SourceSHA256: modules[0].SourceSHA256}}, Reads: []TypedDictRead{}, Pydantic: []ReachabilityDefinition{{Path: modules[0].Path, Line: 3, End: 3, Name: "field"}}}
			change(&response)
			data, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeTypeProject(data, modules); err == nil {
				t.Fatal("invalid Pydantic source evidence was accepted")
			}
		})
	}
}
