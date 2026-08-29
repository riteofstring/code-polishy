package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

const ChangeBoundaryCheck = "repository.changeBoundary"

type ChangeBoundaryResult struct {
	Base            string
	AllowedModules  []string
	AllowedPaths    []string
	AllowedNewPaths []string
	ChangedPaths    []string
	Findings        []policy.Finding
}

func CheckChangeBoundary(root, policyRoot, configPath, base string, allowedModules, allowedPaths, allowedNewPaths []string) (ChangeBoundaryResult, error) {
	bare, normalizedConfig, err := trustedBoundaryRepository(root, policyRoot, configPath, base)
	if err != nil {
		return ChangeBoundaryResult{}, err
	}
	modules, allowed, err := validateAllowedModules(bare.Config, allowedModules)
	if err != nil {
		return ChangeBoundaryResult{}, err
	}
	exactPaths, exactAllowed, newPaths, newAllowed, err := validateAllowedPaths(bare, base, normalizedConfig, allowedPaths, allowedNewPaths)
	if err != nil {
		return ChangeBoundaryResult{}, err
	}
	paths, additions, err := bare.changedPathsSinceExact(base)
	if err != nil {
		return ChangeBoundaryResult{}, err
	}
	committedPaths, committedNewPaths, err := bare.committedPathsSinceExact(base, newAllowed)
	if err != nil {
		return ChangeBoundaryResult{}, err
	}
	paths = uniqueSorted(append(paths, committedPaths...))
	findings := make([]policy.Finding, 0)
	for _, path := range paths {
		findings = append(findings, boundaryFindings(bare, normalizedConfig, path, allowed, exactAllowed, newAllowed, additions, committedNewPaths)...)
	}
	return ChangeBoundaryResult{
		Base: base, AllowedModules: modules, AllowedPaths: exactPaths, AllowedNewPaths: newPaths, ChangedPaths: paths, Findings: findings,
	}, nil
}

func trustedBoundaryRepository(root, policyRoot, configPath, base string) (Repository, string, error) {
	repo, err := Open(root, policyRoot, policy.Config{})
	if err != nil {
		return Repository{}, "", err
	}
	if err := repo.requireExactCommit(base); err != nil {
		return Repository{}, "", err
	}
	if configPath == "" {
		configPath = policy.ConfigFilename
	}
	normalizedConfig, err := repo.NormalizePath(configPath)
	if err != nil {
		return Repository{}, "", fmt.Errorf("trusted configuration path: %w", err)
	}
	data, err := repo.ReadAt(base, normalizedConfig)
	if err != nil {
		return Repository{}, "", fmt.Errorf("read trusted configuration: %w", err)
	}
	repo.Config, err = policy.Parse(data, normalizedConfig)
	if err != nil {
		return Repository{}, "", fmt.Errorf("compile trusted configuration: %w", err)
	}
	return repo, normalizedConfig, nil
}

func validateAllowedModules(config policy.Config, requested []string) ([]string, map[string]bool, error) {
	if len(requested) == 0 {
		return nil, nil, errors.New("change boundary requires at least one allowed module")
	}
	modules := append([]string{}, requested...)
	sort.Strings(modules)
	allowed := make(map[string]bool, len(modules))
	for _, module := range modules {
		if allowed[module] {
			return nil, nil, fmt.Errorf("allowed module %q was specified more than once", module)
		}
		if _, exists := config.ModuleByName[module]; !exists {
			return nil, nil, fmt.Errorf("trusted configuration has no module %q", module)
		}
		allowed[module] = true
	}
	return modules, allowed, nil
}

func validateAllowedPaths(repo Repository, base, configPath string, requested, requestedNew []string) ([]string, map[string]bool, []string, map[string]bool, error) {
	exactPaths, exactAllowed, err := normalizeAllowedPaths(repo, configPath, "--allow-path", requested, nil, false)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	newPaths, newAllowed, err := normalizeAllowedPaths(repo, configPath, "--allow-new-path", requestedNew, exactAllowed, true)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	for _, path := range exactPaths {
		if !repo.regularFileAt(base, path) {
			return nil, nil, nil, nil, fmt.Errorf("allowed path must be a tracked file at the trusted base: %s", path)
		}
	}
	for _, path := range newPaths {
		exists, err := repo.pathAt(base, path)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("read allowed new path at the trusted base: %w", err)
		}
		if exists {
			return nil, nil, nil, nil, fmt.Errorf("allowed new path exists at the trusted base; use --allow-path instead: %s", path)
		}
	}
	return exactPaths, exactAllowed, newPaths, newAllowed, nil
}

