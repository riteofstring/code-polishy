package pythonfacts

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const runtimeTestProtocolSource = "from typing import Protocol, runtime_checkable\n@runtime_checkable\nclass Contract(Protocol):\n    def run(self):\n        ...\n"
const runtimeTestLoaderImports = "from pkgutil import resolve_name as resolve\nfrom contracts import Contract\n"
const runtimeTestGuardBody = "    plugin = resolve(name)\n    if not isinstance(plugin, Contract):\n        raise TypeError('invalid plugin')\n    return plugin\n"

func TestRuntimeChecksProveRejectingContractsOnTheLoadedValue(t *testing.T) {
	for name, fixture := range map[string]struct {
		body     string
		contract string
		kind     string
		bindings []string
	}{
		"protocol instance":      {runtimeTestGuardBody, runtimeTestProtocolSource, "isinstance", []string{"plugin"}},
		"protocol class":         {strings.ReplaceAll(runtimeTestGuardBody, "isinstance", "issubclass"), runtimeTestProtocolSource, "issubclass", []string{"plugin"}},
		"governed base":          {runtimeTestGuardBody, "class Contract:\n    pass\n", "isinstance", []string{"plugin"}},
		"alias chain":            {"    loaded = resolve(name)\n    alias = loaded\n    plugin: Contract = alias\n    if not isinstance(plugin, Contract):\n        raise TypeError\n    return plugin\n", runtimeTestProtocolSource, "isinstance", []string{"loaded", "alias", "plugin"}},
		"inline":                 {"    if not isinstance(resolve(name), Contract):\n        raise TypeError\n", runtimeTestProtocolSource, "isinstance", []string{}},
		"inherited base":         {runtimeTestGuardBody, "class Base:\n    pass\nclass Contract(Base):\n    pass\n", "isinstance", []string{"plugin"}},
		"data protocol instance": {runtimeTestGuardBody, strings.ReplaceAll(runtimeTestProtocolSource, "    def run(self):\n        ...", "    label: str"), "isinstance", []string{"plugin"}},
	} {
		t.Run(name, func(t *testing.T) {
			modules, input := runtimeTestInput(t, runtimeTestLoaderImports+"def load(name):\n"+fixture.body, fixture.contract, fixture.kind, "contracts.Contract")
			result, err := ResolveRuntimeChecks(t.Context(), typeTestInterpreter(t), modules, []RuntimeCheckInput{input})
			if err != nil || len(result.Problems) != 0 || len(result.Evidence) != 1 {
				t.Fatalf("loaded value did not acquire runtime evidence: %+v, %v", result, err)
			}
			evidence := result.Evidence[0]
			names := []string{}
			for _, binding := range evidence.Bindings {
				names = append(names, binding.Name)
			}
			if evidence.RuntimeType != "contracts.Contract" || evidence.Kind != fixture.kind || !reflect.DeepEqual(names, fixture.bindings) {
				t.Fatalf("runtime type or loaded-value trace changed: %+v", evidence)
			}
			slices.Reverse(modules)
			reordered, err := ResolveRuntimeChecks(t.Context(), typeTestInterpreter(t), modules, []RuntimeCheckInput{input})
			if err != nil || !reflect.DeepEqual(result, reordered) {
				t.Fatalf("reordering source inputs changed runtime evidence: %+v, %v", reordered, err)
			}
		})
	}
}

func TestRuntimeChecksRejectUncheckedOrEscapingValues(t *testing.T) {
	for name, body := range map[string]string{
		"discarded boolean":       "    plugin = resolve(name)\n    isinstance(plugin, Contract)\n    return plugin\n",
		"truthiness":              "    plugin = resolve(name)\n    if plugin:\n        isinstance(plugin, Contract)\n    return plugin\n",
		"wrong value":             strings.Replace(runtimeTestGuardBody, "isinstance(plugin", "isinstance(name", 1),
		"use before check":        strings.Replace(runtimeTestGuardBody, "    if", "    plugin.run()\n    if", 1),
		"early return":            strings.Replace(runtimeTestGuardBody, "    if", "    return plugin\n    if", 1),
		"conditional check":       "    plugin = resolve(name)\n    if name:\n        if not isinstance(plugin, Contract):\n            raise TypeError\n    return plugin\n",
		"caught guard":            "    plugin = resolve(name)\n    try:\n        if not isinstance(plugin, Contract):\n            raise TypeError\n    except TypeError:\n        pass\n    return plugin\n",
		"suppressed inline check": "    with suppress(TypeError):\n        if not isinstance(resolve(name), Contract):\n            raise TypeError\n",
		"rebound value":           strings.Replace(runtimeTestGuardBody, "    if", "    plugin = name\n    if", 1),
		"rebound alias":           "    loaded = resolve(name)\n    plugin = loaded\n    loaded = name\n    if not isinstance(plugin, Contract):\n        raise TypeError\n    return plugin\n",
		"nested loader call":      strings.Replace(runtimeTestGuardBody, "resolve(name)", "resolve(name)()", 1),
		"no rejection":            "    plugin = resolve(name)\n    if not isinstance(plugin, Contract):\n        print('invalid')\n    return plugin\n",
		"wrong scope":             "    plugin = resolve(name)\n    def nested():\n        if not isinstance(plugin, Contract):\n            raise TypeError\n    return plugin\n",
		"shadowed builtin":        "    isinstance = lambda value, contract: True\n" + runtimeTestGuardBody,
	} {
		t.Run(name, func(t *testing.T) {
			modules, input := runtimeTestInput(t, runtimeTestLoaderImports+"def load(name):\n"+body, runtimeTestProtocolSource, "isinstance", "contracts.Contract")
			assertRuntimeCheckRejected(t, modules, input)
		})
	}
}

