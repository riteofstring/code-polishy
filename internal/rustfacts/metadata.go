package rustfacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
	"golang.org/x/mod/semver"
)

type Workspace struct {
	Root           string
	TargetDir      string
	Packages       []Package
	Members        []string
	DefaultMembers []string
}

type Package struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Version      string              `json:"version"`
	Manifest     string              `json:"manifest_path"`
	Edition      string              `json:"edition"`
	RustVersion  string              `json:"rust_version"`
	Source       *string             `json:"source"`
	Targets      []Target            `json:"targets"`
	Dependencies []Dependency        `json:"dependencies"`
	Features     map[string][]string `json:"features"`
}

type Target struct {
	Name             string   `json:"name"`
	Kinds            []string `json:"kind"`
	CrateTypes       []string `json:"crate_types"`
	Path             string   `json:"src_path"`
	Edition          string   `json:"edition"`
	RequiredFeatures []string `json:"required-features"`
	Test             bool     `json:"test"`
	Doctest          bool     `json:"doctest"`
}

type Dependency struct {
	Name            string   `json:"name"`
	Requirement     string   `json:"req"`
	Source          *string  `json:"source"`
	Kind            string   `json:"kind"`
	Rename          string   `json:"rename"`
	Path            string   `json:"path"`
	Target          string   `json:"target"`
	Optional        bool     `json:"optional"`
	DefaultFeatures bool     `json:"uses_default_features"`
	Features        []string `json:"features"`
}

type workspaceWire struct {
	Version        int             `json:"version"`
	Root           string          `json:"workspace_root"`
	TargetDir      string          `json:"target_directory"`
	Packages       []Package       `json:"packages"`
	Members        []string        `json:"workspace_members"`
	DefaultMembers []string        `json:"workspace_default_members"`
	Resolve        json.RawMessage `json:"resolve"`
}

func ParseWorkspace(repo repository.Repository, manifest string, data []byte) (Workspace, error) {
	wire, err := decodeWorkspace(data)
	if err != nil {
		return Workspace{}, err
	}
	return workspaceFromWire(repo, manifest, wire)
}

func workspaceFromWire(repo repository.Repository, manifest string, wire workspaceWire) (Workspace, error) {
	root, err := containedPath(repo, filepath.Join(wire.Root, "Cargo.toml"))
	if err != nil {
		return Workspace{}, fmt.Errorf("cargo workspace root: %w", err)
	}
	if wire.TargetDir != TargetDirectory(repo) {
		return Workspace{}, errors.New("cargo metadata did not retain the governed target directory")
	}
	workspace := Workspace{Root: filepath.ToSlash(filepath.Dir(root)), TargetDir: wire.TargetDir, Packages: wire.Packages, Members: wire.Members, DefaultMembers: wire.DefaultMembers}
	if err := validateWorkspace(repo, manifest, &workspace); err != nil {
		return Workspace{}, err
	}
	return workspace, nil
}

func decodeWorkspace(data []byte) (workspaceWire, error) {
	wire, err := decodeMetadata(data)
	if err != nil {
		return workspaceWire{}, err
	}
	if !bytes.Equal(bytes.TrimSpace(wire.Resolve), []byte("null")) {
		return workspaceWire{}, errors.New("workspace-only discovery must not claim a resolved dependency graph")
	}
	return wire, nil
}

func decodeMetadata(data []byte) (workspaceWire, error) {
	if len(data) > 8<<20 {
		return workspaceWire{}, errors.New("cargo metadata exceeds 8388608 bytes")
	}
	var wire workspaceWire
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&wire); err != nil {
		return workspaceWire{}, fmt.Errorf("invalid Cargo metadata: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return workspaceWire{}, errors.New("cargo metadata must contain one JSON value")
	}
	if wire.Version != 1 || len(wire.Packages) == 0 || len(wire.Members) == 0 || wire.DefaultMembers == nil {
		return workspaceWire{}, errors.New("cargo metadata is missing version-1 workspace facts")
	}
	return wire, nil
}

