package engine

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestGateRunRejectsChangedInstalledReachabilityEvidence(t *testing.T) {
	engine := installedReachabilityEngine(t)
	head, err := engine.Repository.CleanHead()
	if err != nil {
		t.Fatal(err)
	}
	commands := []MergeGateExecutionCommand{{Category: gaterun.Check, Scope: "repository", Cost: "quick", Command: policy.Command{Name: "check", Argv: []string{"verify"}, Cwd: ".", TimeoutSeconds: 30}}}
	identity, err := gateRunIdentity(engine, gaterun.MergeGate, "main", head, head, "recommended", commands, testGateRunBehaviorReview(), nil)
	if err != nil || identity.PythonReachabilitySHA256 == "" {
		t.Fatalf("gate omitted installed inputs: %+v, %v", identity, err)
	}
	storeGitEvidenceGate(t, engine.Repository.Root, identity)
	if _, err := gaterun.LoadReport(engine.Repository.Root, identity); err != nil {
		t.Fatal(err)
	}
	controller := &gateRunController{candidate: head, gitEvidenceSHA256: identity.GitEvidenceSHA256, policyValiditySHA256: identity.PolicyValiditySHA256, pythonReachabilitySHA256: identity.PythonReachabilitySHA256}
	if err := controller.candidateIntegrityError(engine); err != nil {
		t.Fatal(err)
	}
	writeEngineReachabilityDistribution(t, engine.Repository.Root, "class Contract:\n    def run(self):\n        return 2\n")
	current, err := engine.Repository.CleanHead()
	if err != nil || current != head {
		t.Fatalf("installed edit changed Git candidate: %s, %v", current, err)
	}
	changed, err := gateRunIdentity(engine, gaterun.MergeGate, "main", head, head, "recommended", commands, testGateRunBehaviorReview(), nil)
	if err != nil || changed.PythonReachabilitySHA256 == identity.PythonReachabilitySHA256 {
		t.Fatalf("installed edit retained gate identity: %+v, %v", changed, err)
	}
	if _, err := gaterun.LoadReport(engine.Repository.Root, changed); err == nil {
		t.Fatal("changed installed sources reused a passed gate")
	}
	if err := controller.candidateIntegrityError(engine); err == nil || !strings.Contains(err.Error(), "evidence changed") {
		t.Fatalf("changed installed sources allowed success publication: %v", err)
	}
	if _, err := engine.alreadyPassedMergeGate(t.Context(), "main", MergeGateExecutionPlan{}, controller); err == nil || !strings.Contains(err.Error(), "evidence changed") {
		t.Fatalf("installed sources changed after lookup but retained reuse: %v", err)
	}
}

func installedReachabilityEngine(t *testing.T) *Engine {
	t.Helper()
	root := t.TempDir()
	writeEngineFile(t, root, ".gitignore", ".venv/\n.code-polishy-reports/\n", 0o600)
	writeEngineFile(t, root, "pyproject.toml", "[project]\nname='app'\ndependencies=['framework==1.0']\n", 0o600)
	writeEngineFile(t, root, "uv.lock", "version=1\n[[package]]\nname='framework'\nversion='1.0'\nsource={registry='https://packages.example.test/simple'}\n", 0o600)
	writeEngineReachabilityDistribution(t, root, "class Contract:\n    def run(self):\n        return 1\n")
	initializeEngineGitRepository(t, root)
	config := policy.Config{}
	config.Scope.PythonDynamicReferences = []policy.PythonDynamicReference{{Kind: "target", Project: "pyproject.toml", Consumer: policy.PythonDynamicConsumer{Kind: "base", Distribution: "framework"}}}
	return &Engine{Repository: repository.Repository{Root: root, PolicyRoot: root, Config: config}, Runner: &recordingEngineRunner{}}
}

func writeEngineReachabilityDistribution(t *testing.T, root, source string) {
	t.Helper()
	site := ".venv/lib/python3.12/site-packages/"
	metadata := "framework-1.0.dist-info/"
	record := ""
	for name, content := range map[string]string{"framework.py": source, metadata + "METADATA": "Metadata-Version: 2.4\nName: framework\nVersion: 1.0\n\n"} {
		writeEngineFile(t, root, site+name, content, 0o600)
		digest := sha256.Sum256([]byte(content))
		record += fmt.Sprintf("%s,sha256=%s,%d\n", name, base64.RawURLEncoding.EncodeToString(digest[:]), len(content))
	}
	writeEngineFile(t, root, site+metadata+"RECORD", record+metadata+"RECORD,,\n", 0o600)
}
