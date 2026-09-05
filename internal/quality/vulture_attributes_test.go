package quality

import (
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type externalWriteFixture struct {
	source    string
	attribute policy.PythonExternalAttribute
	extra     map[string]string
}

func TestPythonVultureExternalReceiversKeepOnlyTheProvenAssignment(t *testing.T) {
	fixtures := externalWriteFixtures()
	alias := fixtures["parameter"]
	alias.source = strings.Replace(alias.source, "from external.runtime import Settings", "from external.runtime import Settings as Alias", 1)
	alias.source = strings.Replace(alias.source, "settings: Settings", "settings: Alias", 1)
	fixtures["import alias"] = alias
	reexport := fixtures["parameter"]
	reexport.source = strings.Replace(reexport.source, "from external.runtime import Settings", "from contracts import Settings", 1)
	reexport.extra = map[string]string{"src/contracts.py": "from external.runtime import Settings as Settings\n"}
	fixtures["local re-export"] = reexport
	for name, fixture := range fixtures {
		t.Run(name, func(t *testing.T) {
			response := runExternalWriteFixture(t, fixture)
			if len(response.Problems) != 0 || !slices.Contains(response.Resolved, pythonVultureAttributeID(fixture.attribute)) {
				t.Fatalf("external receiver was not resolved: %+v", response)
			}
			if slices.ContainsFunc(response.Diagnostics, func(diagnostic pythonVultureDiagnostic) bool { return diagnostic.Name == "output_path" }) {
				t.Fatalf("proven assignment reported dead: %+v", response.Diagnostics)
			}
			if !slices.ContainsFunc(response.Diagnostics, func(diagnostic pythonVultureDiagnostic) bool { return diagnostic.Name == "unused_path" }) {
				t.Fatalf("adjacent unread assignment was hidden: %+v", response.Diagnostics)
			}
		})
	}
}

func TestPythonVultureExternalReceiversRejectStaleAndUnprovenWrites(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		change  func(*externalWriteFixture)
		want    string
	}{
		{"moved binding", "parameter", func(f *externalWriteFixture) { f.attribute.Receiver.Binding.Column++ }, "binding"},
		{"moved write", "parameter", func(f *externalWriteFixture) { f.attribute.Write.Column++ }, "stale or ambiguous"},
		{"changed type", "parameter", func(f *externalWriteFixture) { f.attribute.Receiver.Type = "another.Settings" }, "does not match"},
		{"local type", "parameter", func(f *externalWriteFixture) {
			f.source = strings.Replace(f.source, "from external.runtime import Settings", "class Settings: pass", 1)
		}, "not an exact external import"},
		{"type shadow", "parameter", func(f *externalWriteFixture) { f.source += "Settings = object\n" }, "shadowed or ambiguous"},
		{"re-export cycle", "parameter", func(f *externalWriteFixture) {
			f.source = strings.Replace(f.source, "from external.runtime import Settings", "from contracts import Settings", 1)
			f.extra = map[string]string{
				"src/contracts.py": "from bridge import Settings as Settings\n",
				"src/bridge.py":    "from contracts import Settings as Settings\n",
			}
		}, "cyclic"},
		{"escaping re-export", "parameter", func(f *externalWriteFixture) {
			f.source = strings.Replace(f.source, "from external.runtime import Settings", "from contracts.bridge import Settings", 1)
			f.extra = map[string]string{
				"src/contracts/__init__.py": "",
				"src/contracts/bridge.py":   "from ..external.runtime import Settings\n",
			}
		}, "escapes"},
		{"wildcard", "parameter", func(f *externalWriteFixture) {
			f.source = strings.Replace(f.source, "import Settings", "import *", 1)
		}, "wildcard"},
		{"union annotation", "parameter", func(f *externalWriteFixture) {
			f.source = strings.Replace(f.source, "settings: Settings", "settings: Settings | None", 1)
		}, "exact type"},
		{"parameter rebound", "parameter", insertExternalReceiverRebinding, "rebound"},
		{"local rebound", "local", insertExternalReceiverRebinding, "rebound"},
		{"nested default rebinding", "parameter", func(f *externalWriteFixture) {
			f.source = strings.Replace(f.source, "    settings.output_path", "    def helper(value=(settings := object())): pass\n    settings.output_path", 1)
			f.attribute.Write.Line++
		}, "rebound"},
		{"nested annotation rebinding", "parameter", func(f *externalWriteFixture) {
			f.source = strings.Replace(f.source, "    settings.output_path", "    def helper(value: (settings := object())): pass\n    settings.output_path", 1)
			f.attribute.Write.Line++
		}, "rebound"},
		{"nonlocal rebinding", "parameter", func(f *externalWriteFixture) {
			f.source = strings.Replace(f.source, "    settings.output_path", "    def change():\n        nonlocal settings\n        settings = object()\n    change()\n    settings.output_path", 1)
			f.attribute.Write.Line += 4
		}, "nonlocal binding"},
		{"uninitialized local", "local", func(f *externalWriteFixture) {
			f.source = strings.Replace(f.source, " = Settings()", "", 1)
		}, "annotated local binding"},
		{"same line collision", "parameter", func(f *externalWriteFixture) {
			f.source = strings.Replace(f.source, `settings.output_path = "result"`, `settings.output_path = "result"; other.output_path = "unused"`, 1)
		}, "same-line"},
		{"nested chain", "parameter", func(f *externalWriteFixture) {
			f.source = strings.Replace(f.source, "settings.output_path", "settings.child.output_path", 1)
		}, "stale or ambiguous"},
		{"self without external base", "self base", func(f *externalWriteFixture) {
			f.source = strings.Replace(f.source, "class Runtime(Settings):", "class Runtime:", 1)
		}, "consumer is stale or ambiguous"},
		{"self consumer moved", "self base", func(f *externalWriteFixture) { f.attribute.Receiver.Consumer.Site.Column++ }, "consumer is stale or ambiguous"},
		{"self registration consumes another class", "self registration", func(f *externalWriteFixture) {
			f.source = strings.Replace(f.source, "register(Runtime)", "register(Other)", 1)
		}, "consumer is stale or ambiguous"},
		{"duplicate registration", "self registration", func(f *externalWriteFixture) { f.source += "register(Runtime)\n" }, "consumer is stale or ambiguous"},
		{"class method", "self base", func(f *externalWriteFixture) {
			f.source = strings.Replace(f.source, "    def configure", "    @classmethod\n    def configure", 1)
			f.attribute.Receiver.Binding.Line++
			f.attribute.Write.Line++
		}, "instance method"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := externalWriteFixtures()[test.fixture]
			test.change(&fixture)
			response := runExternalWriteFixture(t, fixture)
			if len(response.Resolved) != 0 || len(response.Problems) != 1 || !strings.Contains(response.Problems[0].Message, test.want) {
				t.Fatalf("external receiver result = %+v; want %q", response, test.want)
			}
			if !slices.ContainsFunc(response.Diagnostics, func(diagnostic pythonVultureDiagnostic) bool { return diagnostic.Name == "output_path" }) {
				t.Fatalf("unproven assignment was hidden: %+v", response.Diagnostics)
			}
		})
	}
}

