package supplychain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	workflowfacts "github.com/riteofstring/code-polishy/internal/workflow"
)

var (
	exactVersion = regexp.MustCompile(`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	fullCommit   = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	fullDigest   = regexp.MustCompile(`^sha256:[0-9a-fA-F]{64}$`)
	goVersion    = regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
)

type nodeManifest struct {
	Path           string
	Root           string
	Manager        string
	ManagerVersion string
	Dependencies   map[string]string
}

type packageJSON struct {
	PackageManager       string            `json:"packageManager"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
	Overrides            json.RawMessage   `json:"overrides"`
	AuditConfig          nodeAuditConfig   `json:"auditConfig"`
	PNPM                 struct {
		OnlyBuiltDependencies    []string        `json:"onlyBuiltDependencies"`
		IgnoredBuiltDependencies []string        `json:"ignoredBuiltDependencies"`
		Overrides                json.RawMessage `json:"overrides"`
		AuditConfig              nodeAuditConfig `json:"auditConfig"`
	} `json:"pnpm"`
}

type nodeAuditConfig struct {
	IgnoreCVEs []string `json:"ignoreCves"`
}

func Static(ctx context.Context, repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, path := range files {
		findings = append(findings, staticPathFindings(repo, path)...)
	}
	findings = append(findings, pnpmGovernanceFindings(ctx, repo, files)...)
	findings = append(findings, pnpmProjectFindings(ctx, repo, files)...)
	return uniqueFindings(findings)
}

func staticPathFindings(repo repository.Repository, path string) []policy.Finding {
	name := filepath.Base(path)
	switch {
	case name == "package.json":
		_, findings := checkPackageJSON(repo, path)
		return findings
	case name == "go.mod":
		return checkGoModule(repo, path)
	case name == "go.work":
		return checkGoWorkspace(repo, path)
	case name == "pyproject.toml":
		return checkPythonProject(repo, path)
	case name == "osv-scanner.toml":
		return []policy.Finding{{
			Check: "supplyChain.auditIgnore", Path: path, Subject: "osv-scanner configuration",
			Message: "repository OSV configuration is forbidden because scanner ignores bypass governed vulnerability assessments",
		}}
	case strings.HasPrefix(path, ".github/workflows/") && (strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml")):
		return checkWorkflowPins(repo, path)
	case isDockerfile(path):
		return checkContainerPins(repo, path)
	default:
		return nil
	}
}

func CoverageFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, path := range files {
		ecosystem := dependencyEcosystem(repo, path)
		switch ecosystem {
		case "node":
			findings = append(findings, manifestCapabilityFindings(repo, files, path, ecosystem, nodeCapabilities(repo, path))...)
		case "node-yarn", "node-bun":
			findings = append(findings, manifestCapabilityFindings(repo, files, path, ecosystem, []string{"lock-sync", "release-age"})...)
		case "python-uv":
			findings = append(findings, manifestCapabilityFindings(repo, files, path, ecosystem, []string{"lock-sync"})...)
		case "unsupported":
			capabilities := []string{"dependency-policy", "lock-sync", "release-age", "security"}
			findings = append(findings, manifestCapabilityFindings(repo, files, path, ecosystem, capabilities)...)
		}
		if isDockerfile(path) {
			findings = append(findings, containerFileCoverageFindings(repo, path)...)
		}
	}
	findings = append(findings, containerCoverageFindings(repo.Config)...)
	findings = append(findings, customDependencyCoverageFindings(repo.Config)...)
	findings = append(findings, DependencyOverrideGovernanceFindings(repo, files)...)
	findings = append(findings, securityMonitoringFindings(repo, files)...)
	return uniqueFindings(findings)
}

func nodeCapabilities(repo repository.Repository, path string) []string {
	capabilities := []string{}
	if !pnpmGoverned(repo, path) {
		capabilities = append(capabilities, "lock-sync")
	}
	if nodeManagerName(repo, path) != "pnpm" {
		capabilities = append(capabilities, "security")
	}
	return capabilities
}

func dependencyEcosystem(repo repository.Repository, path string) string {
	name := filepath.Base(path)
	switch name {
	case "package.json":
		manager := nodeManagerName(repo, path)
		if manager == "yarn" || manager == "bun" {
			return "node-" + manager
		}
		return "node"
	case "pyproject.toml":
		if pythonProjectSupported(repo, path) {
			return "python-uv"
		}
		return "unsupported"
	case "go.mod", "go.work":
		return "go"
	case "Cargo.toml", "Pipfile", "pom.xml", "build.gradle", "build.gradle.kts", "Gemfile", "composer.json", "Package.swift", "pubspec.yaml":
		return "unsupported"
	default:
		if strings.HasPrefix(name, "requirements") && strings.HasSuffix(name, ".txt") {
			return "unsupported"
		}
		return ""
	}
}

func nodeManagerName(repo repository.Repository, path string) string {
	data, err := repo.Read(path)
	if err != nil {
		return ""
	}
	var payload packageJSON
	if json.Unmarshal(data, &payload) != nil {
		return ""
	}
	manager, _, _ := strings.Cut(payload.PackageManager, "@")
	if manager == "" {
		manager, _, _ = inheritedNodeManager(repo, path)
	}
	return manager
}

func manifestCapabilityFindings(repo repository.Repository, files []string, manifest, ecosystem string, capabilities []string) []policy.Finding {
	modules := manifestModules(repo, files, manifest, ecosystemLanguages(ecosystem, manifest))
	if len(modules) == 0 {
		modules = []string{"repository"}
	}
	findings := []policy.Finding{}
	for _, module := range modules {
		for _, capability := range capabilities {
			providerModule := module
			if providerModule == "repository" {
				providerModule = ""
			}
			if !hasSupplyProvider(repo.Config.Checks, providerModule, capability) {
				findings = append(findings, policy.Finding{Check: "policy.supplyChainCoverage", Path: manifest, Subject: module + ":" + capability, Message: fmt.Sprintf("dependency manifest needs a %s provider covering %s", capability, module)})
			}
		}
	}
	return findings
}

func manifestModules(repo repository.Repository, files []string, manifest string, languages []string) []string {
	directory := filepath.ToSlash(filepath.Dir(manifest))
	modules := []string{}
	for _, path := range files {
		outsideDirectory := directory != "." && path != directory && !strings.HasPrefix(path, directory+"/")
		if !repo.IsExecutableSource(path) || outsideDirectory || !slices.Contains(languages, repo.Language(path)) {
			continue
		}
		modules = append(modules, repo.ModuleNames(path)...)
	}
	sort.Strings(modules)
	return slices.Compact(modules)
}

func hasSupplyProvider(commands []policy.Command, module, capability string) bool {
	for _, command := range commands {
		moduleCovered := module == "" && len(command.Modules) == 0 || module != "" && (len(command.Modules) == 0 || slices.Contains(command.Modules, module))
		profileCovered := intersects(command.RunOn, supplyProfiles(capability))
		if moduleCovered && profileCovered && slices.Contains(command.Provides, capability) {
			return true
		}
	}
	return false
}

func supplyProfiles(capability string) []string {
	switch capability {
	case "dependency-policy", "lock-sync":
		return []string{"supply-chain"}
	case "release-age":
		return []string{"supply-chain-online"}
	case "security":
		return []string{"security"}
	default:
		return []string{"supply-chain", "supply-chain-online", "security"}
	}
}

func ecosystemLanguages(ecosystem, manifest string) []string {
	switch {
	case strings.HasPrefix(ecosystem, "node"):
		return []string{"typescript"}
	case ecosystem == "python-uv", filepath.Base(manifest) == "pyproject.toml", filepath.Base(manifest) == "Pipfile", strings.HasPrefix(filepath.Base(manifest), "requirements"):
		return []string{"python"}
	case filepath.Base(manifest) == "Cargo.toml":
		return []string{"rust"}
	case filepath.Base(manifest) == "pom.xml", strings.HasPrefix(filepath.Base(manifest), "build.gradle"):
		return []string{"jvm"}
	case filepath.Base(manifest) == "Gemfile":
		return []string{"ruby"}
	case filepath.Base(manifest) == "composer.json":
		return []string{"php"}
	case filepath.Base(manifest) == "Package.swift":
		return []string{"swift"}
	case filepath.Base(manifest) == "pubspec.yaml":
		return []string{"dart"}
	default:
		return nil
	}
}

func containerCoverageFindings(config policy.Config) []policy.Finding {
	findings := []policy.Finding{}
	if slices.Contains(config.Project.Capabilities, "container") && !containerSecurityCovered(config, "") {
		findings = append(findings, policy.Finding{Check: "policy.supplyChainCoverage", Path: policy.ConfigFilename, Subject: "container:security", Message: "container capability requires a repository-wide supply-chain security provider"})
	}
	for _, module := range config.Modules {
		if slices.Contains(module.Capabilities, "container") && !containerSecurityCovered(config, module.Name) {
			findings = append(findings, policy.Finding{Check: "policy.supplyChainCoverage", Path: policy.ConfigFilename, Subject: module.Name + ":container:security", Message: "container module requires a supply-chain security provider"})
		}
	}
	return findings
}

func containerSecurityCovered(config policy.Config, module string) bool {
	if len(config.SupplyChain.ArtifactSecurity.Targets) > 0 {
		return hasArtifactSecurityTarget(config, module)
	}
	return hasSupplyProvider(config.Checks, module, "security")
}

func containerFileCoverageFindings(repo repository.Repository, path string) []policy.Finding {
	if len(repo.Config.SupplyChain.ArtifactSecurity.Targets) > 0 {
		return firstClassContainerFileCoverageFindings(repo, path)
	}
	return providerContainerFileCoverageFindings(repo, path)
}

func firstClassContainerFileCoverageFindings(repo repository.Repository, path string) []policy.Finding {
	if artifactTargetBuildsDockerfile(repo.Config, path) {
		return nil
	}
	owners := repo.ModuleNames(path)
	if len(owners) == 1 && hasArtifactSecurityTarget(repo.Config, owners[0]) {
		return nil
	}
	if len(owners) == 0 && hasArtifactSecurityTarget(repo.Config, "") {
		return nil
	}
	message := "container definition requires a first-class artifact security target covering its module"
	return []policy.Finding{{Check: "policy.supplyChainCoverage", Path: path, Subject: "container:artifact-security", Message: message}}
}

func artifactTargetBuildsDockerfile(config policy.Config, path string) bool {
	for _, target := range config.SupplyChain.ArtifactSecurity.Targets {
		if target.Mode == "dockerfile" && target.Dockerfile == path {
			return true
		}
	}
	return false
}

func providerContainerFileCoverageFindings(repo repository.Repository, path string) []policy.Finding {
	if slices.Contains(repo.Config.Project.Capabilities, "container") {
		return nil
	}
	module, label, declared := containerFileOwner(repo, path)
	if declared || hasSupplyProvider(repo.Config.Checks, module, "security") {
		return nil
	}
	message := "container definition requires a supply-chain security provider covering " + label
	return []policy.Finding{{Check: "policy.supplyChainCoverage", Path: path, Subject: label + ":container:security", Message: message}}
}

func containerFileOwner(repo repository.Repository, path string) (string, string, bool) {
	owners := repo.ModuleNames(path)
	if len(owners) != 1 {
		return "", "repository", false
	}
	module := owners[0]
	for _, configured := range repo.Config.Modules {
		if configured.Name == module {
			return module, module, slices.Contains(configured.Capabilities, "container")
		}
	}
	return module, module, false
}

func hasArtifactSecurityTarget(config policy.Config, module string) bool {
	for _, target := range config.SupplyChain.ArtifactSecurity.Targets {
		if target.Module == module || module != "" && target.Module == "" {
			return true
		}
	}
	return false
}

func customDependencyCoverageFindings(config policy.Config) []policy.Finding {
	capabilities := []string{"dependency-policy", "lock-sync", "release-age", "security"}
	findings := []policy.Finding{}
	if slices.Contains(config.Project.Capabilities, "custom-dependencies") {
		findings = append(findings, supplyCapabilityFindings(config.Checks, "", "repository", capabilities)...)
	}
	for _, module := range config.Modules {
		if slices.Contains(module.Capabilities, "custom-dependencies") {
			findings = append(findings, supplyCapabilityFindings(config.Checks, module.Name, module.Name, capabilities)...)
		}
	}
	return findings
}

func supplyCapabilityFindings(commands []policy.Command, module, label string, capabilities []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, capability := range capabilities {
		if !hasSupplyProvider(commands, module, capability) {
			findings = append(findings, policy.Finding{Check: "policy.supplyChainCoverage", Path: policy.ConfigFilename, Subject: label + ":custom-dependencies:" + capability, Message: "custom dependency ecosystem requires a " + capability + " provider"})
		}
	}
	return findings
}

func requireOneJSONValue(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("the report contains more than one JSON value")
}

func uniqueFindings(findings []policy.Finding) []policy.Finding {
	seen := map[string]bool{}
	result := []policy.Finding{}
	for _, finding := range findings {
		key := finding.Check + "\x00" + finding.Path + "\x00" + finding.Subject + "\x00" + finding.Message
		if !seen[key] {
			seen[key] = true
			result = append(result, finding)
		}
	}
	return result
}

func ToolFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	goChecked := false
	for _, path := range files {
		if filepath.Base(path) == "go.mod" && !goChecked {
			goChecked = true
			if !isExecutable(repo.PolicyTool("govulncheck")) {
				findings = append(findings, policy.Finding{Check: "policy.tool", Path: "repository", Subject: "govulncheck", Message: "pinned govulncheck is unavailable; run ./tools/install-policy-tools.sh"})
				continue
			}
			if pin := repo.ToolPin("govulncheck"); pin == "" || runner.GoModuleVersion(repo.GoTool("go"), repo.PolicyTool("govulncheck")) != pin {
				findings = append(findings, policy.Finding{Check: "policy.tool", Path: "repository", Subject: "govulncheck", Message: "govulncheck does not match tools/govulncheck-version.txt"})
			}
		}
	}
	return findings
}

