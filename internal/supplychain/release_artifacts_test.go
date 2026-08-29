package supplychain

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestStandaloneArtifactReleaseAgeUsesAuthoritativeUpstreamMetadata(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.SupplyChain.MinimumReleaseAgeDays = 30
	repo.Config.SupplyChain.ReleaseArtifacts = []policy.ReleaseArtifact{
		{Name: "go", VersionFile: "scripts/go_version.txt", Source: "go-toolchain"},
		{Name: "node", VersionFile: "tools/node-version.txt", Source: "node-runtime"},
		{Name: "staticcheck", VersionFile: "tools/staticcheck-version.txt", Source: "go-module", Locator: "honnef.co/go/tools"},
		{Name: "ruff", VersionFile: "tools/ruff-version.txt", Source: "github-release", Locator: "astral-sh/ruff", TagPrefix: "v"},
		{Name: "example-cli", VersionFile: "tools/example-version.txt", Source: "npm", Locator: "@example/cli"},
	}
	for path, version := range map[string]string{
		"scripts/go_version.txt": "1.26.6\n", "tools/node-version.txt": "24.18.0\n",
		"tools/staticcheck-version.txt": "v0.7.0\n", "tools/ruff-version.txt": "0.16.0\n",
		"tools/example-version.txt": "2.3.4\r\n",
	} {
		writeSupplyFile(t, repo.Root, path, version)
	}
	released := "2026-08-20T12:00:00Z"
	client := artifactClientFunc(func(request *http.Request) (*http.Response, error) {
		responses := map[string]string{
			"https://api.github.com/repos/golang/go/commits/go1.26.6":           `{"sha":"` + strings.Repeat("a", 40) + `","commit":{"committer":{"date":"` + released + `"},"message":"[release-branch.go1.26] go1.26.6"}}`,
			"https://nodejs.org/dist/index.json":                                `[{"version":"v24.18.0","date":"2026-08-20"}]`,
			"https://proxy.golang.org/honnef.co/go/tools/@v/v0.7.0.info":        `{"Version":"v0.7.0","Time":"` + released + `"}`,
			"https://api.github.com/repos/astral-sh/ruff/releases/tags/v0.16.0": `{"tag_name":"v0.16.0","published_at":"` + released + `"}`,
			"https://registry.npmjs.org/@example%2Fcli":                         `{"time":{"2.3.4":"` + released + `"}}`,
		}
		body, exists := responses[request.URL.String()]
		if !exists {
			return nil, fmt.Errorf("unexpected metadata request %s", request.URL)
		}
		return artifactResponse(http.StatusOK, body), nil
	})
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	findings := releaseArtifactAgeFindings(t.Context(), repo, client, now)
	if len(findings) != len(repo.Config.SupplyChain.ReleaseArtifacts) {
		t.Fatalf("findings = %+v", findings)
	}
	seen := map[string]bool{}
	for _, finding := range findings {
		if finding.ReleaseAge == nil || finding.ReleaseAge.Ecosystem != "artifact" || finding.Path != finding.ReleaseAge.Scope {
			t.Fatalf("artifact finding lost its assessable identity: %+v", finding)
		}
		seen[finding.ReleaseAge.Package] = true
	}
	for _, artifact := range repo.Config.SupplyChain.ReleaseArtifacts {
		if !seen[artifact.Name] {
			t.Errorf("missing release-age finding for %s", artifact.Name)
		}
	}
	for _, finding := range findings {
		if finding.Subject == "node@24.18.0" && !finding.ReleaseAge.Released.Equal(time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("Node date-only metadata was not conservatively bounded: %s", finding.ReleaseAge.Released)
		}
	}
}

func TestStandaloneArtifactReleaseAgeFailsClosedOnPinsAndMetadata(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.SupplyChain.ReleaseArtifacts = []policy.ReleaseArtifact{
		{Name: "missing", VersionFile: "tools/missing-version.txt", Source: "node-runtime"},
		{Name: "floating", VersionFile: "tools/floating-version.txt", Source: "node-runtime"},
		{Name: "ruff", VersionFile: "tools/ruff-version.txt", Source: "github-release", Locator: "astral-sh/ruff"},
	}
	writeSupplyFile(t, repo.Root, "tools/floating-version.txt", "latest\n")
	writeSupplyFile(t, repo.Root, "tools/ruff-version.txt", "0.16.0\n")
	client := artifactClientFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://api.github.com/repos/astral-sh/ruff/releases/tags/0.16.0" {
			return nil, fmt.Errorf("unexpected metadata request %s", request.URL)
		}
		return artifactResponse(http.StatusOK, `{"tag_name":"other","published_at":null}`), nil
	})
	findings := releaseArtifactAgeFindings(t.Context(), repo, client, time.Now().UTC())
	if len(findings) != 3 {
		t.Fatalf("findings = %+v", findings)
	}
	for _, finding := range findings {
		if finding.ReleaseAge != nil || !strings.Contains(finding.Message, "resolve standalone artifact release") {
			t.Fatalf("operational metadata failure became assessable: %+v", finding)
		}
	}
}

