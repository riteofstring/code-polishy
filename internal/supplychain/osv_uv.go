package supplychain

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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

type onlineUVInput struct {
	Scope    string
	Packages []resolvedPackage
}

func onlineUVInputs(repo repository.Repository) ([]onlineUVInput, error) {
	files, err := repo.RawFiles()
	if err != nil {
		return nil, fmt.Errorf("enumerate resolved Python lockfiles: %w", err)
	}
	inputs := []onlineUVInput{}
	remaining := 32 << 20
	for _, scope := range files {
		if filepath.Base(scope) != "uv.lock" || repo.IsExcluded(scope) {
			continue
		}
		if len(inputs) >= 512 || remaining <= 0 {
			return nil, fmt.Errorf("resolved Python inventory exceeds its input limit")
		}
		data, err := readGitEvidenceFile(repo, scope, int64(min(16<<20, remaining)))
		if err != nil {
			return nil, fmt.Errorf("read resolved Python inventory at %s: %w", scope, err)
		}
		packages, err := parseUVLock(data, scope)
		if err != nil {
			return nil, fmt.Errorf("resolved Python inventory at %s is malformed", scope)
		}
		inputs = append(inputs, onlineUVInput{Scope: scope, Packages: packages})
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

func osvUVScans(repo repository.Repository, inputs []onlineUVInput) ([]osvScan, error) {
	scans := []osvScan{}
	for _, input := range inputs {
		if !osvCoversScope(repo.Config, input.Scope) || gitLockPackages(input.Packages) {
			continue
		}
		projection, err := publicUVProjection(input)
		if err != nil {
			return nil, err
		}
		if len(projection) == 0 {
			continue
		}
		path := osvInputDirectory + "/" + gitEvidenceDigest(projection) + ".osv-scanner.json"
		command := osvCommand(repo, "osv-uv-"+safeName(input.Scope), ".")
		command.Argv = append(command.Argv, "--no-resolve", "--lockfile", "osv-scanner:"+path)
		command.Paths = []string{input.Scope}
		scans = append(scans, osvScan{Command: command, Root: ".", Scope: input.Scope, InputPath: path, Projection: projection})
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

func rootHasUVInput(root string, inputs []onlineUVInput) bool {
	for _, input := range inputs {
		if scopeInsideOSVRoot(input.Scope, root) {
			return true
		}
	}
	return false
}

func scopeInsideOSVRoot(scope, root string) bool {
	return root == "." || strings.HasPrefix(scope, strings.TrimSuffix(root, "/")+"/")
}

func publicUVProjection(input onlineUVInput) ([]byte, error) {
	packages := []osvPackageResult{}
	for _, item := range input.Packages {
		if item.Source.Kind == "local" {
			continue
		}
		if item.Source.Kind != "registry" || !publicPythonRegistry(item.Source.Registry) {
			return nil, fmt.Errorf("resolved Python inventory at %s cannot be sent to a public scanner; provide authenticated coverage for its non-public or unsupported sources", input.Scope)
		}
		observed := osvPackageResult{}
		observed.Package.Name, observed.Package.Version, observed.Package.Ecosystem = item.Name, item.Version, "PyPI"
		packages = append(packages, observed)
	}
	if len(packages) == 0 {
		return nil, nil
	}
	result := osvResult{Packages: packages}
	result.Source.Path, result.Source.Type = input.Scope, "lockfile"
	return json.Marshal(osvReport{Results: []osvResult{result}})
}

func publicPythonRegistry(registry string) bool {
	return strings.TrimSuffix(registry, "/") == "https://pypi.org/simple"
}