func TestPythonVultureExternalWriteLeavesLaterSameNamedAssignmentVisible(t *testing.T) {
	fixture := externalWriteFixtures()["parameter"]
	fixture.source += "    settings = object()\n    settings.output_path = \"unused\"\n"
	response := runExternalWriteFixture(t, fixture)
	if len(response.Problems) != 0 || !slices.Contains(response.Resolved, pythonVultureAttributeID(fixture.attribute)) {
		t.Fatalf("earlier proven assignment lost its evidence: %+v", response)
	}
	if !slices.ContainsFunc(response.Diagnostics, func(diagnostic pythonVultureDiagnostic) bool {
		return diagnostic.Name == "output_path" && diagnostic.Line == 7
	}) || slices.ContainsFunc(response.Diagnostics, func(diagnostic pythonVultureDiagnostic) bool {
		return diagnostic.Name == "output_path" && diagnostic.Line == 4
	}) {
		t.Fatalf("assignment evidence escaped its exact occurrence: %+v", response.Diagnostics)
	}
}

func insertExternalReceiverRebinding(fixture *externalWriteFixture) {
	fixture.source = strings.Replace(fixture.source, "    settings.output_path", "    settings = object()\n    settings.output_path", 1)
	fixture.attribute.Write.Line++
}

func externalWriteFixtures() map[string]externalWriteFixture {
	attribute := policy.PythonExternalAttribute{
		Project: "pyproject.toml", Module: "plugin", Callable: "configure", Attribute: "output_path",
		Write: policy.PythonSourceLocation{Line: 4, Column: 5},
		Receiver: policy.PythonExternalReceiver{
			Kind: "parameter", Name: "settings", Binding: policy.PythonSourceLocation{Line: 3, Column: 15}, Type: "external.runtime.Settings",
		},
	}
	fixtures := map[string]externalWriteFixture{
		"parameter": {attribute: attribute, source: "from external.runtime import Settings\n\ndef configure(settings: Settings):\n    settings.output_path = \"result\"\n    settings.unused_path = \"unused\"\n"},
	}
	attribute.Receiver.Kind = "local"
	attribute.Receiver.Binding = policy.PythonSourceLocation{Line: 3, Column: 5}
	fixtures["local"] = externalWriteFixture{attribute: attribute, source: "from external.runtime import Settings\ndef configure():\n    settings: Settings = Settings()\n    settings.output_path = \"result\"\n    settings.unused_path = \"unused\"\n"}
	for _, kind := range []string{"base", "protocol", "decorator", "registration"} {
		fixtures["self "+kind] = externalSelfWriteFixture(kind)
	}
	return fixtures
}

