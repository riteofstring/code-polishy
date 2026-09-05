package quality

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestPythonReachabilityKeepsOnlyConsumerBoundTargets(t *testing.T) {
	response, findings := runDynamicReachability(t, dynamicReachabilityFixture())
	if len(response.Reachability) != 1 || len(response.Reachability[0].Targets) != 1 || len(response.Problems) != 0 {
		t.Fatalf("consumer evidence = %+v, findings = %+v", response, findings)
	}
	if slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "on_event" }) {
		t.Fatalf("consumed method remained dead: %+v", response.Diagnostics)
	}
	if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "unused_hook" }) {
		t.Fatalf("unrelated method was hidden: %+v", response.Diagnostics)
	}
	if slices.ContainsFunc(findings, func(f policy.Finding) bool {
		return f.Check == "policy.pythonReachability" || f.Check == "quality.deadCodeCoverage"
	}) {
		t.Fatalf("valid evidence failed: %+v", findings)
	}
}

func TestPythonReachabilityRejectsDisconnectedTargetConsumers(t *testing.T) {
	for name, mutate := range map[string]func(*dynamicFixture){
		"stale digest":    func(f *dynamicFixture) { f.sources["src/loader.py"] += "\n" },
		"moved call":      func(f *dynamicFixture) { f.reference.Consumer.Site.Line++ },
		"wrong callable":  func(f *dynamicFixture) { f.reference.Consumer.Callable = "missing" },
		"wrong target":    func(f *dynamicFixture) { f.reference.Target.Symbol = "Plugin.unused_hook" },
		"old argument":    func(f *dynamicFixture) { f.reference.Consumer.Argument = "'handler:Plugin'" },
		"external target": func(f *dynamicFixture) { delete(f.sources, "src/handler.py") },
		"wrong project":   func(f *dynamicFixture) { f.reference.Consumer.Importer = "other/loader.py" },
		"shadowed loader": func(f *dynamicFixture) {
			f.sources["src/loader.py"] = strings.Replace(f.sources["src/loader.py"], "def load():", "def load(resolve_name):", 1)
			refreshDynamicConsumer(f)
		},
		"wildcard loader": func(f *dynamicFixture) {
			f.sources["src/loader.py"] = strings.Replace(f.sources["src/loader.py"], "import resolve_name", "import *", 1)
			refreshDynamicConsumer(f)
		},
		"ambiguous target": func(f *dynamicFixture) { f.sources["src/handler.py"] += "\nclass Plugin:\n    pass\n" },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := dynamicReachabilityFixture()
			mutate(&fixture)
			response, findings := runDynamicReachability(t, fixture)
			if len(response.Reachability) != 0 || !slices.ContainsFunc(findings, func(f policy.Finding) bool { return f.Check == "policy.pythonReachability" }) {
				t.Fatalf("disconnected consumer accepted: %+v, %+v", response, findings)
			}
		})
	}
}

func TestPythonReachabilityDerivesCurrentRegistryTargets(t *testing.T) {
	fixture := registryReachabilityFixture()
	first, findings := runDynamicReachability(t, fixture)
	if len(first.Reachability) != 1 || len(first.Reachability[0].Targets) != 1 || len(first.Problems) != 0 {
		t.Fatalf("registry evidence = %+v, %+v", first, findings)
	}
	fixture.registry = `{"plugins":{"alternate":"handler:Plugin.unused_hook"}}`
	second, findings := runDynamicReachability(t, fixture)
	if len(second.Reachability) != 1 || second.Reachability[0].Identity == first.Reachability[0].Identity || second.Reachability[0].Targets[0].Symbol != "Plugin.unused_hook" {
		t.Fatalf("registry update did not change derived targets: %+v, %+v", second, findings)
	}
	if !slices.ContainsFunc(second.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "on_event" }) {
		t.Fatal("removed registry target remained live")
	}
}