func TestStandaloneArtifactOlderThanTheMinimumPasses(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.SupplyChain.MinimumReleaseAgeDays = 30
	repo.Config.SupplyChain.ReleaseArtifacts = []policy.ReleaseArtifact{{
		Name: "ruff", VersionFile: "tools/ruff-version.txt", Source: "github-release", Locator: "astral-sh/ruff",
	}}
	writeSupplyFile(t, repo.Root, "tools/ruff-version.txt", "0.16.0\n")
	client := artifactClientFunc(func(*http.Request) (*http.Response, error) {
		return artifactResponse(http.StatusOK, `{"tag_name":"0.16.0","published_at":"2026-06-01T00:00:00Z"}`), nil
	})
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	if findings := releaseArtifactAgeFindings(t.Context(), repo, client, now); len(findings) != 0 {
		t.Fatalf("old artifact findings = %+v", findings)
	}
}

func TestGitHubTokenTransportScopesCredentialToGitHubAPI(t *testing.T) {
	t.Parallel()
	received := map[string]string{}
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		received[request.URL.String()] = request.Header.Get("Authorization")
		return artifactResponse(http.StatusOK, `{}`), nil
	})
	transport := githubTokenTransport{base: base, token: "github-actions-token"}
	for _, endpoint := range []string{
		"https://api.github.com/repos/example/tool/releases/tags/v1.0.0",
		"http://api.github.com/repos/example/tool/releases/tags/v1.0.0",
		"https://registry.npmjs.org/example",
		"https://api.github.com.example.invalid/repos/example/tool",
	} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if request.Header.Get("Authorization") != "" {
			t.Fatal("credential transport mutated its input request")
		}
	}
	githubEndpoint := "https://api.github.com/repos/example/tool/releases/tags/v1.0.0"
	if received[githubEndpoint] != "Bearer github-actions-token" {
		t.Fatalf("GitHub API authorization = %q", received[githubEndpoint])
	}
	for endpoint, authorization := range received {
		if endpoint != githubEndpoint && authorization != "" {
			t.Fatalf("credential reached %s", endpoint)
		}
	}
}

func TestGitHubTokenTransportFallsBackToPublicMetadata(t *testing.T) {
	t.Parallel()
	authorizations := []string{}
	released := "2026-06-30T09:52:02Z"
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		authorizations = append(authorizations, request.Header.Get("Authorization"))
		if len(authorizations) == 1 {
			return artifactResponse(http.StatusForbidden, `{"message":"Resource not accessible by integration"}`), nil
		}
		return artifactResponse(http.StatusOK, `{"tag_name":"v0.72.0","published_at":"`+released+`"}`), nil
	})
	client := &http.Client{Transport: githubTokenTransport{base: base, token: "github-actions-token"}}
	observed, err := lookupGitHubRelease(t.Context(), client, "aquasecurity/trivy", "v0.72.0")
	if err != nil {
		t.Fatal(err)
	}
	want, err := time.Parse(time.RFC3339, released)
	if err != nil {
		t.Fatal(err)
	}
	if !observed.Equal(want) {
		t.Fatalf("release time = %s, want %s", observed, want)
	}
	if len(authorizations) != 2 || authorizations[0] != "Bearer github-actions-token" || authorizations[1] != "" {
		t.Fatalf("authorization attempts = %q", authorizations)
	}
}

func TestGitHubMetadataFailureReportsSafeReason(t *testing.T) {
	t.Parallel()
	client := artifactClientFunc(func(*http.Request) (*http.Response, error) {
		response := artifactResponse(http.StatusForbidden, `{"message":"API rate limit exceeded"}`)
		response.Header.Set("X-RateLimit-Remaining", "0")
		response.Header.Set("Retry-After", "60")
		return response, nil
	})
	target := map[string]any{}
	err := fetchArtifactMetadata(
		t.Context(), client, "https://api.github.com/repos/aquasecurity/trivy/releases/tags/v0.72.0", &target,
	)
	if err == nil || !strings.Contains(err.Error(), `GitHub message "API rate limit exceeded"`) ||
		!strings.Contains(err.Error(), "rate-limit remaining 0") || !strings.Contains(err.Error(), "retry after 60") {
		t.Fatalf("metadata error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type artifactClientFunc func(*http.Request) (*http.Response, error)

func (function artifactClientFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

func artifactResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}
