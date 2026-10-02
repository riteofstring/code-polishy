package quality

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestPythonRepositoryContractPreservesCapturedReceivers(t *testing.T) {
	contract := policy.PythonContract{Project: "pyproject.toml", Kind: "type", Target: "vendor.api.Base", Members: []string{"prepare"}, Attributes: []string{"_flag"}, Reason: "The framework reads state reset by phase callbacks."}
	for _, test := range []struct {
		name, body string
		dead       bool
	}{
		{"captured self", "        def phase():\n            self._flag = False\n        phase()\n", false},
		{"async capture", "        async def phase():\n            self._flag = False\n        return phase\n", false},
		{"capture across levels", "        def phase():\n            def reset():\n                self._flag = False\n            reset()\n        phase()\n", false},
		{"captured alias", "        receiver = self\n        def phase():\n            receiver._flag = False\n        phase()\n", false},
		{"capture in loop", "        def phase():\n            for number in range(2):\n                self._flag = number\n        phase()\n", false},
		{"parameter shadow", "        def phase(self):\n            self._flag = False\n        phase(object())\n", true},
		{"later local shadow", "        def phase():\n            self._flag = False\n            self = object()\n            return self\n        phase()\n", true},
		{"import shadow", "        def phase():\n            import foreign as self\n            self._flag = False\n        phase()\n", true},
		{"exception shadow", "        def phase():\n            try:\n                unknown()\n            except Exception as self:\n                self._flag = False\n        phase()\n", true},
		{"deleted receiver", "        def phase():\n            del self\n            self._flag = False\n        phase()\n", true},
		{"global receiver", "        def phase():\n            global self\n            self._flag = False\n        phase()\n", true},
		{"earlier outer rebind", "        self = object()\n        def phase():\n            self._flag = False\n        phase()\n", true},
		{"later outer rebind", "        def phase():\n            self._flag = False\n        self = object()\n        phase()\n", true},
		{"nonlocal mutation", "        def phase():\n            self._flag = False\n        def replace():\n            nonlocal self\n            self = object()\n        replace()\n        phase()\n", true},
		{"unrelated captured type", "        receiver = object()\n        def phase():\n            receiver._flag = False\n        phase()\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "from vendor.api import Base\nclass Trial(Base):\n    def prepare(self):\n" + test.body + "def main():\n    trial = Trial()\n    trial.prepare()\n    return trial\n"
			if test.name == "async capture" {
				source = strings.Replace(source, "def prepare", "async def prepare", 1)
			}
			_, _, response, _ := runContractVulture(t, map[string]string{"src/trial.py": source}, []policy.PythonContract{contract})
			if response.Error != "" || response.FactsError != "" || len(response.Problems) != 0 || len(response.Resolved) != 1 {
				t.Fatalf("contract analysis failed: %+v", response)
			}
			dead := slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "_flag" })
			if dead != test.dead {
				t.Fatalf("_flag dead=%v, want %v: %+v", dead, test.dead, response.Diagnostics)
			}
		})
	}
}

