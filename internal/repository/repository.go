package repository

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type Repository struct {
	Root                 string
	PolicyRoot           string
	Config               policy.Config
	DynamicControlInputs []string
	pythonProjectCache   *pythonProjectInventoryCache
}

const DesignDocumentationCheck = "policy.designDocumentation"

type Selection struct {
	Base            string
	Candidate       CandidateDelta
	Files           []string
	All             bool
	PolicySensitive bool
}

type CandidateDelta struct {
	AddedOrModified []string
	Deleted         []string
}

func (candidate CandidateDelta) Paths() []string {
	return uniqueSorted(append(append([]string{}, candidate.AddedOrModified...), candidate.Deleted...))
}

type GoModule struct {
	Root     string
	Manifest string
	Name     string
}

type GovernedTool struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Version string `json:"version"`
}

type GovernedEnvironment struct {
	PathEntries []string       `json:"path_entries"`
	Tools       []GovernedTool `json:"tools"`
}

func Open(root, policyRoot string, config policy.Config) (Repository, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Repository{}, fmt.Errorf("resolve repository root: %w", err)
	}
	resolvedPolicy, err := filepath.EvalSymlinks(policyRoot)
	if err != nil {
		return Repository{}, fmt.Errorf("resolve policy root: %w", err)
	}
	if !isDirectory(resolvedRoot) {
		return Repository{}, fmt.Errorf("repository root is not a directory: %s", resolvedRoot)
	}
	return Repository{Root: resolvedRoot, PolicyRoot: resolvedPolicy, Config: config}, nil
}

func (repo Repository) Select(mode string, explicit []string) (Selection, error) {
	var selection Selection
	var err error
	switch mode {
	case "all":
		var files []string
		files, err = repo.AllFiles()
		selection = Selection{Candidate: CandidateDelta{AddedOrModified: append([]string{}, files...)}, Files: files, All: true}
	case "files":
		if len(explicit) == 0 {
			return Selection{}, errors.New("--files needs at least one path")
		}
		files := []string{}
		for _, path := range explicit {
			normalized, err := repo.NormalizePath(path)
			if err != nil {
				return Selection{}, err
			}
			if !isRegularFile(filepath.Join(repo.Root, filepath.FromSlash(normalized))) {
				return Selection{}, fmt.Errorf("explicit path is missing or not a regular file: %s", normalized)
			}
			if repo.IsExcluded(normalized) {
				return Selection{}, fmt.Errorf("explicit path is outside governed scope: %s", normalized)
			}
			files = append(files, normalized)
		}
		files = uniqueSorted(files)
		selection = Selection{Candidate: CandidateDelta{AddedOrModified: append([]string{}, files...)}, Files: files}
	case "staged":
		selection, err = repo.stagedSelection()
	case "changes", "":
		selection, err = repo.changedSelection()
	default:
		return Selection{}, fmt.Errorf("unknown selection mode %q", mode)
	}
	return repo.validatedSelection(selection, err)
}

func (repo Repository) validatedSelection(selection Selection, err error) (Selection, error) {
	if err != nil {
		return selection, err
	}
	if err := repo.validateDataFiles(selection.Files); err != nil {
		return Selection{}, err
	}
	return selection, nil
}

func (repo Repository) RawFiles() ([]string, error) {
	if repo.hasGit() {
		return repo.rawGitFiles()
	}
	return repo.rawWalkFiles()
}

func (repo Repository) rawGitFiles() ([]string, error) {
	tracked, err := repo.gitLines("ls-files", "-z")
	if err != nil {
		return nil, err
	}
	untracked, err := repo.gitLines("ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, path := range append(tracked, untracked...) {
		if !policy.MatchesAny(path, policy.DefaultExcludes) && isRegularOrSymlink(filepath.Join(repo.Root, filepath.FromSlash(path))) {
			result = append(result, path)
		}
	}
	return uniqueSorted(result), nil
}