func normalizeAllowedPaths(repo Repository, configPath, option string, requested []string, alreadyAllowed map[string]bool, newArtifact bool) ([]string, map[string]bool, error) {
	paths := make([]string, 0, len(requested))
	allowed := make(map[string]bool, len(requested))
	for _, candidate := range requested {
		if candidate == "" {
			return nil, nil, fmt.Errorf("%s requires a non-empty path", option)
		}
		path, err := repo.NormalizePath(candidate)
		if err != nil {
			return nil, nil, fmt.Errorf("%s %q: %w", option, candidate, err)
		}
		if strings.ContainsAny(path, "*?[]{}") {
			return nil, nil, fmt.Errorf("%s must name one exact path, not a glob: %s", option, path)
		}
		if protectedControlPath(path, configPath) || (newArtifact && protectedNewArtifactPath(path, configPath)) {
			return nil, nil, fmt.Errorf("control-plane path cannot be allowed: %s", path)
		}
		if allowed[path] {
			return nil, nil, fmt.Errorf("%s %q was specified more than once", option, path)
		}
		if alreadyAllowed[path] {
			return nil, nil, fmt.Errorf("%s %q duplicates --allow-path", option, path)
		}
		allowed[path] = true
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, allowed, nil
}

func (repo Repository) regularFileAt(revision, path string) bool {
	present, regular := repo.regularFileOrAbsentAt(revision, path)
	return present && regular
}

func (repo Repository) pathAt(revision, path string) (bool, error) {
	entries, err := repo.gitLines("ls-tree", "-z", revision, "--", path)
	if err != nil {
		return false, err
	}
	return len(entries) == 1, nil
}

func (repo Repository) regularWorktreeFile(path string) bool {
	info, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(path)))
	return err == nil && info.Mode().IsRegular()
}

func (repo Repository) regularNewArtifactFile(path string) bool {
	headPresent, headRegular := repo.regularFileOrAbsentAt("HEAD", path)
	return headRegular && repo.regularWorktreeFile(path) && repo.regularIndexFileOrAbsent(path, headPresent)
}

func (repo Repository) regularFileOrAbsentAt(revision, path string) (bool, bool) {
	entries, err := repo.gitLines("ls-tree", "-z", revision, "--", path)
	if err != nil || len(entries) > 1 {
		return false, false
	}
	if len(entries) == 0 {
		return false, true
	}
	return true, strings.HasPrefix(entries[0], "100644 blob ") || strings.HasPrefix(entries[0], "100755 blob ")
}

func (repo Repository) regularIndexFileOrAbsent(path string, headPresent bool) bool {
	entries, err := repo.gitLines("ls-files", "--stage", "-z", "--", path)
	if err != nil {
		return false
	}
	if len(entries) == 0 {
		return !headPresent
	}
	if len(entries) != 1 {
		return false
	}
	return regularIndexEntry(entries[0])
}

func regularIndexEntry(entry string) bool {
	separator := strings.IndexByte(entry, '\t')
	if separator == -1 {
		return false
	}
	fields := strings.Fields(entry[:separator])
	return len(fields) == 3 && fields[2] == "0" && (fields[0] == "100644" || fields[0] == "100755")
}

func boundaryFindings(repo Repository, configPath, path string, allowed, exactAllowed, newAllowed, additions, committedNewPaths map[string]bool) []policy.Finding {
	if protectedControlPath(path, configPath) {
		return []policy.Finding{{
			Check: ChangeBoundaryCheck, Path: path, Subject: "control-plane",
			Message: "task sessions cannot change trusted policy or repository control-plane files",
		}}
	}
	if exactAllowed[path] {
		return nil
	}
	if newAllowed[path] {
		if additions[path] && committedNewPaths[path] && repo.regularNewArtifactFile(path) {
			return nil
		}
		return []policy.Finding{{
			Check: ChangeBoundaryCheck, Path: path, Subject: "new-path",
			Message: "allowed new path must be a regular file addition",
		}}
	}
	owners := repo.ModuleNames(path)
	if len(owners) == 0 {
		return []policy.Finding{{
			Check: ChangeBoundaryCheck, Path: path, Subject: "unowned",
			Message: "changed path is not owned by any module in the trusted-base configuration",
		}}
	}
	if len(owners) > 1 {
		return []policy.Finding{{
			Check: ChangeBoundaryCheck, Path: path, Subject: strings.Join(owners, ","),
			Message: "changed path has ambiguous module ownership in the trusted-base configuration",
		}}
	}
	if !allowed[owners[0]] {
		return []policy.Finding{{
			Check: ChangeBoundaryCheck, Path: path, Subject: owners[0],
			Message: fmt.Sprintf("changed path belongs to module %q, which this task session was not allowed to change", owners[0]),
		}}
	}
	return nil
}