func checkPackageJSON(repo repository.Repository, path string) (nodeManifest, []policy.Finding) {
	data, err := repo.Read(path)
	if err != nil {
		return nodeManifest{}, []policy.Finding{{Check: "supplyChain.nodeManifest", Path: path, Subject: path, Message: err.Error()}}
	}
	var payload packageJSON
	if err := json.Unmarshal(data, &payload); err != nil {
		return nodeManifest{}, []policy.Finding{{Check: "supplyChain.nodeManifest", Path: path, Subject: path, Message: err.Error()}}
	}
	manager, managerRoot, managerFindings := nodeManagerFindings(repo, path, payload.PackageManager)
	dependencies, dependencyFindings := nodeDependencyFindings(repo, path, payload)
	findings := append(managerFindings, dependencyFindings...)
	findings = append(findings, peerDependencyFindings(path, payload)...)
	findings = append(findings, nodeManifestGovernanceFindings(repo, path, manager, payload)...)
	managerVersion := nodeManagerVersion(repo, managerRoot)
	return nodeManifest{Path: path, Root: managerRoot, Manager: manager, ManagerVersion: managerVersion, Dependencies: dependencies}, findings
}

func nodeManagerFindings(repo repository.Repository, path, packageManager string) (string, string, []policy.Finding) {
	if packageManager == "" {
		return inheritedNodeManager(repo, path)
	}
	return validateNodeManager(repo, path, packageManager)
}

