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

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func runtimeCommand(repo repository.Repository, command policy.Command, requested *policy.PackRuntime) (policy.Command, *RuntimeIdentity, error) {
	if requested == nil {
		return command, nil, nil
	}
	if requested.Name != "node" {
		return command, nil, fmt.Errorf("policy-owned runtime %s is unavailable", requested.Name)
	}
	manifest, found, err := release.ReadLauncherManifest(repo.PolicyRoot)
	if err != nil {
		return command, nil, err
	}
	if !found {
		return command, nil, errors.New("pack runtimes require a verified installed Code Polishy release")
	}
	if manifest.Tools.Node != requested.Version {
		return command, nil, fmt.Errorf("pack requires Node %s; the installed release supplies %s", requested.Version, manifest.Tools.Node)
	}
	for _, tool := range repo.CommandEnvironment().Tools {
		if tool.Name != requested.Name {
			continue
		}
		identity, err := verifyRuntimeFile(repo.PolicyRoot, manifest, tool.Path, requested)
		if err != nil {
			return command, nil, err
		}
		arguments := slices.Clone(command.Argv)
		arguments[0] = filepath.Join(command.Adapter.PackRoot, filepath.FromSlash(arguments[0]))
		command.Argv = append([]string{tool.Path}, arguments...)
		return command, &identity, nil
	}
	return command, nil, fmt.Errorf("policy-owned runtime %s is not installed", requested.Name)
}

func verifyRuntimeFile(root string, manifest release.Manifest, executable string, requested *policy.PackRuntime) (RuntimeIdentity, error) {
	relative, err := filepath.Rel(root, executable)
	if err != nil {
		return RuntimeIdentity{}, err
	}
	relative = filepath.ToSlash(relative)
	index := slices.IndexFunc(manifest.Entries, func(entry release.Entry) bool { return entry.Path == relative && entry.SHA256 != "" })
	if index < 0 {
		return RuntimeIdentity{}, errors.New("runtime is not recorded in the installed release")
	}
	directory, err := os.OpenRoot(root)
	if err != nil {
		return RuntimeIdentity{}, err
	}
	defer directory.Close()
	file, err := directory.Open(relative)
	if err != nil {
		return RuntimeIdentity{}, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return RuntimeIdentity{}, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if digest != manifest.Entries[index].SHA256 {
		return RuntimeIdentity{}, errors.New("runtime does not match the installed release identity")
	}
	return RuntimeIdentity{Name: requested.Name, Version: requested.Version, SHA256: digest}, nil
}
