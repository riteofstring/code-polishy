package quality

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

var lengthCheckedExtensions = map[string]bool{
	".bash": true, ".c": true, ".cc": true, ".cpp": true, ".css": true,
	".go": true, ".h": true, ".hpp": true, ".html": true, ".java": true,
	".js": true, ".json": true, ".jsonc": true, ".jsx": true, ".kt": true, ".dart": true,
	".kts": true, ".mjs": true, ".mts": true, ".php": true,
	".proto": true, ".py": true, ".pyi": true, ".rb": true, ".rs": true,
	".sh": true, ".sql": true, ".svelte": true, ".swift": true, ".toml": true,
	".ts": true, ".tsx": true, ".vue": true, ".yaml": true, ".yml": true,
}

func Check(ctx context.Context, repo repository.Repository, selection repository.Selection, commandRunner runner.Runner, profile string) []policy.Finding {
	findings := sourceChecks(repo, selection.Files)
	findings = append(findings, DataSyntaxFindings(ctx, repo, selection.Files)...)
	findings = append(findings, goToolChecks(ctx, repo, selection.Files, commandRunner)...)
	findings = append(findings, shellChecks(ctx, repo, selection.Files, commandRunner)...)
	findings = append(findings, JavaScriptFormatFindings(ctx, repo, selection.Files)...)
	findings = append(findings, JavaScriptLintFindings(ctx, repo, selection.Files)...)
	findings = append(findings, JavaScriptTypeCheckFindings(ctx, repo, selection.Files)...)
	findings = append(findings, JavaScriptDeadCodeFindings(ctx, repo, selection.Files)...)
	findings = append(findings, PythonQualityFindings(ctx, repo, selection.Files, commandRunner)...)
	profiles := []string{profile}
	if profile == "gate" {
		profiles = append(profiles, "check")
	}
	findings = append(findings, RunCommandsForProfiles(ctx, repo, selection, commandRunner, profiles...)...)
	return findings
}

func CheckCommands(repo repository.Repository, selection repository.Selection, profile string) []policy.Command {
	commands := []policy.Command{}
	goFiles := languageFiles(repo, selection.Files, "go")
	if len(goFiles) > 0 {
		if allFiles, err := repo.AllFiles(); err == nil {
			packages, _ := goPackageInventory(repo, goFiles, repo.GoModules(allFiles))
			goCommands, _ := goPackageToolCommands(repo, packages)
			commands = append(commands, goCommands...)
		}
	}
	shellCommands, _ := shellToolCommands(repo, selection.Files)
	commands = append(commands, shellCommands...)
	commands = append(commands, pythonQualityCommands(repo, selection.Files)...)
	profiles := []string{profile}
	if profile == "gate" {
		profiles = append(profiles, "check")
	}
	return append(commands, CommandsForProfiles(repo, selection, profiles...)...)
}

func Format(ctx context.Context, repo repository.Repository, selection repository.Selection, commandRunner runner.Runner) []policy.Finding {
	if repo.ClassifyDocumentationCandidate(selection).Ordinary {
		return DocumentationFormatWrite(ctx, repo, selection.Candidate.AddedOrModified)
	}
	findings := []policy.Finding{}
	goFiles := editableFiles(repo, languageFiles(repo, selection.Files, "go"))
	if len(goFiles) > 0 {
		arguments := []string{"-w"}
		for _, path := range goFiles {
			arguments = append(arguments, filepath.Join(repo.Root, filepath.FromSlash(path)))
		}
		command := policy.Command{Name: "gofmt-write", Argv: append([]string{repo.GoTool("gofmt")}, arguments...), Cwd: ".", TimeoutSeconds: 300}
		if err := commandRunner.Run(ctx, repo.Root, command); err != nil {
			findings = append(findings, commandFinding("quality.format", command.Name, err))
		}
	}
	findings = append(findings, JavaScriptFormatWrite(ctx, repo, selection.Files)...)
	findings = append(findings, RunCommands(ctx, repo, formatSelection(repo, selection), commandRunner, "format")...)
	return findings
}

func RunCommands(ctx context.Context, repo repository.Repository, selection repository.Selection, commandRunner runner.Runner, profile string) []policy.Finding {
	return RunCommandsForProfiles(ctx, repo, selection, commandRunner, profile)
}