func validateNodeManager(repo repository.Repository, ownerPath, packageManager string) (string, string, []policy.Finding) {
	manager, version, found := strings.Cut(packageManager, "@")
	if !found || !slices.Contains([]string{"npm", "pnpm", "yarn", "bun"}, manager) || !exactVersion.MatchString(version) {
		return manager, filepath.ToSlash(filepath.Dir(ownerPath)), []policy.Finding{{Check: "supplyChain.packageManager", Path: ownerPath, Subject: packageManager, Message: "packageManager must pin npm, pnpm, yarn, or bun to an exact semantic version"}}
	}
	directory := filepath.ToSlash(filepath.Dir(ownerPath))
	lockNames := map[string][]string{
		"npm": {"package-lock.json", "npm-shrinkwrap.json"}, "pnpm": {"pnpm-lock.yaml"},
		"yarn": {"yarn.lock"}, "bun": {"bun.lock", "bun.lockb"},
	}
	lockFound := false
	for _, name := range lockNames[manager] {
		candidate := name
		if directory != "." {
			candidate = directory + "/" + name
		}
		if info, statErr := os.Stat(filepath.Join(repo.Root, filepath.FromSlash(candidate))); statErr == nil && info.Mode().IsRegular() {
			lockFound = true
		}
	}
	if !lockFound {
		return manager, directory, []policy.Finding{{Check: "supplyChain.lockfile", Path: ownerPath, Subject: manager, Message: "the exact package manager lockfile is missing"}}
	}
	return manager, directory, nil
}

