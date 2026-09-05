package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/pythonfacts"
)

func PythonReachabilityID(reference policy.PythonDynamicReference) string {
	data, _ := json.Marshal(reference)
	digest := sha256.Sum256(data)
	return "config:reachability:" + hex.EncodeToString(digest[:])
}

func (repo Repository) PythonReachabilityInputs(project PythonProject) []pythonfacts.ReachabilityInput {
	result := []pythonfacts.ReachabilityInput{}
	for _, declaration := range repo.Config.Scope.PythonDynamicReferences {
		if declaration.Project != project.Manifest {
			continue
		}
		data, _ := json.Marshal(declaration)
		input := pythonfacts.ReachabilityInput{ID: PythonReachabilityID(declaration), Declaration: data}
		if err := repo.pythonReachabilityConsumer(project, declaration); err != nil {
			input.Error = err.Error()
		} else if declaration.Registry != nil {
			content, err := repo.pythonReachabilityRegistry(project, declaration.Registry.Path)
			if err != nil {
				input.Error = err.Error()
			} else {
				input.Registry = string(content)
			}
		}
		result = append(result, input)
	}
	slices.SortFunc(result, func(left, right pythonfacts.ReachabilityInput) int { return strings.Compare(left.ID, right.ID) })
	return result
}

func (repo Repository) pythonReachabilityConsumer(project PythonProject, declaration policy.PythonDynamicReference) error {
	consumer := declaration.Consumer
	if !slices.Contains(project.Files, consumer.Importer) || len(repo.OwnerModuleNames(consumer.Importer)) != 1 {
		return fmt.Errorf("consumer is not one governed source in the declared project")
	}
	module, _ := PythonModuleName(project, consumer.Importer)
	if module != consumer.Module {
		return fmt.Errorf("consumer module does not match its source")
	}
	if declaration.Target != nil {
		for _, inferred := range project.DynamicReferences {
			if inferred.Module == declaration.Target.Module && inferred.Symbol == declaration.Target.Symbol {
				return fmt.Errorf("target repeats an inferred entry-point contract")
			}
		}
	}
	return nil
}

func (repo Repository) pythonReachabilityRegistry(project PythonProject, path string) ([]byte, error) {
	if normalized, err := repo.NormalizePath(path); err != nil || normalized != path || repo.IsExcluded(path) || repo.IsGenerated(path) || len(repo.OwnerModuleNames(path)) != 1 {
		return nil, fmt.Errorf("registry must be a contained governed handwritten input")
	}
	if err := repo.pythonReachabilityRegistryProject(project, path); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := repo.containedRegularFileInfo(root, path)
	if err != nil {
		return nil, err
	}
	data, err := readContainedFile(root, path, info, 2<<20)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("registry is empty")
	}
	return data, nil
}

func (repo Repository) pythonReachabilityRegistryProject(project PythonProject, path string) error {
	if project.Root != "." && !strings.HasPrefix(path, project.Root+"/") {
		return fmt.Errorf("registry is outside its declared project")
	}
	for directory := filepath.ToSlash(filepath.Dir(path)); ; directory = filepath.ToSlash(filepath.Dir(directory)) {
		manifest := "pyproject.toml"
		if directory != "." {
			manifest = directory + "/pyproject.toml"
		}
		if _, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(manifest))); err == nil {
			if manifest != project.Manifest {
				return fmt.Errorf("registry belongs to another Python project")
			}
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		if directory == "." {
			return fmt.Errorf("registry has no contained Python project")
		}
	}
}
