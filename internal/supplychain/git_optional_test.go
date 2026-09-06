package supplychain

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestOptionalGitEvidenceRetainsRegistryChecks(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	for _, scenario := range []string{"clean", "configured", "vulnerable", "young", "required"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := currentGitOnlineFixture(t)
			if scenario == "configured" {
				fixture.repo.Config.SupplyChain.GitEvidence.Required = false
				writeSupplyFile(t, fixture.repo.Root, "evidence/git.dsse.json", "invalid artifact")
			} else {
				fixture.repo.Config.SupplyChain.GitEvidence = policy.GitEvidence{Required: scenario == "required"}
			}
			requests := []string{}
			observed := fixture.now
			if scenario == "young" {
				observed = observed.Add(90 * 24 * time.Hour)
			}
			http.DefaultTransport = gitOnlineTransport{now: observed, requests: &requests}
			boundary := &optionalGitBoundary{t: t, vulnerable: scenario == "vulnerable"}
			findings := Online(t.Context(), fixture.repo, []string{"uv.lock"}, boundary)
			warning, blocked := false, false
			for _, finding := range findings {
				if finding.Check == "supplyChain.gitSourceCoverage" && finding.Severity == policy.FindingWarning {
					warning = true
					continue
				}
				expected := map[string]string{"vulnerable": "supplyChain.osvVulnerability", "young": "supplyChain.releaseAge", "required": "supplyChain.gitEvidence"}[scenario]
				if expected == "" || finding.Check != expected {
					t.Fatalf("unexpected finding: %+v", finding)
				}
				blocked = true
			}
			if warning != (scenario != "required") || blocked != (scenario == "vulnerable" || scenario == "young" || scenario == "required") || boundary.projected != (scenario != "required") || len(requests) != 1 {
				t.Fatalf("scenario %s: findings %+v, projection %v, requests %v", scenario, findings, boundary.projected, requests)
			}
			state, err := ReadGitEvidenceState(fixture.repo, fixture.now)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "required" && (state.IdentitySHA256 != "" || len(state.Receipts) != 0) {
				t.Fatalf("optional evidence credited: %+v", state)
			}
		})
	}
}

type optionalGitBoundary struct {
	t          *testing.T
	vulnerable bool
	projected  bool
}

func (boundary *optionalGitBoundary) Run(ctx context.Context, root string, command policy.Command) error {
	_, _, err := boundary.RunWithOutput(ctx, root, command)
	return err
}

func (boundary *optionalGitBoundary) RunWithOutput(_ context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	boundary.t.Helper()
	input, projected := argumentAfter(command.Argv, "--lockfile")
	if !projected {
		return runner.Result{}, runner.Output{Stdout: []byte(`{"results":[]}`)}, nil
	}
	boundary.projected = true
	path := strings.TrimPrefix(input, "osv-scanner:")
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		boundary.t.Fatal(err)
	}
	if !strings.Contains(string(data), `"name":"support-lib"`) || strings.Contains(string(data), "private-kit") || strings.Contains(string(data), "private.example.test") {
		boundary.t.Fatalf("incorrect public inventory: %s", data)
	}
	if !boundary.vulnerable {
		return runner.Result{}, runner.Output{Stdout: []byte(`{"results":[]}`)}, nil
	}
	payload := fmt.Sprintf(`{"results":[{"source":{"path":%q,"type":"lockfile"},"packages":[{"package":{"name":"support-lib","version":"2.0.0","ecosystem":"PyPI"},"vulnerabilities":[{"id":"GHSA-test-public"}]}]}]}`, path)
	return runner.Result{ExitStatus: 1}, runner.Output{Stdout: []byte(payload)}, fmt.Errorf("vulnerability found")
}
