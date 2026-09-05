package quality

import (
	"crypto/sha256"
	"encoding/hex"
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
	response, findings := runDynamicReachability(t, fixture)
	if len(response.Reachability) != 1 || len(response.Problems) != 0 {
		t.Fatalf("external consumer not proven: %+v, %+v", response, findings)
	}
	if slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "on_event" }) || !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "unused_hook" }) {
		t.Fatalf("external contract kept the wrong members: %+v", response.Diagnostics)
	}
	fixture.sources["pyproject.toml"] = "[project]\nname = 'example'\nrequires-python = '==3.12.*'\ndependencies = []\n"
	response, findings = runDynamicReachability(t, fixture)
	if len(response.Reachability) != 0 || !slices.ContainsFunc(findings, func(f policy.Finding) bool { return f.Check == "policy.pythonReachability" }) {
		t.Fatalf("unadmitted dependency kept a target: %+v, %+v", response, findings)
	}
	if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == "on_event" }) {
		t.Fatalf("unadmitted contract hid its former member: %+v", response.Diagnostics)
	}
}
