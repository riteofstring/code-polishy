package supplychain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func osvCommands(repo repository.Repository) ([]policy.Command, error) {
	scans, err := osvScanPlan(repo)
	if err != nil {
		return nil, err
	}
	commands := make([]policy.Command, len(scans))
	for index, scan := range scans {
		commands[index] = scan.Command
	}
	return commands, nil
}

func osvCommand(repo repository.Repository, name, root string) policy.Command {
	return policy.Command{
		Name: name, Argv: []string{repo.PolicyTool("osv-scanner"), "scan", "source", "--all-vulns", "--format", "json", "--verbosity", "error"}, Cwd: root,
		Environment: repo.Config.SupplyChain.Environment, TimeoutSeconds: 1800, ReportProtocol: policy.OSVVulnerabilityReportProtocol,
	}
}

func osvScanPlan(repo repository.Repository) ([]osvScan, error) {
	tool := repo.PolicyTool("osv-scanner")
	if !runner.ExecutableFile(tool) || len(activeOSVRoots(repo.Config)) == 0 {
		return nil, nil
	}
	inputs, err := onlineUVInputs(repo)
	if err != nil {
		return nil, err
	}
	files, err := repo.RecursiveScanFiles()
	if err != nil {
		return nil, fmt.Errorf("enumerate dependency inputs: %w", err)
	}
	emptyPNPMProjects := dependencyFreePNPMProjects(repo, files)
	cargo, err := osvLockInputs(repo, "Cargo.lock", osvCargoInventory)
	if err != nil {
		return nil, err
	}
	scans := []osvScan{}
	for _, root := range activeOSVRoots(repo.Config) {
		command := osvCommand(repo, "osv-scan-"+safeName(root), root)
		command.Argv = append(command.Argv, "--recursive", "--experimental-disable-plugins", "python/uvlock", "--experimental-exclude", "g:**/.code-polishy-reports")
		command.Argv = append(command.Argv, "--experimental-disable-plugins", "rust/cargolock", "--experimental-disable-plugins", "rust/cargotoml")
		if rootHasOnlyCoveredDependencyInputs(repo, root, files, append(append([]osvLockInput{}, inputs...), cargo...), emptyPNPMProjects) {
			command.Argv = append(command.Argv, "--allow-no-lockfiles")
		}
		command.Argv = append(command.Argv, ".")
		scans = append(scans, osvScan{Command: command, Root: root})
	}
	uv, err := osvUVScans(repo, inputs)
	if err != nil {
		return nil, err
	}
	rust, err := osvCargoScans(repo, cargo)
	return append(append(scans, uv...), rust...), err
}

func dependencyFreePNPMProjects(repo repository.Repository, files []string) map[string]map[string]bool {
	candidates := map[string][]nodeManifest{}
	rejected := map[string]bool{}
	for _, manifest := range validNodeManifests(repo, files) {
		if manifest.Manager != "pnpm" {
			continue
		}
		candidates[manifest.Root] = append(candidates[manifest.Root], manifest)
		if len(manifest.Dependencies) != 0 {
			rejected[manifest.Root] = true
		}
	}
	available := map[string]bool{}
	for _, path := range files {
		available[path] = true
	}
	empty := map[string]map[string]bool{}
	for root, manifests := range candidates {
		lock := lockPath(root)
		if rejected[root] || !available[lock] || !dependencyFreePNPMLock(repo, root, manifests) {
			continue
		}
		inputs := map[string]bool{lock: true}
		for _, manifest := range manifests {
			inputs[manifest.Path] = true
		}
		empty[root] = inputs
	}
	return empty
}

func dependencyFreePNPMLock(repo repository.Repository, root string, manifests []nodeManifest) bool {
	result, err := pnpmFacts(context.Background(), repo, root)
	if err != nil || unreadableLock(result, lockPath(root)) != nil || len(result.Unsupported) != 0 || len(result.Importers) == 0 || len(result.Packages) != 0 {
		return false
	}
	expected := map[string]bool{}
	for _, manifest := range manifests {
		expected[manifest.Path] = true
	}
	if len(result.Importers) != len(expected) {
		return false
	}
	for _, importer := range result.Importers {
		if len(importer.Dependencies) != 0 || !expected[importer.Manifest] {
			return false
		}
		delete(expected, importer.Manifest)
	}
	return len(expected) == 0
}