func RunCommandsForProfiles(ctx context.Context, repo repository.Repository, selection repository.Selection, commandRunner runner.Runner, profiles ...string) []policy.Finding {
	commands, findings := configuredCommandsForProfiles(repo, selection, profiles...)
	for _, command := range commands {
		if command.Adapter != nil {
			profile := profiles[0]
			for _, candidate := range profiles {
				if slices.Contains(command.RunOn, candidate) {
					profile = candidate
					break
				}
			}
			findings = append(findings, pack.RunAdapter(ctx, repo, selection, command, commandRunner, profile)...)
			continue
		}
		if err := commandRunner.Run(ctx, repo.Root, command); err != nil {
			findings = append(findings, configuredCommandFailure(command, err))
		}
	}
	return findings
}

func CommandsForProfiles(repo repository.Repository, selection repository.Selection, profiles ...string) []policy.Command {
	commands, _ := configuredCommandsForProfiles(repo, selection, profiles...)
	return commands
}

func configuredCommandsForProfiles(repo repository.Repository, selection repository.Selection, profiles ...string) ([]policy.Command, []policy.Finding) {
	commands := []policy.Command{}
	findings := []policy.Finding{}
	for _, command := range repo.Config.Checks {
		if !sliceIntersects(command.RunOn, profiles) || !commandApplies(repo, command, selection) {
			continue
		}
		if finding := generatedStyleExecutionFinding(repo, command); finding != nil {
			findings = append(findings, *finding)
			continue
		}
		command, runnable := prepareCommand(repo, command, selection, profiles...)
		if runnable {
			commands = append(commands, command)
		}
	}
	return commands, findings
}

func NamedCommands(repo repository.Repository, names []string) ([]policy.Command, error) {
	files, err := repo.AllFiles()
	if err != nil {
		return nil, err
	}
	selection := repository.Selection{Files: files, All: true}
	seen := map[string]bool{}
	commands := make([]policy.Command, 0, len(names))
	for _, name := range names {
		if seen[name] {
			return nil, fmt.Errorf("configured check %q was required more than once", name)
		}
		seen[name] = true
		index := slices.IndexFunc(repo.Config.Checks, func(command policy.Command) bool {
			return command.Name == name
		})
		if index < 0 {
			return nil, fmt.Errorf("unknown configured check %q", name)
		}
		if problem := generatedStyleCommandProblem(repo, repo.Config.Checks[index], files); problem != "" {
			return nil, fmt.Errorf("%s", problem)
		}
		command, runnable := prepareCommand(repo, repo.Config.Checks[index], selection, repo.Config.Checks[index].RunOn...)
		if !runnable {
			return nil, fmt.Errorf("configured check %q has no selected files to execute", name)
		}
		commands = append(commands, command)
	}
	return commands, nil
}

func RunNamedCommands(ctx context.Context, repo repository.Repository, commandRunner runner.Runner, names []string) ([]policy.Finding, error) {
	commands, err := NamedCommands(repo, names)
	if err != nil {
		return nil, err
	}
	files, err := repo.AllFiles()
	if err != nil {
		return nil, err
	}
	if inventory := repo.InspectGeneration(files); len(inventory.Findings) > 0 {
		return inventory.Findings, nil
	}
	findings := []policy.Finding{}
	for _, command := range commands {
		if command.Adapter != nil {
			findings = append(findings, pack.RunAdapter(ctx, repo, repository.Selection{Files: files, All: true}, command, commandRunner, command.RunOn[0])...)
			continue
		}
		if err := commandRunner.Run(ctx, repo.Root, command); err != nil {
			findings = append(findings, configuredCommandFailure(command, err))
		}
	}
	return findings, nil
}

func prepareCommand(repo repository.Repository, command policy.Command, selection repository.Selection, profiles ...string) (policy.Command, bool) {
	if !command.PassFiles {
		return command, true
	}
	selected := selectedCommandPaths(repo, command, selection.Files, profiles...)
	if len(selected) == 0 && len(command.PassFilePaths) > 0 && commandApplies(repo, command, selection) {
		selected = commandFileArguments(command.Cwd, commandEligiblePaths(repo, command, command.PassFilePaths, profiles...))
	}
	if len(selected) == 0 {
		return command, false
	}
	command.Argv = append(append([]string{}, command.Argv...), selected...)
	return command, true
}