func TestPythonRepositoryContractPreservesExactCallbackParameters(t *testing.T) {
	contract := policy.PythonContract{Project: "pyproject.toml", Kind: "type", Target: "vendor.api.Base", Members: []string{"run", "populate_context_post_run", "dispatch"}, CallbackParameters: map[string][]string{"run": {"context"}, "populate_context_post_run": {"context"}, "dispatch": {"payload", "context", "args", "options"}}, Reason: "The runtime supplies arguments to registered callbacks."}
	source := `from vendor.api import Base
class Agent(Base):
    async def run(self, context, unused_optional=None):
        return None
    def populate_context_post_run(self, context):
        return None
    def dispatch(self, payload, /, *args, context, unused_keyword=None, **options):
        return None
class Unrelated:
    def run(self, context):
        return None
def unrelated(context):
    return True
`
	_, _, response, _ := runContractVulture(t, map[string]string{"src/agent.py": source}, []policy.PythonContract{contract})
	if response.Error != "" || response.FactsError != "" || len(response.Problems) != 0 || len(response.Resolved) != 1 {
		t.Fatalf("callback analysis failed: %+v", response)
	}
	for _, diagnostic := range response.Diagnostics {
		if diagnostic.Line < 9 && slices.Contains([]string{"context", "payload", "args", "options", "run", "populate_context_post_run", "dispatch"}, diagnostic.Name) {
			t.Fatalf("externally supplied callback argument reported dead: %+v", diagnostic)
		}
	}
	for _, expected := range []struct {
		line int
		name string
	}{{3, "unused_optional"}, {7, "unused_keyword"}, {10, "context"}, {12, "context"}} {
		if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Line == expected.line && d.Name == expected.name }) {
			t.Fatalf("unrelated unused argument %v hidden: %+v", expected, response.Diagnostics)
		}
	}
	contract.CallbackParameters = nil
	_, _, without, _ := runContractVulture(t, map[string]string{"src/agent.py": source}, []policy.PythonContract{contract})
	if !slices.ContainsFunc(without.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Line == 3 && d.Name == "context" }) {
		t.Fatalf("callback parameter retained without a declaration: %+v", without)
	}
}

func TestPythonRepositoryContractPreservesMultilineCallbackParameters(t *testing.T) {
	contract := policy.PythonContract{Project: "pyproject.toml", Kind: "type", Target: "vendor.api.Base", Members: []string{"run"}, CallbackParameters: map[string][]string{"run": {"context"}}, Reason: "The runtime supplies the annotated context argument."}
	for _, test := range []struct{ name, receiver, prefix, suffix string }{
		{"positional", "self,", "", ""},
		{"positional only", "self,", "", "        /,\n"},
		{"keyword only", "self, *,", "", ""},
		{"variadic positional", "self,", "*", ""},
		{"variadic keyword", "self,", "**", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := fmt.Sprintf("from vendor.api import Base\nclass Agent(Base):\n    def run(\n        %s\n        %scontext: tuple[\n            str,\n            int,\n        ],\n%s    ):\n        return None\n", test.receiver, test.prefix, test.suffix)
			_, _, without, _ := runContractVulture(t, map[string]string{"src/agent.py": source}, nil)
			if !slices.ContainsFunc(without.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Line == 5 && d.End == 8 && d.Name == "context" }) {
				t.Fatalf("multiline unused argument missing without contract: %+v", without)
			}
			_, _, response, _ := runContractVulture(t, map[string]string{"src/agent.py": source}, []policy.PythonContract{contract})
			if response.Error != "" || response.FactsError != "" || len(response.Problems) != 0 || len(response.Resolved) != 1 {
				t.Fatalf("multiline callback analysis failed: %+v", response)
			}
			if slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "context" }) {
				t.Fatalf("externally supplied multiline argument reported dead: %+v", response.Diagnostics)
			}
		})
	}
}

func TestPythonRepositoryContractRejectsStaleCallbackParameters(t *testing.T) {
	contract := policy.PythonContract{Project: "pyproject.toml", Kind: "type", Target: "vendor.api.Base", Members: []string{"run"}, CallbackParameters: map[string][]string{"run": {"context"}}, Attributes: []string{"state"}, Reason: "The runtime passes context to the run callback."}
	for name, method := range map[string]string{
		"missing parameter": "    def run(self, renamed):\n        return None\n",
		"missing member":    "    def unrelated(self, context):\n        return None\n",
		"unrelated member":  "class Other:\n    def run(self, context):\n        return None\n",
	} {
		t.Run(name, func(t *testing.T) {
			source := "from vendor.api import Base\nclass Agent(Base):\n    state = True\n" + method
			_, _, response, _ := runContractVulture(t, map[string]string{"src/agent.py": source}, []policy.PythonContract{contract})
			if response.Error != "" || response.FactsError != "" || len(response.Problems) != 1 || len(response.Resolved) != 0 {
				t.Fatalf("stale callback declaration accepted: %+v", response)
			}
		})
	}
}

