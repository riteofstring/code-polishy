package supplychain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func osvCommands(repo repository.Repository) []policy.Command {
	tool := repo.PolicyTool("osv-scanner")
	if !isExecutable(tool) {
		return nil
	}
	commands := []policy.Command{}
	for _, root := range activeOSVRoots(repo.Config) {
		commands = append(commands, policy.Command{
			Name: "osv-scan-" + safeName(root), Argv: []string{tool, "scan", "source", "--recursive", "--all-vulns", "--format", "json", "--verbosity", "error", "."}, Cwd: root,
			Environment: repo.Config.SupplyChain.Environment, TimeoutSeconds: 1800,
		})
	}
	return commands
}

func scanOSVWithCommands(ctx context.Context, repo repository.Repository, commands []policy.Command, commandRunner runner.Runner) []policy.Finding {
	tool := repo.PolicyTool("osv-scanner")
	if !isExecutable(tool) {
		return []policy.Finding{{
			Check: "policy.securityScanner", Path: "repository", Subject: "osv-scanner",
			Message: "pinned OSV-Scanner is unavailable; run ./tools/install-policy-tools.sh",
		}}
	}
	roots := activeOSVRoots(repo.Config)
	if len(commands) != len(roots) {
		return []policy.Finding{{
			Check: "policy.supplyChain", Path: "repository", Subject: "online command plan",
			Message: "OSV online supply-chain command plan is incomplete",
		}}
	}
	findings := []policy.Finding{}
	for index, root := range roots {
		findings = append(findings, scanOSVRoot(ctx, repo, root, commands[index], commandRunner)...)
	}
	return uniqueFindings(findings)
}

func activeOSVRoots(config policy.Config) []string {
	roots := []string{}
	for _, module := range config.ActivePolicyModules {
		if module.Name == "osv" {
			roots = append(roots, module.Root)
		}
	}
	sort.Strings(roots)
	return compactStrings(roots)
}

func compactStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func scanOSVRoot(ctx context.Context, repo repository.Repository, root string, command policy.Command, commandRunner runner.Runner) []policy.Finding {
	output, runErr := runCommandWithOutput(ctx, repo, command, commandRunner)
	findings, parseErr := parseOSVReport(repo, root, output.Stdout)
	if parseErr != nil {
		message := fmt.Sprintf("parse OSV-Scanner JSON: %v", parseErr)
		if detail := strings.TrimSpace(string(output.Stderr)); detail != "" {
			message += "; " + detail
		} else if runErr != nil {
			message += "; " + runErr.Error()
		}
		return []policy.Finding{{
			Check: "policy.securityScanner", Path: root, Subject: "osv-scanner",
			Message: message,
		}}
	}
	if runErr != nil && len(findings) == 0 {
		message := strings.TrimSpace(string(output.Stderr))
		if message == "" {
			message = runErr.Error()
		}
		return []policy.Finding{{Check: "policy.securityScanner", Path: root, Subject: "osv-scanner", Message: message}}
	}
	return findings
}

type osvReport struct {
	Results []osvResult `json:"results"`
}