func TestRuntimeChecksRequireRuntimeCompatibleProtocols(t *testing.T) {
	for name, contract := range map[string]string{
		"property protocol class": strings.ReplaceAll(runtimeTestProtocolSource, "    def run(self):", "    @property\n    def run(self):"),
		"dunder data protocol":    strings.ReplaceAll(runtimeTestProtocolSource, "    def run(self):\n        ...", "    __call__: object"),
		"not runtime checkable":   strings.ReplaceAll(runtimeTestProtocolSource, "@runtime_checkable\n", ""),
		"data protocol class":     strings.ReplaceAll(runtimeTestProtocolSource, "    def run(self):\n        ...", "    label: str"),
		"inherited data protocol": "from typing import Protocol, runtime_checkable\nclass Base(Protocol):\n    label: str\n@runtime_checkable\nclass Contract(Base, Protocol):\n    def run(self):\n        ...\n",
		"unknown decorator":       strings.ReplaceAll(runtimeTestProtocolSource, "@runtime_checkable", "@unknown"),
		"custom metaclass":        "class Meta(type):\n    pass\nclass Contract(metaclass=Meta):\n    pass\n",
		"unknown external base":   "from external import Base\nclass Contract(Base):\n    pass\n",
		"conditional protocol":    "from typing import Protocol, runtime_checkable\nif True:\n    @runtime_checkable\n    class Contract(Protocol):\n        pass\n",
		"wildcard scope":          runtimeTestProtocolSource + "from unknown import *\n",
	} {
		t.Run(name, func(t *testing.T) {
			loader := runtimeTestLoaderImports + "def load(name):\n" + strings.ReplaceAll(runtimeTestGuardBody, "isinstance", "issubclass")
			modules, input := runtimeTestInput(t, loader, contract, "issubclass", "contracts.Contract")
			assertRuntimeCheckRejected(t, modules, input)
		})
	}
}

func TestRuntimeChecksResolveExactLocalValidators(t *testing.T) {
	valid := "def validate(plugin):\n    if not isinstance(plugin, Contract):\n        raise TypeError('invalid plugin')\n    return plugin\n"
	for name, fixture := range map[string]struct {
		validator, body string
		accepted        bool
	}{
		"generator helper": {strings.Replace(valid, "raise TypeError('invalid plugin')", "raise (yield plugin)", 1), "    plugin = resolve(name)\n    validate(plugin)\n    return plugin\n", false},
		"direct":           {valid, "    plugin = resolve(name)\n    validate(plugin)\n    return plugin\n", true},
		"inline":           {valid, "    return validate(resolve(name))\n", true},
		"async helper":     {"async " + valid, "    plugin = resolve(name)\n    validate(plugin)\n    return plugin\n", false},
		"annotation only":  {"def validate(plugin: Contract):\n    return plugin\n", "    plugin = resolve(name)\n    validate(plugin)\n    return plugin\n", false},
		"early return":     {strings.Replace(valid, "    if", "    return plugin\n    if", 1), "    plugin = resolve(name)\n    validate(plugin)\n    return plugin\n", false},
		"ignored check":    {"def validate(plugin):\n    isinstance(plugin, Contract)\n    return plugin\n", "    plugin = resolve(name)\n    validate(plugin)\n    return plugin\n", false},
		"wrong parameter":  {strings.Replace(valid, "isinstance(plugin", "isinstance(Contract", 1), "    plugin = resolve(name)\n    validate(plugin)\n    return plugin\n", false},
		"check after use":  {valid, "    plugin = resolve(name)\n    plugin.run()\n    validate(plugin)\n    return plugin\n", false},
		"caught validator": {valid, "    plugin = resolve(name)\n    try:\n        validate(plugin)\n    except TypeError:\n        pass\n    return plugin\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			loader := "from pkgutil import resolve_name as resolve\nfrom contracts import validate\ndef load(name):\n" + fixture.body
			modules, input := runtimeTestInput(t, loader, runtimeTestProtocolSource+fixture.validator, "validator-call", "contracts.validate")
			if !fixture.accepted {
				assertRuntimeCheckRejected(t, modules, input)
				return
			}
			result, err := ResolveRuntimeChecks(t.Context(), typeTestInterpreter(t), modules, []RuntimeCheckInput{input})
			if err != nil || len(result.Problems) != 0 || len(result.Evidence) != 1 || result.Evidence[0].RuntimeType != "contracts.Contract" || result.Evidence[0].Contract != "contracts.validate" {
				t.Fatalf("validator did not prove its rejecting type contract: %+v, %v", result, err)
			}
		})
	}
}

