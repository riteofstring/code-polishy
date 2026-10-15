package repository

import (
	"fmt"
	"maps"
	"slices"
)

type CargoDependencyDeclaration struct {
	Table     string
	Alias     string
	Package   string
	Version   string
	Path      string
	Git       string
	Revision  string
	Branch    string
	Tag       string
	Registry  string
	Workspace bool
	Optional  bool
	Fields    []string
}

type cargoDependencyTables struct {
	Dependencies      map[string]any `toml:"dependencies"`
	DevDependencies   map[string]any `toml:"dev-dependencies"`
	BuildDependencies map[string]any `toml:"build-dependencies"`
}

type cargoWorkspaceDocument struct {
	CargoWorkspaceManifest
	Dependencies map[string]any `toml:"dependencies"`
}

type cargoManifestDocument struct {
	Package   *CargoPackageManifest   `toml:"package"`
	Workspace *cargoWorkspaceDocument `toml:"workspace"`
	cargoDependencyTables
	Target  map[string]cargoDependencyTables `toml:"target"`
	Patch   map[string]map[string]any        `toml:"patch"`
	Replace map[string]any                   `toml:"replace"`
}

func cargoDependencyDeclarations(document cargoManifestDocument) ([]CargoDependencyDeclaration, error) {
	declarations, err := cargoTableDeclarations("", document.cargoDependencyTables)
	if err != nil {
		return nil, err
	}
	for _, target := range slices.Sorted(maps.Keys(document.Target)) {
		entries, err := cargoTableDeclarations("target."+target+".", document.Target[target])
		if err != nil {
			return nil, err
		}
		declarations = append(declarations, entries...)
	}
	if document.Workspace != nil {
		entries, err := cargoDeclarationTable("workspace.dependencies", document.Workspace.Dependencies)
		if err != nil {
			return nil, err
		}
		declarations = append(declarations, entries...)
	}
	for _, registry := range slices.Sorted(maps.Keys(document.Patch)) {
		entries, err := cargoDeclarationTable("patch."+registry, document.Patch[registry])
		if err != nil {
			return nil, err
		}
		declarations = append(declarations, entries...)
	}
	replaced, err := cargoDeclarationTable("replace", document.Replace)
	return append(declarations, replaced...), err
}

func cargoTableDeclarations(prefix string, tables cargoDependencyTables) ([]CargoDependencyDeclaration, error) {
	declarations := []CargoDependencyDeclaration{}
	for _, table := range []struct {
		name    string
		entries map[string]any
	}{{"dependencies", tables.Dependencies}, {"dev-dependencies", tables.DevDependencies}, {"build-dependencies", tables.BuildDependencies}} {
		entries, err := cargoDeclarationTable(prefix+table.name, table.entries)
		if err != nil {
			return nil, err
		}
		declarations = append(declarations, entries...)
	}
	return declarations, nil
}

func cargoDeclarationTable(table string, entries map[string]any) ([]CargoDependencyDeclaration, error) {
	declarations := []CargoDependencyDeclaration{}
	for _, alias := range slices.Sorted(maps.Keys(entries)) {
		declaration, err := cargoDeclaration(table, alias, entries[alias])
		if err != nil {
			return nil, fmt.Errorf("cargo dependency %s.%s: %w", table, alias, err)
		}
		declarations = append(declarations, declaration)
	}
	return declarations, nil
}

func cargoDeclaration(table, alias string, value any) (CargoDependencyDeclaration, error) {
	declaration := CargoDependencyDeclaration{Table: table, Alias: alias, Package: alias}
	if version, ok := value.(string); ok {
		declaration.Version, declaration.Fields = version, []string{"version"}
		return declaration, nil
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return declaration, fmt.Errorf("declaration must be a version string or table")
	}
	declaration.Fields = slices.Sorted(maps.Keys(fields))
	text := map[string]*string{
		"package": &declaration.Package, "version": &declaration.Version, "path": &declaration.Path,
		"git": &declaration.Git, "rev": &declaration.Revision, "branch": &declaration.Branch,
		"tag": &declaration.Tag, "registry": &declaration.Registry,
	}
	for _, name := range declaration.Fields {
		if destination := text[name]; destination != nil {
			value, ok := fields[name].(string)
			if !ok {
				return declaration, fmt.Errorf("%s must be a string", name)
			}
			*destination = value
			continue
		}
		if err := cargoDependencyOption(&declaration, name, fields[name]); err != nil {
			return declaration, err
		}
	}
	return declaration, nil
}

func cargoDependencyOption(declaration *CargoDependencyDeclaration, name string, value any) error {
	switch name {
	case "workspace", "optional", "default-features":
		enabled, ok := value.(bool)
		if !ok {
			return fmt.Errorf("%s must be a boolean", name)
		}
		if name == "workspace" {
			declaration.Workspace = enabled
			if !enabled {
				return fmt.Errorf("workspace dependency inheritance must be true")
			}
		}
		if name == "optional" {
			declaration.Optional = enabled
		}
	case "features":
		features, ok := value.([]any)
		if !ok {
			return fmt.Errorf("dependency features must be an array of strings")
		}
		for _, feature := range features {
			if _, ok := feature.(string); !ok {
				return fmt.Errorf("dependency features must be strings")
			}
		}
	}
	return nil
}