type osvResult struct {
	Source struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"source"`
	Packages []osvPackageResult `json:"packages"`
}

type osvPackageResult struct {
	Package struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Ecosystem string `json:"ecosystem"`
	} `json:"package"`
	Groups          []osvGroup         `json:"groups"`
	Vulnerabilities []osvVulnerability `json:"vulnerabilities"`
}

type osvGroup struct {
	IDs         []string `json:"ids"`
	Aliases     []string `json:"aliases"`
	MaxSeverity string   `json:"max_severity"`
}

type osvVulnerability struct {
	ID      string   `json:"id"`
	Aliases []string `json:"aliases"`
}

func parseOSVReport(repo repository.Repository, root string, payload []byte) ([]policy.Finding, error) {
	if len(bytes.TrimSpace(payload)) == 0 {
		return nil, errors.New("scanner returned no JSON report")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, err
	}
	if _, exists := fields["results"]; !exists {
		return nil, errors.New("scanner report omitted results")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var report osvReport
	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}
	if err := requireOneJSONValue(decoder); err != nil {
		return nil, err
	}
	findings := []policy.Finding{}
	for _, result := range report.Results {
		scope, err := osvScope(repo, root, result.Source.Path)
		if err != nil {
			return nil, err
		}
		for _, observed := range result.Packages {
			findings = append(findings, osvPackageFindings(scope, observed)...)
		}
	}
	sort.Slice(findings, func(left, right int) bool {
		return findings[left].Path+"\x00"+findings[left].Subject < findings[right].Path+"\x00"+findings[right].Subject
	})
	return uniqueFindings(findings), nil
}

func osvScope(repo repository.Repository, root, source string) (string, error) {
	if strings.TrimSpace(source) == "" {
		return "", errors.New("OSV result omitted its dependency source path")
	}
	path := filepath.Clean(source)
	if filepath.IsAbs(path) {
		relative, err := filepath.Rel(repo.Root, path)
		if err != nil {
			return "", err
		}
		path = relative
	} else if root != "." {
		path = filepath.Join(filepath.FromSlash(root), path)
	}
	return repo.NormalizePath(path)
}

func osvPackageFindings(scope string, observed osvPackageResult) []policy.Finding {
	if observed.Package.Name == "" || observed.Package.Version == "" {
		return []policy.Finding{{
			Check: "policy.securityScanner", Path: scope, Subject: "osv-scanner",
			Message: "OSV result omitted an exact package name or version",
		}}
	}
	ecosystem := osvEcosystem(observed.Package.Ecosystem, scope)
	groups := observed.Groups
	if len(groups) == 0 {
		for _, vulnerability := range observed.Vulnerabilities {
			groups = append(groups, osvGroup{IDs: append([]string{vulnerability.ID}, vulnerability.Aliases...)})
		}
	}
	findings := []policy.Finding{}
	for _, group := range groups {
		identifiers := osvGroupIdentifiers(group, observed.Vulnerabilities)
		advisory := preferredAdvisory(identifiers)
		if advisory == "" {
			advisory = "OSV:unknown"
		}
		severity := policy.NormalizeVulnerabilitySeverity(group.MaxSeverity)
		identity := policy.VulnerabilityIdentity{
			Ecosystem: ecosystem, Advisory: advisory, Aliases: identifiers, Package: observed.Package.Name,
			AffectedVersion: observed.Package.Version, Scope: scope, Severity: severity,
		}
		message := fmt.Sprintf("OSV reports %s in %s@%s (severity %s)", advisory, observed.Package.Name, observed.Package.Version, severity)
		findings = append(findings, policy.Finding{
			Check: "supplyChain.osvVulnerability", Path: scope, Subject: policy.VulnerabilitySubject(identity),
			Message: message, Vulnerability: &identity,
		})
	}
	return findings
}

func osvGroupIdentifiers(group osvGroup, vulnerabilities []osvVulnerability) []string {
	identifiers := append(append([]string{}, group.IDs...), group.Aliases...)
	grouped := make(map[string]bool, len(identifiers))
	for _, identifier := range identifiers {
		grouped[identifier] = true
	}
	for _, vulnerability := range vulnerabilities {
		if !grouped[vulnerability.ID] {
			continue
		}
		identifiers = append(identifiers, vulnerability.ID)
		identifiers = append(identifiers, vulnerability.Aliases...)
	}
	return compactNonempty(identifiers)
}

func osvEcosystem(ecosystem, scope string) string {
	switch strings.ToLower(ecosystem) {
	case "npm":
		if strings.HasSuffix(scope, "pnpm-lock.yaml") {
			return "pnpm"
		}
		return "npm"
	case "go":
		return "go"
	case "pypi":
		return "pypi"
	default:
		return strings.ToLower(ecosystem)
	}
}

func preferredAdvisory(values []string) string {
	values = compactNonempty(values)
	for _, prefix := range []string{"GHSA-", "CVE-"} {
		for _, value := range values {
			if strings.HasPrefix(value, prefix) {
				return value
			}
		}
	}
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func compactNonempty(values []string) []string {
	filtered := values[:0]
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			filtered = append(filtered, value)
		}
	}
	sort.Strings(filtered)
	return compactStrings(filtered)
}
