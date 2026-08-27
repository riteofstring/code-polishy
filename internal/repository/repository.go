package repository

import (
	"bytes"
	"errors"
	"fmt"
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
	Root       string
	PolicyRoot string
	Config     policy.Config
}

type Selection struct {
	// Base is the exact revision from which Git changes were selected. It is
	// empty for selections that do not compare a candidate with a revision.
	Base    string
	Files   []string
	Deleted []string
	All     bool
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
	switch mode {
	case "all":
		files, err := repo.AllFiles()
		return Selection{Files: files, All: true}, err
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
		return Selection{Files: uniqueSorted(files)}, nil
	case "staged":
		return repo.stagedSelection()
	case "changes", "":
		return repo.changedSelection()
	default:
		return Selection{}, fmt.Errorf("unknown selection mode %q", mode)
	}
}

func (repo Repository) AllFiles() ([]string, error) {
	if repo.hasGit() {
		tracked, err := repo.gitLines("ls-files", "-z")
		if err != nil {
			return nil, err
		}
		untracked, err := repo.gitLines("ls-files", "-z", "--others", "--exclude-standard")
		if err != nil {
			return nil, err
		}
		return repo.existingIncluded(append(tracked, untracked...)), nil
	}
	files := []string{}
	err := filepath.WalkDir(repo.Root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(repo.Root, path)
		if err != nil {
			return err
		}
		normalized := filepath.ToSlash(relative)
		if entry.IsDir() {
			if normalized != "." && repo.IsExcluded(normalized+"/placeholder") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() && !repo.IsExcluded(normalized) {
			files = append(files, normalized)
		}
		return nil
	})
	return uniqueSorted(files), err
}

// RawFiles inventories tracked and visible untracked files without applying the
// target's configured excludes. It lets doctor prove that an adapter did not
// hide executable code, manifests, or workflows from the shared baseline.
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
		return Selection{Files: files, All: true}, err
	}
	head, err := repo.gitLines("rev-parse", "--verify", "HEAD")
	if err != nil || len(head) != 1 || !exactRevision(head[0]) {
		return Selection{}, errors.New("resolve current HEAD: expected one exact commit")
	}
	return repo.selectionSince(head[0])
}

// SelectBase returns the committed and working-tree changes since the merge
// base of base and HEAD. Untracked files participate as current work.
func (repo Repository) SelectBase(base string) (Selection, error) {
	mergeBase, err := repo.MergeBase(base)
	if err != nil {
		return Selection{}, err
	}
	return repo.selectionSince(mergeBase)
}