func (repo Repository) rawWalkFiles() ([]string, error) {
	result := []string{}
	err := filepath.WalkDir(repo.Root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, relErr := filepath.Rel(repo.Root, path)
		if relErr != nil {
			return relErr
		}
		normalized := filepath.ToSlash(relative)
		if entry.IsDir() {
			if normalized != "." && policy.MatchesAny(normalized+"/placeholder", policy.DefaultExcludes) {
				return filepath.SkipDir
			}
			return nil
		}
		if isRegularOrSymlink(path) && !policy.MatchesAny(normalized, policy.DefaultExcludes) {
			result = append(result, normalized)
		}
		return nil
	})
	return uniqueSorted(result), err
}

func (repo Repository) changedSelection() (Selection, error) {
	if !repo.hasGit() || repo.git("rev-parse", "--verify", "HEAD") != nil {
		files, err := repo.AllFiles()
		return Selection{Candidate: CandidateDelta{AddedOrModified: append([]string{}, files...)}, Files: files, All: true}, err
	}
	head, err := repo.gitLines("rev-parse", "--verify", "HEAD")
	if err != nil || len(head) != 1 || !exactRevision(head[0]) {
		return Selection{}, errors.New("resolve current HEAD: expected one exact commit")
	}
	return repo.selectionSince(head[0])
}

func (repo Repository) SelectBase(base string) (Selection, error) {
	mergeBase, err := repo.MergeBase(base)
	if err != nil {
		return Selection{}, err
	}
	selection, err := repo.selectionSince(mergeBase)
	return repo.validatedSelection(selection, err)
}

func (repo Repository) MergeBase(base string) (string, error) {
	if !repo.hasGit() || repo.git("rev-parse", "--verify", "HEAD") != nil {
		return "", errors.New("--base requires a Git repository with at least one commit")
	}
	if base == "" || strings.TrimSpace(base) != base || strings.HasPrefix(base, "-") || strings.ContainsAny(base, " \t\r\n\x00") {
		return "", fmt.Errorf("invalid base reference %q", base)
	}
	mergeBases, err := repo.gitLines("merge-base", base, "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve merge base for %q: %w", base, err)
	}
	if len(mergeBases) != 1 || !exactRevision(mergeBases[0]) {
		return "", fmt.Errorf("resolve merge base for %q: expected one exact commit", base)
	}
	return mergeBases[0], nil
}

func (repo Repository) FilesAt(revision string) ([]string, error) {
	if !exactRevision(revision) {
		return nil, errors.New("revision must be an exact Git object ID")
	}
	files, err := repo.gitLines("ls-tree", "-r", "-z", "--name-only", revision)
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, path := range files {
		if !repo.IsExcluded(path) {
			result = append(result, path)
		}
	}
	return result, nil
}

func (repo Repository) ReadAt(revision, path string) ([]byte, error) {
	if !exactRevision(revision) {
		return nil, errors.New("revision must be an exact Git object ID")
	}
	normalized, err := repo.NormalizePath(path)
	if err != nil {
		return nil, err
	}
	command := exec.Command("git", "-C", repo.Root, "cat-file", "blob", revision+":"+normalized)
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("read %s at %s: %w", normalized, revision, err)
	}
	return output, nil
}

func (repo Repository) ReadRegularFileAt(revision, path string) ([]byte, bool, error) {
	if !exactRevision(revision) {
		return nil, false, errors.New("revision must be an exact Git object ID")
	}
	normalized, err := repo.NormalizePath(path)
	if err != nil {
		return nil, false, err
	}
	entries, err := repo.gitLines("ls-tree", "-z", revision, "--", ":(literal)"+normalized)
	if err != nil {
		return nil, false, fmt.Errorf("inspect %s at %s: %w", normalized, revision, err)
	}
	if len(entries) == 0 {
		return nil, false, nil
	}
	if len(entries) != 1 {
		return nil, false, fmt.Errorf("inspect %s at %s: expected one exact tree entry", normalized, revision)
	}
	separator := strings.IndexByte(entries[0], '\t')
	if separator < 0 || entries[0][separator+1:] != normalized {
		return nil, false, fmt.Errorf("inspect %s at %s: invalid tree entry", normalized, revision)
	}
	fields := strings.Fields(entries[0][:separator])
	if len(fields) != 3 || fields[1] != "blob" || fields[0] != "100644" && fields[0] != "100755" {
		return nil, false, fmt.Errorf("inspect %s at %s: path is not a regular file", normalized, revision)
	}
	data, err := repo.ReadAt(revision, normalized)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func exactRevision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return true
}

