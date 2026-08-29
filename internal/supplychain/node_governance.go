package supplychain

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func nodeManifestGovernanceFindings(repo repository.Repository, path, manager string, payload packageJSON) []policy.Finding {
	findings := nodeAuditIgnoreFindings(path, payload)
	for _, block := range nodeOverrideBlocks(payload) {
		if !nonemptyJSONObject(block.raw) {
			continue
		}
		findings = append(findings, nodeOverrideBlockFindings(repo, path, manager, block.field, block.raw)...)
	}
	return findings
}

func nodeAuditIgnoreFindings(path string, payload packageJSON) []policy.Finding {
	advisories := append(append([]string{}, payload.AuditConfig.IgnoreCVEs...), payload.PNPM.AuditConfig.IgnoreCVEs...)
	findings := make([]policy.Finding, 0, len(advisories))
	for _, advisory := range advisories {
		findings = append(findings, policy.Finding{
			Check: "supplyChain.auditIgnore", Path: path, Subject: advisory,
			Message: "silent auditConfig.ignoreCves entries are forbidden; use an exact governed vulnerability assessment",
		})
	}
	return findings
}

type nodeOverrideBlock struct {
	field string
	raw   json.RawMessage
}

func nodeOverrideBlocks(payload packageJSON) []nodeOverrideBlock {
	return []nodeOverrideBlock{
		{field: "overrides", raw: payload.Overrides},
		{field: "pnpm.overrides", raw: payload.PNPM.Overrides},
	}
}

func nodeOverrideBlockFindings(repo repository.Repository, path, manager, field string, raw json.RawMessage) []policy.Finding {
	digest, err := canonicalJSONSHA256(raw)
	if err != nil {
		return []policy.Finding{{Check: "supplyChain.dependencyOverride", Path: path, Subject: field, Message: err.Error()}}
	}
	governance, found := dependencyOverridePolicy(repo.Config.SupplyChain.DependencyOverridePolicies, path, field)
	if !found {
		message := fmt.Sprintf("%s requires an exact dependencyOverridePolicies owner with content SHA-256 %s", field, digest)
		return []policy.Finding{{Check: "supplyChain.dependencyOverride", Path: path, Subject: field, Message: message}}
	}
	if governance.Ecosystem == manager && governance.ContentSHA256 == digest {
		return nil
	}
	message := fmt.Sprintf("override block changed or has the wrong ecosystem; observed %s SHA-256 %s", manager, digest)
	return []policy.Finding{{Check: "supplyChain.dependencyOverride", Path: path, Subject: governance.ID, Message: message}}
}

func DependencyOverrideGovernanceFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	for _, governance := range repo.Config.SupplyChain.DependencyOverridePolicies {
		if finding, exists := dependencyOverrideGovernanceFinding(repo, files, governance, today); exists {
			findings = append(findings, finding)
		}
	}
	return findings
}

func dependencyOverrideGovernanceFinding(repo repository.Repository, files []string, governance policy.DependencyOverridePolicy, today time.Time) (policy.Finding, bool) {
	if governance.Expires.Before(today) {
		return policy.Finding{
			Check: "policy.dependencyOverridePolicyExpired", Path: policy.ConfigFilename, Subject: governance.ID,
			Message: fmt.Sprintf("dependency override policy expired on %s", governance.Expires.Format("2006-01-02")),
		}, true
	}
	if !slices.Contains(files, governance.Path) {
		return unusedDependencyOverridePolicy(governance), true
	}
	payload, ok := readPackageJSON(repo, governance.Path)
	if !ok {
		return unusedDependencyOverridePolicy(governance), true
	}
	raw := payload.Overrides
	if governance.Field == "pnpm.overrides" {
		raw = payload.PNPM.Overrides
	}
	if !nonemptyJSONObject(raw) {
		return unusedDependencyOverridePolicy(governance), true
	}
	return policy.Finding{}, false
}

func readPackageJSON(repo repository.Repository, path string) (packageJSON, bool) {
	data, err := repo.Read(path)
	if err != nil {
		return packageJSON{}, false
	}
	var payload packageJSON
	if json.Unmarshal(data, &payload) != nil {
		return packageJSON{}, false
	}
	return payload, true
}

