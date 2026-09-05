package pythonfacts

import (
	"encoding/json"
	"fmt"
	"slices"
)

type ModuleImport struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Literal bool   `json:"literal"`
	ComputedImport
}

type moduleImportCall struct {
	Site      SourceLocation             `json:"site"`
	EndSite   SourceLocation             `json:"endSite"`
	Shape     string                     `json:"shape"`
	Arguments []objectImportCallArgument `json:"arguments"`
	Keywords  []json.RawMessage          `json:"keywords"`
}

func validateModuleImports(imports []ModuleImport, modules []TypeModule) error {
	inputs := map[objectImportCallSite]moduleImportCall{}
	for _, module := range modules {
		var facts struct {
			Calls []moduleImportCall `json:"calls"`
		}
		if err := json.Unmarshal(module.Facts, &facts); err != nil {
			return err
		}
		for _, call := range facts.Calls {
			inputs[objectImportCallSite{Path: module.Path, Site: call.Site, EndSite: call.EndSite}] = call
		}
	}
	seen := map[objectImportCallSite]bool{}
	for _, imported := range imports {
		key := objectImportCallSite{Path: imported.Path, Site: SourceLocation{Line: imported.Line, Column: imported.Column}, EndSite: SourceLocation{Line: imported.EndLine, Column: imported.EndColumn}}
		call, found := inputs[key]
		if !found || seen[key] || !validModuleImport(imported, call) {
			return fmt.Errorf("module import evidence does not match one current bounded source call")
		}
		seen[key] = true
	}
	return nil
}

func validModuleImport(imported ModuleImport, call moduleImportCall) bool {
	argument := objectImportCallArgument{}
	if len(call.Arguments) > 0 {
		argument = call.Arguments[0]
	}
	if !validModuleImportIdentity(imported, call, argument.Text) || !validModuleImportCollections(imported) {
		return false
	}
	if !validModuleImportScope(imported) {
		return false
	}
	if imported.EvidenceError != "" {
		return !imported.Literal && len(imported.Targets) == 0 && len(imported.Configuration) == 0 && imported.EntryPointGroup == ""
	}
	return validModuleImportTargets(imported, call, argument)
}

func validModuleImportIdentity(imported ModuleImport, call moduleImportCall, argument string) bool {
	return imported.Argument == argument && len(imported.Argument) <= 4096 && imported.Shape != "" && imported.Shape == call.Shape && len(imported.Shape) <= 16384 && imported.Callable != ""
}

func validModuleImportCollections(imported ModuleImport) bool {
	return imported.Targets != nil && imported.Configuration != nil && len(imported.Targets) <= 4096 && len(imported.Configuration) <= 4096 && len(imported.EvidenceError) <= 4096
}

func validModuleImportScope(imported ModuleImport) bool {
	return (imported.Callee == "importlib.import_module" || imported.Callee == "builtins.__import__") && (imported.Kind == "type-only" || imported.Kind == "proven-dynamic")
}

func validModuleImportTargets(imported ModuleImport, call moduleImportCall, argument objectImportCallArgument) bool {
	if len(call.Arguments) != 1 || len(call.Keywords) != 0 {
		return false
	}
	if imported.Literal {
		return argument.Kind == "string" && slices.Equal(imported.Targets, []string{argument.Value}) && len(imported.Configuration) == 0 && imported.EntryPointGroup == ""
	}
	return argument.Kind != "string" && len(imported.Targets)+len(imported.Configuration)+len(imported.EntryPointGroup) > 0
}