func (repo Repository) selectionSince(revision string) (Selection, error) {
	changed, err := repo.gitLines("diff", "-z", "--name-only", "--no-renames", "--diff-filter=ACMR", revision, "--")
	if err != nil {
		return Selection{}, err
	}
	untracked, err := repo.gitLines("ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return Selection{}, err
	}
	deleted, err := repo.gitLines("diff", "-z", "--name-only", "--no-renames", "--diff-filter=D", revision, "--")
	if err != nil {
		return Selection{}, err
	}
	rawChanged := append(append([]string{}, changed...), untracked...)
	candidate := CandidateDelta{AddedOrModified: repo.candidatePaths(rawChanged), Deleted: repo.candidatePaths(deleted)}
	files := repo.existingIncluded(rawChanged)
	policySensitive := repo.policyInputChanged(candidate.Paths())
	selection := Selection{Base: revision, Candidate: candidate, Files: files, PolicySensitive: policySensitive}
	if len(candidate.Deleted) > 0 || repo.hasNonRegularIncluded(rawChanged) || policySensitive {
		selection.Files, err = repo.AllFiles()
		selection.All = true
	}
	return selection, err
}

func (repo Repository) stagedSelection() (Selection, error) {
	if !repo.hasGit() {
		return Selection{}, errors.New("--staged requires a Git repository")
	}
	staged, deleted, err := repo.stagedChanges()
	if err != nil {
		return Selection{}, err
	}
	candidate := CandidateDelta{AddedOrModified: repo.candidatePaths(staged), Deleted: repo.candidatePaths(deleted)}
	expand, policySensitive, err := repo.stagedSelectionRequiresAll(staged, candidate)
	if err != nil {
		return Selection{}, err
	}
	if expand {
		return repo.fullStagedSelection(candidate, policySensitive)
	}
	if err := repo.rejectStagedWorkingTreeDivergence(staged); err != nil {
		return Selection{}, err
	}
	return Selection{Candidate: candidate, Files: repo.existingIncluded(staged), PolicySensitive: policySensitive}, nil
}

func (repo Repository) stagedChanges() ([]string, []string, error) {
	staged, err := repo.gitLines("diff", "-z", "--cached", "--name-only", "--no-renames", "--diff-filter=ACMR", "--")
	if err != nil {
		return nil, nil, err
	}
	deleted, err := repo.gitLines("diff", "-z", "--cached", "--name-only", "--no-renames", "--diff-filter=D", "--")
	if err != nil {
		return nil, nil, err
	}
	return staged, deleted, nil
}

func (repo Repository) stagedSelectionRequiresAll(staged []string, candidate CandidateDelta) (bool, bool, error) {
	nonRegular, err := repo.hasStagedNonRegularIncluded(staged)
	if err != nil {
		return false, false, err
	}
	policySensitive := repo.policyInputChanged(candidate.Paths())
	return len(candidate.Deleted) > 0 || nonRegular || policySensitive, policySensitive, nil
}

func (repo Repository) fullStagedSelection(candidate CandidateDelta, policySensitive bool) (Selection, error) {
	files, err := repo.AllFiles()
	return Selection{Candidate: candidate, Files: files, All: true, PolicySensitive: policySensitive}, err
}

func (repo Repository) rejectStagedWorkingTreeDivergence(staged []string) error {
	unstaged, err := repo.gitLines("diff", "-z", "--name-only", "--no-renames", "--diff-filter=ACMRD", "--")
	if err != nil {
		return err
	}
	unstagedSet := toSet(unstaged)
	conflicts := stagedWorkingTreeConflicts(staged, unstagedSet)
	if len(conflicts) > 0 {
		return fmt.Errorf("staged files also have unstaged changes; refusing to inspect working-tree content: %s", strings.Join(conflicts, ", "))
	}
	return nil
}

func stagedWorkingTreeConflicts(staged []string, unstaged map[string]bool) []string {
	conflicts := []string{}
	for _, path := range staged {
		if unstaged[path] {
			conflicts = append(conflicts, path)
		}
	}
	return uniqueSorted(conflicts)
}