func selectedCommandPaths(repo repository.Repository, command policy.Command, files []string, profiles ...string) []string {
	allowed := map[string]bool{}
	for _, path := range command.PassFilePaths {
		allowed[path] = true
	}
	selected := []string{}
	for _, path := range files {
		if len(allowed) > 0 {
			if allowed[path] && commandEligiblePath(repo, command, path, profiles...) {
				selected = append(selected, commandFileArgument(command.Cwd, path))
			}
			continue
		}
		if pathMatchesCommand(repo, path, command) && commandEligiblePath(repo, command, path, profiles...) {
			selected = append(selected, commandFileArgument(command.Cwd, path))
		}
	}
	return selected
}

func commandEligiblePaths(repo repository.Repository, command policy.Command, paths []string, profiles ...string) []string {
	selected := []string{}
	for _, path := range paths {
		if commandEligiblePath(repo, command, path, profiles...) {
			selected = append(selected, path)
		}
	}
	return selected
}

func commandEligiblePath(repo repository.Repository, command policy.Command, path string, profiles ...string) bool {
	if repo.IsData(path) && slices.Contains(command.Provides, "format") {
		return false
	}
	if !repo.IsGenerated(path) {
		return true
	}
	return !styleOnlyCommand(command)
}

func formatSelection(repo repository.Repository, selection repository.Selection) repository.Selection {
	filtered := selection
	filtered.Files = []string{}
	for _, path := range selection.Files {
		if !repo.IsData(path) {
			filtered.Files = append(filtered.Files, path)
		}
	}
	filtered.Candidate.AddedOrModified = []string{}
	for _, path := range selection.Candidate.AddedOrModified {
		if !repo.IsData(path) {
			filtered.Candidate.AddedOrModified = append(filtered.Candidate.AddedOrModified, path)
		}
	}
	return filtered
}

func commandFileArguments(cwd string, paths []string) []string {
	arguments := make([]string, 0, len(paths))
	for _, path := range paths {
		arguments = append(arguments, commandFileArgument(cwd, path))
	}
	return arguments
}

func commandFileArgument(cwd, path string) string {
	if cwd == "" || cwd == "." {
		return path
	}
	relative, err := filepath.Rel(filepath.FromSlash(cwd), filepath.FromSlash(path))
	if err != nil {
		return path
	}
	return filepath.ToSlash(relative)
}

func CoverageFindings(repo repository.Repository, files []string) []policy.Finding {
	languagesByModule, findings := inventoryModuleLanguages(repo, files)
	styleLanguages, _ := inventoryModuleLanguages(repo, editableFiles(repo, files))
	findings = append(findings, repo.GeneratedJavaScriptOwnershipFindings(files)...)
	findings = append(findings, repo.InspectGeneration(files).Findings...)
	findings = append(findings, generatedStyleCoverageFindings(repo, files)...)
	findings = append(findings, sourceCommentCoverageFindings(repo, files)...)
	findings = append(findings, customLanguageRuleFindings(repo, files)...)
	findings = append(findings, adapterCoverageFindings(repo.Config, languagesByModule, styleLanguages)...)
	findings = append(findings, builtInProviderFindings(repo, files)...)
	findings = append(findings, buildCoverageFindings(repo.Config, languagesByModule)...)
	findings = append(findings, pack.CoverageFindings(repo, files)...)
	return findings
}

func customLanguageRuleFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, rule := range repo.Config.Scope.Languages {
		matched := false
		for _, path := range files {
			if slices.Contains(repo.Languages(path), rule.Name) {
				matched = true
				break
			}
		}
		if !matched {
			message := fmt.Sprintf("custom language %q does not match any governed non-built-in source", rule.Name)
			findings = append(findings, policy.Finding{Check: "policy.languageCoverage", Path: policy.ConfigFilename, Subject: rule.Name, Message: message})
		}
	}
	return findings
}

