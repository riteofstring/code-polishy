package supplychain

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestOnlineAcceptsSignedPrivateGitEvidenceAndRetainsRegistryAge(t *testing.T) {
	fixture := currentGitOnlineFixture(t)
	fixture.write(t)
	requests := []string{}
	previous := http.DefaultTransport
	http.DefaultTransport = gitOnlineTransport{now: fixture.now, requests: &requests}
	t.Cleanup(func() { http.DefaultTransport = previous })
	boundary := &gitOnlineBoundary{t: t}
	findings := Online(t.Context(), fixture.repo, []string{"uv.lock"}, boundary)
	if len(findings) != 0 || boundary.scans != 1 || !slices.Equal(requests, []string{"/pypi/support-lib/json"}) {
		t.Fatalf("online Git admission = %+v, scans %d, requests %v", findings, boundary.scans, requests)
	}
}

func TestOnlineRetainsActionableGitEvidenceFailures(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	for _, failure := range []string{"authentication", "unsupported-scanner", "expired", "supplyChain.gitVulnerability", "supplyChain.releaseAge"} {
		t.Run(failure, func(t *testing.T) {
			fixture := currentGitOnlineFixture(t)
			changeOnlineEvidence(&fixture.statement, failure)
			fixture.write(t)
			requests := []string{}
			http.DefaultTransport = gitOnlineTransport{now: fixture.now, requests: &requests}
			findings := Online(t.Context(), fixture.repo, []string{"uv.lock"}, &gitOnlineBoundary{t: t})
			if len(findings) != 1 || findings[0].Subject != failure && findings[0].Check != failure {
				t.Fatalf("online %s = %+v", failure, findings)
			}
		})
	}
}

func currentGitOnlineFixture(t *testing.T) gitEvidenceFixture {
	t.Helper()
	fixture := newGitEvidenceFixture(t, true)
	shift := time.Now().UTC().Sub(fixture.now)
	fixture.now = fixture.now.Add(shift)
	fixture.statement.IssuedAt = fixture.statement.IssuedAt.Add(shift)
	fixture.statement.ExpiresAt = fixture.statement.ExpiresAt.Add(shift)
	fixture.statement.Scan.CompletedAt = fixture.statement.Scan.CompletedAt.Add(shift)
	fixture.statement.Scan.AdvisoryUpdated = fixture.statement.Scan.AdvisoryUpdated.Add(shift)
	fixture.statement.Subjects[0].Observation.Timestamp = fixture.statement.Subjects[0].Observation.Timestamp.Add(shift)
	fixture.repo.PolicyRoot = fixture.repo.Root
	fixture.repo.Config.ActivePolicyModules = []policy.ActivePolicyModule{{Name: "osv", Root: "."}}
	writeSupplyFile(t, fixture.repo.Root, ".tools/bin/osv-scanner", "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(fixture.repo.PolicyTool("osv-scanner"), 0o700); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func changeOnlineEvidence(statement *gitEvidenceStatement, failure string) {
	switch failure {
	case "authentication":
		statement.Scan.Coverage = "authentication-unavailable"
	case "unsupported-scanner":
		statement.Scan.Coverage = "unsupported"
	case "expired":
		statement.ExpiresAt = statement.IssuedAt
	case "supplyChain.gitVulnerability":
		item := statement.Scan.Licenses[0]
		exploited := true
		statement.Scan.Vulnerabilities = []gitEvidenceVulnerability{{Ecosystem: item.Ecosystem, Name: item.Name, Version: item.Version, Source: item.Source, ID: "CVE-2026-12345", Severity: "critical", KnownExploited: &exploited}}
	case "supplyChain.releaseAge":
		statement.Subjects[0].Observation.Timestamp = statement.IssuedAt
	}
}

type gitOnlineTransport struct {
	now      time.Time
	requests *[]string
}

func (transport gitOnlineTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != "pypi.org" || request.URL.Path != "/pypi/support-lib/json" {
		return nil, fmt.Errorf("unexpected public request: %s", request.URL.Redacted())
	}
	*transport.requests = append(*transport.requests, request.URL.Path)
	payload := fmt.Sprintf(`{"releases":{"2.0.0":[{"upload_time_iso_8601":%q}]}}`, transport.now.AddDate(0, 0, -90).Format(time.RFC3339))
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload)), Header: http.Header{}}, nil
}

type gitOnlineBoundary struct {
	t          *testing.T
	scans      int
	projection bool
}

func (boundary *gitOnlineBoundary) Run(context.Context, string, policy.Command) error {
	return fmt.Errorf("unexpected unstructured command")
}

func (boundary *gitOnlineBoundary) RunWithOutput(_ context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	boundary.t.Helper()
	if filepath.Base(command.Argv[0]) != "osv-scanner" {
		return runner.Result{}, runner.Output{}, fmt.Errorf("unexpected online tool")
	}
	boundary.scans++
	input, projected := argumentAfter(command.Argv, "--lockfile")
	if !projected {
		plugin, found := argumentAfter(command.Argv, "--experimental-disable-plugins")
		if !found || plugin != "python/uvlock" {
			boundary.t.Fatal("public recursive scan can read private uv inputs")
		}
		return runner.Result{ExitStatus: 0}, runner.Output{Stdout: []byte(`{"results":[]}`)}, nil
	}
	boundary.projection = true
	path, _ := strings.CutPrefix(input, "osv-scanner:")
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		boundary.t.Fatal(err)
	}
	for _, private := range []string{"private.example.test", "private-kit", "support-lib", "0123456789abcdef0123456789abcdef01234567"} {
		if strings.Contains(string(data), private) {
			boundary.t.Fatalf("public projection contains a private lock coordinate: %s", private)
		}
	}
	if !strings.Contains(string(data), `"name":"requests"`) {
		boundary.t.Fatal("public registry dependency lost coverage")
	}
	payload := fmt.Sprintf(`{"results":[{"source":{"path":%q,"type":"lockfile"},"packages":[{"package":{"name":"requests","version":"2.31.0","ecosystem":"PyPI"},"vulnerabilities":[{"id":"GHSA-test-public"}]}]}]}`, path)
	return runner.Result{ExitStatus: 1}, runner.Output{Stdout: []byte(payload)}, fmt.Errorf("vulnerability found")
}
