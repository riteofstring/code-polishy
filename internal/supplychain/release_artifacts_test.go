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
			goReleaseHistoryEndpoint:                                     `<html><p id="go1.26.6">go1.26.6 (released 2026-08-20)</p></html>`,
			"https://nodejs.org/dist/index.json":                         `[{"version":"v24.18.0","date":"2026-08-20"}]`,
			"https://proxy.golang.org/honnef.co/go/tools/@v/v0.7.0.info": `{"Version":"v0.7.0","Time":"` + released + `"}`,
			"https://github.com/astral-sh/ruff/releases.atom":            `<feed><entry><id>tag:github.com,2008:Repository/123/v0.16.0</id><updated>` + released + `</updated></entry></feed>`,
			"https://registry.npmjs.org/@example%2Fcli":                  `{"time":{"2.3.4":"` + released + `"}}`,
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
		if (finding.Subject == "go@1.26.6" || finding.Subject == "node@24.18.0") &&
			!finding.ReleaseAge.Released.Equal(time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("date-only metadata was not conservatively bounded for %s: %s", finding.Subject, finding.ReleaseAge.Released)
		}
	}
}

func TestGoToolchainReleaseHistoryFailsClosedOnMalformedEntries(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`<p id="go1.26.5">go1.26.5 (released 2026-08-20)</p>`,
		`<p id="go1.26.6">go1.26.6</p>`,
		`<p id="go1.26.6">go1.26.6 (released yesterday)</p>`,
		`<p id="go1.26.6">go1.26.6 (released 2026-08-20) (released 2026-08-21)</p>`,
	} {
		body := body
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			client := artifactClientFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.String() != goReleaseHistoryEndpoint || request.Header.Get("Accept") != "text/html" {
					return nil, fmt.Errorf("unexpected Go release history request")
				}
				return artifactResponse(http.StatusOK, body), nil
			})
			if _, err := lookupGoToolchainRelease(t.Context(), client, "1.26.6"); err == nil {
				t.Fatal("malformed Go release history entry was accepted")
			}
		})
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
		if request.URL.String() != "https://github.com/astral-sh/ruff/releases.atom" {
			return nil, fmt.Errorf("unexpected metadata request %s", request.URL)
		}
		return artifactResponse(http.StatusServiceUnavailable, ""), nil
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
		return artifactResponse(http.StatusOK, `<feed><entry><id>tag:github.com,2008:Repository/123/0.16.0</id><updated>2026-06-01T00:00:00Z</updated></entry></feed>`), nil
	})
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	if findings := releaseArtifactAgeFindings(t.Context(), repo, client, now); len(findings) != 0 {
		t.Fatalf("old artifact findings = %+v", findings)
	}
}

func TestGitHubReleaseFeedPaginatesToTheExactTag(t *testing.T) {
	t.Parallel()
	requests := []string{}
	released := "2026-06-30T09:52:02Z"
	client := artifactClientFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.String())
		if request.Header.Get("Accept") != "application/atom+xml" || request.Header.Get("Authorization") != "" {
			return nil, fmt.Errorf("unexpected GitHub release feed headers")
		}
		switch request.URL.String() {
		case "https://github.com/aquasecurity/trivy/releases.atom":
			return artifactResponse(http.StatusOK, `<feed><entry><id>tag:github.com,2008:Repository/180687624/v0.73.0</id><updated>2026-08-03T10:56:21Z</updated></entry></feed>`), nil
		case "https://github.com/aquasecurity/trivy/releases.atom?after=v0.73.0":
			return artifactResponse(http.StatusOK, `<feed><entry><id>tag:github.com,2008:Repository/180687624/v0.72.0</id><updated>`+released+`</updated></entry></feed>`), nil
		default:
			return nil, fmt.Errorf("unexpected metadata request %s", request.URL)
		}
	})
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
	if len(requests) != 2 {
		t.Fatalf("metadata requests = %q", requests)
	}
}

func TestGitHubReleaseFeedRejectsMalformedMetadata(t *testing.T) {
	t.Parallel()
	client := artifactClientFunc(func(*http.Request) (*http.Response, error) {
		return artifactResponse(http.StatusOK, `<feed><entry><id>unexpected</id><updated>2026-06-30T09:52:02Z</updated></entry></feed>`), nil
	})
	if _, err := lookupGitHubRelease(t.Context(), client, "aquasecurity/trivy", "v0.72.0"); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("malformed feed error = %v", err)
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
		t.Context(), client, "https://github.com/aquasecurity/trivy/releases.atom", &target,
	)
	if err == nil || !strings.Contains(err.Error(), `GitHub message "API rate limit exceeded"`) ||
		!strings.Contains(err.Error(), "rate-limit remaining 0") || !strings.Contains(err.Error(), "retry after 60") {
		t.Fatalf("metadata error = %v", err)
	}
}

type artifactClientFunc func(*http.Request) (*http.Response, error)

func (function artifactClientFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

func artifactResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}