func rootHasOnlyCoveredDependencyInputs(repo repository.Repository, root string, files []string, lockInputs []osvLockInput, pnpmProjects map[string]map[string]bool) bool {
	allowed := coveredDependencyInputs(root, files, lockInputs, pnpmProjects)
	return len(allowed) != 0 && !hasUncoveredDependencyInput(repo, root, files, allowed)
}

func coveredDependencyInputs(root string, files []string, lockInputs []osvLockInput, pnpmProjects map[string]map[string]bool) map[string]bool {
	allowed := map[string]bool{}
	available := map[string]bool{}
	for _, path := range files {
		available[path] = true
	}
	for _, input := range lockInputs {
		if !scopeInsideOSVRoot(input.Scope, root) {
			continue
		}
		allowed[input.Scope] = true
		name := "pyproject.toml"
		if filepath.Base(input.Scope) == "Cargo.lock" {
			name = "Cargo.toml"
		}
		manifest := filepath.ToSlash(filepath.Join(filepath.Dir(input.Scope), name))
		if available[manifest] {
			allowed[manifest] = true
		}
	}
	for project, inputs := range pnpmProjects {
		if !scopeInsideOSVRoot(lockPath(project), root) {
			continue
		}
		for input := range inputs {
			allowed[input] = true
		}
	}
	return allowed
}

func hasUncoveredDependencyInput(repo repository.Repository, root string, files []string, allowed map[string]bool) bool {
	for _, path := range files {
		if scopeInsideOSVRoot(path, root) && isOSVPackageSourceInput(repo, path) && !allowed[path] {
			return true
		}
	}
	return false
}