func (repo Repository) NormalizePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		relative, err := filepath.Rel(repo.Root, path)
		if err != nil {
			return "", err
		}
		path = relative
	}
	path = filepath.ToSlash(filepath.Clean(path))
	path = strings.TrimPrefix(path, "./")
	if path == ".." || strings.HasPrefix(path, "../") || path == "." {
		return "", fmt.Errorf("path must stay inside the repository: %s", path)
	}
	return path, nil
}

func (repo Repository) Resolve(path string) (string, error) {
	normalized, err := repo.NormalizePath(path)
	if err != nil {
		return "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(repo.Root)
	if err != nil {
		return "", err
	}
	absolute := filepath.Join(resolvedRoot, filepath.FromSlash(normalized))
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	if !inside(resolvedRoot, resolved) {
		return "", fmt.Errorf("repository path resolves outside the repository: %s", normalized)
	}
	return resolved, nil
}

func (repo Repository) IsExcluded(path string) bool {
	if repo.isDynamicControlInput(path) {
		return false
	}
	patterns := append(append([]string{}, policy.DefaultExcludes...), repo.Config.Scope.Exclude...)
	return policy.MatchesAny(path, patterns)
}

func (repo Repository) IsGenerated(path string) bool {
	if repo.IsData(path) || repo.isDynamicControlInput(path) {
		return false
	}
	patterns := append(append([]string{}, policy.DefaultGenerated...), repo.Config.Scope.Generated...)
	return policy.MatchesAny(path, patterns)
}

func (repo Repository) IsData(path string) bool {
	return !repo.IsControlInput(path) &&
		!repo.IsExecutableSource(path) &&
		policy.MatchesAny(path, repo.Config.Scope.Data)
}

func (repo Repository) IsTest(path string) bool {
	return policy.IsTestPath(path)
}

func (repo Repository) IsDevelopment(path string) bool {
	return repo.IsTest(path) || policy.MatchesAny(path, repo.Config.Scope.Development)
}

func (repo Repository) GoTool(name string) string {
	executableName := name
	if runtime.GOOS == "windows" {
		executableName += ".exe"
	}
	return filepath.Join(repo.PolicyRoot, ".tools", "go", runtime.GOOS+"-"+runtime.GOARCH, "go", "bin", executableName)
}

func (repo Repository) CommandEnvironment() GovernedEnvironment {
	goDirectory := filepath.Dir(repo.GoTool("go"))
	policyBin := filepath.Join(repo.PolicyRoot, ".tools", "bin")
	shellcheckDirectory := filepath.Join(repo.PolicyRoot, ".tools", "shellcheck", shellcheckPlatform())
	javascriptPlatform := runtime.GOOS + "-" + releaseArchitecture(runtime.GOARCH)
	nodeDirectory := filepath.Join(repo.PolicyRoot, ".tools", "javascript", javascriptPlatform, "node", "bin")
	bundleBin := filepath.Join(repo.PolicyRoot, ".tools", "javascript", "bundle", "node_modules", ".bin")
	environment := GovernedEnvironment{
		PathEntries: []string{goDirectory},
		Tools: []GovernedTool{
			{Name: "go", Path: repo.GoTool("go"), Version: repo.ToolPin("go")},
			{Name: "gofmt", Path: repo.GoTool("gofmt"), Version: repo.ToolPin("go")},
		},
	}
	for _, optional := range []struct {
		directory string
		tools     []GovernedTool
	}{
		{directory: policyBin, tools: []GovernedTool{
			{Name: "staticcheck", Path: repo.PolicyTool("staticcheck"), Version: repo.ToolPin("staticcheck")},
			{Name: "govulncheck", Path: repo.PolicyTool("govulncheck"), Version: repo.ToolPin("govulncheck")},
			{Name: "osv-scanner", Path: repo.PolicyTool("osv-scanner"), Version: repo.ToolPin("osv-scanner")},
			{Name: "ruff", Path: repo.PolicyTool("ruff"), Version: repo.ToolPin("ruff")},
			{Name: "ty", Path: repo.PolicyTool("ty"), Version: repo.ToolPin("ty")},
		}},
		{directory: shellcheckDirectory, tools: []GovernedTool{{Name: "shellcheck", Path: filepath.Join(shellcheckDirectory, executableName("shellcheck")), Version: repo.ToolPin("shellcheck")}}},
		{directory: nodeDirectory, tools: []GovernedTool{{Name: "node", Path: filepath.Join(nodeDirectory, executableName("node")), Version: repo.ToolPin("node")}}},
		{directory: bundleBin},
	} {
		if !isDirectory(optional.directory) {
			continue
		}
		environment.PathEntries = append(environment.PathEntries, optional.directory)
		for _, tool := range optional.tools {
			if tool.Version != "" {
				environment.Tools = append(environment.Tools, tool)
			}
		}
	}
	return environment
}

func shellcheckPlatform() string {
	architecture := runtime.GOARCH
	switch architecture {
	case "amd64":
		architecture = "x86_64"
	case "arm64":
		architecture = "aarch64"
	}
	return runtime.GOOS + "-" + architecture
}

func releaseArchitecture(architecture string) string {
	if architecture == "amd64" {
		return "x64"
	}
	return architecture
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func (repo Repository) PolicyTool(name string) string {
	return filepath.Join(repo.PolicyRoot, ".tools", "bin", executableName(name))
}

func (repo Repository) ToolPin(name string) string {
	relative := filepath.Join("tools", name+"-version.txt")
	if name == "go" {
		relative = filepath.Join("scripts", "go_version.txt")
	}
	data, err := os.ReadFile(filepath.Join(repo.PolicyRoot, relative))
	if err != nil {
		return ""
	}
	pin := strings.TrimPrefix(strings.TrimSpace(string(data)), "v")
	if pin == "" || strings.ContainsAny(pin, " \t\n\r") {
		return ""
	}
	return pin
}

func (repo Repository) Language(path string) string {
	languages := repo.Languages(path)
	if len(languages) == 0 {
		return ""
	}
	return languages[0]
}

func (repo Repository) Languages(path string) []string {
	if language := repo.builtInLanguage(path); language != "" {
		return []string{language}
	}
	languages := []string{}
	for _, rule := range repo.Config.Scope.Languages {
		if policy.MatchesAny(path, rule.Paths) {
			languages = append(languages, rule.Name)
		}
	}
	return languages
}

func (repo Repository) builtInLanguage(path string) string {
	extension := strings.ToLower(filepath.Ext(path))
	languages := map[string]string{
		".go": "go", ".ts": "typescript", ".tsx": "typescript", ".js": "typescript",
		".jsx": "typescript", ".mjs": "typescript", ".cjs": "typescript", ".mts": "typescript",
		".cts": "typescript", ".vue": "typescript", ".svelte": "typescript", ".astro": "typescript",
		".py": "python", ".pyi": "python", ".sh": "shell", ".bash": "shell", ".rs": "rust",
		".java": "jvm", ".kt": "jvm", ".kts": "jvm", ".rb": "ruby", ".php": "php",
		".swift": "swift", ".c": "native", ".h": "native", ".cc": "native", ".cpp": "native",
		".cxx": "native", ".hpp": "native", ".proto": "protobuf", ".sql": "sql", ".dart": "dart",
	}
	if language := languages[extension]; language != "" {
		return language
	}
	if repo.hasShellShebang(path) {
		return "shell"
	}
	return ""
}

func (repo Repository) IsExecutableSource(path string) bool {
	return len(repo.Languages(path)) > 0
}

func (repo Repository) ModuleNames(path string) []string {
	names := []string{}
	for _, module := range repo.Config.Modules {
		if policy.MatchesAny(path, module.Paths) {
			names = append(names, module.Name)
		}
	}
	return names
}

func (repo Repository) DesignDocumentFindings() []policy.Finding {
	findings := []policy.Finding{}
	for _, document := range repo.Config.Documentation.Design {
		if message := repo.designDocumentPathMessage(document.Path); message != "" {
			findings = append(findings, designDocumentFinding(document.Path, message))
		}
		if document.Module != "" && !repo.hasModule(document.Module) {
			findings = append(findings, designDocumentFinding(document.Path, "mapped module does not exist"))
		}
		for _, sourcePath := range document.SourcePaths {
			if message := repo.designSourcePathMessage(sourcePath); message != "" {
				findings = append(findings, designDocumentFinding(sourcePath, message))
			}
		}
	}
	sort.Slice(findings, func(left, right int) bool {
		return findings[left].Check+"\x00"+findings[left].Path+"\x00"+findings[left].Subject+"\x00"+findings[left].Message <
			findings[right].Check+"\x00"+findings[right].Path+"\x00"+findings[right].Subject+"\x00"+findings[right].Message
	})
	return findings
}

func (repo Repository) DesignDocumentsForFiles(paths []string) []string {
	documents := map[string]bool{}
	for _, path := range paths {
		normalized, err := repo.NormalizePath(path)
		if err != nil {
			continue
		}
		if document, exists := repo.designDocumentForSource(normalized); exists {
			documents[document.Path] = true
			continue
		}
		for _, module := range repo.ModuleNames(normalized) {
			if document, exists := repo.designDocumentForModule(module); exists {
				documents[document.Path] = true
			}
		}
	}
	return sortedSet(documents)
}

func (repo Repository) DesignDocumentsForModules(modules []string) ([]string, error) {
	selected := map[string]bool{}
	for _, module := range modules {
		if !repo.hasModule(module) {
			return nil, fmt.Errorf("unknown module %q", module)
		}
		if selected[module] {
			return nil, fmt.Errorf("module %q was specified more than once", module)
		}
		selected[module] = true
	}
	documents := map[string]bool{}
	for _, document := range repo.Config.Documentation.Design {
		if document.Module != "" && selected[document.Module] {
			documents[document.Path] = true
			continue
		}
		for _, sourcePath := range document.SourcePaths {
			for _, module := range repo.ModuleNames(sourcePath) {
				if selected[module] {
					documents[document.Path] = true
					break
				}
			}
		}
	}
	return sortedSet(documents), nil
}

func (repo Repository) designDocumentPathMessage(path string) string {
	if !strings.HasPrefix(path, "docs/design/") || !strings.HasSuffix(path, ".md") {
		return "mapped document must be Markdown under docs/design"
	}
	if err := repo.containedRegularFile(path); err != nil {
		return "mapped document is missing, non-regular, or escapes the repository"
	}
	return ""
}

func (repo Repository) designSourcePathMessage(path string) string {
	if err := repo.containedRegularFile(path); err != nil {
		return "mapped source path is missing, non-regular, or escapes the repository"
	}
	if repo.IsExcluded(path) {
		return "mapped source path is excluded from governed scope"
	}
	if !repo.IsSourceCommentSource(path) {
		return "mapped source path is not governed source"
	}
	if owners := repo.ModuleNames(path); len(owners) != 1 {
		return "mapped source path must belong to exactly one module"
	}
	return ""
}

func (repo Repository) containedRegularFile(path string) error {
	normalized, err := repo.NormalizePath(path)
	if err != nil || normalized != path {
		return errors.New("path is not a canonical repository path")
	}
	info, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(path)))
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("path is missing or non-regular")
	}
	_, err = repo.Resolve(path)
	return err
}

