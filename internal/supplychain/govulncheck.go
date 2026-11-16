package supplychain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type goVulnerabilityReport struct {
	Config struct {
		ProtocolVersion string `json:"protocol_version"`
		ScanLevel       string `json:"scan_level"`
		ScanMode        string `json:"scan_mode"`
	}
	SBOM struct {
		Modules []goVulnerabilityModule
		Roots   []string
	}
	Advisories map[string]osvVulnerability
	Findings   []goVulnerabilityTrace
}

type goVulnerabilityModule struct{ Path, Version string }

type goVulnerabilityTrace struct {
	OSV          string
	FixedVersion string `json:"fixed_version"`
	Trace        []struct{ Module, Version, Package, Function, Receiver string }
}

func scanGoVulnerabilities(ctx context.Context, repo repository.Repository, module repository.GoModule, command policy.Command, commandRunner runner.Runner) []policy.Finding {
	result, output, runErr := runner.RunWithReport(ctx, commandRunner, repo.Root, command, func(result runner.Result, output runner.Output, runErr error) bool {
		_, err := decodeGoVulnerabilityReport(output.Stdout)
		return runErr == nil && result.ExitStatus == 0 && err == nil
	})
	if result.ExitStatus != 0 && runErr == nil {
		runErr = fmt.Errorf("govulncheck exited with status %d", result.ExitStatus)
	}
	report, err := decodeGoVulnerabilityReport(output.Stdout)
	if err = errors.Join(runErr, err); err != nil {
		return []policy.Finding{goVulnerabilityCoverage(module.Manifest, err)}
	}
	return goVulnerabilityFindings(ctx, module.Manifest, report, registryClient())
}

func decodeGoVulnerabilityReport(payload []byte) (goVulnerabilityReport, error) {
	report := goVulnerabilityReport{Advisories: map[string]osvVulnerability{}}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	seen := map[string]bool{}
	for {
		var message map[string]json.RawMessage
		if err := decoder.Decode(&message); err != nil {
			if err == io.EOF {
				break
			}
			return report, fmt.Errorf("invalid govulncheck JSON: %w", err)
		}
		if len(message) != 1 {
			return report, errors.New("govulncheck message must contain exactly one record")
		}
		for kind, data := range message {
			if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
				return report, errors.New("govulncheck record is null")
			}
			if err := report.readMessage(kind, data, seen); err != nil {
				return report, err
			}
			seen[kind] = true
		}
	}
	return report, report.validate()
}

func (report goVulnerabilityReport) validate() error {
	if report.Config.ProtocolVersion != "v1.0.0" || report.Config.ScanMode != "source" || report.Config.ScanLevel != "symbol" || len(report.SBOM.Modules) == 0 || len(report.SBOM.Roots) == 0 || slices.Contains(report.SBOM.Roots, "") {
		return errors.New("govulncheck must report a v1.0.0 source symbol scan with scanned modules and roots")
	}
	for _, finding := range report.Findings {
		if err := report.validateTrace(finding); err != nil {
			return err
		}
	}
	return nil
}

func (report *goVulnerabilityReport) readMessage(kind string, data []byte, seen map[string]bool) error {
	switch kind {
	case "config", "SBOM":
		if seen[kind] {
			return fmt.Errorf("govulncheck repeated %s", kind)
		}
		if kind == "config" {
			return json.Unmarshal(data, &report.Config)
		}
		return json.Unmarshal(data, &report.SBOM)
	case "osv":
		var advisory osvVulnerability
		if err := json.Unmarshal(data, &advisory); err != nil {
			return err
		}
		if advisory.ID == "" {
			return errors.New("govulncheck advisory has no identifier")
		}
		report.Advisories[advisory.ID] = advisory
	case "finding":
		var finding goVulnerabilityTrace
		if err := json.Unmarshal(data, &finding); err != nil {
			return err
		}
		report.Findings = append(report.Findings, finding)
	case "progress":
	default:
		return fmt.Errorf("unsupported govulncheck message %q", kind)
	}
	return nil
}

func (report goVulnerabilityReport) validateTrace(finding goVulnerabilityTrace) error {
	advisory, found := report.Advisories[finding.OSV]
	if !found || len(finding.Trace) == 0 {
		return errors.New("govulncheck finding has no advisory or trace")
	}
	frame := finding.Trace[0]
	if frame.Function == "" && len(finding.Trace) != 1 {
		return errors.New("govulncheck module or package finding has an invalid call trace")
	}
	if frame.Function != "" && (frame.Package == "" || !semver.IsValid(frame.Version)) {
		return errors.New("govulncheck called symbol has no package or exact module version")
	}
	if report.scannedModule(frame.Module, frame.Version) && slices.ContainsFunc(advisory.Affected, func(affected osvAffectedRecord) bool {
		return affected.Package.Name == frame.Module && affected.Package.Ecosystem == "Go"
	}) {
		return nil
	}
	return fmt.Errorf("govulncheck finding %s does not match a scanned module and affected advisory package", finding.OSV)
}

func (report goVulnerabilityReport) scannedModule(path, version string) bool {
	return slices.ContainsFunc(report.SBOM.Modules, func(module goVulnerabilityModule) bool {
		return module.Path != "" && module.Path == path && module.Version == version
	})
}

func goVulnerabilityFindings(ctx context.Context, scope string, report goVulnerabilityReport, client artifactHTTPClient) []policy.Finding {
	findings := []policy.Finding{}
	seen := map[string]bool{}
	severities := map[string][2]string{}
	for _, trace := range report.Findings {
		frame := trace.Trace[0]
		if frame.Function == "" {
			continue
		}
		identity := policy.VulnerabilityIdentity{Ecosystem: "go", Advisory: trace.OSV, Aliases: report.Advisories[trace.OSV].Aliases, Package: frame.Module, AffectedVersion: frame.Version, Scope: scope}
		subject := policy.VulnerabilitySubject(identity)
		if seen[subject] {
			continue
		}
		seen[subject] = true
		severity, found := severities[trace.OSV]
		if !found {
			value, source, err := goAdvisorySeverity(ctx, client, report.Advisories[trace.OSV])
			severity = [2]string{value, source}
			severities[trace.OSV] = severity
			if err != nil {
				findings = append(findings, goVulnerabilityCoverage(scope, err))
			}
		}
		identity.Severity = severity[0]
		symbol := frame.Package + "." + strings.TrimPrefix(frame.Receiver+"."+frame.Function, ".")
		message := fmt.Sprintf("%s vulnerability reported through %s; severity source: %s", strings.ToUpper(identity.Severity), symbol, severity[1])
		if trace.FixedVersion != "" {
			message += "; fixed in " + trace.FixedVersion
		}
		findings = append(findings, policy.Finding{Check: "supplyChain.goVulnerability", Path: scope, Subject: subject, Message: message, Vulnerability: &identity})
	}
	return findings
}

func goVulnerabilityCoverage(scope string, err error) policy.Finding {
	return policy.Finding{Check: "policy.securityScanner", Path: scope, Subject: "govulncheck", Message: err.Error()}
}
