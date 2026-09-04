package repository

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

type GoModuleInventory struct {
	Modules  []GoModule
	Problems []GoModuleProblem
}

type GoModuleProblem struct {
	Path    string
	Message string
}

func (repo Repository) InspectGoModules(files []string) GoModuleInventory {
	manifestPaths, workspacePaths := goModuleControlPaths(files)
	inventory := GoModuleInventory{}
	moduleByManifest := map[string]int{}
	manifestsByName := map[string][]string{}
	for _, manifest := range manifestPaths {
		data, err := repo.Read(manifest)
		if err != nil {
			inventory.Problems = append(inventory.Problems, goModuleProblem(manifest, "Go module manifest cannot be read", err))
			continue
		}
		parsed, err := modfile.Parse(manifest, data, nil)
		if err != nil {
			inventory.Problems = append(inventory.Problems, goModuleProblem(manifest, "Go module manifest is malformed", err))
			continue
		}
		if parsed.Module == nil || parsed.Module.Mod.Path == "" {
			inventory.Problems = append(inventory.Problems, GoModuleProblem{Path: manifest, Message: "Go module manifest has no module declaration"})
			continue
		}
		if err := module.CheckImportPath(parsed.Module.Mod.Path); err != nil {
			inventory.Problems = append(inventory.Problems, goModuleProblem(manifest, "Go module identity is malformed", err))
			continue
		}
		module := GoModule{Root: filepath.ToSlash(filepath.Dir(manifest)), Manifest: manifest, Name: parsed.Module.Mod.Path}
		moduleByManifest[manifest] = len(inventory.Modules)
		inventory.Modules = append(inventory.Modules, module)
		manifestsByName[module.Name] = append(manifestsByName[module.Name], manifest)
	}
	for name, manifests := range manifestsByName {
		if len(manifests) < 2 {
			continue
		}
		sort.Strings(manifests)
		inventory.Problems = append(inventory.Problems, GoModuleProblem{
			Path: manifests[0], Message: fmt.Sprintf("Go module identity %q is declared by multiple manifests: %s", name, strings.Join(manifests, ", ")),
		})
	}
	workspaceByManifest := map[string]string{}
	for _, workspace := range workspacePaths {
		problems := repo.inspectGoWorkspace(workspace, moduleByManifest, workspaceByManifest)
		inventory.Problems = append(inventory.Problems, problems...)
	}
	sort.Slice(inventory.Modules, func(left, right int) bool {
		if len(inventory.Modules[left].Root) != len(inventory.Modules[right].Root) {
			return len(inventory.Modules[left].Root) > len(inventory.Modules[right].Root)
		}
		return inventory.Modules[left].Manifest < inventory.Modules[right].Manifest
	})
	sort.Slice(inventory.Problems, func(left, right int) bool {
		return inventory.Problems[left].Path+"\x00"+inventory.Problems[left].Message < inventory.Problems[right].Path+"\x00"+inventory.Problems[right].Message
	})
	return inventory
}

func (repo Repository) GoModules(files []string) []GoModule {
	return repo.InspectGoModules(files).Modules
}

func goModuleControlPaths(files []string) ([]string, []string) {
	manifestSet := map[string]bool{}
	workspaceSet := map[string]bool{}
	for _, candidate := range files {
		path := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(candidate)), "./")
		switch filepath.Base(path) {
		case "go.mod":
			manifestSet[path] = true
		case "go.work":
			workspaceSet[path] = true
		}
	}
	manifests := sortedSet(manifestSet)
	workspaces := sortedSet(workspaceSet)
	return manifests, workspaces
}

func (repo Repository) inspectGoWorkspace(
	workspace string,
	moduleByManifest map[string]int,
	workspaceByManifest map[string]string,
) []GoModuleProblem {
	data, err := repo.Read(workspace)
	if err != nil {
		return []GoModuleProblem{goModuleProblem(workspace, "Go workspace manifest cannot be read", err)}
	}
	parsed, err := modfile.ParseWork(workspace, data, nil)
	if err != nil {
		return []GoModuleProblem{goModuleProblem(workspace, "Go workspace manifest is malformed", err)}
	}
	problems := []GoModuleProblem{}
	seen := map[string]bool{}
	for _, use := range parsed.Use {
		manifest, resolveErr := repo.goWorkspaceUseManifest(workspace, use.Path)
		if resolveErr != nil {
			problems = append(problems, goModuleProblem(workspace, fmt.Sprintf("Go workspace use path %q is invalid", use.Path), resolveErr))
			continue
		}
		if seen[manifest] {
			problems = append(problems, GoModuleProblem{Path: workspace, Message: fmt.Sprintf("Go workspace repeats module manifest %q", manifest)})
			continue
		}
		seen[manifest] = true
		if _, found := moduleByManifest[manifest]; !found {
			problems = append(problems, GoModuleProblem{Path: workspace, Message: fmt.Sprintf("Go workspace use path %q does not resolve to one parsed repository module", use.Path)})
			continue
		}
		if previous, ambiguous := workspaceByManifest[manifest]; ambiguous && previous != workspace {
			problems = append(problems, GoModuleProblem{
				Path: workspace, Message: fmt.Sprintf("Go module manifest %q is owned by both %q and %q", manifest, previous, workspace),
			})
			continue
		}
		workspaceByManifest[manifest] = workspace
	}
	return problems
}

func (repo Repository) goWorkspaceUseManifest(workspace, declared string) (string, error) {
	normalized := strings.ReplaceAll(declared, "\\", "/")
	if windowsAbsolutePath(normalized) && !filepath.IsAbs(filepath.FromSlash(normalized)) {
		return "", fmt.Errorf("path is absolute for another platform")
	}
	native := filepath.FromSlash(normalized)
	target := native
	if !filepath.IsAbs(native) {
		target = filepath.Join(repo.Root, filepath.FromSlash(filepath.Dir(workspace)), native)
	}
	manifest, err := repo.NormalizePath(filepath.Join(target, "go.mod"))
	if err != nil {
		return "", err
	}
	if _, err := repo.Resolve(manifest); err != nil {
		return "", err
	}
	return manifest, nil
}

func windowsAbsolutePath(path string) bool {
	return len(path) >= 3 && (path[0] >= 'A' && path[0] <= 'Z' || path[0] >= 'a' && path[0] <= 'z') && path[1] == ':' && path[2] == '/'
}

func goModuleProblem(path, prefix string, err error) GoModuleProblem {
	return GoModuleProblem{Path: path, Message: prefix + ": " + err.Error()}
}
