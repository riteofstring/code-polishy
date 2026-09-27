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

func TestPythonRepositoryContractPreservesSubclassAttributeWritesInLoops(t *testing.T) {
	source := `from vendor.api import Base
class Timed(Base):
    def _run(self, remaining):
        while remaining:
            self._downloaded = False
            self._unrelated = False
            remaining -= 1
    def _rebound(self, remaining):
        while remaining:
            self = object()
            self._downloaded = False
            remaining -= 1
    def _constructed(self, remaining):
        self = Timed()
        while remaining:
            self._downloaded = False
            remaining -= 1
def process(value: Base, remaining):
    for _ in range(remaining):
        value._downloaded = False
def process_rebound(value: Base, remaining):
    value = Timed()
    while remaining:
        value._downloaded = False
        remaining -= 1
def process_branch_assignment_first(value: Base, remaining, replace):
    if replace:
        value = Timed()
    else:
        while remaining:
            value._branch_assignment_first = False
            remaining -= 1
def process_branch_loop_first(value: Base, remaining, replace):
    if replace:
        while remaining:
            value._branch_loop_first = False
            remaining -= 1
    else:
        value = Timed()
def process_branch_merge(value: Base, remaining, replace):
    if replace:
        value = Timed()
    while remaining:
        value._branch_merge = False
        remaining -= 1
`
	contract := policy.PythonContract{Project: "pyproject.toml", Kind: "type", Target: "vendor.api.Base", Members: []string{"_run"}, Attributes: []string{"_downloaded", "_branch_assignment_first", "_branch_loop_first", "_branch_merge"}, Reason: "The framework reads the download state after running each trial."}
	_, _, response, _ := runContractVulture(t, map[string]string{"src/trial.py": source}, []policy.PythonContract{contract})
	if response.Error != "" || len(response.Problems) != 0 || len(response.Resolved) != 1 {
		t.Fatalf("contract resolution failed: %+v", response)
	}
	if slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "_downloaded" && d.Line == 5 }) {
		t.Fatalf("declared loop attribute was reported dead: %+v", response.Diagnostics)
	}
	if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "_unrelated" }) {
		t.Fatalf("unrelated loop attribute was hidden: %+v", response.Diagnostics)
	}
	if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "_downloaded" && d.Line == 11 }) {
		t.Fatalf("rebound loop receiver was treated as the contracted type: %+v", response.Diagnostics)
	}
	if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "_downloaded" && d.Line == 16 }) {
		t.Fatalf("constructed loop receiver inherited parameter evidence: %+v", response.Diagnostics)
	}
	if slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "_downloaded" && d.Line == 20 }) {
		t.Fatalf("declared typed-parameter loop attribute was reported dead: %+v", response.Diagnostics)
	}
	if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "_downloaded" && d.Line == 24 }) {
		t.Fatalf("rebound typed-parameter loop receiver retained parameter evidence: %+v", response.Diagnostics)
	}
	for _, name := range []string{"_branch_assignment_first", "_branch_loop_first"} {
		if slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == name }) {
			t.Fatalf("still-bound branch receiver %s was reported dead: %+v", name, response.Diagnostics)
		}
	}
	if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "_branch_merge" }) {
		t.Fatalf("possibly rebound branch receiver retained parameter evidence: %+v", response.Diagnostics)
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

func TestPythonEntryPointRejectsConditionalOrAmbiguousExports(t *testing.T) {
	contract := policy.PythonContract{Project: "pyproject.toml", Kind: "entry-point", Target: "plugins:exported", Reason: "Runtime configuration selects this object."}
	for name, source := range map[string]string{
		"conditional": "class Adapter: pass\nif condition:\n    exported = Adapter()\n",
		"reassigned":  "class Adapter: pass\nexported = Adapter()\nexported = Adapter()\n",
		"missing":     "class Adapter: pass\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, response, _ := runContractVulture(t, map[string]string{"src/plugins.py": source}, []policy.PythonContract{contract})
			if response.Error != "" || len(response.Problems) != 1 {
				t.Fatalf("invalid export was accepted: %+v", response)
			}
		})
	}
}

func TestPythonContractExtendsBundledType(t *testing.T) {
	contract := policy.PythonContract{Project: "pyproject.toml", Kind: "type", Target: "io.RawIOBase", Members: []string{"custom_callback"}, Reason: "The application adds a callback to its stream interface."}
	source := "import io\nclass Stream(io.RawIOBase):\n    def custom_callback(self):\n        return 1\n    def unused_hook(self):\n        return 2\n"
	_, _, response, _ := runContractVulture(t, map[string]string{"src/streams.py": source}, []policy.PythonContract{contract})
	if response.Error != "" || len(response.Problems) != 0 {
		t.Fatalf("extension rejected: %+v", response)
	}
	for _, diagnostic := range response.Diagnostics {
		if diagnostic.Name == "custom_callback" {
			t.Fatalf("configured callback is dead: %+v", response)
		}
	}
	if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "unused_hook" }) {
		t.Fatal("unrelated hook was hidden")
	}
}