func inheritedNodeManager(repo repository.Repository, manifestPath string) (string, string, []policy.Finding) {
	directory := filepath.ToSlash(filepath.Dir(manifestPath))
	for directory != "." {
		directory = filepath.ToSlash(filepath.Dir(directory))
		candidate := "package.json"
		if directory != "." {
			candidate = directory + "/package.json"
		}
		data, err := os.ReadFile(filepath.Join(repo.Root, filepath.FromSlash(candidate)))
		if err == nil {
			var owner packageJSON
			if json.Unmarshal(data, &owner) == nil && owner.PackageManager != "" {
				return validateNodeManager(repo, candidate, owner.PackageManager)
			}
		}
	}
	return "", filepath.ToSlash(filepath.Dir(manifestPath)), []policy.Finding{{Check: "supplyChain.packageManager", Path: manifestPath, Subject: "missing", Message: "package must declare an exact packageManager or inherit one from a lockfile-owning ancestor"}}
}

func nodeDependencyFindings(repo repository.Repository, path string, payload packageJSON) (map[string]string, []policy.Finding) {
	allDependencies := map[string]string{}
	findings := []policy.Finding{}
	for group, dependencies := range map[string]map[string]string{
		"dependencies": payload.Dependencies, "devDependencies": payload.DevDependencies, "optionalDependencies": payload.OptionalDependencies,
	} {
		for name, version := range dependencies {
			allDependencies[name] = version
			if localProtocol(version, repo.Config.SupplyChain.AllowedDependencyProtocols) {
				if err := validateLocalDependency(repo, path, version); err != nil {
					findings = append(findings, policy.Finding{Check: "supplyChain.localDependency", Path: path, Subject: name, Message: err.Error()})
				}
				continue
			}
			if !exactVersion.MatchString(version) && !fullGitVersion(version) {
				findings = append(findings, policy.Finding{Check: "supplyChain.exactVersion", Path: path, Subject: name, Message: fmt.Sprintf("%s dependency %q must use an exact version or full commit", group, version)})
			}
		}
	}
	return allDependencies, findings
}