func validateWorkspace(repo repository.Repository, manifest string, workspace *Workspace) error {
	ids := map[string]bool{}
	manifests := map[string]bool{filepath.ToSlash(filepath.Join(workspace.Root, "Cargo.toml")): true}
	for index := range workspace.Packages {
		pkg := &workspace.Packages[index]
		if ids[pkg.ID] || !completeWorkspacePackage(*pkg) {
			return fmt.Errorf("cargo package %q has incomplete, duplicate or non-workspace identity", pkg.ID)
		}
		if err := validatePackage(repo, pkg); err != nil {
			return fmt.Errorf("cargo package %q: %w", pkg.Name, err)
		}
		ids[pkg.ID] = true
		manifests[pkg.Manifest] = true
	}
	if !manifests[manifest] || len(workspace.Members) != len(ids) {
		return errors.New("cargo metadata does not describe the requested manifest and its workspace members")
	}
	if err := validateMembers(workspace.Members, ids); err != nil {
		return err
	}
	return validateMembers(workspace.DefaultMembers, ids)
}

func completeWorkspacePackage(pkg Package) bool {
	return pkg.Source == nil && completePackage(pkg)
}

func completePackage(pkg Package) bool {
	return pkg.ID != "" && pkg.Name != "" && pkg.Version != "" &&
		len(pkg.Targets) > 0 && pkg.Features != nil && pkg.Dependencies != nil
}

func validateMembers(members []string, ids map[string]bool) error {
	seen := map[string]bool{}
	for _, id := range members {
		if !ids[id] || seen[id] {
			return fmt.Errorf("cargo workspace membership repeats or omits package %q", id)
		}
		seen[id] = true
	}
	return nil
}

func validatePackage(repo repository.Repository, pkg *Package) error {
	manifest, err := containedPath(repo, pkg.Manifest)
	if err != nil || filepath.Base(manifest) != "Cargo.toml" {
		return fmt.Errorf("invalid manifest path %q", pkg.Manifest)
	}
	pkg.Manifest = manifest
	if !supportedEdition(pkg.Edition) {
		return fmt.Errorf("unsupported Rust edition %q", pkg.Edition)
	}
	if pkg.RustVersion != "" && !semver.IsValid("v"+pkg.RustVersion) {
		return fmt.Errorf("invalid minimum Rust version %q", pkg.RustVersion)
	}
	for index := range pkg.Targets {
		if err := validateTarget(repo, &pkg.Targets[index], pkg.Source != nil); err != nil {
			return err
		}
	}
	return validateDependencies(repo, pkg.Dependencies)
}

func validateTarget(repo repository.Repository, target *Target, remote bool) error {
	if target.Name == "" || len(target.Kinds) == 0 || len(target.CrateTypes) == 0 || !supportedEdition(target.Edition) {
		return fmt.Errorf("target %q has incomplete or unsupported facts", target.Name)
	}
	path, err := containedPath(repo, target.Path)
	if err != nil && !(remote && inactiveDependencyTarget(*target) && errors.Is(err, fs.ErrNotExist)) {
		return fmt.Errorf("target %q: %w", target.Name, err)
	}
	target.Path = path
	return nil
}

func inactiveDependencyTarget(target Target) bool {
	return len(target.Kinds) > 0 && !slices.ContainsFunc(target.Kinds, func(kind string) bool {
		return !slices.Contains([]string{"bin", "example", "test", "bench"}, kind)
	})
}

func validateDependencies(repo repository.Repository, dependencies []Dependency) error {
	for index := range dependencies {
		dependency := &dependencies[index]
		if dependency.Name == "" || dependency.Requirement == "" {
			return errors.New("dependency declaration has no name or version requirement")
		}
		if dependency.Path != "" {
			path, err := containedPath(repo, filepath.Join(dependency.Path, "Cargo.toml"))
			if err != nil {
				return fmt.Errorf("dependency %q: %w", dependency.Name, err)
			}
			dependency.Path = filepath.ToSlash(filepath.Dir(path))
		}
	}
	return nil
}

func supportedEdition(edition string) bool {
	return slices.Contains([]string{"2015", "2018", "2021", "2024"}, edition)
}

func containedPath(repo repository.Repository, absolute string) (string, error) {
	if !filepath.IsAbs(absolute) {
		return "", errors.New("cargo reported a non-absolute input path")
	}
	relative, err := filepath.Rel(repo.Root, absolute)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return "", fmt.Errorf("cargo input %q escapes the repository", absolute)
	}
	path := filepath.ToSlash(relative)
	if _, err := repo.ReadContainedFile(path, 8<<20); err != nil {
		return path, fmt.Errorf("cargo input %q: %w", path, err)
	}
	return path, nil
}