func inventoryModuleLanguages(repo repository.Repository, files []string) (map[string]map[string]bool, []policy.Finding) {
	languagesByModule := map[string]map[string]bool{}
	findings := []policy.Finding{}
	for _, module := range repo.Config.Modules {
		languagesByModule[module.Name] = map[string]bool{}
	}
	for _, path := range files {
		if !repo.IsExecutableSource(path) {
			continue
		}
		languages := repo.Languages(path)
		if len(languages) > 1 {
			findings = append(findings, policy.Finding{Check: "policy.languageCoverage", Path: path, Subject: strings.Join(languages, ","), Message: "executable source matches more than one custom language rule"})
			continue
		}
		owners := repo.OwnerModuleNames(path)
		if len(owners) == 0 {
			findings = append(findings, policy.Finding{Check: "policy.moduleCoverage", Path: path, Subject: "unowned", Message: "executable source does not belong to a module"})
			continue
		}
		if len(owners) > 1 {
			findings = append(findings, policy.Finding{Check: "policy.moduleCoverage", Path: path, Subject: strings.Join(owners, ","), Message: "executable source belongs to more than one module"})
			continue
		}
		languagesByModule[owners[0]][languages[0]] = true
	}
	return languagesByModule, findings
}

func adapterCoverageFindings(config policy.Config, languagesByModule, styleLanguages map[string]map[string]bool) []policy.Finding {
	findings := []policy.Finding{}
	for module, languages := range languagesByModule {
		for language := range languages {
			for _, capability := range requiredCapabilities(language) {
				if (capability == "format" || capability == "complexity") && !styleLanguages[module][language] {
					continue
				}
				if builtInCapability(language, capability) {
					continue
				}
				missing := missingProviderProfiles(config.Checks, module, capability, "check", "gate")
				if len(missing) > 0 {
					message := fmt.Sprintf("%s module %q has no %s provider for %s", language, module, capability, strings.Join(missing, " and "))
					findings = append(findings, policy.Finding{Check: "policy.checkCoverage", Path: policy.ConfigFilename, Subject: module + ":" + capability, Message: message})
				}
			}
		}
	}
	return findings
}

func builtInProviderFindings(repo repository.Repository, files []string) []policy.Finding {
	formatted := map[string]bool{}
	if bearsJavaScript(repo, files) {
		for _, path := range javascriptFormatFiles(repo, files) {
			formatted[path] = true
		}
	}
	findings := []policy.Finding{}
	for _, command := range repo.Config.Checks {

		if command.Managed {
			continue
		}
		for _, capability := range command.Provides {
			if !slices.Contains(builtInCapabilities, capability) {
				continue
			}
			covered, owned := builtInCoveredSource(repo, files, formatted, command, capability)
			if !owned {
				continue
			}
			message := fmt.Sprintf("Code Polishy decides %s for the source this check covers, including %s", capability, covered)
			findings = append(findings, policy.Finding{
				Check: "policy.builtInCapability", Path: policy.ConfigFilename,
				Subject: command.Name + ":" + capability, Message: message,
			})
		}
	}
	return findings
}

func builtInCoveredSource(repo repository.Repository, files []string, formatted map[string]bool, command policy.Command, capability string) (string, bool) {
	example := ""
	for _, path := range files {
		if !commandCovers(repo, command, path) {
			continue
		}
		if (capability == "format" || capability == "complexity") && repo.IsGenerated(path) {
			continue
		}
		language := repo.Language(path)

		if language != "" && !builtInCapability(language, capability) {
			return "", false
		}

		if !builtInDecides(formatted, path, language, capability) {
			continue
		}
		if example == "" || path < example {
			example = path
		}
	}
	return example, example != ""
}

func builtInDecides(formatted map[string]bool, path, language, capability string) bool {
	if language != "" && language != "typescript" {
		return true
	}
	if capability == "format" {
		return formatted[path]
	}
	return language == "typescript" && javascriptSourceExtensions[strings.ToLower(filepath.Ext(path))]
}

func commandCovers(repo repository.Repository, command policy.Command, path string) bool {
	if len(command.Paths) == 0 && len(command.Modules) == 0 {
		return true
	}
	return pathMatchesCommand(repo, path, command)
}

func buildCoverageFindings(config policy.Config, languagesByModule map[string]map[string]bool) []policy.Finding {
	if !requiresBuild(config) {
		return nil
	}
	findings := []policy.Finding{}
	for _, module := range config.Modules {
		if len(languagesByModule[module.Name]) > 0 && !hasProvider(config.Checks, module.Name, "build", "build") {
			findings = append(findings, policy.Finding{Check: "policy.checkCoverage", Path: policy.ConfigFilename, Subject: module.Name + ":build", Message: "code-bearing module has no build provider"})
		}
	}
	return findings
}

func ToolFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	languages := map[string]bool{}
	for _, path := range files {
		languages[repo.Language(path)] = true
	}
	if languages["go"] {
		findings = append(findings, goToolFindings(repo)...)
	}
	if languages["shell"] {
		findings = append(findings, shellToolFindings(repo)...)
	}
	return findings
}

func goToolFindings(repo repository.Repository) []policy.Finding {
	findings := []policy.Finding{}
	goTool := repo.GoTool("go")
	if !isExecutable(goTool) {
		findings = append(findings, toolFinding("go", "pinned Go toolchain is unavailable; run ./tools/install-policy-tools.sh"))
	} else if !requiredGoVersion(repo, goTool) {
		findings = append(findings, toolFinding("go", "Go toolchain does not match scripts/go_version.txt"))
	}
	if !isExecutable(repo.GoTool("gofmt")) {
		findings = append(findings, toolFinding("gofmt", "pinned gofmt is unavailable; run ./tools/install-policy-tools.sh"))
	}
	if !isExecutable(repo.PolicyTool("staticcheck")) {
		return append(findings, toolFinding("staticcheck", "pinned staticcheck is unavailable; run ./tools/install-policy-tools.sh"))
	}
	if pin := repo.ToolPin("staticcheck"); pin == "" || runner.GoModuleVersion(repo.GoTool("go"), repo.PolicyTool("staticcheck")) != pin {
		findings = append(findings, toolFinding("staticcheck", "staticcheck does not match tools/staticcheck-version.txt"))
	}
	return findings
}

func shellToolFindings(repo repository.Repository) []policy.Finding {
	shellcheck := filepath.Join(repo.PolicyRoot, "tools", "shellcheck.sh")
	if !isExecutable(shellcheck) {
		return []policy.Finding{toolFinding("shellcheck", "pinned ShellCheck wrapper is unavailable")}
	}
	if !requiredShellCheckVersion(repo, shellcheck) {
		return []policy.Finding{toolFinding("shellcheck", "ShellCheck does not match tools/shellcheck-version.txt")}
	}
	return nil
}

func requiredGoVersion(repo repository.Repository, goTool string) bool {
	pin := repo.ToolPin("go")
	if pin == "" {
		return false
	}
	output, err := safeOutput(goTool, "version")
	fields := strings.Fields(string(output))
	return err == nil && len(fields) >= 3 && fields[2] == "go"+strings.TrimPrefix(pin, "go")
}

func requiredShellCheckVersion(repo repository.Repository, shellcheck string) bool {
	pin := repo.ToolPin("shellcheck")
	if pin == "" {
		return false
	}
	output, err := safeOutput(shellcheck, "--version")
	return err == nil && strings.Contains(string(output), "version: "+pin)
}

func sourceChecks(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, path := range files {
		if repo.IsGenerated(path) || repo.IsData(path) {
			continue
		}
		findings = append(findings, checkTextFile(repo, path)...)
	}
	findings = append(findings, sourceCommentFindings(repo, files)...)
	return findings
}

func checkTextFile(repo repository.Repository, path string) []policy.Finding {
	data, err := repo.Read(path)
	if err != nil {
		return []policy.Finding{{Check: "quality.path", Path: path, Subject: path, Message: err.Error()}}
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return nil
	}
	findings := newlineFindings(path, data)
	if !checksFileLength(repo, path) || isLengthInput(path) {
		return findings
	}
	lineCount := bytes.Count(data, []byte("\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lineCount++
	}
	limit := repo.Config.Quality.MaxFileLines
	if repo.IsTest(path) {
		limit = repo.Config.Quality.MaxTestFileLines
	}
	if lineCount > limit {
		findings = append(findings, policy.Finding{Check: "quality.fileLength", Path: path, Subject: strconv.Itoa(lineCount), Message: fmt.Sprintf("file has %d lines; maximum is %d", lineCount, limit)})
	}
	return findings
}

func checksFileLength(repo repository.Repository, path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	if extension == ".md" || extension == ".markdown" {
		return false
	}
	return lengthCheckedExtensions[extension] || repo.IsExecutableSource(path)
}

func newlineFindings(path string, data []byte) []policy.Finding {
	findings := []policy.Finding{}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		findings = append(findings, policy.Finding{Check: "quality.finalNewline", Path: path, Subject: "EOF", Message: "text file must end with a newline"})
	}
	for lineIndex, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimRight(line, " \t\r")) != len(bytes.TrimRight(line, "\r")) {
			findings = append(findings, policy.Finding{Check: "quality.trailingWhitespace", Path: path, Subject: strconv.Itoa(lineIndex + 1), Message: fmt.Sprintf("line %d has trailing whitespace", lineIndex+1)})
		}
	}
	return findings
}

