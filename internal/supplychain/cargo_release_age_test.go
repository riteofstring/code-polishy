package supplychain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestCargoReleaseAgeCoversTheWholeLockAndRetainsExactAssessments(t *testing.T) {
	repo := supplyRepository(t)
	repo.Config.SupplyChain.MinimumReleaseAgeDays = 30
	now := time.Now().UTC()
	transport := &cargoAgeTransport{released: map[string]time.Time{"1.0.0": now.AddDate(0, 0, -60), "2.0.0": now.AddDate(0, 0, -5)}}
	installCargoAgeTransport(t, transport)
	lock := "version=4\n"
	for _, version := range []string{"1.0.0", "2.0.0"} {
		lock += fmt.Sprintf("[[package]]\nname='crate-name'\nversion=%q\nsource=%q\nchecksum=%q\n", version, "registry+https://github.com/rust-lang/crates.io-index", strings.Repeat("a", 64))
	}
	writeSupplyFile(t, repo.Root, "Cargo.lock", lock)
	writeSupplyFile(t, repo.Root, "nested/Cargo.lock", lock)
	findings := cargoReleaseAgeFindings(t.Context(), repo, []string{"Cargo.lock", "nested/Cargo.lock"}, nil)
	if len(findings) != 2 || len(transport.requests) != 1 || !slices.Equal(transport.requests[0].URL.Query()["nums[]"], []string{"1.0.0", "2.0.0"}) {
		t.Fatalf("locked release coverage = %+v, requests = %+v", findings, transport.requests)
	}
	for _, finding := range findings {
		if finding.ReleaseAge == nil || finding.ReleaseAge.Ecosystem != "crates.io" || finding.ReleaseAge.Version != "2.0.0" {
			t.Fatalf("young crate lost exact release identity: %+v", finding)
		}
	}
	assessment := policy.ReleaseAgeAssessment{ID: "security-fix", Ecosystem: "crates.io", Package: "crate-name", Version: "2.0.0", Scope: "Cargo.lock", Expires: policy.Date{Time: now.AddDate(0, 0, 5)}}
	kept, accepted := policy.ApplyReleaseAgeAssessments(findings, []policy.ReleaseAgeAssessment{assessment}, now, true)
	if len(accepted) != 1 || len(kept) != 1 || kept[0].Path != "nested/Cargo.lock" {
		t.Fatalf("exact scope assessment leaked across Cargo locks: kept=%+v, accepted=%+v", kept, accepted)
	}
}

func TestCargoReleaseAgeDoesNotSendGitOrPrivateSourcesToTheRegistry(t *testing.T) {
	repo := supplyRepository(t)
	transport := &cargoAgeTransport{released: map[string]time.Time{"1.0.0": time.Now().UTC().AddDate(0, 0, -90)}}
	installCargoAgeTransport(t, transport)
	lock := fmt.Sprintf("version=4\n[[package]]\nname='crate-name'\nversion='1.0.0'\nsource=%q\nchecksum=%q\n", "registry+https://github.com/rust-lang/crates.io-index", strings.Repeat("a", 64))
	lock += fmt.Sprintf("[[package]]\nname='private-git'\nversion='1.0.0'\nsource='git+https://private.example.test/repo?rev=%s#%s'\n", strings.Repeat("b", 40), strings.Repeat("b", 40))
	lock += fmt.Sprintf("[[package]]\nname='private-registry'\nversion='1.0.0'\nsource='registry+https://private.example.test/index'\nchecksum=%q\n", strings.Repeat("c", 64))
	writeSupplyFile(t, repo.Root, "Cargo.lock", lock)
	findings := cargoReleaseAgeFindings(t.Context(), repo, []string{"Cargo.lock"}, nil)
	if len(transport.requests) != 1 || len(findings) != 2 || !supplyChecks(findings)["supplyChain.cargoSource"] {
		t.Fatalf("mixed-source release checks = %+v, requests = %+v", findings, transport.requests)
	}
	if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Check == "supplyChain.gitSourceCoverage" && finding.Severity == policy.FindingWarning
	}) {
		t.Fatalf("optional Git policy changed: %+v", findings)
	}
	repo.Config.SupplyChain.GitEvidence.Required = true
	findings = cargoReleaseAgeFindings(t.Context(), repo, []string{"Cargo.lock"}, nil)
	if !supplyChecks(findings)["supplyChain.gitEvidence"] {
		t.Fatalf("required Git evidence was silently accepted: %+v", findings)
	}
}