func (repo Repository) hasModule(name string) bool {
	if _, exists := repo.Config.ModuleByName[name]; exists {
		return true
	}
	for _, module := range repo.Config.Modules {
		if module.Name == name {
			return true
		}
	}
	return false
}

func (repo Repository) designDocumentForSource(path string) (policy.DesignDocument, bool) {
	for _, document := range repo.Config.Documentation.Design {
		if slices.Contains(document.SourcePaths, path) {
			return document, true
		}
	}
	return policy.DesignDocument{}, false
}

func (repo Repository) designDocumentForModule(module string) (policy.DesignDocument, bool) {
	for _, document := range repo.Config.Documentation.Design {
		if document.Module == module {
			return document, true
		}
	}
	return policy.DesignDocument{}, false
}

func designDocumentFinding(subject, message string) policy.Finding {
	return policy.Finding{Check: DesignDocumentationCheck, Path: policy.ConfigFilename, Subject: subject, Message: message}
}

func sortedSet(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (repo Repository) GoModules(files []string) []GoModule {
	modules := []GoModule{}
	for _, path := range files {
		if filepath.Base(path) != "go.mod" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(repo.Root, filepath.FromSlash(path)))
		if err != nil {
			continue
		}
		name := ""
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "module ") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "module "))
				break
			}
		}
		root := filepath.ToSlash(filepath.Dir(path))
		modules = append(modules, GoModule{Root: root, Manifest: path, Name: name})
	}
	sort.Slice(modules, func(left, right int) bool {
		return len(modules[left].Root) > len(modules[right].Root)
	})
	return modules
}

