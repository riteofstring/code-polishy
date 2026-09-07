package repository

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type AnalysisOwner struct {
	Name    string
	Pack    string
	Native  bool
	Problem string
}

func (repo Repository) AnalysisOwner(path, capability, profile string) AnalysisOwner {
	if profile == "" {
		profile = repo.AnalysisProfile()
	}
	if len(repo.Config.UnavailablePacks) > 0 {
		return AnalysisOwner{Problem: "selected language packs are unavailable: " + strings.Join(repo.Config.UnavailablePacks, ", ")}
	}
	claimed := false
	owner := AnalysisOwner{}
	for _, command := range repo.Config.Checks {
		if !repo.commandClaimsAnalysis(command, path, capability) {
			continue
		}
		claimed = true
		if !analysisProfileMatches(command.RunOn, profile) {
			continue
		}
		if owner.Name != "" {
			return AnalysisOwner{Problem: fmt.Sprintf("%s and %s both provide %s for %s", owner.Name, command.Name, capability, path)}
		}
		owner = AnalysisOwner{Name: command.Name, Pack: command.Adapter.PackName}
	}
	if owner.Name != "" {
		return owner
	}
	if claimed {
		return AnalysisOwner{Problem: fmt.Sprintf("the selected provider has no %s operation for profile %s", capability, profile)}
	}
	if repo.NativeCapability(path, capability) {
		return AnalysisOwner{Name: "native:" + repo.Language(path), Native: true}
	}
	return AnalysisOwner{Problem: fmt.Sprintf("no provider analyzes %s for %s", capability, path)}
}

func analysisProfileMatches(profiles []string, requested string) bool {
	return slices.Contains(profiles, requested) || requested == "gate" && slices.Contains(profiles, "check")
}

func (repo Repository) CommandOwnsPath(command policy.Command, path string) bool {
	if len(command.Paths) > 0 && !policy.MatchesAny(path, command.Paths) {
		return false
	}
	if command.Adapter != nil && len(command.Adapter.Languages) > 0 && slices.Contains([]string{"format", "lint", "typecheck", "complexity", "dead-code", "architecture"}, command.Adapter.Capability) {
		if !slices.ContainsFunc(command.Adapter.Languages, func(language policy.LanguageRule) bool { return policy.MatchesAny(path, language.Paths) }) {
			return false
		}
	}
	if len(command.Modules) == 0 {
		return true
	}
	for _, module := range repo.OwnerModuleNames(path) {
		if slices.Contains(command.Modules, module) {
			return true
		}
	}
	return false
}

func (repo Repository) WithAnalysisProfile(profile string) Repository {
	repo.analysisProfile = profile
	return repo
}

func (repo Repository) AnalysisProfile() string {
	if repo.analysisProfile == "" {
		return "check"
	}
	return repo.analysisProfile
}

func (repo Repository) NativeAnalysis(path, capability string) bool {
	return repo.AnalysisOwner(path, capability, "").Native
}

func (repo Repository) NativeUnit(files []string, capabilities ...string) (bool, error) {
	native, other := false, false
	for _, path := range files {
		for _, capability := range capabilities {
			if repo.NativeAnalysis(path, capability) {
				native = true
			} else {
				other = true
			}
		}
	}
	if native && other {
		return false, fmt.Errorf("the %s compilation unit is split between native and selected providers; bind the complete unit to one provider", strings.Join(capabilities, "/"))
	}
	return native, nil
}

func (repo Repository) NativeAnalysisFiles(files []string, capability, profile string) []string {
	selected := make([]string, 0, len(files))
	for _, path := range files {
		if repo.AnalysisOwner(path, capability, profile).Native {
			selected = append(selected, path)
		}
	}
	return selected
}

func (repo Repository) NativeCapability(path, capability string) bool {
	language := repo.Language(path)
	extension := strings.ToLower(filepath.Ext(path))
	if capability == "format" && slices.Contains([]string{".css", ".html", ".json", ".jsonc", ".md", ".markdown", ".yaml", ".yml"}, extension) {
		return true
	}
	switch language {
	case "go", "python":
		return slices.Contains([]string{"format", "lint", "typecheck", "complexity", "dead-code", "architecture"}, capability)
	case "shell":
		return slices.Contains([]string{"format", "lint", "typecheck"}, capability)
	case "typescript":
		return slices.Contains([]string{".cjs", ".cts", ".js", ".jsx", ".mjs", ".mts", ".ts", ".tsx"}, extension) &&
			slices.Contains([]string{"format", "lint", "typecheck", "complexity", "dead-code", "architecture"}, capability)
	}
	return false
}

func (repo Repository) commandClaimsAnalysis(command policy.Command, path, capability string) bool {
	return command.Adapter != nil && slices.Contains(command.Provides, capability) && repo.CommandOwnsPath(command, path)
}