func peerDependencyFindings(path string, payload packageJSON) []policy.Finding {
	findings := []policy.Finding{}
	for name, peerRange := range payload.PeerDependencies {
		devVersion, exists := payload.DevDependencies[name]
		if !exists || !exactVersion.MatchString(devVersion) {
			findings = append(findings, policy.Finding{Check: "supplyChain.peerDependency", Path: path, Subject: name, Message: fmt.Sprintf("peer range %q needs a matching exact devDependency", peerRange)})
		}
	}
	return findings
}

func checkGoModule(repo repository.Repository, path string) []policy.Finding {
	data, err := repo.Read(path)
	if err != nil {
		return []policy.Finding{{Check: "supplyChain.goModule", Path: path, Subject: path, Message: err.Error()}}
	}
	findings := []policy.Finding{}
	hasRegistryRequirement := false
	block := ""
	for lineNumber, raw := range strings.Split(string(data), "\n") {
		registry, lineFindings := checkGoModuleLine(repo, path, lineNumber+1, raw, &block)
		hasRegistryRequirement = hasRegistryRequirement || registry
		findings = append(findings, lineFindings...)
	}
	if hasRegistryRequirement && !regularFile(filepath.Join(repo.Root, filepath.Dir(path), "go.sum")) {
		findings = append(findings, policy.Finding{Check: "supplyChain.lockfile", Path: path, Subject: "go.sum", Message: "go.sum is required when go.mod has registry dependencies"})
	}
	return findings
}

func checkGoModuleLine(repo repository.Repository, path string, lineNumber int, raw string, block *string) (bool, []policy.Finding) {
	line := strings.TrimSpace(strings.SplitN(raw, "//", 2)[0])
	if line == "require (" || line == "replace (" {
		*block = strings.TrimSuffix(line, " (")
		return false, nil
	}
	if line == ")" && *block != "" {
		*block = ""
		return false, nil
	}
	requirement := *block == "require" || strings.HasPrefix(line, "require ")
	requirementLine := strings.TrimSpace(strings.TrimPrefix(line, "require "))
	registry, findings := goRequirementFindings(path, lineNumber, requirementLine, requirement)
	replaceLine := line
	if *block == "replace" {
		replaceLine = "replace " + line
	}
	findings = append(findings, goReplaceFindings(repo, path, replaceLine)...)
	return registry, findings
}

func goRequirementFindings(path string, lineNumber int, line string, requirement bool) (bool, []policy.Finding) {
	fields := strings.Fields(line)
	if !requirement || len(fields) < 2 || strings.HasPrefix(fields[0], "./") || strings.HasPrefix(fields[0], "../") {
		return false, nil
	}
	if !goVersion.MatchString(fields[1]) {
		return true, []policy.Finding{{Check: "supplyChain.goVersion", Path: path, Subject: fields[0], Message: fmt.Sprintf("line %d module requirement must use an exact Go version", lineNumber)}}
	}
	return true, nil
}

func goReplaceFindings(repo repository.Repository, path, line string) []policy.Finding {
	if !strings.HasPrefix(line, "replace ") || !strings.Contains(line, "=>") {
		return nil
	}
	target := strings.TrimSpace(strings.SplitN(line, "=>", 2)[1])
	fields := strings.Fields(target)
	if len(fields) == 2 && goVersion.MatchString(fields[1]) {
		return nil
	}
	if len(fields) != 1 || !strings.HasPrefix(target, "./") && !strings.HasPrefix(target, "../") {
		return []policy.Finding{{Check: "supplyChain.goReplace", Path: path, Subject: target, Message: "external replacements must use an exact module version"}}
	}
	if err := validateRelativeFromManifest(repo, path, target); err != nil {
		return []policy.Finding{{Check: "supplyChain.goReplace", Path: path, Subject: target, Message: err.Error()}}
	}
	return nil
}