func TestPythonReachabilityRejectsRegistrySubstitutionAndMalformedInputs(t *testing.T) {
	for name, mutate := range map[string]func(*dynamicFixture){
		"wrong pointer":    func(f *dynamicFixture) { f.reference.Registry.JSONPointer = "/other" },
		"wrong path":       func(f *dynamicFixture) { f.reference.Registry.Path = "src/missing.json" },
		"empty collection": func(f *dynamicFixture) { f.registry = `{"plugins":{}}` },
		"duplicate keys": func(f *dynamicFixture) {
			f.registry = `{"plugins":{"one":"handler:Plugin.on_event","one":"handler:Plugin.unused_hook"}}`
		},
		"relative target":   func(f *dynamicFixture) { f.registry = `{"plugins":[".handler:Plugin.on_event"]}` },
		"wildcard target":   func(f *dynamicFixture) { f.registry = `{"plugins":["handler:Plugin.*"]}` },
		"import expression": func(f *dynamicFixture) { f.registry = `{"plugins":["handler:Plugin()"]}` },
		"unbound selector": func(f *dynamicFixture) {
			f.sources["src/loader.py"] = strings.Replace(f.sources["src/loader.py"], "def load(name):", "def load():", 1)
			refreshDynamicConsumer(f)
		},
		"non JSON input": func(f *dynamicFixture) { f.registry = "not json" },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := registryReachabilityFixture()
			mutate(&fixture)
			response, findings := runDynamicReachability(t, fixture)
			if len(response.Reachability) != 0 || !slices.ContainsFunc(findings, func(f policy.Finding) bool { return f.Check == "policy.pythonReachability" }) {
				t.Fatalf("invalid registry accepted: %+v, %+v", response, findings)
			}
		})
	}
}

type dynamicFixture struct {
	reference policy.PythonDynamicReference
	sources   map[string]string
	registry  string
}

func dynamicReachabilityFixture() dynamicFixture {
	fixture := dynamicFixture{reference: policy.PythonDynamicReference{Kind: "target", Project: "pyproject.toml", Target: &policy.PythonDynamicTarget{Module: "handler", Symbol: "Plugin.on_event"}, Consumer: policy.PythonDynamicConsumer{Kind: "callsite", Importer: "src/loader.py", Module: "loader", Callable: "load", Callee: "pkgutil.resolve_name", Shape: "module-object-call/v1", Argument: "'handler:Plugin.on_event'"}}, sources: map[string]string{
		"src/loader.py":  "from pkgutil import resolve_name\ndef load():\n    return resolve_name('handler:Plugin.on_event')\n",
		"src/handler.py": "class Plugin:\n    def on_event(self):\n        return 1\n    def unused_hook(self):\n        return 2\n",
	}}
	refreshDynamicConsumer(&fixture)
	return fixture
}

func registryReachabilityFixture() dynamicFixture {
	fixture := dynamicReachabilityFixture()
	fixture.reference.Kind, fixture.reference.Target = "registry", nil
	fixture.reference.Registry = &policy.PythonDynamicRegistry{Path: "src/registry.json", JSONPointer: "/plugins"}
	fixture.reference.Consumer.Argument = "json.loads(Path('src/registry.json').read_text(encoding='utf-8'))['plugins'][name]"
	fixture.sources["src/loader.py"] = "from pkgutil import resolve_name\nimport json\nfrom pathlib import Path\ndef load(name):\n    return resolve_name(" + fixture.reference.Consumer.Argument + ")\n"
	fixture.registry = `{"plugins":{"default":"handler:Plugin.on_event"}}`
	refreshDynamicConsumer(&fixture)
	return fixture
}

func refreshDynamicConsumer(fixture *dynamicFixture) {
	source := fixture.sources["src/loader.py"]
	digest := sha256.Sum256([]byte(source))
	fixture.reference.Consumer.SourceSHA256 = hex.EncodeToString(digest[:])
	for index, line := range strings.Split(source, "\n") {
		if column := strings.Index(line, "resolve_name("); column >= 0 {
			fixture.reference.Consumer.Site = policy.PythonSourceLocation{Line: index + 1, Column: column + 1}
		}
	}
}

func runDynamicReachability(t *testing.T, fixture dynamicFixture) (pythonVultureResponse, []policy.Finding) {
	t.Helper()
	repo := pythonQualityRepository(t)
	repo.PolicyRoot = pythonVulturePolicyRoot(t)
	if !pythonVultureRuntimeInstalled(t, repo) {
		t.Fatal("consumer tests require carried CPython and Vulture")
	}
	repo.Config.Scope.PythonDynamicReferences = []policy.PythonDynamicReference{fixture.reference}
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = 'example'\nrequires-python = '==3.12.*'\ndependencies = []\n")
	for path, source := range fixture.sources {
		writeQualityFile(t, repo.Root, path, source)
	}
	if fixture.registry != "" {
		writeQualityFile(t, repo.Root, "src/registry.json", fixture.registry)
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
	if err != nil {
		t.Fatal(err)
	}
	if fixture.reference.Registry != nil {
		plan := pythonQualityPlanFor(repo, []string{fixture.reference.Registry.Path})
		if len(plan.projects) != 1 {
			t.Fatalf("registry-only selection missed its consumer project: %+v", plan)
		}
	}
	if len(response.Reachability) > 0 && !slices.Contains(response.Resolved, repository.PythonReachabilityID(fixture.reference)) {
		t.Fatal("consumer evidence lost its declaration identity")
	}
	return response, pythonVultureFindings(repo, project, output.Stdout)
}
