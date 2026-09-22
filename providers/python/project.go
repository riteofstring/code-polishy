package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const projectProtocol = "code-polishy-python-project/v1"

type projectExecutor interface {
	inspect(context.Context, request, []projectInput) (map[string]projectFact, []inputFile, error)
}

type osPythonProject struct {
	executable string
	script     string
}

type projectInput struct {
	Path     string `json:"path"`
	Manifest string `json:"manifest"`
	Kind     string `json:"kind"`
	Source   string `json:"source"`
}

type projectRequest struct {
	Protocol string         `json:"protocol"`
	Inputs   []projectInput `json:"inputs"`
}

type projectResponse struct {
	Protocol string        `json:"protocol"`
	Projects []projectFact `json:"projects"`
}

type projectFact struct {
	Manifest       string           `json:"manifest"`
	RequiresPython string           `json:"requiresPython"`
	TargetVersion  string           `json:"targetVersion"`
	BackendPaths   []string         `json:"backendPaths"`
	Problems       []projectProblem `json:"problems"`
}

type projectProblem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (runner osPythonProject) inspect(ctx context.Context, request request, inputs []projectInput) (map[string]projectFact, []inputFile, error) {
	if err := runner.validate(request); err != nil {
		return nil, nil, err
	}
	if len(inputs) == 0 || len(inputs) > 4096 {
		return nil, nil, errors.New("python project discovery requires 1 to 4096 metadata inputs")
	}
	inputs, identities, err := readProjectInputs(request, inputs)
	if err != nil {
		return nil, nil, err
	}
	data, err := json.Marshal(projectRequest{Protocol: projectProtocol, Inputs: inputs})
	if err != nil {
		return nil, nil, err
	}
	output, err := executeTool(ctx, runner.executable, request.ProjectRoot, []string{"-I", "-B", runner.script}, data)
	if err != nil {
		return nil, nil, err
	}
	projects, err := decodeProjectResponse(output, inputs)
	return projects, identities, err
}

func readProjectInputs(request request, inputs []projectInput) ([]projectInput, []inputFile, error) {
	expected, err := contextIdentities(request.Context)
	if err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(request.ProjectRoot)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	identities := make([]inputFile, 0, len(inputs))
	for index := range inputs {
		data, digest, err := readVerifiedInput(root, inputs[index].Path, expected[inputs[index].Path])
		if err != nil {
			return nil, nil, err
		}
		if !utf8.Valid(data) || len(data) > 2<<20 {
			return nil, nil, fmt.Errorf("python project input %s must be valid UTF-8 of at most 2 MiB", inputs[index].Path)
		}
		inputs[index].Source = string(data)
		identities = append(identities, inputFile{Path: inputs[index].Path, SHA256: digest})
	}
	return inputs, identities, nil
}

func (runner osPythonProject) validate(request request) error {
	if !hasTool(request.Tools, "python", "python", "3.12.13+20260728") {
		return errors.New("python discovery does not bind CPython 3.12.13+20260728")
	}
	if strings.TrimSpace(runner.executable) == "" || !filepath.IsAbs(runner.executable) {
		return errors.New("sealed python path must be absolute")
	}
	if strings.TrimSpace(runner.script) == "" || !filepath.IsAbs(runner.script) {
		return errors.New("python project helper path must be absolute")
	}
	info, err := os.Stat(runner.script)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("python project helper is unavailable")
	}
	return nil
}

func decodeProjectResponse(data []byte, inputs []projectInput) (map[string]projectFact, error) {
	response, err := parseProjectResponse(data)
	if err != nil {
		return nil, err
	}
	wanted, allowed := projectInputAuthority(inputs)
	if len(response.Projects) != len(wanted) {
		return nil, errors.New("python project facts have the wrong project count")
	}
	return indexProjectFacts(response.Projects, wanted, allowed)
}

func parseProjectResponse(data []byte) (projectResponse, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	response := projectResponse{}
	if err := decoder.Decode(&response); err != nil {
		return projectResponse{}, fmt.Errorf("decode python project facts: %w", err)
	}
	if err := requireEnd(decoder); err != nil {
		return projectResponse{}, err
	}
	if response.Protocol != projectProtocol {
		return projectResponse{}, errors.New("python project facts have the wrong protocol identity")
	}
	return response, nil
}

func projectInputAuthority(inputs []projectInput) (map[string]bool, map[string]map[string]bool) {
	wanted := map[string]bool{}
	allowed := map[string]map[string]bool{}
	for _, input := range inputs {
		if allowed[input.Manifest] == nil {
			allowed[input.Manifest] = map[string]bool{}
		}
		allowed[input.Manifest][input.Path] = true
		if input.Kind == "manifest" {
			wanted[input.Manifest] = true
		}
	}
	return wanted, allowed
}

func indexProjectFacts(values []projectFact, wanted map[string]bool, allowed map[string]map[string]bool) (map[string]projectFact, error) {
	projects := map[string]projectFact{}
	for _, project := range values {
		if !wanted[project.Manifest] || projects[project.Manifest].Manifest != "" {
			return nil, errors.New("python project facts contain an unknown or repeated manifest")
		}
		if err := validateProjectFact(project, allowed[project.Manifest]); err != nil {
			return nil, err
		}
		projects[project.Manifest] = project
	}
	return projects, nil
}

func validateProjectFact(project projectFact, allowed map[string]bool) error {
	if project.BackendPaths == nil || project.Problems == nil {
		return fmt.Errorf("python project facts for %s omit explicit collections", project.Manifest)
	}
	for _, backend := range project.BackendPaths {
		if err := exactProjectDirectory(backend); err != nil {
			return fmt.Errorf("python project backend path: %w", err)
		}
	}
	for _, problem := range project.Problems {
		if !allowed[problem.Path] || strings.TrimSpace(problem.Message) == "" || len(problem.Message) > 4096 {
			return errors.New("python project facts contain an invalid problem")
		}
	}
	if len(project.Problems) != 0 {
		return nil
	}
	if project.RequiresPython == "" {
		return errors.New("python project facts omit the interpreter requirement")
	}
	_, err := pythonVersionForTarget(project.TargetVersion)
	return err
}

func exactProjectDirectory(value string) error {
	if value == "." {
		return nil
	}
	if err := exactPath(value); err != nil {
		return err
	}
	if strings.HasSuffix(value, "/") {
		return fmt.Errorf("%q is not an exact project directory", value)
	}
	return nil
}