func goToolChecks(ctx context.Context, repo repository.Repository, files []string, commandRunner runner.Runner) []policy.Finding {
	goFiles := languageFiles(repo, files, "go")
	if len(goFiles) == 0 {
		return nil
	}
	findings := goFormatFindings(ctx, repo, editableFiles(repo, goFiles))
	for _, path := range goFiles {
		if !repo.IsGenerated(path) && !repo.IsData(path) {
			findings = append(findings, goComplexityFindings(repo, path)...)
		}
	}
	allFiles, err := repo.AllFiles()
	if err != nil {
		return append(findings, policy.Finding{Check: "quality.go", Path: "repository", Subject: "inventory", Message: err.Error()})
	}
	modules := repo.GoModules(allFiles)
	packages, packageFindings := goPackageInventory(repo, goFiles, modules)
	findings = append(findings, packageFindings...)
	findings = append(findings, runGoPackageTools(ctx, repo, packages, commandRunner)...)
	return findings
}

func goPackageInventory(repo repository.Repository, goFiles []string, modules []repository.GoModule) (map[string]map[string]bool, []policy.Finding) {
	packages := map[string]map[string]bool{}
	findings := []policy.Finding{}
	for _, path := range goFiles {
		module, found := repo.OwningGoModule(path, modules)
		if !found {
			findings = append(findings, policy.Finding{Check: "quality.goModule", Path: path, Subject: path, Message: "Go source is not owned by a go.mod"})
			continue
		}
		directory := filepath.ToSlash(filepath.Dir(path))
		relative := directory
		if module.Root != "." {
			relative = strings.TrimPrefix(strings.TrimPrefix(directory, module.Root), "/")
		}
		if relative == "." || relative == "" {
			relative = "."
		} else {
			relative = "./" + relative
		}
		if packages[module.Root] == nil {
			packages[module.Root] = map[string]bool{}
		}
		packages[module.Root][relative] = true
	}
	return packages, findings
}

func runGoPackageTools(ctx context.Context, repo repository.Repository, packages map[string]map[string]bool, commandRunner runner.Runner) []policy.Finding {
	commands, findings := goPackageToolCommands(repo, packages)
	for _, command := range commands {
		if err := commandRunner.Run(ctx, repo.Root, command); err != nil {
			check := "quality.goVet"
			if strings.HasPrefix(command.Name, "staticcheck-") {
				check = "quality.deadCode"
			}
			findings = append(findings, commandFinding(check, command.Name, err))
		}
	}
	return findings
}

func goPackageToolCommands(repo repository.Repository, packages map[string]map[string]bool) ([]policy.Command, []policy.Finding) {
	commands := []policy.Command{}
	findings := []policy.Finding{}
	moduleRoots := map[string]bool{}
	for moduleRoot := range packages {
		moduleRoots[moduleRoot] = true
	}
	for _, moduleRoot := range sortedKeys(moduleRoots) {
		packageNames := sortedKeys(packages[moduleRoot])
		arguments := append([]string{"vet"}, packageNames...)
		commands = append(commands, policy.Command{
			Name: "go-vet-" + safeName(moduleRoot), Argv: append([]string{repo.GoTool("go")}, arguments...), Cwd: moduleRoot, TimeoutSeconds: 900,
		})
		staticcheck := repo.PolicyTool("staticcheck")
		if isExecutable(staticcheck) {
			commands = append(commands, policy.Command{
				Name: "staticcheck-" + safeName(moduleRoot), Argv: append([]string{staticcheck}, packageNames...), Cwd: moduleRoot, TimeoutSeconds: 900,
			})
		} else {
			findings = append(findings, toolFinding("staticcheck", "pinned staticcheck is unavailable; run ./tools/install-policy-tools.sh"))
		}
	}
	return commands, findings
}

