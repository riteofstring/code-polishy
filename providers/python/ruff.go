package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const maximumToolOutput = 8 << 20
const maximumToolError = 64 << 10
const ruffBaselineSelection = "B,C4,E,F,I,PIE,RUF,SIM,UP"

type ruffExecutor interface {
	format(context.Context, string, analysisScope, string, []byte) ([]byte, error)
	lint(context.Context, string, analysisScope, []string, map[string]bool) ([]responseFinding, error)
}

type osRuff struct {
	executable string
}

type ruffDiagnostic struct {
	Code     string `json:"code"`
	Filename string `json:"filename"`
	Location struct {
		Row    int `json:"row"`
		Column int `json:"column"`
	} `json:"location"`
	Message string `json:"message"`
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	remaining := buffer.limit - buffer.buffer.Len()
	if len(data) > remaining {
		buffer.exceeded = true
		if remaining > 0 {
			_, _ = buffer.buffer.Write(data[:remaining])
		}
		return len(data), nil
	}
	return buffer.buffer.Write(data)
}

func (runner osRuff) format(ctx context.Context, workspace string, scope analysisScope, file string, source []byte) ([]byte, error) {
	if err := runner.validate(); err != nil {
		return nil, err
	}
	directory, relative, target, err := ruffLocation(workspace, scope, file)
	if err != nil {
		return nil, err
	}
	arguments := []string{
		"format", "--no-cache", "--target-version", target, "--line-length", "88",
		"--stdin-filename", relative, "-",
	}
	return executeTool(ctx, runner.executable, directory, arguments, source)
}

func (runner osRuff) lint(ctx context.Context, workspace string, scope analysisScope, files []string, generated map[string]bool) ([]responseFinding, error) {
	if err := runner.validate(); err != nil {
		return nil, err
	}
	directory, relative, target, err := ruffFiles(workspace, scope, files)
	if err != nil {
		return nil, err
	}
	baseline := []string{
		"check", "--no-cache", "--no-fix", "--isolated", "--target-version", target,
		"--config", "line-length = 88", "--config", "lint.pycodestyle.max-line-length = 88",
		"--select", ruffBaselineSelection, "--ignore-noqa", "--no-respect-gitignore", "--no-force-exclude",
		"--output-format", "json", "--exit-zero", "--",
	}
	targetRules := []string{
		"check", "--no-cache", "--no-fix", "--target-version", target,
		"--config", "line-length = 88", "--config", "lint.pycodestyle.max-line-length = 88",
		"--no-respect-gitignore", "--no-force-exclude", "--output-format", "json", "--exit-zero", "--",
	}
	baseline = append(baseline, relative...)
	targetRules = append(targetRules, relative...)
	outputs := make([][]byte, 0, 2)
	for _, arguments := range [][]string{baseline, targetRules} {
		output, runErr := executeTool(ctx, runner.executable, directory, arguments, nil)
		if runErr != nil {
			return nil, runErr
		}
		outputs = append(outputs, output)
	}
	return parseRuffFindings(workspace, scope, files, generated, outputs)
}

func (runner osRuff) validate() error {
	if strings.TrimSpace(runner.executable) == "" || !filepath.IsAbs(runner.executable) {
		return errors.New("sealed Ruff path must be absolute")
	}
	return nil
}

func ruffFiles(workspace string, scope analysisScope, files []string) (string, []string, string, error) {
	directory := filepath.Join(workspace, filepath.FromSlash(scope.Root))
	target, err := ruffTarget(scope.Data)
	if err != nil {
		return "", nil, "", err
	}
	relative := make([]string, 0, len(files))
	for _, file := range files {
		_, name, _, locationErr := ruffLocation(workspace, scope, file)
		if locationErr != nil {
			return "", nil, "", locationErr
		}
		relative = append(relative, name)
	}
	return directory, relative, target, nil
}

func ruffLocation(workspace string, scope analysisScope, file string) (string, string, string, error) {
	directory := filepath.Join(workspace, filepath.FromSlash(scope.Root))
	relative, err := filepath.Rel(directory, filepath.Join(workspace, filepath.FromSlash(file)))
	if err != nil {
		return "", "", "", err
	}
	relative = filepath.ToSlash(relative)
	if relative == "." || relative == ".." || strings.HasPrefix(relative, "../") {
		return "", "", "", fmt.Errorf("python source %s escapes scope root %s", file, scope.Root)
	}
	target, err := ruffTarget(scope.Data)
	if err != nil {
		return "", "", "", err
	}
	return directory, relative, target, nil
}

func ruffTarget(data json.RawMessage) (string, error) {
	value := pythonScopeData{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("decode python scope data: %w", err)
	}
	if value.TargetVersion != "py312" {
		return "", errors.New("python scope does not bind the supported Ruff target py312")
	}
	return value.TargetVersion, nil
}

