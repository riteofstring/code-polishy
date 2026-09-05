package pythonfacts

import (
	"encoding/json"
	"fmt"
)

type objectImportCallSite struct {
	Path    string
	Site    SourceLocation
	EndSite SourceLocation
}

type objectImportCallArgument struct {
	Kind    string         `json:"kind"`
	Value   string         `json:"value"`
	Text    string         `json:"text"`
	Site    SourceLocation `json:"site"`
	EndSite SourceLocation `json:"endSite"`
}

func objectImportCallInputs(modules []TypeModule) (map[objectImportCallSite]objectImportCallArgument, error) {
	result := map[objectImportCallSite]objectImportCallArgument{}
	for _, module := range modules {
		var facts struct {
			Calls []struct {
				Site      SourceLocation             `json:"site"`
				EndSite   SourceLocation             `json:"endSite"`
				Arguments []objectImportCallArgument `json:"arguments"`
			} `json:"calls"`
		}
		if err := json.Unmarshal(module.Facts, &facts); err != nil {
			return nil, err
		}
		for _, call := range facts.Calls {
			argument := objectImportCallArgument{}
			if len(call.Arguments) > 0 {
				argument = call.Arguments[0]
			}
			result[objectImportCallSite{Path: module.Path, Site: call.Site, EndSite: call.EndSite}] = argument
		}
	}
	return result, nil
}

func validateObjectImportCalls(modules []TypeModule, imports []ObjectImport) error {
	inputs, err := objectImportCallInputs(modules)
	if err != nil {
		return err
	}
	for _, imported := range imports {
		argument, found := inputs[objectImportCallSite{Path: imported.Path, Site: imported.Site, EndSite: imported.EndSite}]
		if !found || argument.Text != imported.Argument {
			return fmt.Errorf("object import evidence does not match one source call argument")
		}
		if err := validateObjectImportArgument(argument, imported); err != nil {
			return err
		}
	}
	return nil
}

func validateObjectImportArgument(argument objectImportCallArgument, imported ObjectImport) error {
	if imported.Error != "" {
		if imported.Registry != nil || imported.Guard != nil {
			return fmt.Errorf("unproven object import has input evidence")
		}
		return nil
	}
	if imported.Guard != nil {
		if argument.Kind != "name" {
			return fmt.Errorf("guarded object input is not a source parameter")
		}
		return nil
	}
	if argument.Kind == "string" {
		if imported.Registry != nil || len(imported.Targets) != 1 || imported.Targets[0].Module+":"+imported.Targets[0].Symbol != argument.Value {
			return fmt.Errorf("literal object import evidence substitutes the source target")
		}
	} else if imported.Registry == nil {
		return fmt.Errorf("nonliteral object import has no exact registry evidence")
	}
	return nil
}
