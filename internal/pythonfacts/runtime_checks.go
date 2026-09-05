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
	"path/filepath"
	"slices"
	"strings"
)

//go:embed runtime_checks.py
var runtimeChecksSource string

const runtimeCheckProtocol = "python-runtime-check-project/v1"

type RuntimeCheck struct {
	Kind     string         `json:"kind"`
	Protocol string         `json:"protocol"`
	Site     SourceLocation `json:"site"`
}

type RuntimeCheckInput struct {
	ID       string          `json:"id"`
	Consumer json.RawMessage `json:"consumer"`
	Check    RuntimeCheck    `json:"check"`
}

type RuntimeCheckEvidence struct {
	ID            string                   `json:"id"`
	Identity      string                   `json:"identity"`
	Importer      string                   `json:"importer"`
	LoaderSite    SourceLocation           `json:"loaderSite"`
	LoaderEndSite SourceLocation           `json:"loaderEndSite"`
	CheckSite     SourceLocation           `json:"checkSite"`
	CheckEndSite  SourceLocation           `json:"checkEndSite"`
	Kind          string                   `json:"kind"`
	Contract      string                   `json:"contract"`
	RuntimeType   string                   `json:"runtimeType"`
	Bindings      []ReachabilityDefinition `json:"bindings"`
}

type RuntimeCheckProblem struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type RuntimeCheckProject struct {
	Evidence []RuntimeCheckEvidence
	Problems []RuntimeCheckProblem
	Identity string
}

func ResolveRuntimeChecks(ctx context.Context, python string, modules []TypeModule, requests []RuntimeCheckInput) (RuntimeCheckProject, error) {
	ordered := slices.Clone(modules)
	slices.SortFunc(ordered, func(left, right TypeModule) int { return strings.Compare(left.Path, right.Path) })
	if len(ordered) > maximumProjectSources || !filepath.IsAbs(python) {
		return RuntimeCheckProject{}, fmt.Errorf("runtime check project source count or interpreter is invalid")
	}
	header, err := json.Marshal(struct {
		Protocol string              `json:"protocol"`
		Count    int                 `json:"count"`
		Requests []RuntimeCheckInput `json:"requests"`
	}{runtimeCheckProtocol, len(ordered), append([]RuntimeCheckInput{}, requests...)})
	if err != nil {
		return RuntimeCheckProject{}, err
	}
	if len(header)+1 > maximumResponseSize {
		return RuntimeCheckProject{}, fmt.Errorf("runtime check header exceeds its request boundary")
	}
	input := &typeProjectReader{modules: ordered, current: bytes.NewReader(append(header, '\n'))}
	digest := sha256.New()
	program := TypeSupportSource() + ReachabilitySupportSource() + embeddedPythonModule("__main__", runtimeChecksSource)
	data, err := runFactProject(ctx, python, io.TeeReader(input, digest), program)
	if err != nil {
		return RuntimeCheckProject{}, err
	}
	result, err := decodeRuntimeCheckProject(data, ordered, requests)
	if err != nil {
		return RuntimeCheckProject{}, err
	}
	_, _ = digest.Write(data)
	result.Identity = hex.EncodeToString(digest.Sum(nil))
	return result, nil
}

func decodeRuntimeCheckProject(data []byte, modules []TypeModule, requests []RuntimeCheckInput) (RuntimeCheckProject, error) {
	var wire struct {
		Protocol string                 `json:"protocol"`
		Covered  []typeCoverage         `json:"covered"`
		Evidence []RuntimeCheckEvidence `json:"evidence"`
		Problems []RuntimeCheckProblem  `json:"problems"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return RuntimeCheckProject{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return RuntimeCheckProject{}, fmt.Errorf("runtime check output has trailing data")
	}
	if wire.Protocol != runtimeCheckProtocol || wire.Evidence == nil || wire.Problems == nil {
		return RuntimeCheckProject{}, fmt.Errorf("runtime check output is incomplete")
	}
	if _, err := projectCoveredFiles(wire.Covered, modules); err != nil {
		return RuntimeCheckProject{}, err
	}
	result := RuntimeCheckProject{Evidence: wire.Evidence, Problems: wire.Problems}
	return result, validateRuntimeCheckProject(modules, requests, result)
}
