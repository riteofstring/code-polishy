package pack

import (
	"path"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type AnalysisUnit struct {
	ID            string   `json:"id"`
	Root          string   `json:"root"`
	PackageRoot   string   `json:"packageRoot"`
	Manifest      string   `json:"manifest"`
	WorkspaceRoot string   `json:"workspaceRoot"`
	Configuration string   `json:"configuration"`
	Members       []string `json:"members"`
	EntryFiles    []string `json:"entryFiles"`
}

type LintActivation struct {
	ReactHooks       bool `json:"reactHooks"`
	JSXAccessibility bool `json:"jsxAccessibility"`
}

type unitInventory struct {
	packages       map[string]string
	configurations map[string]string
}

func newUnitInventory(paths []string) unitInventory {
	inventory := unitInventory{packages: map[string]string{}, configurations: map[string]string{}}
	candidates := map[string][]string{}
	for _, file := range paths {
		directory, name := path.Dir(file), path.Base(file)
		if name == "package.json" {
			inventory.packages[directory] = file
		}
		if javaScriptConfiguration(file) {
			candidates[directory] = append(candidates[directory], file)
		}
	}
	for directory, files := range candidates {
		for _, preferred := range []string{"tsconfig.json", "jsconfig.json"} {
			if file := path.Join(directory, preferred); slices.Contains(files, file) {
				inventory.configurations[directory] = file
				break
			}
		}
		if inventory.configurations[directory] == "" && len(files) == 1 {
			inventory.configurations[directory] = files[0]
		}
	}
	return inventory
}

func javaScriptConfiguration(file string) bool {
	name := path.Base(file)
	return name == "jsconfig.json" || strings.HasPrefix(name, "tsconfig") && strings.HasSuffix(name, ".json")
}

func nearestUnitValue(values map[string]string, file string) string {
	for directory := path.Dir(file); ; directory = path.Dir(directory) {
		if value := values[directory]; value != "" {
			return value
		}
		if directory == "." {
			return ""
		}
	}
}

func (inventory unitInventory) unit(repo repository.Repository, file string) AnalysisUnit {
	context := repo.JavaScriptContextPath(file)
	manifest := nearestUnitValue(inventory.packages, context)
	configuration := nearestUnitValue(inventory.configurations, context)
	root := path.Dir(manifest)
	if configuration != "" {
		root = path.Dir(configuration)
	}
	unit := AnalysisUnit{Root: root, PackageRoot: path.Dir(manifest), Manifest: manifest, Configuration: configuration, Members: []string{}, EntryFiles: []string{}}
	unit.WorkspaceRoot = unit.PackageRoot
	for directory := unit.PackageRoot; directory != "."; {
		directory = path.Dir(directory)
		if inventory.packages[directory] != "" {
			unit.WorkspaceRoot = directory
		}
	}
	unit.ID = repo.Language(file) + ":" + manifest + ":" + configuration
	return unit
}

func prepareUnits(repo repository.Repository, request *Request, paths []string) []string {
	inventory := newUnitInventory(paths)
	units := map[string]*AnalysisUnit{}
	sources := []SourceInput{}
	selectedUnits := map[string]bool{}
	for _, file := range paths {
		if repo.Language(file) == "" || repo.IsData(file) {
			continue
		}
		unit := inventory.unit(repo, file)
		if units[unit.ID] == nil {
			units[unit.ID] = &unit
		}
		source := sourceInput(repo, request, file, unit.ID)
		sources = append(sources, source)
		if source.Owner == request.Provider && request.Provider != "" || slices.Contains(request.Files, file) {
			units[unit.ID].Members = append(units[unit.ID].Members, file)
			if coreEntryPoint(repo, unit.PackageRoot, file) {
				units[unit.ID].EntryFiles = append(units[unit.ID].EntryFiles, file)
			}
		}
		if slices.Contains(request.Files, file) {
			selectedUnits[unit.ID] = true
		}
	}
	return selectUnitContext(request, paths, units, sources, selectedUnits)
}

func sourceInput(repo repository.Repository, request *Request, file, unit string) SourceInput {
	source := SourceInput{Path: file, Language: repo.Language(file), Test: repo.IsTest(file), Generated: repo.IsGenerated(file), Development: repo.IsDevelopment(file), Unit: unit}
	source.Owner = repo.AnalysisOwner(file, request.Capability, request.Profile).Name
	if !packCapabilitySelects(repo, request.Capability, file) {
		source.Owner = ""
	}
	if declaration, found := repo.GeneratedJavaScriptOwner(file); found {
		source.SourcePackage = declaration.SourcePackage
	}
	activation := repo.JavaScriptLintActivation(file)
	source.Lint = LintActivation{ReactHooks: activation.ReactHooks, JSXAccessibility: activation.JSXAccessibility}
	return source
}

func selectUnitContext(request *Request, paths []string, units map[string]*AnalysisUnit, sources []SourceInput, selected map[string]bool) []string {
	wide := slices.Contains([]string{"architecture", "typecheck", "dead-code"}, request.Capability)
	context := slices.Clone(request.Files)
	initializeRequestScope(request)
	for _, key := range sortedUnitKeys(units) {
		unit := units[key]
		if !selected[key] && (!wide || len(unit.Members) == 0) {
			continue
		}
		appendRequestUnit(request, *unit, wide)
		if selected[key] || request.Capability == "dead-code" {
			context = append(context, unitContext(paths, *unit, wide)...)
		}
	}
	appendSourceContext(request, sources, units, wide)
	request.DiagnosticFiles = sortedUnique(request.DiagnosticFiles)
	return sortedUnique(context)
}

func sortedUnitKeys(units map[string]*AnalysisUnit) []string {
	keys := make([]string, 0, len(units))
	for key := range units {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func unitContext(paths []string, unit AnalysisUnit, wide bool) []string {
	context := []string{}
	if wide {
		context = append(context, unit.Members...)
	}
	for _, file := range paths {
		name := path.Base(file)
		metadata := name == "package.json" || javaScriptConfiguration(file) || name == "pnpm-workspace.yaml" || strings.HasPrefix(name, "astro.config.")
		if metadata && (pathWithin(unit.Root, path.Dir(file)) || pathWithin(unit.PackageRoot, path.Dir(file))) {
			context = append(context, file)
		}
	}
	return context
}

func pathWithin(file, root string) bool {
	return root == "." || file == root || strings.HasPrefix(file, root+"/")
}

func coreEntryPoint(repo repository.Repository, root, file string) bool {
	if repo.IsTest(file) || policy.MatchesAny(file, repo.Config.Scope.EntryPoints) {
		return true
	}
	directory, name := path.Dir(file), path.Base(file)
	stem := strings.TrimSuffix(name, path.Ext(name))
	entry := slices.Contains([]string{"cli", "index", "main"}, stem)
	return directory == root && (entry || strings.HasSuffix(stem, ".config")) || directory == path.Join(root, "src") && entry
}

func initializeRequestScope(request *Request) {
	request.Units = []AnalysisUnit{}
	request.DiagnosticFiles = slices.Clone(request.Files)
	request.WriteFiles = []string{}
	if request.Capability == "format" && request.Mode == "write" {
		request.WriteFiles = slices.Clone(request.Files)
	}
}

func appendRequestUnit(request *Request, unit AnalysisUnit, wide bool) {
	if wide {
		request.DiagnosticFiles = append(request.DiagnosticFiles, unit.Members...)
	} else {
		unit.Members = slices.DeleteFunc(slices.Clone(unit.Members), func(file string) bool { return !slices.Contains(request.Files, file) })
		unit.EntryFiles = []string{}
	}
	request.Units = append(request.Units, unit)
}

func appendSourceContext(request *Request, sources []SourceInput, units map[string]*AnalysisUnit, wide bool) {
	languages := map[string]bool{}
	for _, source := range sources {
		if len(units[source.Unit].Members) > 0 {
			languages[source.Language] = true
		}
	}
	for _, source := range sources {
		if wide && languages[source.Language] || slices.Contains(request.Files, source.Path) {
			request.Policy.Files = append(request.Policy.Files, source)
		}
	}
}