func protectedControlPath(path, configPath string) bool {
	path = filepath.ToSlash(path)

	return path == configPath || slices.Contains([]string{policy.LockFilename, "CODEOWNERS", ".github/CODEOWNERS"}, path) ||
		strings.HasPrefix(path, ".github/workflows/") || path == ".gitignore" || strings.HasSuffix(path, "/.gitignore") ||
		path == ".gitattributes" || strings.HasSuffix(path, "/.gitattributes")
}

func protectedNewArtifactPath(path, configPath string) bool {
	for _, protected := range []string{configPath, policy.LockFilename, "CODEOWNERS", ".github/CODEOWNERS", ".github/workflows"} {
		if path == protected || strings.HasPrefix(path, protected+"/") || strings.HasPrefix(protected, path+"/") {
			return true
		}
	}
	return containsPathComponent(path, ".gitignore") || containsPathComponent(path, ".gitattributes")
}

func containsPathComponent(path, component string) bool {
	return slices.Contains(strings.Split(path, "/"), component)
}

func (repo Repository) changedPathsSinceExact(revision string) ([]string, map[string]bool, error) {
	if err := repo.requireExactCommit(revision); err != nil {
		return nil, nil, err
	}
	type diffView struct {
		options   []string
		revisions []string
	}

	views := []diffView{
		{revisions: []string{revision, "HEAD"}},
		{revisions: []string{revision}},
		{options: []string{"--cached"}, revisions: []string{revision}},
	}
	changed := []string{}
	added := []string{}
	for _, view := range views {
		paths, err := repo.diffPathNames(view.options, view.revisions, false)
		if err != nil {
			return nil, nil, err
		}
		changed = append(changed, paths...)
		paths, err = repo.diffPathNames(view.options, view.revisions, true)
		if err != nil {
			return nil, nil, err
		}
		added = append(added, paths...)
	}
	untracked, err := repo.gitLines("ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, nil, err
	}
	additions := make(map[string]bool, len(added)+len(untracked))
	for _, path := range append(added, untracked...) {
		additions[path] = true
	}
	return uniqueSorted(append(changed, untracked...)), additions, nil
}

func (repo Repository) committedPathsSinceExact(revision string, newAllowed map[string]bool) ([]string, map[string]bool, error) {
	committedNewPaths := make(map[string]bool, len(newAllowed))
	for path := range newAllowed {
		committedNewPaths[path] = true
	}
	commits, err := repo.gitLines("rev-list", revision+"..HEAD")
	if err != nil {
		return nil, nil, fmt.Errorf("list committed changes since trusted base: %w", err)
	}
	paths := []string{}
	for _, commit := range commits {
		changed, err := repo.gitLines("diff-tree", "-m", "--root", "--no-commit-id", "-r", "-z", "--name-only", "--no-renames", commit)
		if err != nil {
			return nil, nil, fmt.Errorf("list paths changed by candidate commit %s: %w", commit, err)
		}
		paths = append(paths, changed...)
		for _, path := range changed {
			if newAllowed[path] && !repo.regularFileAt(commit, path) {
				committedNewPaths[path] = false
			}
		}
	}
	return uniqueSorted(paths), committedNewPaths, nil
}

func (repo Repository) diffPathNames(options, revisions []string, additionsOnly bool) ([]string, error) {
	arguments := append([]string{"diff"}, options...)
	arguments = append(arguments, "-z", "--name-only", "--no-renames")
	if additionsOnly {
		arguments = append(arguments, "--diff-filter=A")
	}
	arguments = append(arguments, revisions...)
	arguments = append(arguments, "--")
	return repo.gitLines(arguments...)
}

func (repo Repository) requireExactCommit(revision string) error {
	if !exactRevision(revision) {
		return errors.New("trusted base must be an exact Git object ID")
	}
	if err := repo.git("cat-file", "-e", revision+"^{commit}"); err != nil {
		return fmt.Errorf("trusted base is not an available commit: %s", revision)
	}
	return nil
}
