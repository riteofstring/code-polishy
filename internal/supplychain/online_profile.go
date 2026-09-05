package supplychain

import (
	"context"
	"path/filepath"
	"slices"
	"time"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func Online(ctx context.Context, repo repository.Repository, files []string, commandRunner runner.Runner) []policy.Finding {
	commands, err := OnlineCommands(repo, files)
	if err != nil {
		return []policy.Finding{{
			Check: "policy.securityScanner", Path: "repository", Subject: "online command plan", Message: err.Error(),
		}}
	}
	return OnlineWithCommands(ctx, repo, files, commands, commandRunner)
}

func OnlineCommands(repo repository.Repository, files []string) ([]policy.Command, error) {
	commands := []policy.Command{}
	for _, module := range repo.GoModules(files) {
		commands = append(commands, goOnlineCommands(repo, module)...)
	}
	auditCommands, err := pnpmAuditCommands(repo, files)
	if err != nil {
		return nil, err
	}
	commands = append(commands, auditCommands...)
	securityCommands, err := osvCommands(repo)
	if err != nil {
		return nil, err
	}
	commands = append(commands, securityCommands...)
	return commands, nil
}

func OnlineWithCommands(ctx context.Context, repo repository.Repository, files []string, commands []policy.Command, commandRunner runner.Runner) []policy.Finding {
	expected, err := OnlineCommands(repo, files)
	if err != nil {
		return []policy.Finding{{
			Check: "policy.securityScanner", Path: "repository", Subject: "online command plan", Message: err.Error(),
		}}
	}
	if !sameOnlineCommandPlan(expected, commands) {
		return []policy.Finding{{Check: "policy.supplyChain", Path: "repository", Subject: "online command plan", Message: "online supply-chain command plan differs from policy-derived commands"}}
	}
	findings := releaseArtifactAgeFindings(ctx, repo, registryClient(), time.Now().UTC())
	findings, commandIndex := onlineGoModuleFindings(ctx, repo, files, commands, commandRunner, findings)
	findings, commandIndex = onlineNodeFindings(ctx, repo, files, commands, commandRunner, findings, commandIndex)
	findings = append(findings, onlinePythonFindings(ctx, repo)...)
	if activePolicyModule(repo.Config, "osv") {
		findings = append(findings, scanOSVWithCommands(ctx, repo, commands[commandIndex:], commandRunner)...)
		commandIndex = len(commands)
	}
	if commandIndex != len(commands) {
		return append(findings, policy.Finding{Check: "policy.supplyChain", Path: "repository", Subject: "online command plan", Message: "online supply-chain command plan has unexpected commands"})
	}
	return findings
}

func onlineGoModuleFindings(ctx context.Context, repo repository.Repository, files []string, commands []policy.Command, commandRunner runner.Runner, findings []policy.Finding) ([]policy.Finding, int) {
	commandIndex := 0
	for _, module := range repo.GoModules(files) {
		moduleCommands := goOnlineCommands(repo, module)
		planned := commands[commandIndex : commandIndex+len(moduleCommands)]
		commandIndex += len(moduleCommands)
		findings = append(findings, checkGoOnlineWithCommands(ctx, repo, module, planned, commandRunner)...)
	}
	return findings, commandIndex
}

func onlineNodeFindings(ctx context.Context, repo repository.Repository, files []string, commands []policy.Command, commandRunner runner.Runner, findings []policy.Finding, commandIndex int) ([]policy.Finding, int) {
	for _, manifest := range uniqueAuditableNodeManifests(validNodeManifests(repo, files)) {
		findings = append(findings, checkResolvedNodeReleaseAge(ctx, repo, manifest)...)
		if manifest.Manager == "pnpm" {
			findings = append(findings, auditNodeWithCommand(ctx, repo, manifest, commands[commandIndex], commandRunner)...)
			commandIndex++
		}
	}
	return findings, commandIndex
}

func onlinePythonFindings(ctx context.Context, repo repository.Repository) []policy.Finding {
	inputs, err := onlineUVInputs(repo)
	if err != nil {
		return []policy.Finding{{Check: "supplyChain.releaseAgeCoverage", Path: "repository", Subject: "uv:resolved-graph", Message: err.Error()}}
	}
	findings := []policy.Finding{}
	for _, input := range inputs {
		findings = append(findings, resolvedPythonFindings(ctx, repo, input.Scope, input.Packages, time.Now().UTC())...)
	}
	return findings
}

func pnpmAuditCommands(repo repository.Repository, files []string) ([]policy.Command, error) {
	commands := []policy.Command{}
	for _, manifest := range uniqueAuditableNodeManifests(validNodeManifests(repo, files)) {
		if manifest.Manager != "pnpm" {
			continue
		}
		invocation, err := (javascript.Bundle{PolicyRoot: repo.PolicyRoot}).AuditCommand(repo.Root, manifest.Root)
		if err != nil {
			return nil, err
		}
		command := policy.Command{
			Name: "pnpm-audit-" + safeName(manifest.Root), Argv: append([]string{}, invocation.Argv...), Cwd: invocation.Cwd,
			SealedEnvironment: invocation.SealedEnvironment, TimeoutSeconds: invocation.TimeoutSeconds,
		}
		commands = append(commands, command)
	}
	return commands, nil
}

func sameOnlineCommandPlan(expected, actual []policy.Command) bool {
	if len(expected) != len(actual) {
		return false
	}
	for index := range expected {
		if !sameOnlineCommand(expected[index], actual[index]) {
			return false
		}
	}
	return true
}

func sameOnlineCommand(left, right policy.Command) bool {
	return sameOnlineCommandIdentity(left, right) && sameOnlineCommandCollections(left, right)
}

func sameOnlineCommandIdentity(left, right policy.Command) bool {
	return left.Name == right.Name && left.Cwd == right.Cwd && left.TimeoutSeconds == right.TimeoutSeconds &&
		left.Managed == right.Managed && left.PassFiles == right.PassFiles
}

func sameOnlineCommandCollections(left, right policy.Command) bool {
	return slices.Equal(left.Argv, right.Argv) && slices.Equal(left.Provides, right.Provides) &&
		slices.Equal(left.Paths, right.Paths) && slices.Equal(left.Modules, right.Modules) &&
		slices.Equal(left.RunOn, right.RunOn) && slices.Equal(left.Environment, right.Environment) &&
		slices.Equal(left.ExclusiveResources, right.ExclusiveResources) && slices.Equal(left.PassFilePaths, right.PassFilePaths)
}

func activePolicyModule(config policy.Config, name string) bool {
	for _, module := range config.ActivePolicyModules {
		if module.Name == name {
			return true
		}
	}
	return false
}

func validNodeManifests(repo repository.Repository, files []string) []nodeManifest {
	manifests := []nodeManifest{}
	for _, path := range files {
		if filepath.Base(path) != "package.json" {
			continue
		}
		manifest, _ := checkPackageJSON(repo, path)
		_, lockFound := nodeLockPath(repo, manifest.Root, manifest.Manager)
		if slices.Contains([]string{"npm", "pnpm"}, manifest.Manager) && exactVersion.MatchString(manifest.ManagerVersion) && lockFound {
			manifests = append(manifests, manifest)
		}
	}
	return manifests
}

func uniqueAuditableNodeManifests(manifests []nodeManifest) []nodeManifest {
	result := []nodeManifest{}
	seen := map[string]bool{}
	for _, manifest := range manifests {
		key := manifest.Manager + "\x00" + manifest.Root
		auditable := manifest.Manager == "npm" || manifest.Manager == "pnpm"
		if auditable && !seen[key] {
			seen[key] = true
			result = append(result, manifest)
		}
	}
	return result
}
