package pythonfacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

//go:embed runtime_loaders.py
var runtimeLoadersSource string

//go:embed runtime_loader_syntax.py
var runtimeLoaderSyntaxSource string

type RuntimeLoaderInput struct {
	Consumer     json.RawMessage `json:"consumer"`
	Check        RuntimeCheck    `json:"check"`
	InputGrammar string          `json:"inputGrammar"`
	Source       string          `json:"source"`
}

type RuntimeLoaderTarget struct {
	Path   string         `json:"path"`
	Module string         `json:"module"`
	Symbol string         `json:"symbol"`
	Site   SourceLocation `json:"site"`
}

type RuntimeLoaderResult struct {
	Error       string                `json:"error"`
	Targets     []RuntimeLoaderTarget `json:"targets"`
	RuntimeType string                `json:"runtimeType"`
	Identity    string                `json:"-"`
}

func ResolveRuntimeLoader(ctx context.Context, python string, modules []TypeModule, request RuntimeLoaderInput) (RuntimeLoaderResult, error) {
	header, err := json.Marshal(struct {
		Protocol string             `json:"protocol"`
		Count    int                `json:"count"`
		Request  RuntimeLoaderInput `json:"request"`
	}{"python-runtime-loader-project/v1", len(modules), request})
	if err != nil {
		return RuntimeLoaderResult{}, err
	}
	if len(header)+1 > maximumResponseSize || len(modules) > maximumProjectSources {
		return RuntimeLoaderResult{}, fmt.Errorf("runtime loader input exceeds its boundary")
	}
	input := &typeProjectReader{modules: modules, current: bytes.NewReader(append(header, '\n'))}
	program := ParserSupportSource() + TypeSupportSource() + ReachabilitySupportSource() + embeddedPythonModule("runtime_checks", runtimeChecksSource) + embeddedPythonModule("runtime_loader_syntax", runtimeLoaderSyntaxSource) + embeddedPythonModule("__main__", runtimeLoadersSource)
	digest := sha256.New()
	data, err := runFactProject(ctx, python, io.TeeReader(input, digest), program)
	if err != nil {
		return RuntimeLoaderResult{}, err
	}
	var result RuntimeLoaderResult
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF || result.Targets == nil || result.Error == "" && result.RuntimeType == "" {
		return result, fmt.Errorf("runtime loader response is incomplete")
	}
	if err := validateRuntimeLoaderTargets(modules, result.Targets); err != nil {
		return result, err
	}
	_, _ = digest.Write(data)
	result.Identity = hex.EncodeToString(digest.Sum(nil))
	return result, nil
}

func validateRuntimeLoaderTargets(modules []TypeModule, targets []RuntimeLoaderTarget) error {
	calls, err := objectImportCallInputs(modules)
	if err != nil {
		return err
	}
	for _, target := range targets {
		found := false
		for location, argument := range calls {
			if location.Path == target.Path && location.Site == target.Site && argument.Kind == "string" && argument.Value == target.Module+":"+target.Symbol {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("runtime loader target has no matching source call")
		}
	}
	return nil
}