func (repo Repository) OwningGoModule(path string, modules []GoModule) (GoModule, bool) {
	for _, module := range modules {
		if module.Root == "." || path == module.Root || strings.HasPrefix(path, module.Root+"/") {
			return module, true
		}
	}
	return GoModule{}, false
}

func (repo Repository) Read(path string) ([]byte, error) {
	resolved, err := repo.Resolve(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(resolved)
}

func (repo Repository) existingIncluded(paths []string) []string {
	files := []string{}
	for _, path := range paths {
		path = strings.TrimPrefix(filepath.ToSlash(path), "./")
		if path == "" || repo.IsExcluded(path) {
			continue
		}
		info, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(path)))
		if err == nil && (info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
			files = append(files, path)
		}
	}
	return uniqueSorted(files)
}

func (repo Repository) candidatePaths(paths []string) []string {
	result := []string{}
	for _, path := range paths {
		path = strings.TrimPrefix(filepath.ToSlash(path), "./")
		if path != "" && !repo.IsExcluded(path) {
			result = append(result, path)
		}
	}
	return uniqueSorted(result)
}

func (repo Repository) hasNonRegularIncluded(paths []string) bool {
	for _, path := range paths {
		path = strings.TrimPrefix(filepath.ToSlash(path), "./")
		if path == "" || repo.IsExcluded(path) {
			continue
		}
		info, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(path)))
		if err == nil && !info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func (repo Repository) hasStagedNonRegularIncluded(paths []string) (bool, error) {
	for _, path := range paths {
		path = strings.TrimPrefix(filepath.ToSlash(path), "./")
		if path == "" || repo.IsExcluded(path) {
			continue
		}
		entries, err := repo.gitLines("ls-files", "--stage", "-z", "--", path)
		if err != nil {
			return false, err
		}
		if len(entries) != 1 || !regularIndexEntry(entries[0]) {
			return true, nil
		}
	}
	return false, nil
}