func checkGoWorkspace(repo repository.Repository, path string) []policy.Finding {
	data, err := repo.Read(path)
	if err != nil {
		return []policy.Finding{{Check: "supplyChain.goWorkspace", Path: path, Subject: path, Message: err.Error()}}
	}
	findings := []policy.Finding{}
	block := ""
	for lineNumber, raw := range strings.Split(string(data), "\n") {
		findings = append(findings, goWorkspaceLineFindings(repo, path, lineNumber+1, raw, &block)...)
	}
	return findings
}

func goWorkspaceLineFindings(repo repository.Repository, path string, lineNumber int, raw string, block *string) []policy.Finding {
	line := strings.TrimSpace(strings.SplitN(raw, "//", 2)[0])
	if line == "use (" || line == "replace (" {
		*block = strings.TrimSuffix(line, " (")
		return nil
	}
	if line == ")" && *block != "" {
		*block = ""
		return nil
	}
	if *block == "replace" {
		return goReplaceFindings(repo, path, "replace "+line)
	}
	if strings.HasPrefix(line, "replace ") {
		return goReplaceFindings(repo, path, line)
	}
	if *block != "use" && !strings.HasPrefix(line, "use ") {
		return nil
	}
	target := strings.TrimSpace(strings.TrimPrefix(line, "use "))
	if target == "" {
		return nil
	}
	if err := validateRelativeFromManifest(repo, path, target); err != nil {
		message := fmt.Sprintf("line %d workspace module path is invalid: %v", lineNumber, err)
		return []policy.Finding{{Check: "supplyChain.goWorkspace", Path: path, Subject: target, Message: message}}
	}
	return nil
}

func checkPythonProject(repo repository.Repository, path string) []policy.Finding {
	project, err := repo.ReadPythonProject(path)
	if err != nil {
		return []policy.Finding{{Check: "supplyChain.pythonManifest", Path: path, Subject: path, Message: err.Error()}}
	}
	if !project.IsPythonProject() {
		return nil
	}
	findings := pythonDependencyFindings(repo, path, project.Requirements)
	lockPath := pythonLockPath(project.Root)
	if !regularFile(filepath.Join(repo.Root, filepath.FromSlash(lockPath))) {
		return append(findings, policy.Finding{Check: "supplyChain.lockfile", Path: path, Subject: lockPath, Message: "uv.lock is required for Python projects"})
	}
	data, err := repo.Read(lockPath)
	if err != nil {
		return append(findings, policy.Finding{Check: "supplyChain.lockConsistency", Path: lockPath, Subject: "uv:resolved-graph", Message: err.Error()})
	}
	packages, err := parseUVLock(data, lockPath)
	if err != nil {
		return append(findings, policy.Finding{Check: "supplyChain.lockConsistency", Path: lockPath, Subject: "uv:resolved-graph", Message: err.Error()})
	}
	return append(findings, pythonGitLockFindings(project, packages, lockPath)...)
}

func pythonLockPath(root string) string {
	if root == "." || root == "" {
		return "uv.lock"
	}
	return root + "/uv.lock"
}

func pythonDependencyFindings(repo repository.Repository, path string, dependencies []repository.PythonRequirement) []policy.Finding {
	findings := []policy.Finding{}
	for _, dependency := range dependencies {
		switch dependency.Kind {
		case repository.PythonRegistryRequirement:
			if _, exact := dependency.ExactRegistryVersion(); exact {
				continue
			}
			findings = append(findings, policy.Finding{
				Check: "supplyChain.pythonExactVersion", Path: path, Subject: dependency.Name,
				Message: fmt.Sprintf("Python dependency %q must use == with an exact version", dependency.Raw),
			})
		case repository.PythonGitRequirement:
			continue
		case repository.PythonFileRequirement:
			if !slices.Contains(repo.Config.SupplyChain.AllowedDependencyProtocols, "file:") {
				findings = append(findings, policy.Finding{Check: "supplyChain.pythonExactVersion", Path: path, Subject: dependency.Name, Message: "Python file dependencies are not allowed by policy"})
			} else if err := validateRelativeFromManifest(repo, path, dependency.FilePath); err != nil {
				findings = append(findings, policy.Finding{Check: "supplyChain.localDependency", Path: path, Subject: dependency.Name, Message: err.Error()})
			}
		default:
			findings = append(findings, policy.Finding{
				Check: "supplyChain.pythonSource", Path: path, Subject: dependency.Name,
				Message: fmt.Sprintf("Python dependency %q uses an unsupported direct URL", dependency.Raw),
			})
		}
	}
	return findings
}

func pythonProjectSupported(repo repository.Repository, path string) bool {
	project, err := repo.ReadPythonProject(path)
	return err == nil && project.IsPythonProject()
}