func goFormatFindings(ctx context.Context, repo repository.Repository, files []string) []policy.Finding {
	if len(files) == 0 {
		return nil
	}
	arguments := []string{"-l"}
	for _, path := range files {
		arguments = append(arguments, filepath.Join(repo.Root, filepath.FromSlash(path)))
	}
	command := exec.CommandContext(ctx, repo.GoTool("gofmt"), arguments...)
	command.Env = runner.Environment(nil)
	output, err := command.Output()
	if err != nil {
		return []policy.Finding{{Check: "quality.gofmt", Path: "repository", Subject: "gofmt", Message: err.Error()}}
	}
	findings := []policy.Finding{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		relative, relErr := filepath.Rel(repo.Root, line)
		if relErr != nil {
			relative = line
		}
		findings = append(findings, policy.Finding{Check: "quality.gofmt", Path: filepath.ToSlash(relative), Subject: "format", Message: "file is not gofmt formatted"})
	}
	return findings
}

func goComplexityFindings(repo repository.Repository, path string) []policy.Finding {
	data, err := repo.Read(path)
	if err != nil {
		return []policy.Finding{{Check: "quality.goSyntax", Path: path, Subject: path, Message: err.Error()}}
	}
	set := token.NewFileSet()
	parsed, err := parser.ParseFile(set, path, data, parser.AllErrors)
	if err != nil {
		return []policy.Finding{{Check: "quality.goSyntax", Path: path, Subject: path, Message: err.Error()}}
	}
	limit := repo.Config.Quality.Complexity.Go
	if repo.IsTest(path) {
		limit = repo.Config.Quality.Complexity.GoTest
	}
	findings := []policy.Finding{}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		complexity := cyclomaticComplexity(function.Body)
		if complexity >= limit {
			position := set.Position(function.Pos())
			findings = append(findings, policy.Finding{Check: "quality.goComplexity", Path: path, Subject: function.Name.Name, Message: fmt.Sprintf("line %d function %s has cyclomatic complexity %d; must be below %d", position.Line, function.Name.Name, complexity, limit)})
		}
	}
	return findings
}

func cyclomaticComplexity(body *ast.BlockStmt) int {
	complexity := 1
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			complexity++
		case *ast.CaseClause:
			if len(typed.List) > 0 {
				complexity++
			}
		case *ast.CommClause:
			if typed.Comm != nil {
				complexity++
			}
		case *ast.BinaryExpr:
			if typed.Op == token.LAND || typed.Op == token.LOR {
				complexity++
			}
		}
		return true
	})
	return complexity
}

func shellChecks(ctx context.Context, repo repository.Repository, files []string, commandRunner runner.Runner) []policy.Finding {
	commands, findings := shellToolCommands(repo, files)
	for _, command := range commands {
		if err := commandRunner.Run(ctx, repo.Root, command); err != nil {
			if strings.HasPrefix(command.Name, "shell-syntax-") {
				path := command.Paths[0]
				findings = append(findings, policy.Finding{Check: "quality.shellSyntax", Path: path, Subject: path, Message: err.Error()})
			} else {
				findings = append(findings, commandFinding("quality.shellcheck", command.Name, err))
			}
		}
	}
	return findings
}

func shellToolCommands(repo repository.Repository, files []string) ([]policy.Command, []policy.Finding) {
	shellFiles := languageFiles(repo, files, "shell")
	if len(shellFiles) == 0 {
		return nil, nil
	}
	commands := make([]policy.Command, 0, len(shellFiles)+1)
	for _, path := range shellFiles {
		commands = append(commands, policy.Command{Name: "shell-syntax-" + safeName(path), Argv: []string{"bash", "-n", path}, Cwd: ".", Paths: []string{path}, TimeoutSeconds: 60})
	}
	wrapper := filepath.Join(repo.PolicyRoot, "tools", "shellcheck.sh")
	if !isExecutable(wrapper) {
		return commands, []policy.Finding{toolFinding("shellcheck", "pinned ShellCheck is unavailable; run ./tools/install-policy-tools.sh")}
	}

	arguments := []string{wrapper, "--external-sources"}
	for _, path := range shellFiles {
		arguments = append(arguments, filepath.Join(repo.Root, filepath.FromSlash(path)))
	}
	return append(commands, policy.Command{Name: "shellcheck", Argv: arguments, Cwd: ".", TimeoutSeconds: 300}), nil
}

