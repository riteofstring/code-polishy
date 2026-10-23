package supplychain

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestNodeRuntimeCoverageFindsUninventoriedPinsAndWorkflowSelections(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, ".node-version", "24.18.0\n")
	writeSupplyFile(t, repo.Root, ".nvmrc", "25.2.1\n")
	path := ".github/workflows/node.yml"
	writeSupplyFile(t, repo.Root, path, "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-node@v4\n        with:\n          node-version-file: .node-version\n")
	files := []string{".node-version", ".nvmrc", path}
	if findings := nodeRuntimeCoverageFindings(repo, files); len(findings) != 3 {
		t.Fatalf("uninventoried pins passed: %+v", findings)
	}
	repo.Config.SupplyChain.ReleaseArtifacts = []policy.ReleaseArtifact{{Name: "node", VersionFile: ".node-version", Source: "node-runtime"}}
	if findings := nodeRuntimeCoverageFindings(repo, files); len(findings) != 1 || findings[0].Path != ".nvmrc" {
		t.Fatalf("declared version file did not cover the workflow: %+v", findings)
	}
	for _, version := range []string{"24", "25.2.1", "${{ matrix.node }}"} {
		if findings := setupNodeCoverageFindings(path, map[string]string{"node-version": version, "node-version-file": ".node-version"}, map[string]string{".node-version": "24.18.0"}); len(findings) != 1 {
			t.Fatalf("higher-precedence version %q bypassed coverage: %+v", version, findings)
		}
	}
	if findings := setupNodeCoverageFindings(path, map[string]string{"node-version": "24.18.0"}, map[string]string{".node-version": "24.18.0"}); len(findings) != 0 {
		t.Fatalf("inventoried literal version rejected: %+v", findings)
	}
}

func TestNodeRuntimeSupportUsesUpstreamLifecycleAndFailsClosed(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		body    string
		status  int
		now     string
		message string
	}{
		{"supported", `{"v25":{"start":"2025-10-15","end":"2026-06-01"}}`, 200, "2026-05-31", ""},
		{"end boundary", `{"v25":{"start":"2025-10-15","end":"2026-06-01"}}`, 200, "2026-06-01", "end of life on 2026-06-01"},
		{"missing line", `{}`, 200, "2026-10-21", "no valid lifecycle"},
		{"malformed date", `{"v25":{"start":"2025-10-15","end":"never"}}`, 200, "2026-10-21", "no valid lifecycle"},
		{"unavailable", `{}`, 503, "2026-10-21", "resolve Node support schedule"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repo := supplyRepository(t)
			repo.Config.SupplyChain.ReleaseArtifacts = []policy.ReleaseArtifact{{Name: "node", VersionFile: ".node-version", Source: "node-runtime"}}
			writeSupplyFile(t, repo.Root, ".node-version", "25.2.1\n")
			client := artifactClientFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.String() != nodeReleaseScheduleEndpoint {
					return nil, fmt.Errorf("unexpected endpoint %s", request.URL)
				}
				return artifactResponse(test.status, test.body), nil
			})
			now, err := time.Parse(time.DateOnly, test.now)
			if err != nil {
				t.Fatal(err)
			}
			findings := nodeRuntimeSupportFindings(t.Context(), repo, client, now)
			if test.message == "" {
				if len(findings) != 0 {
					t.Fatalf("supported line rejected: %+v", findings)
				}
			} else if len(findings) != 1 || findings[0].ReleaseAge != nil || !strings.Contains(findings[0].Message, test.message) {
				t.Fatalf("support status not enforced: %+v", findings)
			}
		})
	}
}
