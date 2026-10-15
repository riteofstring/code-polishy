package supplychain

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const osvInputDirectory = ".code-polishy-reports/supply-chain/osv-inputs"

type osvScan struct {
	Command    policy.Command
	Root       string
	Scope      string
	InputPath  string
	Projection []byte
}

type osvLockInput struct {
	Scope    string
	Packages []resolvedPackage
}

func onlineUVInputs(repo repository.Repository) ([]osvLockInput, error) {
	return osvLockInputs(repo, "uv.lock", parseUVLock)
}

func osvLockInputs(repo repository.Repository, name string, parse func([]byte, string) ([]resolvedPackage, error)) ([]osvLockInput, error) {
	return resolvedLockInputs(repo, map[string]func([]byte, string) ([]resolvedPackage, error){name: parse})
}

func resolvedLockInputs(repo repository.Repository, parsers map[string]func([]byte, string) ([]resolvedPackage, error)) ([]osvLockInput, error) {
	files, err := repo.RawFiles()
	if err != nil {
		return nil, fmt.Errorf("enumerate resolved lock inventory: %w", err)
	}
	slices.Sort(files)
	inputs := []osvLockInput{}
	remaining := 32 << 20
	for _, scope := range files {
		parse, selected := parsers[filepath.Base(scope)]
		if !selected || repo.IsExcluded(scope) {
			continue
		}
		if len(inputs) >= 512 || remaining <= 0 {
			return nil, fmt.Errorf("resolved lock inventory exceeds its input limit")
		}
		data, err := readGitEvidenceFile(repo, scope, int64(min(16<<20, remaining)))
		if err != nil {
			return nil, fmt.Errorf("read resolved inventory at %s: %w", scope, err)
		}
		packages, err := parse(data, scope)
		if err != nil {
			return nil, fmt.Errorf("resolved inventory at %s is malformed or has unsupported sources", scope)
		}
		inputs = append(inputs, osvLockInput{Scope: scope, Packages: packages})
		remaining -= len(data)
	}
	return inputs, nil
}

func gitLockPackages(packages []resolvedPackage) bool {
	for _, item := range packages {
		if item.Source.Kind == "git" {
			return true
		}
	}
	return false
}

func osvUVScans(repo repository.Repository, inputs []osvLockInput) ([]osvScan, error) {
	scans := []osvScan{}
	for _, input := range inputs {
		if !osvCoversScope(repo.Config, input.Scope) || (repo.Config.SupplyChain.GitEvidence.Required && gitLockPackages(input.Packages)) {
			continue
		}
		projection, err := publicLockProjection(input, "PyPI", "https://pypi.org/simple")
		if err != nil {
			return nil, err
		}
		if len(projection) == 0 {
			continue
		}
		scans = append(scans, osvProjectionScan(repo, "uv", input.Scope, projection))
	}
	return scans, nil
}

func osvCoversScope(config policy.Config, scope string) bool {
	for _, root := range activeOSVRoots(config) {
		if scopeInsideOSVRoot(scope, root) {
			return true
		}
	}
	return false
}

func scopeInsideOSVRoot(scope, root string) bool {
	return root == "." || strings.HasPrefix(scope, strings.TrimSuffix(root, "/")+"/")
}

func publicLockProjection(input osvLockInput, ecosystem, registry string) ([]byte, error) {
	packages := []osvPackageResult{}
	for _, item := range input.Packages {
		if item.Source.Kind == "local" || item.Source.Kind == "git" {
			continue
		}
		if item.Source.Kind != "registry" || strings.TrimSuffix(item.Source.Registry, "/") != registry {
			return nil, fmt.Errorf("resolved inventory at %s cannot be sent to a public scanner; provide authenticated coverage for its non-public or unsupported sources", input.Scope)
		}
		observed := osvPackageResult{}
		observed.Package.Name, observed.Package.Version, observed.Package.Ecosystem = item.Name, item.Version, ecosystem
		packages = append(packages, observed)
	}
	if len(packages) == 0 {
		return nil, nil
	}
	result := osvResult{Packages: packages}
	result.Source.Path, result.Source.Type = input.Scope, "lockfile"
	return json.Marshal(osvReport{Results: []osvResult{result}})
}

func osvProjectionScan(repo repository.Repository, kind, scope string, projection []byte) osvScan {
	path := osvInputDirectory + "/" + gitEvidenceDigest(projection) + ".osv-scanner.json"
	command := osvCommand(repo, "osv-"+kind+"-"+safeName(scope), ".")
	command.Argv = append(command.Argv, "--no-resolve", "--lockfile", "osv-scanner:"+path)
	command.Paths = []string{scope}
	return osvScan{Command: command, Root: ".", Scope: scope, InputPath: path, Projection: projection}
}

func publicPythonRegistry(registry string) bool {
	return strings.TrimSuffix(registry, "/") == "https://pypi.org/simple"
}