func commandApplies(repo repository.Repository, command policy.Command, selection repository.Selection) bool {
	if len(command.Paths) == 0 && len(command.Modules) == 0 {
		return selection.All
	}
	paths := append(append([]string{}, selection.Files...), selection.Candidate.Deleted...)
	for _, path := range paths {
		if pathMatchesCommand(repo, path, command) {
			return true
		}
	}
	return selection.All && (len(command.Paths) > 0 || len(command.Modules) > 0)
}

func pathMatchesCommand(repo repository.Repository, path string, command policy.Command) bool {
	if len(command.Paths) > 0 {
		return policy.MatchesAny(path, command.Paths)
	}
	for _, module := range repo.OwnerModuleNames(path) {
		if slices.Contains(command.Modules, module) {
			return true
		}
	}
	return false
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func hasProvider(commands []policy.Command, module, capability string, profiles ...string) bool {
	for _, command := range commands {
		if slices.Contains(command.Provides, capability) && slices.Contains(command.Modules, module) && sliceIntersects(command.RunOn, profiles) {
			return true
		}
	}
	return false
}

func missingProviderProfiles(commands []policy.Command, module, capability string, profiles ...string) []string {
	missing := []string{}
	for _, profile := range profiles {
		if !hasProvider(commands, module, capability, profile) {
			missing = append(missing, profile)
		}
	}
	return missing
}

func sliceIntersects(left, right []string) bool {
	for _, value := range left {
		if slices.Contains(right, value) {
			return true
		}
	}
	return false
}

func requiredCapabilities(language string) []string {
	switch language {
	case "go":
		return []string{"format", "lint", "typecheck", "complexity", "dead-code", "architecture"}
	case "shell":
		return []string{"format", "lint", "typecheck"}
	default:
		return []string{"format", "lint", "typecheck", "complexity", "dead-code", "architecture"}
	}
}

var builtInCapabilities = []string{"format", "lint", "typecheck", "complexity", "dead-code", "architecture"}

func builtInCapability(language, capability string) bool {
	switch language {
	case "go", "typescript", "python":
		return slices.Contains(builtInCapabilities, capability)
	case "shell":
		return slices.Contains([]string{"format", "lint", "typecheck"}, capability)
	}
	return false
}

func requiresBuild(config policy.Config) bool {
	if slices.Contains([]string{"application", "library", "tooling"}, config.Project.Kind) {
		return true
	}
	for _, capability := range config.Project.Capabilities {
		if slices.Contains([]string{"backend", "frontend", "ui", "cli"}, capability) {
			return true
		}
	}
	return false
}

func languageFiles(repo repository.Repository, files []string, language string) []string {
	selected := []string{}
	for _, path := range files {
		if repo.Language(path) == language {
			selected = append(selected, path)
		}
	}
	return selected
}

func editableFiles(repo repository.Repository, files []string) []string {
	selected := []string{}
	for _, path := range files {
		if !repo.IsGenerated(path) && !repo.IsData(path) {
			selected = append(selected, path)
		}
	}
	return selected
}

func commandFinding(check, name string, err error) policy.Finding {
	return policy.Finding{Check: check, Path: "repository", Subject: name, Message: err.Error()}
}

func configuredCommandFailure(command policy.Command, err error) policy.Finding {
	if slices.Contains(command.Provides, "security") || slices.Contains(command.Provides, "security-monitoring") {
		return policy.Finding{Check: "policy.securityProvider", Path: "repository", Subject: command.Name, Message: err.Error()}
	}
	return commandFinding("quality.command", command.Name, err)
}

func toolFinding(tool, message string) policy.Finding {
	return policy.Finding{Check: "policy.tool", Path: "repository", Subject: tool, Message: message}
}

func safeName(value string) string {
	value = strings.Trim(value, "./")
	value = strings.NewReplacer("/", "-", "\\", "-", ".", "-").Replace(value)
	if value == "" {
		return "root"
	}
	return value
}

func isLengthInput(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	return name == "go.sum" || strings.Contains(name, "lock") || name == "license"
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

func safeOutput(name string, arguments ...string) ([]byte, error) {
	command := exec.Command(name, arguments...)
	command.Env = runner.Environment(nil)
	return command.CombinedOutput()
}