func TestPythonRepositoryContractCallbackParametersSpanFiles(t *testing.T) {
	contract := policy.PythonContract{Project: "pyproject.toml", Kind: "type", Target: "vendor.api.Base", Members: []string{"run", "after"}, CallbackParameters: map[string][]string{"run": {"context"}, "after": {"result"}}, Reason: "Separate adapters implement different callback members."}
	sources := map[string]string{}
	for name, parameter := range map[string]string{"run": "context", "after": "result"} {
		sources["src/"+name+".py"] = fmt.Sprintf("from vendor.api import Base\nclass Agent(Base):\n    def %s(self, %s):\n        return None\n", name, parameter)
	}
	_, _, response, _ := runContractVulture(t, sources, []policy.PythonContract{contract})
	if response.Error != "" || response.FactsError != "" || len(response.Problems) != 0 || len(response.Resolved) != 1 {
		t.Fatalf("cross-file callbacks rejected: %+v", response)
	}
	for _, diagnostic := range response.Diagnostics {
		if slices.Contains([]string{"context", "result", "run", "after"}, diagnostic.Name) {
			t.Fatalf("cross-file callback argument reported dead: %+v", diagnostic)
		}
	}
}

func TestPythonRepositoryContractKeepsAmbiguousParameterFindings(t *testing.T) {
	contract := policy.PythonContract{Project: "pyproject.toml", Kind: "type", Target: "vendor.api.Base", Members: []string{"run"}, CallbackParameters: map[string][]string{"run": {"context"}}, Reason: "The runtime supplies the callback context argument."}
	for name, method := range map[string]string{
		"lambda parameter": "    def run(self, context=lambda context: None):\n        return None\n",
		"local write":      "    def run(self, context): context = None\n",
	} {
		t.Run(name, func(t *testing.T) {
			source := "from vendor.api import Base\nclass Agent(Base):\n" + method
			_, _, response, _ := runContractVulture(t, map[string]string{"src/agent.py": source}, []policy.PythonContract{contract})
			if response.Error != "" || response.FactsError != "" || len(response.Problems) != 0 || len(response.Resolved) != 1 {
				t.Fatalf("callback analysis failed: %+v", response)
			}
			if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Line == 3 && d.Name == "context" }) {
				t.Fatalf("same-line unused declaration hidden by callback argument: %+v", response)
			}
		})
	}
}

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

func TestPythonRepositoryContractPreservesReceiversAcrossComprehensionScopes(t *testing.T) {
	source := `from vendor.api import Base
class Handler(Base):
    def collect(self, values):
        listed = [value for value in values]
        self._after_list = False
        mapped = {value: value for value in values}
        self._after_dict = False
        unique = {value for value in values}
        self._after_set = False
        total = sum(value for value in values)
        self._after_generator = False
        shadowed = [self for self in values]
        self._after_shadow = False
        return listed, mapped, unique, total, shadowed
    def rebound(self, values):
        selected = [value for value in values if (self := value)]
        self._after_rebind = False
        return selected
`
	preserved := []string{"_after_list", "_after_dict", "_after_set", "_after_generator", "_after_shadow"}
	contract := policy.PythonContract{Project: "pyproject.toml", Kind: "type", Target: "vendor.api.Base", Attributes: append(preserved, "_after_rebind"), Reason: "The framework reads state written by handler subclasses."}
	_, _, response, _ := runContractVulture(t, map[string]string{"src/handler.py": source}, []policy.PythonContract{contract})
	if response.Error != "" || len(response.Problems) != 0 || len(response.Resolved) != 1 {
		t.Fatalf("contract resolution failed: %+v", response)
	}
	for _, name := range preserved {
		if slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == name }) {
			t.Fatalf("declared attribute %s after a comprehension was reported dead: %+v", name, response.Diagnostics)
		}
	}
	if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "_after_rebind" }) {
		t.Fatalf("rebound comprehension receiver retained contract evidence: %+v", response.Diagnostics)
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
