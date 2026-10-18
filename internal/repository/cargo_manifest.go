package repository

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type CargoManifest struct {
	Path         string
	Root         string
	Package      *CargoPackageManifest
	Workspace    *CargoWorkspaceManifest
	Dependencies []CargoDependencyDeclaration
	Patches      map[string]json.RawMessage
}

type CargoPackageManifest struct {
	Name      string `toml:"name"`
	Workspace string `toml:"workspace"`
}

type CargoWorkspaceManifest struct {
	Members        []string `toml:"members"`
	Exclude        []string `toml:"exclude"`
	DefaultMembers []string `toml:"default-members"`
}

type CargoManifestProblem struct {
	Path    string
	Line    int
	Message string
}

type CargoManifestInventory struct {
	Manifests []CargoManifest
	Problems  []CargoManifestProblem
}

func (repo Repository) InspectCargoManifests(files []string) CargoManifestInventory {
	inventory := CargoManifestInventory{}
	paths := map[string]bool{}
	for _, path := range files {
		if filepath.Base(path) == "Cargo.toml" {
			paths[path] = true
		}
	}
	if len(paths) == 0 {
		return inventory
	}
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		inventory.Problems = append(inventory.Problems, cargoManifestProblem("Cargo.toml", err))
		return inventory
	}
	defer root.Close()
	for _, path := range sortedSet(paths) {
		manifest, err := repo.readCargoManifest(root, path)
		if err != nil {
			inventory.Problems = append(inventory.Problems, cargoManifestProblem(path, err))
			continue
		}
		inventory.Manifests = append(inventory.Manifests, manifest)
	}
	return inventory
}

func (repo Repository) readCargoManifest(root *os.Root, path string) (CargoManifest, error) {
	info, err := repo.containedRegularFileInfo(root, path)
	if err != nil {
		return CargoManifest{}, err
	}
	data, err := readContainedFile(root, path, info, 8<<20)
	if err != nil {
		return CargoManifest{}, err
	}
	return ParseCargoManifest(path, data)
}

func ParseCargoManifest(path string, data []byte) (CargoManifest, error) {
	if len(data) > 8<<20 {
		return CargoManifest{}, errors.New("cargo manifest exceeds 8388608 bytes")
	}
	var document cargoManifestDocument
	if err := toml.Unmarshal(data, &document); err != nil {
		return CargoManifest{}, err
	}
	if err := validateCargoManifestDocument(document); err != nil {
		return CargoManifest{}, err
	}
	dependencies, err := cargoDependencyDeclarations(document)
	if err != nil {
		return CargoManifest{}, err
	}
	manifest := CargoManifest{Path: path, Root: filepath.ToSlash(filepath.Dir(path)), Package: document.Package, Dependencies: dependencies}
	manifest.Patches = map[string]json.RawMessage{}
	for registry, declarations := range document.Patch {
		encoded, err := json.Marshal(declarations)
		if err != nil {
			return CargoManifest{}, err
		}
		manifest.Patches["patch."+registry] = encoded
	}
	if document.Workspace != nil {
		manifest.Workspace = &document.Workspace.CargoWorkspaceManifest
	}
	return manifest, nil
}

func validateCargoManifestDocument(document cargoManifestDocument) error {
	if document.Package == nil && document.Workspace == nil {
		return errors.New("manifest must declare a package or workspace")
	}
	if document.Package != nil && strings.TrimSpace(document.Package.Name) == "" {
		return errors.New("package must declare its name")
	}
	if document.Package != nil && document.Package.Workspace != "" && document.Workspace != nil {
		return errors.New("a package cannot declare both package.workspace and a workspace table")
	}
	return nil
}

func cargoManifestProblem(path string, err error) CargoManifestProblem {
	problem := CargoManifestProblem{Path: path, Message: "Cargo manifest cannot be read: " + err.Error()}
	var decoding *toml.DecodeError
	if errors.As(err, &decoding) {
		problem.Line, _ = decoding.Position()
	}
	return problem
}
