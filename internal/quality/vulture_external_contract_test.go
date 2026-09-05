package quality

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestPythonReachabilityExternalBaseKeepsOnlyTheBoundMember(t *testing.T) {
	source := "from framework import Contract\nclass Plugin(Contract):\n    def on_event(self):\n        return 1\n    def unused_hook(self):\n        return 2\n"
	digest := sha256.Sum256([]byte(source))
	fixture := dynamicFixture{
		reference: policy.PythonDynamicReference{Kind: "target", Project: "pyproject.toml", Target: &policy.PythonDynamicTarget{Module: "app", Symbol: "Plugin.on_event"}, Consumer: policy.PythonDynamicConsumer{
			Kind: "base", Importer: "src/app.py", Module: "app", Site: policy.PythonSourceLocation{Line: 2, Column: 1}, SourceSHA256: hex.EncodeToString(digest[:]),
			Distribution: "framework", Qualified: "framework.Contract", Implementation: "Plugin", Member: "on_event",
		}},
		sources: map[string]string{
			"src/app.py":     source,
			"pyproject.toml": "[project]\nname = 'example'\nrequires-python = '==3.12.*'\ndependencies = ['framework==1.0.0']\n",
			"uv.lock":        "version = 1\n[[package]]\nname = 'framework'\nversion = '1.0.0'\nsource = { registry = 'https://packages.example.test/simple' }\n",
		},
	}
	addExternalContractDistribution(fixture.sources)
	response, findings := runDynamicReachability(t, fixture)
	if len(response.Reachability) != 1 || len(response.Problems) != 0 {
		t.Fatalf("external consumer not proven: %+v, %+v", response, findings)
	}
	if slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "on_event" }) || !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "unused_hook" }) {
		t.Fatalf("external contract kept the wrong members: %+v", response.Diagnostics)
	}
	fixture.reference.Consumer.Member = "unused_hook"
	fixture.reference.Target.Symbol = "Plugin.unused_hook"
	invented, rejected := runDynamicReachability(t, fixture)
	if len(invented.Reachability) != 0 || !slices.ContainsFunc(rejected, func(f policy.Finding) bool { return f.Check == "policy.pythonReachability" }) || !slices.ContainsFunc(invented.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "unused_hook" }) {
		t.Fatalf("invented interface member acquired reachability: %+v, %+v", invented, rejected)
	}
	fixture.reference.Consumer.Member = "on_event"
	fixture.reference.Target.Symbol = "Plugin.on_event"
	fixture.sources["pyproject.toml"] = "[project]\nname = 'example'\nrequires-python = '==3.12.*'\ndependencies = []\n"
	response, findings = runDynamicReachability(t, fixture)
	if len(response.Reachability) != 0 || !slices.ContainsFunc(findings, func(f policy.Finding) bool { return f.Check == "policy.pythonReachability" }) {
		t.Fatalf("unadmitted dependency kept a target: %+v, %+v", response, findings)
	}
	if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "on_event" }) {
		t.Fatalf("unadmitted contract hid its former member: %+v", response.Diagnostics)
	}
}

func addExternalContractDistribution(sources map[string]string) {
	root := ".venv/lib/python3.12/site-packages/"
	metadata := "framework-1.0.0.dist-info/"
	record := ""
	for path, source := range map[string]string{
		"framework.py":        "class Contract:\n    def on_event(self):\n        raise NotImplementedError\n",
		metadata + "METADATA": "Metadata-Version: 2.4\nName: framework\nVersion: 1.0.0\n\n",
	} {
		sources[root+path] = source
		digest := sha256.Sum256([]byte(source))
		record += fmt.Sprintf("%s,sha256=%s,%d\n", path, base64.RawURLEncoding.EncodeToString(digest[:]), len(source))
	}
	sources[root+metadata+"RECORD"] = record + metadata + "RECORD,,\n"
}
