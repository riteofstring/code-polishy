package supplychain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowConfigurationDeclaresRunnersWithoutSuppressingChecks(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	workflow := ".github/workflows/security.yml"
	source := "on:\n  schedule:\n    - cron: '0 4 * * *'\njobs:\n  scan:\n    runs-on: [self-hosted, linux, x64, build-pool]\n    steps:\n      - uses: actions/checkout@0123456789012345678901234567890123456789\n      - run: code-polishy supply-chain\n"
	writeSupplyFile(t, repo.Root, workflow, source)
	if findings := checkWorkflowPins(repo, workflow); len(findings) == 0 {
		t.Fatal("undeclared runner label was accepted")
	}
	configuration := ".github/actionlint.yaml"
	writeSupplyFile(t, repo.Root, configuration, "self-hosted-runner:\n  labels: [build-pool]\n")
	if findings := checkWorkflowPins(repo, workflow); len(findings) != 0 {
		t.Fatalf("declared runner rejected: %+v", findings)
	}
	if !hasWeeklySecurityWorkflow(repo, []string{workflow}) {
		t.Fatal("declared runner did not retain weekly monitoring evidence")
	}
	writeSupplyFile(t, repo.Root, workflow, strings.ReplaceAll(source, "0123456789012345678901234567890123456789", "main"))
	if findings := checkWorkflowPins(repo, workflow); !supplyChecks(findings)["supplyChain.workflowPin"] {
		t.Fatalf("runner declaration suppressed pin checking: %+v", findings)
	}
	writeSupplyFile(t, repo.Root, workflow, strings.ReplaceAll(source, "build-pool", "misspelled-pool"))
	if findings := checkWorkflowPins(repo, workflow); len(findings) == 0 {
		t.Fatal("undeclared label accepted after reading configuration")
	}
	writeSupplyFile(t, repo.Root, workflow, source)
	for _, invalid := range []string{"self-hosted-runner: [", "self-hosted-runner:\n  labels: ['*']\n", "paths:\n  '**':\n    ignore: ['.*']\n"} {
		writeSupplyFile(t, repo.Root, configuration, invalid)
		if findings := checkWorkflowPins(repo, workflow); len(findings) == 0 {
			t.Errorf("invalid or weakening configuration accepted: %s", invalid)
		}
	}
	writeSupplyFile(t, repo.Root, configuration, "self-hosted-runner:\n  labels: [build-pool]\n")
	writeSupplyFile(t, repo.Root, ".github/actionlint.yml", "self-hosted-runner:\n  labels: [build-pool]\n")
	if findings := checkWorkflowPins(repo, workflow); len(findings) == 0 || !strings.Contains(findings[0].Message, "ambiguous") {
		t.Fatalf("ambiguous configuration accepted: %+v", findings)
	}
	if err := os.Remove(filepath.Join(repo.Root, configuration)); err != nil {
		t.Fatal(err)
	}
	if findings := checkWorkflowPins(repo, workflow); len(findings) != 0 {
		t.Fatalf(".yml configuration rejected: %+v", findings)
	}
}