func unusedDependencyOverridePolicy(governance policy.DependencyOverridePolicy) policy.Finding {
	return policy.Finding{
		Check: "policy.dependencyOverridePolicyUnused", Path: policy.ConfigFilename, Subject: governance.ID,
		Message: "dependency override policy does not own a non-empty override block at its exact path and field",
	}
}

func dependencyOverridePolicy(policies []policy.DependencyOverridePolicy, path, field string) (policy.DependencyOverridePolicy, bool) {
	for _, governance := range policies {
		if governance.Path == path && governance.Field == field {
			return governance, true
		}
	}
	return policy.DependencyOverridePolicy{}, false
}

func nonemptyJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && len(object) > 0
}

func canonicalJSONSHA256(raw json.RawMessage) (string, error) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("decode override block: %w", err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode canonical override block: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return fmt.Sprintf("%x", digest[:]), nil
}

func pnpmGovernanceFindings(ctx context.Context, repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, root := range pnpmLifecycleRoots(repo, files) {
		findings = append(findings, pnpmRootGovernanceFindings(ctx, repo, root)...)
	}
	return findings
}

func pnpmRootGovernanceFindings(ctx context.Context, repo repository.Repository, root string) []policy.Finding {
	owner := packageJSONPath(root)
	payload, ok := readPackageJSON(repo, owner)
	if !ok {
		return nil
	}

	workspace, unread := pnpmWorkspaceSettings(ctx, repo, root)
	if len(unread) > 0 {
		return unread
	}
	findings := lifecycleDeclarationFindings(owner, payload, workspace)
	manager, version, found := strings.Cut(payload.PackageManager, "@")
	if !found || manager != "pnpm" || !exactVersion.MatchString(version) || semverBefore(version, "10.16.0") {
		return findings
	}
	return append(findings, pnpmRootSecurityFindings(repo.Config, root, version, workspace)...)
}

func pnpmRootSecurityFindings(config policy.Config, root, version string, workspace pnpmWorkspace) []policy.Finding {
	if workspace.Path == "" {
		message := "pnpm 10.16+ requires one pnpm-workspace.yaml or pnpm-workspace.yml owner for native supply-chain settings"
		return []policy.Finding{{
			Check: "supplyChain.pnpmSecurity", Path: packageJSONPath(root), Subject: "pnpm-workspace", Message: message,
		}}
	}
	path, settings := workspace.Path, workspace.Settings
	findings := pnpmMinimumAgeFindings(config, root, path, version, settings)
	findings = append(findings, requiredPNPMSetting(path, settings, "trustPolicy", "no-downgrade", !semverBefore(version, "10.21.0"))...)
	findings = append(findings, requiredPNPMSetting(path, settings, "blockExoticSubdeps", "true", !semverBefore(version, "10.26.0"))...)
	findings = append(findings, requiredPNPMSetting(path, settings, "minimumReleaseAgeIgnoreMissingTime", "false", !semverBefore(version, "11.0.0"))...)
	findings = append(findings, requiredPNPMSetting(path, settings, "minimumReleaseAgeStrict", "true", !semverBefore(version, "11.0.0"))...)
	findings = append(findings, requiredPNPMSetting(path, settings, "trustLockfile", "false", !semverBefore(version, "11.3.0"))...)
	for _, forbidden := range []string{"trustPolicyExclude", "trustPolicyIgnoreAfter"} {
		if values, exists := settings[forbidden]; exists && len(values) > 0 {
			findings = append(findings, policy.Finding{
				Check: "supplyChain.pnpmSecurity", Path: path, Subject: forbidden,
				Message: forbidden + " weakens native trust verification and has no governed exception surface",
			})
		}
	}
	return findings
}