func runtimeTestInput(t *testing.T, loader, contract, kind, protocol string) ([]TypeModule, RuntimeCheckInput) {
	t.Helper()
	project, err := AnalyzeProjectSources(t.Context(), typeTestInterpreter(t), []Input{
		{Path: "app.py", Source: loader + "\nraise RuntimeError('source must not execute')\n"},
		{Path: "contracts.py", Source: contract},
	})
	if err != nil {
		t.Fatal(err)
	}
	facts := decodeConsumerTestFacts(t, project.Sources[0])
	load := consumerTestCall(t, facts, "resolve")
	checkName := kind
	if kind == "validator-call" {
		checkName = "validate"
	}
	check := consumerTestCall(t, facts, checkName)
	consumer, err := json.Marshal(map[string]any{
		"kind": "callsite", "importer": "app.py", "module": "app", "callable": "load",
		"site": load.Site, "callee": "pkgutil.resolve_name", "shape": "module-object-call/v1",
		"argument": load.Arguments[0].Text, "sourceSha256": project.Sources[0].SHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	return typeTestModules(project.Sources), RuntimeCheckInput{ID: "external-loader", Consumer: consumer, Check: RuntimeCheck{Kind: kind, Protocol: protocol, Site: SourceLocation{Line: check.Site.Line, Column: check.Site.Column}}}
}

func assertRuntimeCheckRejected(t *testing.T, modules []TypeModule, input RuntimeCheckInput) {
	t.Helper()
	result, err := ResolveRuntimeChecks(t.Context(), typeTestInterpreter(t), modules, []RuntimeCheckInput{input})
	if err != nil || len(result.Evidence) != 0 || len(result.Problems) != 1 || result.Problems[0].ID != input.ID || result.Problems[0].Message == "" {
		t.Fatalf("unsupported runtime check did not fail with exact evidence: %+v, %v", result, err)
	}
}

func assertPartitionedRuntimeChecks(t *testing.T, python string, first, second SourceProject) {
	t.Helper()
	consumer := fmt.Sprintf(`{"kind":"callsite","importer":"a_loader.py","module":"a_loader","callable":"load","site":{"line":3,"column":14},"callee":"pkgutil.resolve_name","shape":"module-object-call/v1","argument":"'z_plugin:Plugin'","sourceSha256":"%s"}`, first.Sources[0].SHA256)
	input := RuntimeCheckInput{ID: "partitioned-loader", Consumer: json.RawMessage(consumer), Check: RuntimeCheck{Kind: "issubclass", Protocol: "z_bridge.Contract", Site: SourceLocation{Line: 4, Column: 12}}}
	result, err := ResolveRuntimeChecks(t.Context(), python, typeTestModules(first.Sources), []RuntimeCheckInput{input})
	if err != nil || len(result.Problems) != 0 || len(result.Evidence) != 1 || result.Evidence[0].RuntimeType != "z_contracts.Contract" || len(result.Evidence[0].Bindings) != 1 {
		t.Fatalf("runtime check did not resolve across project partitions: %+v, %v", result, err)
	}
	reordered, err := ResolveRuntimeChecks(t.Context(), python, typeTestModules(second.Sources), []RuntimeCheckInput{input})
	if err != nil || !reflect.DeepEqual(result, reordered) {
		t.Fatalf("partition order changed runtime contract evidence: %+v, %v", reordered, err)
	}
}
