package pack

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func toolchainCommand(repo repository.Repository, command policy.Command, requested []policy.PackTool) (policy.Command, []ToolIdentity, error) {
	if len(requested) == 0 {
		return command, []ToolIdentity{}, nil
	}
	if err := validateToolDeclarations(requested, "tools"); err != nil {
		return command, nil, err
	}
	manifest, found, err := release.ReadLauncherManifest(repo.PolicyRoot)
	if err != nil {
		return command, nil, err
	}
	if !found {
		return command, nil, errors.New("pack toolchains require a verified installed Code Polishy release")
	}
	return bindToolchain(repo.PolicyRoot, manifest, repo.CommandEnvironment().Tools, command, requested)
}

func bindToolchain(root string, manifest release.Manifest, available []repository.GovernedTool, command policy.Command, requested []policy.PackTool) (policy.Command, []ToolIdentity, error) {
	if command.Adapter == nil || len(command.Argv) == 0 {
		return command, nil, errors.New("pack toolchain command is incomplete")
	}
	identities := make([]ToolIdentity, 0, len(requested))
	launcher := ""
	for _, required := range requested {
		index := slices.IndexFunc(available, func(tool repository.GovernedTool) bool { return tool.Name == required.Name })
		if index < 0 {
			return command, nil, fmt.Errorf("policy-owned tool %s %s is not installed", required.Name, required.Version)
		}
		tool := available[index]
		if tool.Version != required.Version {
			return command, nil, fmt.Errorf("pack requires %s %s; the installed release supplies %s", required.Name, required.Version, tool.Version)
		}
		identity, err := verifyToolFile(root, manifest, tool.Path, required)
		if err != nil {
			return command, nil, err
		}
		identities = append(identities, identity)
		command.EnvironmentOverrides = append(command.EnvironmentOverrides, toolEnvironmentName(required.ID)+"="+tool.Path)
		if required.Launcher {
			launcher = tool.Path
		}
	}
	arguments := slices.Clone(command.Argv)
	arguments[0] = filepath.Join(command.Adapter.PackRoot, filepath.FromSlash(arguments[0]))
	command.Argv = arguments
	if launcher != "" {
		command.Argv = append([]string{launcher}, arguments...)
	}
	return command, identities, nil
}

func verifyToolFile(root string, manifest release.Manifest, executable string, requested policy.PackTool) (ToolIdentity, error) {
	relative, err := filepath.Rel(root, executable)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ToolIdentity{}, errors.New("tool is outside the installed release")
	}
	relative = filepath.ToSlash(relative)
	resolved, entry, err := resolveManifestTool(root, manifest.Entries, relative)
	if err != nil {
		return ToolIdentity{}, err
	}
	file, err := os.Open(resolved)
	if err != nil {
		return ToolIdentity{}, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ToolIdentity{}, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if digest != entry.SHA256 {
		return ToolIdentity{}, errors.New("tool does not match the installed release identity")
	}
	return ToolIdentity{ID: requested.ID, Name: requested.Name, Version: requested.Version, SHA256: digest}, nil
}

func resolveManifestTool(root string, entries []release.Entry, relative string) (string, release.Entry, error) {
	byPath := make(map[string]release.Entry, len(entries))
	for _, entry := range entries {
		byPath[entry.Path] = entry
	}
	seen := map[string]bool{}
	current := relative
	for {
		entry, err := nextManifestToolEntry(byPath, seen, current)
		if err != nil {
			return "", release.Entry{}, err
		}
		if entry.SHA256 != "" {
			return manifestToolFile(root, current, entry)
		}
		current, err = manifestToolLinkTarget(root, current, entry.Symlink)
		if err != nil {
			return "", release.Entry{}, err
		}
	}
}

func nextManifestToolEntry(entries map[string]release.Entry, seen map[string]bool, current string) (release.Entry, error) {
	entry, found := entries[current]
	if !found || seen[current] {
		return release.Entry{}, errors.New("tool is not recorded in the installed release")
	}
	seen[current] = true
	return entry, nil
}

func manifestToolFile(root, current string, entry release.Entry) (string, release.Entry, error) {
	absolute := filepath.Join(root, filepath.FromSlash(current))
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return "", release.Entry{}, errors.New("recorded tool target is not a regular file")
	}
	return absolute, entry, nil
}

func manifestToolLinkTarget(root, current, target string) (string, error) {
	absolute := filepath.Join(root, filepath.FromSlash(current))
	if err := verifyManifestToolLink(absolute, target); err != nil {
		return "", err
	}
	normalizedTarget := filepath.ToSlash(target)
	if path.IsAbs(normalizedTarget) || filepath.IsAbs(target) {
		return "", errors.New("recorded tool link escapes the installed release")
	}
	next := path.Clean(path.Join(path.Dir(current), normalizedTarget))
	if next == "." || next == ".." || strings.HasPrefix(next, "../") || path.IsAbs(next) {
		return "", errors.New("recorded tool link escapes the installed release")
	}
	return next, nil
}

func verifyManifestToolLink(absolute, target string) error {
	info, err := os.Lstat(absolute)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return errors.New("recorded tool link is not a symbolic link")
	}
	actual, err := os.Readlink(absolute)
	if err != nil || actual != target {
		return errors.New("tool link does not match the installed release identity")
	}
	return nil
}

func toolEnvironmentName(id string) string {
	return "CODE_POLISHY_TOOL_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(id))
}
