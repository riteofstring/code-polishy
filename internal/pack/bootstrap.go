package pack

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
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
	index := slices.IndexFunc(manifest.Entries, func(entry release.Entry) bool { return entry.Path == relative && entry.SHA256 != "" })
	if index < 0 {
		return ToolIdentity{}, errors.New("tool is not recorded in the installed release")
	}
	directory, err := os.OpenRoot(root)
	if err != nil {
		return ToolIdentity{}, err
	}
	defer directory.Close()
	file, err := directory.Open(relative)
	if err != nil {
		return ToolIdentity{}, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ToolIdentity{}, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if digest != manifest.Entries[index].SHA256 {
		return ToolIdentity{}, errors.New("tool does not match the installed release identity")
	}
	return ToolIdentity{ID: requested.ID, Name: requested.Name, Version: requested.Version, SHA256: digest}, nil
}

func toolEnvironmentName(id string) string {
	return "CODE_POLISHY_TOOL_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(id))
}