func executeTool(ctx context.Context, executable, directory string, arguments []string, stdin []byte) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = directory
	command.Env = os.Environ()
	if stdin != nil {
		command.Stdin = bytes.NewReader(stdin)
	}
	stdout := &boundedBuffer{limit: maximumToolOutput}
	stderr := &boundedBuffer{limit: maximumToolError}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if stdout.exceeded || stderr.exceeded {
		return nil, errors.New("python analyzer output exceeded its bounded stream limit")
	}
	if err != nil {
		message := strings.TrimSpace(stderr.buffer.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("python analyzer failed: %s", message)
	}
	return stdout.buffer.Bytes(), nil
}

func parseRuffFindings(workspace string, scope analysisScope, files []string, generated map[string]bool, outputs [][]byte) ([]responseFinding, error) {
	allowed := map[string]bool{}
	for _, file := range files {
		allowed[file] = true
	}
	findings := []responseFinding{}
	seen := map[string]bool{}
	for _, output := range outputs {
		diagnostics, err := decodeRuffDiagnostics(output)
		if err != nil {
			return nil, err
		}
		for _, diagnostic := range diagnostics {
			finding, err := ruffFinding(workspace, scope, diagnostic, allowed)
			if err != nil {
				return nil, err
			}
			if generated[finding.Path] && cosmeticRuffRule(diagnostic.Code) {
				continue
			}
			identity := fmt.Sprintf("%s\x00%d\x00%d\x00%s\x00%s", finding.Path, finding.Line, finding.Column, finding.Rule, finding.Message)
			if !seen[identity] {
				seen[identity] = true
				findings = append(findings, finding)
			}
		}
	}
	sortFindings(findings)
	return findings, nil
}

func decodeRuffDiagnostics(data []byte) ([]ruffDiagnostic, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	diagnostics := []ruffDiagnostic{}
	if err := decoder.Decode(&diagnostics); err != nil {
		return nil, fmt.Errorf("decode ruff output: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("ruff output contains more than one JSON value")
	}
	if len(diagnostics) > 10000 {
		return nil, errors.New("ruff output exceeds 10000 diagnostics")
	}
	return diagnostics, nil
}

func ruffFinding(workspace string, scope analysisScope, diagnostic ruffDiagnostic, allowed map[string]bool) (responseFinding, error) {
	if diagnostic.Code == "" || len(diagnostic.Code) > 128 || diagnostic.Location.Row < 1 || diagnostic.Location.Column < 1 {
		return responseFinding{}, errors.New("ruff returned an invalid diagnostic identity or location")
	}
	message := strings.TrimSpace(diagnostic.Message)
	if message == "" || len(message) > 4096 {
		return responseFinding{}, errors.New("ruff returned an invalid diagnostic message")
	}
	file, err := ruffResultPath(workspace, scope, diagnostic.Filename)
	if err != nil {
		return responseFinding{}, err
	}
	if !allowed[file] {
		return responseFinding{}, fmt.Errorf("ruff returned a diagnostic for unselected source %s", file)
	}
	return responseFinding{
		Capability: "lint", Path: file, Line: diagnostic.Location.Row, Column: diagnostic.Location.Column,
		Subject: diagnostic.Code, Message: message, Rule: "ruff." + strings.ToLower(diagnostic.Code),
	}, nil
}

func ruffResultPath(workspace string, scope analysisScope, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.New("ruff returned an empty diagnostic path")
	}
	var absolute string
	if filepath.IsAbs(filepath.FromSlash(value)) {
		absolute = filepath.Clean(filepath.FromSlash(value))
	} else {
		absolute = filepath.Join(workspace, filepath.FromSlash(scope.Root), filepath.FromSlash(value))
	}
	relative, err := filepath.Rel(workspace, absolute)
	if err != nil {
		return "", err
	}
	relative = filepath.ToSlash(relative)
	if err := exactPath(relative); err != nil {
		return "", fmt.Errorf("ruff diagnostic path: %w", err)
	}
	return relative, nil
}

func cosmeticRuffRule(code string) bool {
	family := strings.TrimRight(code, "0123456789")
	if family != code && slices.Contains([]string{"I", "Q", "D", "N"}, family) {
		return true
	}
	if strings.HasPrefix(code, "E2") || strings.HasPrefix(code, "E3") {
		return true
	}
	return slices.Contains([]string{
		"COM812", "COM819", "E111", "E114", "E115", "E116", "E401", "E501", "E502",
		"E701", "E702", "E703", "E704", "E741", "E742", "E743", "W191", "W291", "W292", "W293",
	}, code)
}