func TestCargoReleaseAgeMissingMetadataCannotBeWaived(t *testing.T) {
	repo := supplyRepository(t)
	transport := &cargoAgeTransport{released: map[string]time.Time{}}
	installCargoAgeTransport(t, transport)
	packages := []resolvedPackage{{Ecosystem: "crates.io", Name: "crate-name", Version: "1.0.0", Scope: "Cargo.lock"}}
	findings := releaseAgeFindings(t.Context(), repo, packages, time.Now().UTC())
	if len(findings) != 1 || findings[0].ReleaseAge != nil || !strings.Contains(findings[0].Message, "omitted the exact timestamp") {
		t.Fatalf("missing metadata claimed age coverage: %+v", findings)
	}
	assessment := policy.ReleaseAgeAssessment{ID: "incomplete", Ecosystem: "crates.io", Package: "crate-name", Version: "1.0.0", Scope: "Cargo.lock", Expires: policy.Date{Time: time.Now().UTC().AddDate(0, 0, 1)}}
	kept, accepted := policy.ApplyReleaseAgeAssessments(findings, []policy.ReleaseAgeAssessment{assessment}, time.Now().UTC(), true)
	if len(accepted) != 0 || len(kept) != 1 {
		t.Fatalf("an assessment filled missing registry evidence: %+v, %+v", kept, accepted)
	}
}

func TestCargoReleaseAgeRunsInTheOnlineSupplyChainProfile(t *testing.T) {
	repo := supplyRepository(t)
	transport := &cargoAgeTransport{released: map[string]time.Time{"1.0.0": time.Now().UTC().AddDate(0, 0, -1)}}
	installCargoAgeTransport(t, transport)
	lock := fmt.Sprintf("version=4\n[[package]]\nname='crate-name'\nversion='1.0.0'\nsource=%q\nchecksum=%q\n", "registry+https://github.com/rust-lang/crates.io-index", strings.Repeat("a", 64))
	writeSupplyFile(t, repo.Root, "Cargo.lock", lock)
	findings := Online(t.Context(), repo, []string{"Cargo.lock"}, &cargoSupplyRunner{})
	if len(findings) != 1 || findings[0].ReleaseAge == nil || len(transport.requests) != 1 {
		t.Fatalf("online profile omitted Cargo age policy: %+v, requests=%+v", findings, transport.requests)
	}
}

func TestCargoReleaseAgeIncludesWorkspaceLocksBeyondTheSelectedFiles(t *testing.T) {
	repo := supplyRepository(t)
	transport := &cargoAgeTransport{released: map[string]time.Time{"1.0.0": time.Now().UTC().AddDate(0, 0, -1)}}
	installCargoAgeTransport(t, transport)
	lock := repository.CargoLock{Path: "workspace/Cargo.lock", Packages: []repository.CargoLockedPackage{
		{Name: "crate-name", Version: "1.0.0", Source: repository.CargoCratesIOSource},
	}}
	findings := cargoReleaseAgeFindings(t.Context(), repo, []string{"workspace/app/Cargo.toml"}, []repository.CargoLock{lock})
	if len(findings) != 1 || findings[0].Path != lock.Path || findings[0].ReleaseAge == nil || len(transport.requests) != 1 {
		t.Fatalf("workspace lock outside selected files escaped age checks: %+v", findings)
	}
}

func TestCargoReleaseMetadataRejectsWrongAndIncompleteIdentity(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"versions":[{"crate":"different","num":"1.0.0","created_at":"2026-01-01T00:00:00Z"}]}`,
		`{"versions":[{"crate":"crate-name","num":"1.0.0"}]}`,
		`{"versions":[{"crate":"crate-name","num":"1.0.0","created_at":"invalid"}]}`,
		`{"versions":[{"crate":"crate-name","num":"1.0.0","created_at":"2026-01-01T00:00:00Z"},{"crate":"crate-name","num":"1.0.0","created_at":"2026-01-01T00:00:00Z"}]}`,
		`{"versions":[]} {}`,
		`{"versions":[]}` + strings.Repeat(" ", 8<<20),
	} {
		if _, err := decodeCargoReleaseTimes(strings.NewReader(body), "crate-name"); err == nil {
			t.Fatal("invalid registry evidence passed")
		}
	}
}

func installCargoAgeTransport(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
}

type cargoAgeTransport struct {
	released map[string]time.Time
	requests []*http.Request
}

func (transport *cargoAgeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.Host != "crates.io" || request.URL.Path != "/api/v1/crates/crate-name/versions" {
		return nil, fmt.Errorf("unexpected public request: %s", request.URL.Redacted())
	}
	if request.UserAgent() != "Code-Polishy (https://github.com/riteofstring/code-polishy)" {
		return nil, fmt.Errorf("crate request did not identify Code Polishy")
	}
	transport.requests = append(transport.requests, request)
	versions := []map[string]any{}
	for _, version := range request.URL.Query()["nums[]"] {
		if released, exists := transport.released[version]; exists {
			versions = append(versions, map[string]any{"crate": "crate-name", "num": version, "created_at": released})
		}
	}
	data, err := json.Marshal(map[string]any{"versions": versions})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(data))), Header: http.Header{}}, nil
}

func TestCargoReleaseLookupHonorsCanceledRequests(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := lookupCargoReleaseGroup(ctx, &http.Client{Transport: &cargoAgeTransport{}}, releaseGroup{Kind: "crates.io", Name: "crate-name", Items: []resolvedPackage{{Version: "1.0.0"}}})
	if err == nil {
		t.Fatal("canceled registry lookup succeeded")
	}
}