func (repo Repository) policyInputChanged(paths []string) bool {
	releaseArtifactPins := map[string]bool{}
	for _, artifact := range repo.Config.SupplyChain.ReleaseArtifacts {
		releaseArtifactPins[normalizePolicyInputPath(artifact.VersionFile)] = true
	}
	for _, path := range paths {
		normalized := normalizePolicyInputPath(path)
		if repo.IsControlInput(normalized) || releaseArtifactPins[normalized] {
			return true
		}
	}
	return false
}

func normalizePolicyInputPath(path string) string {
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "./")
}

func (repo Repository) hasShellShebang(path string) bool {
	resolved, err := repo.Resolve(path)
	if err != nil {
		return false
	}
	file, err := os.Open(resolved)
	if err != nil {
		return false
	}
	prefix := make([]byte, 512)
	count, readErr := file.Read(prefix)
	closeErr := file.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || closeErr != nil {
		return false
	}
	first := string(bytes.SplitN(prefix[:count], []byte("\n"), 2)[0])
	return strings.HasPrefix(first, "#!") && (strings.Contains(first, "/sh") || strings.Contains(first, "/bash") || strings.Contains(first, " sh") || strings.Contains(first, " bash"))
}

func (repo Repository) hasGit() bool {
	return repo.git("rev-parse", "--git-dir") == nil
}

func (repo Repository) git(arguments ...string) error {
	command := exec.Command("git", append([]string{"-C", repo.Root}, arguments...)...)
	command.Stdout = nil
	command.Stderr = nil
	return command.Run()
}

func (repo Repository) gitLines(arguments ...string) ([]string, error) {
	command := exec.Command("git", append([]string{"-C", repo.Root}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(arguments, " "), err)
	}
	separator := byte('\n')
	if slices.Contains(arguments, "-z") {
		separator = 0
	}
	parts := bytes.Split(output, []byte{separator})
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) > 0 {
			lines = append(lines, filepath.ToSlash(string(part)))
		}
	}
	return uniqueSorted(lines), nil
}

func uniqueSorted(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func inside(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isRegularOrSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && (info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0)
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