func pythonGitLockFindings(project repository.PythonProject, packages []resolvedPackage, lockPath string) []policy.Finding {
	findings := []policy.Finding{}
	for _, requirement := range project.Requirements {
		if requirement.Kind != repository.PythonGitRequirement || requirement.Usage == "build" {
			continue
		}
		finding := pythonGitLockFinding(requirement, matchingResolvedPackages(packages, requirement.Name), lockPath)
		if finding != nil {
			findings = append(findings, *finding)
		}
	}
	return findings
}

func matchingResolvedPackages(packages []resolvedPackage, name string) []resolvedPackage {
	matches := []resolvedPackage{}
	for _, item := range packages {
		if item.Name == name {
			matches = append(matches, item)
		}
	}
	return matches
}

func pythonGitLockFinding(requirement repository.PythonRequirement, candidates []resolvedPackage, lockPath string) *policy.Finding {
	message := pythonGitLockMessage(requirement, candidates)
	if message == "" {
		return nil
	}
	return &policy.Finding{Check: "supplyChain.lockConsistency", Path: lockPath, Subject: requirement.Name, Message: message}
}

func pythonGitLockMessage(requirement repository.PythonRequirement, candidates []resolvedPackage) string {
	if len(candidates) != 1 {
		return fmt.Sprintf("uv.lock must contain exactly one source record for Git dependency %q", requirement.Name)
	}
	locked := candidates[0]
	if locked.Source.Kind != "git" {
		return fmt.Sprintf("uv.lock does not retain usable Git source evidence for dependency %q", requirement.Name)
	}
	if !requirement.Git.SameRepository(locked.Source.Git) {
		return fmt.Sprintf("uv.lock Git repository for dependency %q differs from pyproject.toml", requirement.Name)
	}
	if requirement.Git.DeclaredRef != locked.Source.Git.DeclaredRef {
		return fmt.Sprintf("uv.lock Git declared ref for dependency %q differs from pyproject.toml", requirement.Name)
	}
	if requirement.Git.Commit != locked.Source.Git.Commit {
		return fmt.Sprintf("uv.lock Git commit for dependency %q differs from pyproject.toml", requirement.Name)
	}
	if requirement.Git.Subdirectory != locked.Source.Git.Subdirectory {
		return fmt.Sprintf("uv.lock Git subdirectory for dependency %q differs from pyproject.toml", requirement.Name)
	}
	return ""
}

func checkWorkflowPins(repo repository.Repository, path string) []policy.Finding {
	facts, err := workflowfacts.Read(path, repo.Read)
	if err != nil {
		return []policy.Finding{{Check: "supplyChain.workflow", Path: path, Subject: path, Message: err.Error()}}
	}
	findings := []policy.Finding{}
	for _, job := range facts.Jobs {
		if job.Uses != "" {
			findings = append(findings, workflowUseFindings(path, job.Uses, job.Line)...)
		}
		for _, step := range job.Steps {
			if step.Uses != "" {
				findings = append(findings, workflowUseFindings(path, step.Uses, step.Line)...)
			}
		}
	}
	return findings
}

func workflowUseFindings(path, value string, line int) []policy.Finding {
	if strings.HasPrefix(value, "./") {
		return nil
	}
	if strings.HasPrefix(value, "docker://") {
		digest := ""
		if _, after, found := strings.Cut(value, "@"); found {
			digest = after
		}
		if !fullDigest.MatchString(digest) {
			return []policy.Finding{{Check: "supplyChain.workflowPin", Path: path, Subject: value, Message: fmt.Sprintf("line %d Docker action must use a full sha256 digest", line)}}
		}
		return nil
	}
	_, reference, found := strings.Cut(value, "@")
	if !found || !fullCommit.MatchString(reference) {
		return []policy.Finding{{Check: "supplyChain.workflowPin", Path: path, Subject: value, Message: fmt.Sprintf("line %d GitHub Action must use a full 40-character commit", line)}}
	}
	return nil
}

func checkContainerPins(repo repository.Repository, path string) []policy.Finding {
	data, err := repo.Read(path)
	if err != nil {
		return nil
	}
	findings := []policy.Finding{}
	stages := map[string]bool{}
	for lineNumber, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(strings.ToUpper(line), "FROM ") {
			continue
		}
		image, alias := dockerBase(line)
		if image == "" {
			continue
		}
		if image == "scratch" || stages[strings.ToLower(image)] {
			stages[strings.ToLower(alias)] = alias != ""
			continue
		}
		_, digest, found := strings.Cut(image, "@")
		if strings.Contains(image, "${") {
			findings = append(findings, policy.Finding{Check: "supplyChain.containerPin", Path: path, Subject: image, Message: fmt.Sprintf("line %d dynamic base images are forbidden; pin the digest directly", lineNumber+1)})
		} else if !found || !fullDigest.MatchString(digest) {
			findings = append(findings, policy.Finding{Check: "supplyChain.containerPin", Path: path, Subject: image, Message: fmt.Sprintf("line %d base image must use a full sha256 digest", lineNumber+1)})
		}
		if alias != "" {
			stages[strings.ToLower(alias)] = true
		}
	}
	return findings
}

