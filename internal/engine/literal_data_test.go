package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestFormatValidatesLiteralDataBeforeWritingAndPreservesBytes(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "literal", false: "executable"}[valid], func(t *testing.T) {
			root := t.TempDir()
			source := "const catalog={items:[]};export default catalog;\n"
			if !valid {
				source = "export default fetch('https://example.com');\n"
			}
			writeEngineFile(t, root, "data/catalog.js", source, 0o600)
			writeEngineFile(t, root, "index.ts", "export const value = 1;\n", 0o600)
			writeEngineFile(t, root, "package.json", "{\"type\":\"module\"}\n", 0o600)
			settings := "{\"value\":1}\n"
			writeEngineFile(t, root, "settings.json", settings, 0o600)
			config := policy.Config{Quality: policy.EffectiveQuality(policy.Quality{}), Scope: policy.Scope{Data: []string{"data/catalog.js"}}, Modules: []policy.Module{{Name: "application", Paths: []string{"data/**", "settings.json", "package.json"}}}, ModuleByName: map[string]int{"application": 0}}
			repo, err := repository.Open(root, enginePolicyRoot(t), config)
			if err != nil {
				t.Fatal(err)
			}
			engine := Engine{Repository: repo, Runner: runner.OSRunner{}}
			report := engine.Format(t.Context(), repository.Selection{Files: []string{"data/catalog.js", "settings.json"}})
			if HasFindings(report) == valid {
				t.Fatalf("format findings = %+v", report.Findings)
			}
			if valid && (report.Formatting == nil || report.Formatting.Protected != 1 || report.Formatting.Rewritten != 1) {
				t.Fatalf("format outcome = %+v", report.Formatting)
			}
			data, err := os.ReadFile(filepath.Join(root, "data/catalog.js"))
			if err != nil || string(data) != source {
				t.Fatalf("protected module changed: %q %v", data, err)
			}
			if !valid {
				data, err := os.ReadFile(filepath.Join(root, "settings.json"))
				if err != nil || string(data) != settings {
					t.Fatalf("invalid data allowed a formatting write: %q %v", data, err)
				}
			}
		})
	}
}
