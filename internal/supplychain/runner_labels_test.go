package supplychain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeclaredRunnerLabelsPreserveMonitoringAndActionPinChecks(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	path := ".github/workflows/security.yml"
	writeSupplyFile(t, repo.Root, path, "on:\n  schedule:\n    - cron: '0 4 * * 1'\njobs:\n  scan:\n    runs-on: builder-linux-arm64\n    steps:\n      - uses: actions/checkout@v4\n      - run: code-polishy supply-chain\n")
	configuration := ".github/actionlint.yaml"
	writeSupplyFile(t, repo.Root, configuration, "self-hosted-runner:\n  labels: [builder-linux-arm64]\n")
	if !hasWeeklySecurityWorkflow(repo, []string{path}) {
		t.Fatal("declared runner concealed the weekly security workflow")
	}
	findings := checkWorkflowPins(repo, path)
	if len(findings) != 1 || findings[0].Subject != "actions/checkout@v4" {
		t.Fatalf("action pin enforcement changed: %+v", findings)
	}
	if err := os.Remove(filepath.Join(repo.Root, configuration)); err != nil {
		t.Fatal(err)
	}
	if hasWeeklySecurityWorkflow(repo, []string{path}) {
		t.Fatal("an undeclared runner proved monitoring coverage")
	}
}