func scanOSVWithCommands(ctx context.Context, repo repository.Repository, commands []policy.Command, commandRunner runner.Runner) []policy.Finding {
	tool := repo.PolicyTool("osv-scanner")
	if !runner.ExecutableFile(tool) {
		return []policy.Finding{{
			Check: "policy.securityScanner", Path: "repository", Subject: "osv-scanner",
			Message: "pinned OSV-Scanner is unavailable; run ./tools/install-policy-tools.sh",
		}}
	}
	scans, err := osvScanPlan(repo)
	if err != nil {
		return []policy.Finding{{Check: "policy.securityScanner", Path: "repository", Subject: "osv-scanner", Message: err.Error()}}
	}
	if len(commands) != len(scans) {
		return []policy.Finding{{
			Check: "policy.supplyChain", Path: "repository", Subject: "online command plan",
			Message: "OSV online supply-chain command plan is incomplete",
		}}
	}
	findings := []policy.Finding{}
	for index, scan := range scans {
		if !sameOnlineCommand(scan.Command, commands[index]) {
			return []policy.Finding{{Check: "policy.supplyChain", Path: "repository", Subject: "online command plan", Message: "OSV command differs from its public input plan"}}
		}
	}
	for _, scan := range scans {
		findings = append(findings, scanOSVRoot(ctx, repo, scan, commandRunner)...)
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

func scanOSVRoot(ctx context.Context, repo repository.Repository, scan osvScan, commandRunner runner.Runner) []policy.Finding {
	root := scan.Root
	result, output, runErr := runOSVCommandWithOutput(ctx, repo, scan, commandRunner)
	findings, parseErr := parseOSVScanReport(repo, scan, output.Stdout)
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
	if runErr != nil && !acceptedOSVFindingExit(ctx, result, findings, runErr) {
		message := strings.TrimSpace(string(output.Stderr))
		if message == "" {
			message = runErr.Error()
		}
		return append(findings, policy.Finding{Check: "policy.securityScanner", Path: root, Subject: "osv-scanner", Message: message})
	}
	return findings
}

func runOSVCommandWithOutput(ctx context.Context, repo repository.Repository, scan osvScan, commandRunner runner.Runner) (runner.Result, runner.Output, error) {
	invocation := runner.Invocation{Root: repo.Root, Command: scan.Command, Capture: runner.CaptureStructured, Accept: func(result runner.Result, output runner.Output, runErr error) bool {
		return acceptedOSVReportExit(ctx, repo, scan, result, output, runErr)
	}}
	if scan.Snapshot != nil {
		invocation.Snapshot = scan.Snapshot
	}
	return commandRunner.Execute(ctx, invocation)
}

func acceptedOSVReportExit(ctx context.Context, repo repository.Repository, scan osvScan, result runner.Result, output runner.Output, runErr error) bool {
	findings, err := parseOSVScanReport(repo, scan, output.Stdout)
	return err == nil && ctx.Err() == nil && !scannerCoverageFailure(findings) && (runErr == nil || acceptedOSVFindingExit(ctx, result, findings, runErr))
}

func scannerCoverageFailure(findings []policy.Finding) bool {
	return slices.ContainsFunc(findings, func(finding policy.Finding) bool { return finding.Check == "policy.securityScanner" })
}

func acceptedOSVFindingExit(ctx context.Context, result runner.Result, findings []policy.Finding, runErr error) bool {
	if ctx.Err() != nil || runErr == nil || result.ExitStatus != 1 || runner.FailureCategoryFor(ctx, result, runErr) != runner.FailureCommandExit {
		return false
	}
	if len(findings) == 0 {
		return false
	}
	for _, finding := range findings {
		if finding.Check == "supplyChain.osvUnmaintained" && finding.Severity == policy.FindingWarning {
			continue
		}
		if finding.Check != "supplyChain.osvVulnerability" || finding.Vulnerability == nil {
			return false
		}
	}
	return true
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
	ID               string              `json:"id"`
	Aliases          []string            `json:"aliases"`
	Severity         []json.RawMessage   `json:"severity"`
	Affected         []osvAffectedRecord `json:"affected"`
	DatabaseSpecific struct {
		Severity string `json:"severity"`
	} `json:"database_specific"`
}

func decodeOSVReport(payload []byte) (osvReport, error) {
	var report osvReport
	if len(bytes.TrimSpace(payload)) == 0 {
		return report, errors.New("scanner returned no JSON report")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return report, err
	}
	if _, exists := fields["results"]; !exists {
		return report, errors.New("scanner report omitted results")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&report); err != nil {
		return report, err
	}
	if err := requireOneJSONValue(decoder); err != nil {
		return report, err
	}
	return report, nil
}

func parseOSVScanReport(repo repository.Repository, scan osvScan, payload []byte) ([]policy.Finding, error) {
	report, err := decodeOSVReport(payload)
	if err != nil {
		return nil, err
	}
	if err := validateOSVInventoryReport(scan, report); err != nil {
		return nil, err
	}
	findings := []policy.Finding{}
	for _, result := range report.Results {
		scope, err := osvScope(repo, scan.Root, result.Source.Path)
		if err != nil {
			return nil, err
		}
		if scan.InputPath != "" {
			if scope != scan.InputPath {
				return nil, errors.New("scanner report refers to an input outside its public package projection")
			}
			scope = scan.Scope
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
		return osvAbsoluteScope(repo, path)
	} else if root != "." {
		path = filepath.Join(filepath.FromSlash(root), path)
	}
	return repo.NormalizePath(path)
}

func osvAbsoluteScope(repo repository.Repository, path string) (string, error) {
	relative, err := filepath.Rel(repo.Root, path)
	if err == nil && filepath.IsLocal(relative) {
		return repo.NormalizePath(relative)
	}
	physicalRoot, err := filepath.EvalSymlinks(repo.Root)
	if err != nil {
		return "", err
	}
	relative, err = filepath.Rel(physicalRoot, path)
	if err != nil {
		return "", err
	}
	return repo.NormalizePath(relative)
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
		if osvUnmaintainedGroup(group, observed, identifiers) {
			findings = append(findings, osvUnmaintainedFinding(scope, observed, identifiers))
			continue
		}
		advisory := preferredAdvisory(identifiers)
		if advisory == "" {
			advisory = "OSV:unknown"
		}
		severity, source := osvSeverity(group, observed.Vulnerabilities, identifiers)
		identity := policy.VulnerabilityIdentity{
			Ecosystem: ecosystem, Advisory: advisory, Aliases: identifiers, Package: observed.Package.Name,
			AffectedVersion: observed.Package.Version, Scope: scope, Severity: severity,
		}
		message := fmt.Sprintf("OSV reports %s in %s@%s (severity %s; %s)", advisory, observed.Package.Name, observed.Package.Version, severity, source)
		findings = append(findings, policy.Finding{
			Check: "supplyChain.osvVulnerability", Path: scope, Subject: policy.VulnerabilitySubject(identity),
			Message: message, Vulnerability: &identity, Fields: map[string]string{"severitySource": source},
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
