package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestArchitectureDiagnosesUnmappedTestsFromResolvedProductionImports(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name         string
		imports      string
		expected     string
		alternatives int
	}{
		{"convergent", "import _ \"example.test/app/domain\"\n", "domain", 0},
		{"ambiguous", "import _ \"example.test/app/domain\"\nimport _ \"example.test/app/api\"\n", "", 2},
		{"incomplete", "import _ \"example.test/app/domain\"\nimport _ \"example.test/app/missing\"\n", "", 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			writeEngineFile(t, root, "go.mod", "module example.test/app\n", 0o600)
			writeEngineFile(t, root, "domain/value.go", "package domain\n", 0o600)
			writeEngineFile(t, root, "api/handler.go", "package api\n", 0o600)
			writeEngineFile(t, root, "checks/contract_test.go", "package checks_test\n"+testCase.imports, 0o600)
			config := policy.Config{Modules: []policy.Module{{Name: "domain", Paths: []string{"domain/**"}}, {Name: "api", Paths: []string{"api/**"}}}, ModuleByName: map[string]int{"domain": 0, "api": 1}}
			for _, module := range config.Modules {
				config.Tests.Suites = append(config.Tests.Suites, policy.TestSuite{Name: module.Name + "-unit", Scope: "module", Cost: "quick", Modules: []string{module.Name}, Paths: []string{"checks/**"}, RunOn: []string{"focused", "recommended", "full"}})
			}
			policyEngine := &Engine{Repository: repository.Repository{Root: root, PolicyRoot: root, Config: config}}
			report := policyEngine.Architecture(t.Context(), repository.Selection{Files: []string{"checks/contract_test.go"}})
			findings := []policy.Finding{}
			for _, finding := range report.Findings {
				if finding.Check == "policy.testOwnership" {
					findings = append(findings, finding)
				}
			}
			if len(findings) != 1 || findings[0].Fields["expectedOwner"] != testCase.expected || report.SourceDependencyGraph != nil {
				t.Fatalf("report = %+v", report)
			}
			if testCase.expected != "" {
				var suggested policy.TestOwnership
				if err := json.Unmarshal(findings[0].Remediation.Configuration, &suggested); err != nil {
					t.Fatal(err)
				}
				if suggested.Module != "domain" || suggested.FocusedSuite != "domain-unit" || !slices.Equal(suggested.Paths, []string{"checks/contract_test.go"}) {
					t.Fatalf("suggestion = %+v", suggested)
				}
			} else {
				var suggested struct {
					Alternatives []json.RawMessage `json:"alternatives"`
				}
				if err := json.Unmarshal(findings[0].Remediation.Configuration, &suggested); err != nil {
					t.Fatal(err)
				}
				if len(suggested.Alternatives) != testCase.alternatives {
					t.Fatalf("suggestion = %+v", suggested)
				}
			}
		})
	}
}

func declareContentTestOwnership(t *testing.T, root, path string) {
	t.Helper()
	configPath := filepath.Join(root, policy.ConfigFilename)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ownership, err := json.Marshal([]policy.TestOwnership{{Paths: []string{path}, Module: "content", FocusedSuite: "focused"}})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := json.Marshal([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	configured := strings.Replace(string(data), `"ownership":[]`, `"ownership":`+string(ownership), 1)
	configured = strings.Replace(configured, `"name":"focused",`, `"name":"focused","paths":`+string(paths)+`,`, 1)
	writeEngineFile(t, root, policy.ConfigFilename, configured, 0o600)
}
