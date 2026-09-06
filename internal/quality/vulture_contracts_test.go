package quality

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestPythonRepositoryContractsPreserveExactConsumers(t *testing.T) {
	sources := map[string]string{
		"src/bridge.py": "from vendor.api import Base as External\nclass Parent(External):\n    pass\n",
		"src/example.py": `from bridge import Parent
from vendor.api import register
from typing import ClassVar
class Model(Parent):
    field: str
    unused_class_var: ClassVar[str]
    setting = True
    @register()
    def registered(self):
        return 1
    def callback(self):
        return 1
    def unused_hook(self):
        return 1
class Unrelated:
    def callback(self):
        return 1
def configure():
    model = Model()
    model.setting = True
    model = Unrelated()
    model.setting = False
`,
	}
	contract := policy.PythonContract{Project: "pyproject.toml", Kind: "type", Target: "vendor.api.Base", Members: []string{"callback"}, Attributes: []string{"setting"}, Decorators: []string{"vendor.api.register"}, AnnotatedFields: true, Reason: "The framework consumes model declarations and callbacks."}
	repo, project, response, output := runContractVulture(t, sources, []policy.PythonContract{contract})
	if response.Error != "" || len(response.Problems) != 0 || len(response.Resolved) != 1 {
		t.Fatalf("contract resolution failed: %+v", response)
	}
	for _, name := range []string{"field", "registered"} {
		if slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == name }) {
			t.Fatalf("consumed %s reported dead: %+v", name, response.Diagnostics)
		}
	}
	for _, name := range []string{"unused_class_var", "unused_hook", "callback", "setting"} {
		if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == name }) {
			t.Fatalf("unrelated %s hidden: %+v", name, response.Diagnostics)
		}
	}
	for _, finding := range pythonVultureFindings(repo, project, output) {
		if finding.Check != "quality.deadCode" {
			t.Fatalf("invalid adapter evidence: %+v", finding)
		}
	}
	_, _, without, _ := runContractVulture(t, sources, nil)
	if !slices.ContainsFunc(without.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "field" }) {
		t.Fatal("third-party fields were preserved without a declaration")
	}
}

func TestPythonRepositoryContractNestedEntryPoints(t *testing.T) {
	source := `class Adapter:
    def execute(self):
        return 1
    def unused_hook(self):
        return 2
class Registry:
    def __init__(self):
        self.primary = Adapter()
registry = Registry()
unused_export = Adapter()
`
	contract := policy.PythonContract{Project: "pyproject.toml", Kind: "entry-point", Target: "plugins:registry.primary", Members: []string{"execute"}, Reason: "Runtime configuration selects this adapter."}
	for _, target := range []string{"plugins:registry.primary", "plugins:registry.missing"} {
		t.Run(target, func(t *testing.T) {
			contract.Target = target
			repo, project, response, output := runContractVulture(t, map[string]string{"src/plugins.py": source}, []policy.PythonContract{contract})
			if response.Error != "" {
				t.Fatalf("analysis failed: %+v", response)
			}
			if target == "plugins:registry.missing" {
				if len(response.Problems) != 1 {
					t.Fatalf("stale export accepted: %+v", response)
				}
				return
			}
			if len(response.Problems) != 0 {
				t.Fatalf("nested export rejected: %+v", response)
			}
			for _, name := range []string{"registry", "primary", "execute"} {
				if slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == name }) {
					t.Fatalf("consumed export %s dead: %+v", name, response.Diagnostics)
				}
			}
			for _, name := range []string{"unused_hook", "unused_export"} {
				if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == name }) {
					t.Fatalf("unrelated export %s hidden", name)
				}
			}
			for _, finding := range pythonVultureFindings(repo, project, output) {
				if finding.Check != "quality.deadCode" {
					t.Fatalf("invalid adapter evidence: %+v", finding)
				}
			}
		})
	}
}

func TestPythonRepositoryContractsRejectUnprovenBindings(t *testing.T) {
	contract := policy.PythonContract{Project: "pyproject.toml", Kind: "type", Target: "vendor.api.Base", Members: []string{"execute"}, Reason: "The framework calls this interface."}
	for name, source := range map[string]string{
		"rebound base":     "from vendor.api import Base\nBase = object\nclass Model(Base):\n    def execute(self):\n        return 1\n",
		"conditional base": "from vendor.api import Base\nif condition:\n    Parent = Base\nelse:\n    Parent = object\nclass Model(Parent):\n    def execute(self):\n        return 1\n",
		"late import":      "class Model(Base):\n    def execute(self):\n        return 1\nfrom vendor.api import Base\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, response, _ := runContractVulture(t, map[string]string{"src/models.py": source}, []policy.PythonContract{contract})
			if response.Error != "" || len(response.Problems) != 1 {
				t.Fatalf("unproven contract did not produce a diagnostic: %+v", response)
			}
			if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "execute" }) {
				t.Fatalf("unproven method hidden: %+v", response)
			}
		})
	}
}

func TestPythonEntryPointAnalysisDoesNotExecuteProjectCode(t *testing.T) {
	contract := policy.PythonContract{Project: "pyproject.toml", Kind: "entry-point", Target: "plugins:exported", Reason: "Runtime configuration selects this function."}
	source := "raise RuntimeError('project code must never execute')\ndef exported():\n    return 1\n"
	_, _, response, _ := runContractVulture(t, map[string]string{"src/plugins.py": source}, []policy.PythonContract{contract})
	if response.Error != "" || len(response.Problems) != 0 || len(response.Resolved) != 1 {
		t.Fatalf("contained static inference failed: %+v", response)
	}
}