func externalSelfWriteFixture(kind string) externalWriteFixture {
	consumer := &policy.PythonExternalConsumer{Kind: kind, Qualified: "external.runtime.Settings", Site: policy.PythonSourceLocation{Line: 2, Column: 15}}
	receiver := policy.PythonExternalReceiver{Kind: "self", Name: "self", Binding: policy.PythonSourceLocation{Line: 3, Column: 19}, Consumer: consumer}
	attribute := policy.PythonExternalAttribute{
		Project: "pyproject.toml", Module: "plugin", Callable: "Runtime.configure", Attribute: "output_path", Receiver: receiver,
		Write: policy.PythonSourceLocation{Line: 4, Column: 9},
	}
	header := "from external.runtime import Settings\nclass Runtime(Settings):\n"
	if kind == "decorator" || kind == "registration" {
		consumer.Qualified = "external.runtime.register"
		header = "from external.runtime import register\nclass Runtime:\n"
	}
	if kind == "decorator" {
		header = "from external.runtime import register\n@register\nclass Runtime:\n"
		consumer.Site = policy.PythonSourceLocation{Line: 2, Column: 2}
		attribute.Receiver.Binding.Line++
		attribute.Write.Line++
	}
	source := header + "    def configure(self):\n        self.output_path = \"result\"\n        self.unused_path = \"unused\"\n"
	if kind == "registration" {
		source += "register(Runtime)\n"
		consumer.Site = policy.PythonSourceLocation{Line: 6, Column: 1}
	}
	return externalWriteFixture{source: source, attribute: attribute}
}

func runExternalWriteFixture(t *testing.T, fixture externalWriteFixture) pythonVultureResponse {
	t.Helper()
	repo := pythonQualityRepository(t)
	repo.PolicyRoot = pythonVulturePolicyRoot(t)
	if !pythonVultureRuntimeInstalled(t, repo) {
		t.Fatal("policy CPython with Vulture must be installed for external-write evidence tests")
	}
	repo.Config.Scope.PythonExternalAttributes = []policy.PythonExternalAttribute{fixture.attribute}
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nrequires-python = \"==3.12.*\"\ndependencies = []\n")
	writeQualityFile(t, repo.Root, "src/plugin.py", fixture.source)
	for path, source := range fixture.extra {
		writeQualityFile(t, repo.Root, path, source)
	}
	project := pythonVultureProject(t, repo)
	command, err := pythonVultureCommand(repo, project)
	if err != nil {
		t.Fatal(err)
	}
	_, output, err := (runner.OSRunner{}).RunStructured(t.Context(), repo.Root, command)
	if err != nil {
		t.Fatal(err)
	}
	response, err := parsePythonVultureResponse(output.Stdout)
	if err != nil || response.Error != "" {
		t.Fatalf("external-write output: %+v, %v", response, err)
	}
	return response
}
