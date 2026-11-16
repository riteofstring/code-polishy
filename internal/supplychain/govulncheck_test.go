package supplychain

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

const cleanGoVulnerabilityReport = `{"config":{"protocol_version":"v1.0.0","scan_mode":"source","scan_level":"symbol"}}
{"SBOM":{"modules":[{"path":"example.test/app"},{"path":"stdlib","version":"v1.26.10"}],"roots":["example.test/app"]}}
`

const goTestAdvisory = `{"osv":{"id":"GO-2026-0001","aliases":["CVE-2026-12345"],"affected":[{"package":{"name":"stdlib","ecosystem":"Go"}}],"database_specific":{"severity":"MODERATE"}}}
`

const goTestCall = `{"finding":{"osv":"GO-2026-0001","fixed_version":"v1.26.11","trace":[{"module":"stdlib","version":"v1.26.10","package":"net/http","receiver":"Client","function":"Do"},{"module":"example.test/app","package":"example.test/app","function":"main"}]}}
`

func TestGoMissingScannerIsACoverageFailure(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	module := repository.GoModule{Manifest: "go.mod", Root: "."}
	findings := checkGoOnlineWithCommands(t.Context(), repo, module, goOnlineCommands(repo, module), supplyToolBoundary{})
	if len(findings) != 1 || findings[0].Check != "policy.securityScanner" || findings[0].Subject != "govulncheck" {
		t.Fatalf("missing scanner findings = %+v", findings)
	}
}

func TestGoVulnerabilityAssessmentMatchesExactCalledAdvisory(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	boundary := &goVulnerabilityBoundary{t: t, payload: cleanGoVulnerabilityReport + goTestAdvisory + goTestCall + goTestCall}
	findings := scanGoVulnerabilities(t.Context(), repo, repository.GoModule{Manifest: "nested/go.mod"}, policy.Command{}, boundary)
	if !boundary.accepted || len(findings) != 1 {
		t.Fatalf("scan accepted=%t; findings=%+v", boundary.accepted, findings)
	}
	finding := findings[0]
	identity := finding.Vulnerability
	expected := &policy.VulnerabilityIdentity{Ecosystem: "go", Advisory: "GO-2026-0001", Package: "stdlib", AffectedVersion: "v1.26.10", Scope: "nested/go.mod", Severity: "moderate", Aliases: []string{"CVE-2026-12345"}}
	if !reflect.DeepEqual(identity, expected) {
		t.Fatalf("advisory identity = %+v", identity)
	}
	if !strings.Contains(finding.Message, "net/http.Client.Do") || !strings.Contains(finding.Message, "v1.26.11") {
		t.Fatalf("diagnostic = %s", finding.Message)
	}
	now := time.Now().UTC()
	assessment := policy.VulnerabilityAssessment{ID: "http-client", Advisory: "CVE-2026-12345", Ecosystem: "go", Package: "stdlib", AffectedVersion: "v1.26.10", Scope: "nested/go.mod", Severity: "moderate", Status: policy.NotAffectedAssessment, Expires: policy.Date{Time: now.AddDate(0, 0, 1)}}
	for _, mismatch := range []string{"", "version", "scope", "advisory", "severity", "exploited", "expired"} {
		t.Run(mismatch, func(t *testing.T) {
			candidate := assessment
			observed := *identity
			switch mismatch {
			case "version":
				candidate.AffectedVersion = "v1.26.9"
			case "scope":
				candidate.Scope = "go.mod"
			case "advisory":
				candidate.Advisory = "CVE-2026-99999"
			case "severity":
				observed.Severity = "high"
			case "exploited":
				observed.KnownExploited = true
			case "expired":
				candidate.Expires = policy.Date{Time: now.AddDate(0, 0, -1)}
			}
			input := finding
			input.Vulnerability = &observed
			kept, assessed := policy.ApplyVulnerabilityAssessments([]policy.Finding{input}, []policy.VulnerabilityAssessment{candidate}, now, policy.VulnerabilityObservations{Packages: true})
			if mismatch == "" && (len(kept) != 0 || len(assessed) != 1) || mismatch != "" && (len(kept) == 0 || len(assessed) != 0) {
				t.Fatalf("kept=%+v assessed=%+v", kept, assessed)
			}
		})
	}
}