// MergeBase resolves a user-supplied ref to the single commit shared with
// HEAD. Callers can then use the opaque exact revision with FilesAt and ReadAt.
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
	files := repo.existingIncluded(rawChanged)
	selection := Selection{Base: revision, Files: files, Deleted: uniqueSorted(deleted)}
	if len(deleted) > 0 || repo.hasNonRegularIncluded(rawChanged) || repo.policyInputChanged(append(rawChanged, deleted...)) {
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
	expand, err := repo.stagedSelectionRequiresAll(staged, deleted)
	if err != nil {
		return Selection{}, err
	}
	if expand {
		return repo.fullStagedSelection(deleted)
	}
	if err := repo.rejectStagedWorkingTreeDivergence(staged); err != nil {
		return Selection{}, err
	}
	return Selection{Files: repo.existingIncluded(staged), Deleted: uniqueSorted(deleted)}, nil
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

func (repo Repository) stagedSelectionRequiresAll(staged, deleted []string) (bool, error) {
	nonRegular, err := repo.hasStagedNonRegularIncluded(staged)
	if err != nil {
		return false, err
	}
	return len(deleted) > 0 || nonRegular || repo.policyInputChanged(append(append([]string{}, staged...), deleted...)), nil
}

func (repo Repository) fullStagedSelection(deleted []string) (Selection, error) {
	files, err := repo.AllFiles()
	return Selection{Files: files, Deleted: uniqueSorted(deleted), All: true}, err
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
	patterns := append(append([]string{}, policy.DefaultExcludes...), repo.Config.Scope.Exclude...)
	return policy.MatchesAny(path, patterns)
}

func (repo Repository) IsGenerated(path string) bool {
	patterns := append(append([]string{}, policy.DefaultGenerated...), repo.Config.Scope.Generated...)
	return policy.MatchesAny(path, patterns)
}

func (repo Repository) IsTest(path string) bool {
	return policy.IsTestPath(path)
}

// IsDevelopment reports whether one governed file exists only to build,
// configure, or exercise the product rather than to ship with it. A test
// already does; which of the rest do is a fact only the target knows, so it
// declares them.
func (repo Repository) IsDevelopment(path string) bool {
	return repo.IsTest(path) || policy.MatchesAny(path, repo.Config.Scope.Development)
}

// GoTool names the pinned Go toolchain executable Code Polishy runs. A release
// carries the toolchain it pins, so there is one path and it is always the
// policy root's: nothing is taken from an ambient PATH, a host installation, or
// an environment override. A policy root without it reports the missing tool
// rather than governing a repository with whatever Go the machine happens to
// have.
func (repo Repository) GoTool(name string) string {
	executableName := name
	if runtime.GOOS == "windows" {
		executableName += ".exe"
	}
	return filepath.Join(repo.PolicyRoot, ".tools", "go", runtime.GOOS+"-"+runtime.GOARCH, "go", "bin", executableName)
}

// CommandEnvironment is the single inventory of release-owned executables
// exposed to governed commands and task-session writers. Callers prepend these
// directories in order, so a host tool with the same name can never outrank
// the release Code Polishy verified.
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

// PolicyTool names one pinned analyzer the policy root carries, under the same
// rule the Go toolchain follows: one path, always the policy root's.
func (repo Repository) PolicyTool(name string) string {
	return filepath.Join(repo.PolicyRoot, ".tools", "bin", executableName(name))
}

// ToolPin is the exact version the policy root pins for one carried executable,
// without a leading `v`: the distributions disagree about it, and a release
// records one form. The pin is a file the release carries beside the tool it
// names, so a check compares what a tool reports against the release's own
// record rather than against a version compiled into the engine.
//
// A policy root that does not record the pin pins nothing, which is reported as
// an empty version so the caller refuses the tool rather than accepting
// whatever happens to be installed.
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
	if extension == "" && repo.hasShellShebang(path) {
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
		if isPolicyInput(normalized) || releaseArtifactPins[normalized] {
			return true
		}
	}
	return false
}

func normalizePolicyInputPath(path string) string {
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "./")
}

func isPolicyInput(path string) bool {
	name := filepath.Base(path)
	// The lock names which Code Polishy release governs this repository, so
	// changing it changes what every check means, exactly as changing the
	// configuration does. Together they are the whole policy control plane a
	// target checks in.
	if slices.Contains([]string{policy.ConfigFilename, policy.LockFilename}, path) ||
		strings.HasPrefix(path, ".github/workflows/") {
		return true
	}
	if containerInputName(name) || lockfileName(name) {
		return true
	}
	if policyToolConfigName(name) {
		return true
	}
	return slices.Contains([]string{
		"go.mod", "go.sum", "go.work", "go.work.sum", "package.json", "pyproject.toml",
		"Cargo.toml", "requirements.txt", "Pipfile", "pom.xml", "build.gradle",
		"build.gradle.kts", "Gemfile", "composer.json", "Package.swift", "Package.resolved",
		"pubspec.yaml",
	}, name) || strings.HasPrefix(name, "requirements") && strings.HasSuffix(name, ".txt")
}

func policyToolConfigName(name string) bool {
	return strings.HasPrefix(name, "eslint.config.") || strings.HasPrefix(name, ".eslintrc") ||
		strings.HasPrefix(name, "knip.") || strings.HasPrefix(name, ".knip.") ||
		strings.HasPrefix(name, "prettier.config.") || name == ".prettierrc" || strings.HasPrefix(name, ".prettierrc.") ||
		strings.HasPrefix(name, "tsconfig") && strings.HasSuffix(name, ".json") ||
		slices.Contains([]string{"ruff.toml", ".ruff.toml", "osv-scanner.toml"}, name)
}

func containerInputName(name string) bool {
	return name == "Dockerfile" || strings.HasPrefix(name, "Dockerfile.") || name == "Containerfile" || strings.HasPrefix(name, "Containerfile.")
}

func lockfileName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".lock") || strings.Contains(lower, "-lock.") || slices.Contains([]string{"pnpm-lock.yaml", "bun.lockb"}, name)
}

func (repo Repository) hasShellShebang(path string) bool {
	data, err := repo.Read(path)
	if err != nil {
		return false
	}
	first := string(bytes.SplitN(data, []byte("\n"), 2)[0])
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
