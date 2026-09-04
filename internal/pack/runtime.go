package pack

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type Resolution struct {
	Commands  []policy.Command
	Languages []policy.LanguageRule
	Manifests []policy.PackDependencyRule
	Findings  []policy.Finding
	Notes     []string
}

func Unavailable(selected []policy.PackSelection, err error) Resolution {
	resolution := Resolution{}
	for _, selection := range selected {
		resolution.Findings = append(resolution.Findings, unavailableFinding(selection, err))
	}
	return resolution
}

func Resolve(selected []policy.PackSelection, dataRoot string) Resolution {
	resolution := Resolution{}
	for _, selection := range selected {
		root := InstalledRoot(dataRoot, selection.Name, selection.Version, selection.Digest)
		receipt, err := VerifyInstalled(root)
		if err != nil {
			resolution.Findings = append(resolution.Findings, unavailableFinding(selection, err))
			continue
		}
		if receipt.Name != selection.Name || receipt.Version != selection.Version || receipt.Digest != selection.Digest {
			resolution.Findings = append(resolution.Findings, unavailableFinding(selection, errors.New("installation receipt does not match the selected identity")))
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, ManifestFilename))
		if err != nil {
			resolution.Findings = append(resolution.Findings, unavailableFinding(selection, err))
			continue
		}
		manifest, err := ParseManifest(data, filepath.Join(root, ManifestFilename))
		if err != nil || !slices.Contains(manifest.Platforms, CurrentPlatform()) {
			if err == nil {
				err = fmt.Errorf("installed pack does not support %s", CurrentPlatform())
			}
			resolution.Findings = append(resolution.Findings, unavailableFinding(selection, err))
			continue
		}
		if _, err := VerifyInstalled(root); err != nil {
			resolution.Findings = append(resolution.Findings, unavailableFinding(selection, fmt.Errorf("pack changed during resolution: %w", err)))
			continue
		}
		compileManifest(root, selection, manifest, &resolution)
		resolution.Notes = append(resolution.Notes, fmt.Sprintf("language pack: %s %s %s", selection.Name, selection.Version, selection.Digest))
	}
	return resolution
}

func Apply(config *policy.Config, resolution Resolution) {
	config.Scope.Languages = append(config.Scope.Languages, resolution.Languages...)
	config.PackManifests = append(config.PackManifests, resolution.Manifests...)
	config.Checks = append(config.Checks, resolution.Commands...)
}

func compileManifest(root string, selection policy.PackSelection, manifest Manifest, resolution *Resolution) {
	patterns := []string{}
	manifestPatterns := map[string][]string{}
	for _, language := range manifest.Languages {
		source := slices.Clone(language.SourcePatterns)
		if len(source) == 0 {
			source = builtInPatterns(language.ID)
		} else {
			resolution.Languages = append(resolution.Languages, policy.LanguageRule{Name: language.ID, Paths: source})
		}
		patterns = append(patterns, source...)
		manifestPatterns[language.ID] = append(manifestPatterns[language.ID], language.DependencyManifests...)
		if len(language.DependencyManifests) > 0 {
			resolution.Manifests = append(resolution.Manifests, policy.PackDependencyRule{Pack: selection.Name, Language: language.ID, Paths: slices.Clone(language.DependencyManifests)})
		}
	}
	patterns = sortedUnique(patterns)
	for _, declared := range manifest.Commands {
		for _, capability := range declared.Capabilities {
			paths := slices.Clone(patterns)
			if slices.Contains([]string{"dependency-policy", "lock-sync", "release-age", "security"}, capability) {
				paths = nil
				for _, values := range manifestPatterns {
					paths = append(paths, values...)
				}
				paths = sortedUnique(paths)
			}
			resolution.Commands = append(resolution.Commands, policy.Command{
				Name:     "pack." + selection.Name + "." + declared.Name + "." + capability,
				Provides: []string{capability}, Argv: slices.Clone(declared.Argv), Cwd: ".", Paths: paths,
				RunOn: slices.Clone(declared.Profiles), Environment: slices.Clone(declared.Environment), ExclusiveResources: []string{},
				TimeoutSeconds: declared.TimeoutSeconds, Managed: true, SealedEnvironment: true,
				Adapter: &policy.PackAdapter{PackName: selection.Name, PackVersion: selection.Version, PackDigest: selection.Digest, PackRoot: root, ProtocolVersion: manifest.ProtocolVersion, Capability: capability},
			})
		}
	}
}

func FullTreeFindings(selected []policy.PackSelection, dataRoot string) []policy.Finding {
	findings := []policy.Finding{}
	for _, selection := range selected {
		root := InstalledRoot(dataRoot, selection.Name, selection.Version, selection.Digest)
		receipt, err := VerifyInstalled(root)
		if err != nil || receipt.Name != selection.Name || receipt.Version != selection.Version || receipt.Digest != selection.Digest {
			if err == nil {
				err = errors.New("installation receipt does not match the selected identity")
			}
			findings = append(findings, unavailableFinding(selection, err))
		}
	}
	return findings
}

func CoverageFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := providerConflictFindings(repo, files)
	manifestOwnersByPath := map[string][]string{}
	for _, declared := range repo.Config.PackManifests {
		for _, manifest := range files {
			if !policy.MatchesAny(manifest, declared.Paths) {
				continue
			}
			manifestOwnersByPath[manifest] = append(manifestOwnersByPath[manifest], declared.Pack)
			owners := manifestOwners(repo, files, manifest, declared.Language)
			if len(owners) == 0 {
				owners = []string{""}
			}
			for _, owner := range owners {
				for _, capability := range []string{"dependency-policy", "lock-sync", "release-age", "security"} {
					if !hasProvider(repo.Config.Checks, owner, capability) {
						subject := declared.Pack + ":" + capability
						if owner != "" {
							subject = owner + ":" + capability
						}
						findings = append(findings, policy.Finding{Check: "policy.packCoverage", Path: manifest, Subject: subject, Message: "pack-owned dependency manifest lacks a complete supply-chain provider"})
					}
				}
			}
		}
	}
	for manifest, owners := range manifestOwnersByPath {
		owners = sortedUnique(owners)
		if len(owners) > 1 {
			findings = append(findings, policy.Finding{Check: "policy.packManifest", Path: manifest, Subject: strings.Join(owners, ","), Message: "dependency manifest is claimed by more than one selected pack"})
		}
	}
	return uniqueFindings(findings)
}

func providerConflictFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	commands := repo.Config.Checks
	for left := range commands {
		for right := left + 1; right < len(commands); right++ {
			if commands[left].Adapter == nil && commands[right].Adapter == nil {
				continue
			}
			for _, capability := range commands[left].Provides {
				if !slices.Contains(commands[right].Provides, capability) || !commandsOverlap(repo, files, commands[left], commands[right]) {
					continue
				}
				findings = append(findings, policy.Finding{Check: "policy.packProvider", Path: policy.ConfigFilename, Subject: capability, Message: fmt.Sprintf("%s and %s both claim authoritative ownership of matching source", commands[left].Name, commands[right].Name)})
			}
		}
	}
	return findings
}

func commandsOverlap(repo repository.Repository, files []string, left, right policy.Command) bool {
	for _, file := range files {
		leftPath := len(left.Paths) == 0 || policy.MatchesAny(file, left.Paths)
		rightPath := len(right.Paths) == 0 || policy.MatchesAny(file, right.Paths)
		if leftPath && rightPath && moduleCommandMatches(repo, left.Modules, file) && moduleCommandMatches(repo, right.Modules, file) {
			return true
		}
	}
	return false
}

func moduleCommandMatches(repo repository.Repository, modules []string, file string) bool {
	if len(modules) == 0 {
		return true
	}
	for _, owner := range repo.ModuleNames(file) {
		if slices.Contains(modules, owner) {
			return true
		}
	}
	return false
}

func manifestOwners(repo repository.Repository, files []string, manifest, language string) []string {
	directory := strings.TrimSuffix(filepath.ToSlash(filepath.Dir(manifest)), ".")
	owners := []string{}
	for _, file := range files {
		if repo.Language(file) != language || directory != "" && !strings.HasPrefix(file, directory+"/") {
			continue
		}
		owners = append(owners, repo.ModuleNames(file)...)
	}
	return sortedUnique(owners)
}

func hasProvider(commands []policy.Command, module, capability string) bool {
	requiredProfiles := map[string][]string{"dependency-policy": {"supply-chain"}, "lock-sync": {"supply-chain"}, "release-age": {"supply-chain-online"}, "security": {"security"}}
	for _, command := range commands {
		moduleCovered := module == "" && len(command.Modules) == 0 || module != "" && (len(command.Modules) == 0 || slices.Contains(command.Modules, module))
		if moduleCovered && slices.Contains(command.Provides, capability) && intersects(command.RunOn, requiredProfiles[capability]) {
			return true
		}
	}
	return false
}

func intersects(left, right []string) bool {
	for _, value := range left {
		if slices.Contains(right, value) {
			return true
		}
	}
	return false
}

func unavailableFinding(selection policy.PackSelection, err error) policy.Finding {
	message := fmt.Sprintf("required pack %s %s %s is unavailable: %v; install it with code-polishy pack install --source PATH", selection.Name, selection.Version, selection.Digest, err)
	return policy.Finding{Check: "policy.packUnavailable", Path: policy.ConfigFilename, Subject: selection.Name, Message: message}
}

func builtInPatterns(language string) []string {
	patterns := map[string][]string{
		"dart": {"**/*.dart"}, "go": {"**/*.go"}, "jvm": {"**/*.java", "**/*.kt", "**/*.kts"},
		"native": {"**/*.c", "**/*.cc", "**/*.cpp", "**/*.cxx", "**/*.h", "**/*.hpp"},
		"php":    {"**/*.php"}, "protobuf": {"**/*.proto"}, "python": {"**/*.py", "**/*.pyi"},
		"ruby": {"**/*.rb"}, "rust": {"**/*.rs"}, "shell": {"**/*.sh", "**/*.bash"},
		"sql": {"**/*.sql"}, "swift": {"**/*.swift"},
		"typescript": {"**/*.ts", "**/*.tsx", "**/*.js", "**/*.jsx", "**/*.mjs", "**/*.cjs", "**/*.vue", "**/*.svelte", "**/*.astro"},
	}
	return slices.Clone(patterns[language])
}

func sortedUnique(values []string) []string {
	result := slices.Clone(values)
	sort.Strings(result)
	return slices.Compact(result)
}

func uniqueFindings(findings []policy.Finding) []policy.Finding {
	sort.Slice(findings, func(left, right int) bool {
		return findings[left].Check+"\x00"+findings[left].Path+"\x00"+findings[left].Subject+"\x00"+findings[left].Message < findings[right].Check+"\x00"+findings[right].Path+"\x00"+findings[right].Subject+"\x00"+findings[right].Message
	})
	return slices.CompactFunc(findings, func(left, right policy.Finding) bool {
		return left.Check == right.Check && left.Path == right.Path && left.Subject == right.Subject && left.Message == right.Message
	})
}