func TestGoVulnerabilityScanPreservesSymbolReachability(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"clean", "database-only", "module-only", "package-only", "called-module"} {
		t.Run(name, func(t *testing.T) {
			payload := cleanGoVulnerabilityReport
			if name != "clean" {
				payload += goTestAdvisory
			}
			if name == "module-only" || name == "package-only" {
				frame := `{"module":"stdlib","version":"v1.26.10"}`
				if name == "package-only" {
					frame = `{"module":"stdlib","version":"v1.26.10","package":"net/http"}`
				}
				payload += `{"finding":{"osv":"GO-2026-0001","trace":[` + frame + `]}}`
			}
			if name == "called-module" {
				payload = strings.ReplaceAll(payload+goTestCall, "stdlib", "example.test/replacement")
			}
			boundary := &goVulnerabilityBoundary{t: t, payload: payload}
			findings := scanGoVulnerabilities(t.Context(), supplyRepository(t), repository.GoModule{Manifest: "go.mod"}, policy.Command{}, boundary)
			if !boundary.accepted || name != "called-module" && len(findings) != 0 || name == "called-module" && (len(findings) != 1 || findings[0].Vulnerability == nil || findings[0].Vulnerability.Package != "example.test/replacement") {
				t.Fatalf("scan accepted=%t; findings=%+v", boundary.accepted, findings)
			}
		})
	}
}

func TestGoVulnerabilityFailuresCannotEstablishCoverage(t *testing.T) {
	t.Parallel()
	valid := cleanGoVulnerabilityReport + goTestAdvisory + goTestCall
	for name, payload := range map[string]string{
		"empty": "", "truncated": valid + `{"finding":`, "null": "null", "null-record": `{"config":null}`,
		"unknown-record": valid + `{"error":"partial analysis"}`, "missing-advisory": cleanGoVulnerabilityReport + goTestCall,
		"module-scan": strings.Replace(valid, `"symbol"`, `"module"`, 1), "binary-scan": strings.Replace(valid, `"source"`, `"binary"`, 1),
		"protocol": strings.Replace(valid, "v1.0.0", "v2.0.0", 1), "no-roots": strings.Replace(valid, `["example.test/app"]`, `[]`, 1),
		"foreign-version": strings.Replace(valid, `"version":"v1.26.10"`, `"version":"v1.26.9"`, 1),
		"unknown-version": strings.ReplaceAll(valid, "v1.26.10", ""), "duplicate-sbom": valid + cleanGoVulnerabilityReport,
		"no-trace":          cleanGoVulnerabilityReport + goTestAdvisory + `{"finding":{"osv":"GO-2026-0001"}}`,
		"unaffected-module": strings.Replace(valid, `"name":"stdlib"`, `"name":"other"`, 1),
		"foreign-ecosystem": strings.Replace(valid, `"ecosystem":"Go"`, `"ecosystem":"PyPI"`, 1),
		"empty-root":        strings.Replace(valid, `["example.test/app"]`, `[""]`, 1),
		"incomplete-call":   strings.Replace(valid, `"function":"Do"`, `"function":""`, 1),
		"operational":       valid, "nonzero": valid,
	} {
		t.Run(name, func(t *testing.T) {
			boundary := &goVulnerabilityBoundary{t: t, payload: payload}
			if name == "operational" {
				boundary.err = errors.New("scanner interrupted")
			}
			if name == "nonzero" {
				boundary.result.ExitStatus = 3
			}
			findings := scanGoVulnerabilities(t.Context(), supplyRepository(t), repository.GoModule{Manifest: "go.mod"}, policy.Command{}, boundary)
			if boundary.accepted || len(findings) != 1 || findings[0].Check != "policy.securityScanner" || findings[0].Vulnerability != nil {
				t.Fatalf("scan accepted=%t; findings=%+v", boundary.accepted, findings)
			}
		})
	}
}

type goVulnerabilityBoundary struct {
	t        *testing.T
	payload  string
	result   runner.Result
	err      error
	accepted bool
}

func (boundary *goVulnerabilityBoundary) Execute(_ context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	boundary.t.Helper()
	if invocation.Capture != runner.CaptureStructured || invocation.Accept == nil {
		boundary.t.Fatal("govulncheck must capture and validate its structured report")
	}
	output := runner.Output{Stdout: []byte(boundary.payload)}
	boundary.accepted = invocation.Accept(boundary.result, output, boundary.err)
	return boundary.result, output, boundary.err
}