func dockerBase(line string) (string, string) {
	fields := strings.Fields(line)
	for index, field := range fields[1:] {
		if !strings.HasPrefix(field, "--") {
			alias := ""
			imageIndex := index + 1
			if imageIndex+2 < len(fields) && strings.EqualFold(fields[imageIndex+1], "AS") {
				alias = fields[imageIndex+2]
			}
			return field, alias
		}
	}
	return "", ""
}

func isDockerfile(path string) bool {
	name := filepath.Base(path)
	return name == "Dockerfile" || strings.HasPrefix(name, "Dockerfile.") || name == "Containerfile" || strings.HasPrefix(name, "Containerfile.")
}

func IsGovernedInput(path string) bool {
	name := filepath.Base(path)
	if strings.HasPrefix(path, ".github/workflows/") || isDockerfile(path) {
		return true
	}
	if supplyLockfileName(name) || strings.HasPrefix(name, "requirements") && strings.HasSuffix(name, ".txt") {
		return true
	}
	return slices.Contains([]string{
		"package.json", "go.mod", "go.sum", "go.work", "go.work.sum", "pyproject.toml", "Cargo.toml",
		"Pipfile", "pom.xml", "build.gradle", "build.gradle.kts", "Gemfile",
		"composer.json", "Package.swift", "Package.resolved", "pubspec.yaml",
	}, name)
}

func supplyLockfileName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".lock") || strings.Contains(lower, "-lock.") || slices.Contains([]string{"pnpm-lock.yaml", "bun.lockb"}, name)
}

func parsePythonDependencies(text string) []string {
	project, err := repository.ParsePythonProject("pyproject.toml", []byte(text))
	if err != nil {
		return nil
	}
	dependencies := make([]string, 0, len(project.Requirements))
	for _, requirement := range project.Requirements {
		dependencies = append(dependencies, requirement.Raw)
	}
	return dependencies
}

func localProtocol(version string, allowed []string) bool {
	for _, protocol := range allowed {
		if strings.HasPrefix(version, protocol) {
			return true
		}
	}
	return false
}

func validateLocalDependency(repo repository.Repository, manifest, version string) error {
	_, relative, found := strings.Cut(version, ":")
	if !found || relative == "" || relative == "*" || strings.HasPrefix(version, "workspace:") && !strings.Contains(relative, "/") {
		return nil
	}
	return validateRelativeFromManifest(repo, manifest, relative)
}

func validateRelativeFromManifest(repo repository.Repository, manifest, relative string) error {
	if relative == "" || filepath.IsAbs(filepath.FromSlash(relative)) || strings.Contains(relative, "%") {
		return fmt.Errorf("local dependency must use an unescaped relative path: %s", relative)
	}
	resolvedRoot, candidate, err := localDependencyCandidate(repo.Root, manifest, relative)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return fmt.Errorf("local dependency path is missing or unresolved: %s", relative)
	}
	if !containedRelative(resolvedRoot, resolved) {
		return fmt.Errorf("local dependency symlink escapes the repository: %s", relative)
	}
	return nil
}

func localDependencyCandidate(root, manifest, relative string) (string, string, error) {
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	if rootErr != nil {
		return "", "", fmt.Errorf("repository root is unresolved: %w", rootErr)
	}
	directory := filepath.Dir(filepath.Join(resolvedRoot, filepath.FromSlash(manifest)))
	candidate := filepath.Clean(filepath.Join(directory, filepath.FromSlash(relative)))
	if !containedRelative(resolvedRoot, candidate) {
		return "", "", fmt.Errorf("local dependency escapes the repository: %s", relative)
	}
	return resolvedRoot, candidate, nil
}

func containedRelative(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func fullGitVersion(version string) bool {
	if !strings.HasPrefix(version, "git+") {
		return false
	}
	_, fragment, found := strings.Cut(version, "#")
	return found && fullCommit.MatchString(fragment)
}

func intersects(left, right []string) bool {
	for _, value := range left {
		if slices.Contains(right, value) {
			return true
		}
	}
	return false
}

func safeName(value string) string {
	value = strings.Trim(value, "./")
	value = strings.NewReplacer("/", "-", "\\", "-", ".", "-").Replace(value)
	if value == "" {
		return "root"
	}
	return value
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
