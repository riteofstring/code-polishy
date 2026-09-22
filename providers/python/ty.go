package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var tyRule = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

type tyExecutor interface {
	typecheck(context.Context, string, analysisScope, []string) ([]responseFinding, error)
}

type osTy struct {
	executable string
	config     string
}

type tyDiagnostic struct {
	CheckName   string `json:"check_name"`
	Description string `json:"description"`
	Location    struct {
		Path      string `json:"path"`
		Positions struct {
			Begin struct {
				Line   int `json:"line"`
				Column int `json:"column"`
			} `json:"begin"`
		} `json:"positions"`
	} `json:"location"`
}

func (runner osTy) typecheck(ctx context.Context, workspace string, scope analysisScope, files []string) ([]responseFinding, error) {
	if err := runner.validate(); err != nil {
		return nil, err
	}
	directory, relative, target, err := ruffFiles(workspace, scope, files)
	if err != nil {
		return nil, err
	}
	version, err := pythonVersionForTarget(target)
	if err != nil {
		return nil, err
	}
	data, err := decodePythonScopeData(scope.Data)
	if err != nil {
		return nil, err
	}
	searchPaths, err := pythonRelativeSourceRoots(scope, data)
	if err != nil {
		return nil, err
	}
	arguments := []string{
		"check", "--config-file", runner.config, "--project", ".", "--python-version", version,
		"--output-format", "gitlab", "--exit-zero", "--no-progress", "--color", "never",
		"--no-respect-ignore-files", "--no-force-exclude", "--include-scripts",
	}
	for _, searchPath := range searchPaths {
		if searchPath != "." {
			arguments = append(arguments, "--extra-search-path", searchPath)
		}
	}
	arguments = append(arguments, "--")
	output, err := executeTool(ctx, runner.executable, directory, append(arguments, relative...), nil)
	if err != nil {
		return nil, err
	}
	return parseTyFindings(workspace, scope, files, output)
}

func (runner osTy) validate() error {
	if strings.TrimSpace(runner.executable) == "" || !filepath.IsAbs(runner.executable) {
		return errors.New("sealed ty path must be absolute")
	}
	if strings.TrimSpace(runner.config) == "" || !filepath.IsAbs(runner.config) {
		return errors.New("ty configuration path must be absolute")
	}
	info, err := os.Stat(runner.config)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("ty configuration is unavailable")
	}
	return nil
}

func parseTyFindings(workspace string, scope analysisScope, files []string, data []byte) ([]responseFinding, error) {
	diagnostics, err := decodeTyDiagnostics(data)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, file := range files {
		allowed[file] = true
	}
	findings := make([]responseFinding, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		finding, err := tyFinding(workspace, scope, diagnostic, allowed)
		if err != nil {
			return nil, err
		}
		findings = append(findings, finding)
	}
	sortFindings(findings)
	return findings, nil
}

func decodeTyDiagnostics(data []byte) ([]tyDiagnostic, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	diagnostics := []tyDiagnostic{}
	if err := decoder.Decode(&diagnostics); err != nil {
		return nil, fmt.Errorf("decode ty output: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("ty output contains more than one JSON value")
	}
	if len(diagnostics) > 10000 {
		return nil, errors.New("ty output exceeds 10000 diagnostics")
	}
	return diagnostics, nil
}

func tyFinding(workspace string, scope analysisScope, diagnostic tyDiagnostic, allowed map[string]bool) (responseFinding, error) {
	rule := strings.TrimSpace(diagnostic.CheckName)
	location := diagnostic.Location.Positions.Begin
	if !tyRule.MatchString(rule) || location.Line < 1 || location.Column < 1 {
		return responseFinding{}, errors.New("ty returned an invalid diagnostic identity or location")
	}
	message := strings.TrimSpace(diagnostic.Description)
	if message == "" || len(message) > 4096 {
		return responseFinding{}, errors.New("ty returned an invalid diagnostic message")
	}
	file, err := ruffResultPath(workspace, scope, diagnostic.Location.Path)
	if err != nil {
		return responseFinding{}, err
	}
	if !allowed[file] {
		return responseFinding{}, fmt.Errorf("ty returned a diagnostic for source outside the checked scope: %s", file)
	}
	return responseFinding{
		Capability: "typecheck", Path: file, Line: location.Line, Column: location.Column,
		Subject: rule, Message: message, Rule: "ty." + rule,
	}, nil
}