func pnpmMinimumAgeFindings(config policy.Config, root, path, version string, settings map[string][]string) []policy.Finding {
	minimum := settings["minimumReleaseAge"]
	want := config.SupplyChain.MinimumReleaseAgeDays * 24 * 60
	if len(minimum) != 1 {
		return []policy.Finding{{
			Check: "supplyChain.pnpmSecurity", Path: path, Subject: "minimumReleaseAge",
			Message: fmt.Sprintf("pnpm minimumReleaseAge must be at least %d minutes", want),
		}}
	}
	observed, err := strconv.Atoi(minimum[0])
	findings := []policy.Finding{}
	if err != nil || observed < want {
		findings = append(findings, policy.Finding{
			Check: "supplyChain.pnpmSecurity", Path: path, Subject: "minimumReleaseAge",
			Message: fmt.Sprintf("pnpm minimumReleaseAge must be at least %d minutes", want),
		})
	}
	exclusions, exists := settings["minimumReleaseAgeExclude"]
	if !exists {
		return findings
	}
	if semverBefore(version, "10.19.0") {
		return append(findings, policy.Finding{
			Check: "supplyChain.pnpmSecurity", Path: path, Subject: "minimumReleaseAgeExclude",
			Message: "this pnpm version cannot scope release-age exclusions to exact versions",
		})
	}
	lockScope := "pnpm-lock.yaml"
	if root != "." {
		lockScope = root + "/pnpm-lock.yaml"
	}
	for _, selector := range exclusions {
		name, selectedVersion, exact := exactPNPMExclusion(selector)
		if !exact || !hasReleaseAgeAssessment(config.SupplyChain.ReleaseAgeAssessments, "pnpm", name, selectedVersion, lockScope) {
			message := "minimumReleaseAgeExclude entries must select one exact package version with a matching releaseAgeAssessment for " + lockScope
			findings = append(findings, policy.Finding{Check: "supplyChain.pnpmSecurity", Path: path, Subject: selector, Message: message})
		}
	}
	return findings
}

func requiredPNPMSetting(path string, settings map[string][]string, name, want string, supported bool) []policy.Finding {
	if !supported {
		return nil
	}
	values := settings[name]
	if len(values) == 1 && values[0] == want {
		return nil
	}
	return []policy.Finding{{
		Check: "supplyChain.pnpmSecurity", Path: path, Subject: name,
		Message: fmt.Sprintf("pnpm %s must be set to %s", name, want),
	}}
}

func exactPNPMExclusion(selector string) (string, string, bool) {
	selector = strings.TrimSpace(selector)
	separator := strings.LastIndex(selector, "@")
	if separator <= 0 || separator == len(selector)-1 || strings.Contains(selector, "||") {
		return "", "", false
	}
	name, version := selector[:separator], selector[separator+1:]
	return name, version, exactVersion.MatchString(version)
}

func hasReleaseAgeAssessment(assessments []policy.ReleaseAgeAssessment, ecosystem, name, version, scope string) bool {
	for _, assessment := range assessments {
		if assessment.Ecosystem == ecosystem && assessment.Package == name && assessment.Version == version && assessment.Scope == scope {
			return true
		}
	}
	return false
}

func semverBefore(observed, required string) bool {
	left := semverNumbers(observed)
	right := semverNumbers(required)
	for index := range left {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}
	return false
}

func semverNumbers(version string) [3]int {
	version = strings.SplitN(version, "-", 2)[0]
	parts := strings.Split(version, ".")
	result := [3]int{}
	for index := 0; index < len(result) && index < len(parts); index++ {
		result[index], _ = strconv.Atoi(parts[index])
	}
	return result
}

func pnpmLifecycleRoots(repo repository.Repository, files []string) []string {
	roots := map[string]bool{}
	for _, path := range files {
		if filepath.Base(path) != "package.json" {
			continue
		}
		manifest, _ := checkPackageJSON(repo, path)
		if manifest.Manager == "pnpm" && len(manifest.Dependencies) > 0 {
			roots[manifest.Root] = true
		}
	}
	ordered := make([]string, 0, len(roots))
	for root := range roots {
		ordered = append(ordered, root)
	}
	sort.Strings(ordered)
	return ordered
}

func packageJSONPath(root string) string {
	if root == "." {
		return "package.json"
	}
	return root + "/package.json"
}

