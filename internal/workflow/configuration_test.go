package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadUsesDeclaredRunnerLabelsWithoutSuppressingWorkflowRules(t *testing.T) {
	t.Parallel()
	configuration := "self-hosted-runner:\n  labels: [builder-linux-arm64]\n"
	source := "on: push\njobs:\n  verify:\n    runs-on: builder-linux-arm64\n    steps:\n      - run: code-polishy check\n"
	for name, test := range map[string]struct {
		configuration string
		filename      string
		second        bool
		invalidNeeds  bool
		problem       string
	}{
		"yaml declaration": {configuration: configuration, filename: "actionlint.yaml"},
		"yml declaration":  {configuration: configuration, filename: "actionlint.yml"},
		"undeclared":       {problem: "runner"},
		"ambiguous":        {configuration: configuration, filename: "actionlint.yaml", second: true, problem: "ambiguous"},
		"malformed":        {configuration: "self-hosted-runner: [", filename: "actionlint.yaml", problem: "invalid actionlint"},
		"glob":             {configuration: "self-hosted-runner:\n  labels: ['builder-*']\n", filename: "actionlint.yaml", problem: "exact non-empty"},
		"suppression":      {configuration: "paths:\n  '**':\n    ignore: ['.*']\n", filename: "actionlint.yaml", problem: "cannot weaken"},
		"invalid workflow": {configuration: configuration, filename: "actionlint.yaml", invalidNeeds: true, problem: "missing"},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".github"), 0o700); err != nil {
				t.Fatal(err)
			}
			write := func(path, contents string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, path), []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			written := source
			if test.invalidNeeds {
				written = strings.Replace(written, "    runs-on:", "    needs: missing\n    runs-on:", 1)
			}
			write("workflow.yml", written)
			if test.filename != "" {
				write(".github/"+test.filename, test.configuration)
			}
			if test.second {
				write(".github/actionlint.yml", configuration)
			}
			facts, err := Read("workflow.yml", func(path string) ([]byte, error) {
				return os.ReadFile(filepath.Join(root, path))
			})
			if test.problem != "" {
				if err == nil || !strings.Contains(err.Error(), test.problem) {
					t.Fatalf("wanted %q, got %v", test.problem, err)
				}
				return
			}
			if err != nil || len(facts.Jobs) != 1 || facts.Jobs[0].Steps[0].Run != "code-polishy check" {
				t.Fatalf("facts=%+v error=%v", facts, err)
			}
		})
	}
}
