package repository

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
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
		manifest, resolveErr := repo.goLocalModuleManifest(workspace, use.Path)
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

func (repo Repository) goLocalModuleManifest(control, declared string) (string, error) {
	normalized := strings.ReplaceAll(declared, "\\", "/")
	if windowsAbsolutePath(normalized) && !filepath.IsAbs(filepath.FromSlash(normalized)) {
		return "", fmt.Errorf("path is absolute for another platform")
	}
	native := filepath.FromSlash(normalized)
	target := native
	if !filepath.IsAbs(native) {
		target = filepath.Join(repo.Root, filepath.FromSlash(filepath.Dir(control)), native)
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

type GoBuild struct {
	Workspace string
	Sources   []GoBuildSource
}

type GoBuildSource struct {
	Root  string
	Paths []string
}

type goLocalReplacement struct {
	path     string
	manifest string
}

func (repo Repository) GoBuild(files []string, main GoModule, modules []GoModule) GoBuild {
	build := GoBuild{Sources: []GoBuildSource{}}
	controls := []string{main.Manifest}
	declared := map[string]bool{main.Root: true}
	if workspace, found := goEnclosingWorkspace(files, main); found {
		if members := repo.goWorkspaceUses(workspace); slices.Contains(members, main.Manifest) {
			build.Workspace = workspace
			controls = append([]string{workspace}, members...)
			for _, member := range members {
				declared[filepath.ToSlash(filepath.Dir(member))] = true
			}
		}
	}
	replaced := map[string]map[string]bool{}
	for _, control := range controls {
		for _, replacement := range repo.goLocalReplacements(control) {
			root := filepath.ToSlash(filepath.Dir(replacement.manifest))
			if replaced[root] == nil {
				replaced[root] = map[string]bool{}
			}
			replaced[root][replacement.path] = true
		}
	}
	for _, module := range modules {
		paths := map[string]bool{}
		if declared[module.Root] {
			paths[module.Name] = true
		}
		maps.Copy(paths, replaced[module.Root])
		if len(paths) > 0 {
			build.Sources = append(build.Sources, GoBuildSource{Root: module.Root, Paths: sortedSet(paths)})
		}
	}
	slices.SortFunc(build.Sources, func(left, right GoBuildSource) int { return strings.Compare(left.Root, right.Root) })
	return build
}

func goEnclosingWorkspace(files []string, main GoModule) (string, bool) {
	_, workspaces := goModuleControlPaths(files)
	nearest := ""
	for _, workspace := range workspaces {
		directory := filepath.ToSlash(filepath.Dir(workspace))
		encloses := directory == "." || main.Root == directory || strings.HasPrefix(main.Root, directory+"/")
		if encloses && (nearest == "" || len(directory) > len(filepath.Dir(nearest))) {
			nearest = workspace
		}
	}
	return nearest, nearest != ""
}

func (repo Repository) goWorkspaceUses(workspace string) []string {
	data, err := repo.Read(workspace)
	if err != nil {
		return nil
	}
	parsed, err := modfile.ParseWork(workspace, data, nil)
	if err != nil {
		return nil
	}
	manifests := []string{}
	for _, use := range parsed.Use {
		if manifest, err := repo.goLocalModuleManifest(workspace, use.Path); err == nil {
			manifests = append(manifests, manifest)
		}
	}
	return manifests
}

func (repo Repository) goLocalReplacements(control string) []goLocalReplacement {
	data, err := repo.Read(control)
	if err != nil {
		return nil
	}
	var replacements []*modfile.Replace
	if filepath.Base(control) == "go.work" {
		parsed, err := modfile.ParseWork(control, data, nil)
		if err != nil {
			return nil
		}
		replacements = parsed.Replace
	} else {
		parsed, err := modfile.Parse(control, data, nil)
		if err != nil {
			return nil
		}
		replacements = parsed.Replace
	}
	local := []goLocalReplacement{}
	for _, replacement := range replacements {
		if replacement.New.Version != "" || !modfile.IsDirectoryPath(replacement.New.Path) {
			continue
		}
		if manifest, err := repo.goLocalModuleManifest(control, replacement.New.Path); err == nil {
			local = append(local, goLocalReplacement{path: replacement.Old.Path, manifest: manifest})
		}
	}
	return local
}

func windowsAbsolutePath(path string) bool {
	return len(path) >= 3 && (path[0] >= 'A' && path[0] <= 'Z' || path[0] >= 'a' && path[0] <= 'z') && path[1] == ':' && path[2] == '/'
}

func goModuleProblem(path, prefix string, err error) GoModuleProblem {
	return GoModuleProblem{Path: path, Message: prefix + ": " + err.Error()}
}
