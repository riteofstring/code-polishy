package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	pathpkg "path"
	"slices"
	"sort"
	"strings"
)

const FindingProtocol = "code-polishy-finding/v1"

func NormalizeFinding(finding Finding) Finding {
	finding.Path = normalizedFindingPath(finding.Path)
	finding.SemanticIdentity = slices.Clone(finding.SemanticIdentity)
	sort.Strings(finding.SemanticIdentity)
	finding.SemanticIdentity = slices.Compact(finding.SemanticIdentity)
	for index := range finding.Related {
		finding.Related[index].Path = normalizedFindingPath(finding.Related[index].Path)
	}
	if finding.Severity == "" {
		finding.Severity = FindingError
	}
	if finding.Status == "" {
		finding.Status = FindingOpen
	}
	if finding.Scope.Kind == "" {
		finding.Scope = defaultFindingScope(finding)
	}
	if finding.SelectionRelation == "" {
		finding.SelectionRelation = SelectionGlobal
	}
	if finding.Remediation.Summary == "" {
		finding.Remediation = defaultFindingRemediation(finding)
	}
	if finding.Remediation.NextCommand == nil && finding.Remediation.Generation == nil {
		finding.Remediation.NextCommand = &FindingCommand{Argv: defaultFindingCommand(finding), Cwd: "."}
	}
	finding.Fingerprint = findingFingerprint(finding)
	return finding
}

func normalizedFindingPath(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	if value == "" || value == "repository" {
		return value
	}
	cleaned := pathpkg.Clean(value)
	if cleaned == "." && value != "." {
		return value
	}
	return cleaned
}

func defaultFindingScope(finding Finding) FindingScope {
	if finding.Path == "" || finding.Path == "repository" || finding.Path == ConfigFilename {
		return FindingScope{Kind: "repository", Value: "repository"}
	}
	if finding.Module != "" {
		return FindingScope{Kind: "module", Value: finding.Module}
	}
	return FindingScope{Kind: "path", Value: finding.Path}
}

func defaultFindingRemediation(finding Finding) FindingRemediation {
	if finding.Check == "test.artifacts" {
		return FindingRemediation{
			Summary:     "Restore writable managed test-artifact storage and resolve the reported finalization error. Inspect the test plan before rerunning affected suites; the execution ID is not a suite name.",
			NextCommand: &FindingCommand{Argv: []string{"code-polishy", "test-plan"}, Cwd: "."},
		}
	}
	return FindingRemediation{
		Summary:     findingRemediationSummary(finding),
		NextCommand: &FindingCommand{Argv: defaultFindingCommand(finding), Cwd: "."},
	}
}

func defaultFindingCommand(finding Finding) []string {
	if strings.HasPrefix(finding.Check, "pack.") {
		return packFindingCommand(finding)
	}
	command := "doctor"
	switch {
	case strings.HasPrefix(finding.Check, "architecture."), finding.Check == "testing.fileCycle":
		command = "architecture"
	case strings.HasPrefix(finding.Check, "quality."), strings.HasPrefix(finding.Check, "portability."):
		command = "check"
	case strings.HasPrefix(finding.Check, "supplyChain."):
		return []string{"code-polishy", "supply-chain"}
	case strings.HasPrefix(finding.Check, "artifactSecurity."):
		return []string{"code-polishy", "artifact-security"}
	case strings.HasPrefix(finding.Check, "test."):
		if finding.Subject != "" {
			return []string{"code-polishy", "test", "--suite", finding.Subject}
		}
		return []string{"code-polishy", "test", "--all"}
	}
	return findingPathCommand(command, finding.Path)
}

func packFindingCommand(finding Finding) []string {
	if finding.Check == "pack.build" {
		return []string{"code-polishy", "verify"}
	}
	if finding.Check == "pack.security" {
		return []string{"code-polishy", "supply-chain"}
	}
	if rule, exists := packRemediationRules[finding.Check]; exists {
		finding.Check = rule
		return defaultFindingCommand(finding)
	}
	return []string{"code-polishy", "doctor"}
}

func findingPathCommand(command, path string) []string {
	if command != "doctor" && path != "" && path != "repository" && path != ConfigFilename {
		return []string{"code-polishy", command, "--files", path}
	}
	if command != "doctor" {
		return []string{"code-polishy", command, "--all"}
	}
	return []string{"code-polishy", command}
}

func findingFingerprint(finding Finding) string {
	path := finding.Path
	line := finding.Line
	column := finding.Column
	subject := finding.Subject
	scope := finding.Scope
	module := finding.Module
	if len(finding.SemanticIdentity) > 0 {
		path = ""
		line = 0
		column = 0
		subject = ""
		scope.Value = ""
		module = ""
	}
	identity := struct {
		Protocol      string
		RuleID        string
		Scope         FindingScope
		Path          string
		Line          int
		Column        int
		Subject       string
		Module        string
		Producer      string
		Fields        map[string]string
		Semantic      []string
		Vulnerability *VulnerabilityIdentity
		ReleaseAge    *ReleaseAgeIdentity
	}{
		Protocol: FindingProtocol, RuleID: finding.Check, Scope: scope,
		Path: path, Line: line, Column: column, Subject: subject,
		Module: module, Producer: finding.GeneratedProducer,
		Fields: finding.Fields, Semantic: finding.SemanticIdentity,
		Vulnerability: finding.Vulnerability, ReleaseAge: finding.ReleaseAge,
	}
	data, _ := json.Marshal(identity)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
