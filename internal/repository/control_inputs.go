package repository

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func (repo Repository) WithDynamicControlInputs(paths []string) (Repository, error) {
	inputs := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		normalized, err := repo.NormalizePath(path)
		if err != nil || normalized != path || seen[path] {
			return Repository{}, fmt.Errorf("dynamic control input %q is not a distinct contained repository path", path)
		}
		seen[path] = true
		inputs = append(inputs, path)
	}
	sort.Strings(inputs)
	repo.DynamicControlInputs = inputs
	return repo, nil
}

func (repo Repository) IsControlInput(path string) bool {
	if repo.pathFactCache != nil {
		return cachedPathFact(repo.pathFactCache, &repo.pathFactCache.controlInputs, path, cloneBool, func() bool { return repo.computeIsControlInput(path) })
	}
	return repo.computeIsControlInput(path)
}

func (repo Repository) computeIsControlInput(path string) bool {
	return policy.IsSensitiveControlInput(path) || repo.isDynamicControlInput(path)
}

func (repo Repository) isManagedBootstrapWrapper(path string) bool {
	if repo.PolicyRoot == "" {
		return false
	}
	template := ""
	switch normalizePolicyInputPath(path) {
	case "code-polishyw":
		template = "code-polishyw"
	case "code-polishyw.ps1":
		template = "code-polishyw.ps1"
	default:
		return false
	}
	wrapperBytes, err := os.ReadFile(filepath.Join(repo.Root, template))
	if err != nil {
		return false
	}
	templateBytes, err := os.ReadFile(filepath.Join(repo.PolicyRoot, "templates", template))
	return err == nil && bytes.Equal(wrapperBytes, templateBytes)
}

func (repo Repository) isDynamicControlInput(path string) bool {
	normalized := normalizePolicyInputPath(path)
	return slices.Contains(repo.DynamicControlInputs, normalized)
}

func (repo Repository) validateDynamicControlInputs() error {
	for _, path := range repo.DynamicControlInputs {
		if policy.MatchesAny(path, repo.Config.Scope.Exclude) {
			return fmt.Errorf("scope.exclude must not classify policy-sensitive GitLab include %s", path)
		}
		if policy.MatchesAny(path, append(append([]string{}, policy.DefaultGenerated...), repo.Config.Scope.Generated...)) {
			return fmt.Errorf("scope.generated must not classify policy-sensitive GitLab include %s", path)
		}
		if policy.MatchesAny(path, repo.Config.Scope.Data) {
			return fmt.Errorf("scope.data must not classify policy-sensitive GitLab include %s", path)
		}
	}
	return nil
}