func lifecycleDeclarationFindings(owner string, payload packageJSON, workspace pnpmWorkspace) []policy.Finding {
	findings := []policy.Finding{}
	packageOnly := payload.PNPM.OnlyBuiltDependencies != nil
	packageIgnored := payload.PNPM.IgnoredBuiltDependencies != nil
	if packageOnly && packageIgnored {
		message := "pnpm.onlyBuiltDependencies and pnpm.ignoredBuiltDependencies are mutually exclusive lifecycle policies"
		findings = append(findings, lifecycleFinding(owner, "pnpm:ambiguous", message))
	}
	workspaceOnly := workspace.declares("onlyBuiltDependencies")
	workspaceIgnored := workspace.declares("ignoredBuiltDependencies")
	if workspaceOnly && workspaceIgnored {
		message := "top-level onlyBuiltDependencies and ignoredBuiltDependencies are mutually exclusive lifecycle policies"
		findings = append(findings, lifecycleFinding(workspace.Path, "pnpm:ambiguous", message))
	}
	packageDeclared := packageOnly || packageIgnored
	workspaceDeclared := workspaceOnly || workspaceIgnored
	if packageDeclared && workspaceDeclared {
		message := "dependency lifecycle policy must have one owner: package.json pnpm fields or pnpm-workspace.yaml top-level fields, not both"
		findings = append(findings, lifecycleFinding(workspace.Path, "pnpm:duplicate-owner", message))
	}
	if !packageDeclared && !workspaceDeclared {
		message := "pnpm lifecycle scripts must be governed by package.json pnpm.onlyBuiltDependencies/pnpm.ignoredBuiltDependencies or a top-level onlyBuiltDependencies/ignoredBuiltDependencies field in pnpm-workspace.yaml"
		findings = append(findings, lifecycleFinding(owner, "pnpm", message))
	}
	return findings
}

func lifecycleFinding(path, subject, message string) policy.Finding {
	return policy.Finding{Check: "supplyChain.lifecycleScripts", Path: path, Subject: subject, Message: message}
}

type pnpmWorkspace struct {
	Path     string
	Settings map[string][]string
}

func (workspace pnpmWorkspace) declares(setting string) bool {
	_, declared := workspace.Settings[setting]
	return declared
}

func pnpmWorkspaceSettings(ctx context.Context, repo repository.Repository, root string) (pnpmWorkspace, []policy.Finding) {
	candidates := pnpmWorkspaceCandidates(repo, root)
	if len(candidates) == 0 {
		return pnpmWorkspace{}, nil
	}
	if len(candidates) > 1 {
		message := "pnpm workspace settings have multiple possible owners; keep only pnpm-workspace.yaml or pnpm-workspace.yml"
		return pnpmWorkspace{}, []policy.Finding{lifecycleFinding(candidates[0], "pnpm:workspace-owner", message)}
	}
	path := candidates[0]
	settingsContext, cancel := context.WithTimeout(ctx, pnpmFactsBudget)
	defer cancel()
	result, err := javascript.Bundle{PolicyRoot: repo.PolicyRoot}.Workspace(settingsContext, repo.Root, []string{path})
	if err != nil {
		return pnpmWorkspace{}, []policy.Finding{{
			Check: "policy.tool", Path: path, Subject: "javascript-bundle", Message: err.Error(),
		}}
	}
	if len(result.Unsupported) > 0 {
		entry := result.Unsupported[0]
		return pnpmWorkspace{}, []policy.Finding{{
			Check: "supplyChain.pnpmSecurity", Path: entry.Path, Subject: "pnpm-workspace",
			Message: "the policy-owned pnpm reader could not read this file: " + entry.Reason,
		}}
	}
	if len(result.Files) != 1 {
		return pnpmWorkspace{}, []policy.Finding{{
			Check: "supplyChain.pnpmSecurity", Path: path, Subject: "pnpm-workspace",
			Message: "the policy-owned pnpm reader reported no settings for this file",
		}}
	}
	settings := map[string][]string{}
	for _, setting := range result.Files[0].Settings {
		settings[setting.Name] = setting.Values
	}
	return pnpmWorkspace{Path: path, Settings: settings}, nil
}

func pnpmWorkspaceCandidates(repo repository.Repository, root string) []string {
	candidates := []string{}
	for _, name := range []string{"pnpm-workspace.yaml", "pnpm-workspace.yml"} {
		path := name
		if root != "." {
			path = root + "/" + name
		}
		if regularFile(filepath.Join(repo.Root, filepath.FromSlash(path))) {
			candidates = append(candidates, path)
		}
	}
	return candidates
}

func nodeManagerVersion(repo repository.Repository, root string) string {
	payload, ok := readPackageJSON(repo, packageJSONPath(root))
	if !ok {
		return ""
	}
	_, version, _ := strings.Cut(payload.PackageManager, "@")
	return version
}
