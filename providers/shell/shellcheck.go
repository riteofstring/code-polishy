package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const maximumShellCheckOutput = 8 << 20
const maximumShellCheckError = 64 << 10

type osShellCheck struct {
	executable string
}

type shellCheckOutput struct {
	Comments []shellCheckDiagnostic `json:"comments"`
}

type shellCheckDiagnostic struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Level   string `json:"level"`
	Code    int    `json:"code"`
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

func (checker osShellCheck) check(ctx context.Context, inputs map[string][]byte, selected []shellSource) ([]responseFinding, error) {
	if strings.TrimSpace(checker.executable) == "" || !filepath.IsAbs(checker.executable) {
		return nil, errors.New("sealed ShellCheck path must be absolute")
	}
	root, err := os.MkdirTemp("", "code-polishy-shell-pack-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	for name, data := range inputs {
		target := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			return nil, err
		}
	}
	byDialect := map[shellDialect][]string{}
	for _, source := range selected {
		byDialect[source.Dialect] = append(byDialect[source.Dialect], source.Path)
	}
	findings := []responseFinding{}
	for _, dialect := range []shellDialect{dialectBash, dialectPOSIX} {
		paths := uniqueSorted(byDialect[dialect])
		if len(paths) == 0 {
			continue
		}
		found, err := checker.run(ctx, root, dialect, paths)
		if err != nil {
			return nil, err
		}
		findings = append(findings, found...)
	}
	return findings, nil
}

func (checker osShellCheck) run(ctx context.Context, root string, dialect shellDialect, paths []string) ([]responseFinding, error) {
	arguments, allowed := shellCheckArguments(root, dialect, paths)
	data, err := executeShellCheck(ctx, checker.executable, root, arguments)
	if err != nil {
		return nil, err
	}
	return shellCheckFindings(root, allowed, data)
}

func shellCheckArguments(root string, dialect shellDialect, paths []string) ([]string, map[string]bool) {
	arguments := []string{"--norc", "--external-sources", "--format=json1", "--severity=style", "--shell=" + string(dialect)}
	allowed := map[string]bool{}
	for _, name := range paths {
		allowed[name] = true
		arguments = append(arguments, filepath.Join(root, filepath.FromSlash(name)))
	}
	return arguments, allowed
}

func executeShellCheck(ctx context.Context, executable, root string, arguments []string) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = root
	command.Env = os.Environ()
	stdout := &boundedBuffer{limit: maximumShellCheckOutput}
	stderr := &boundedBuffer{limit: maximumShellCheckError}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if stdout.exceeded || stderr.exceeded {
		return nil, errors.New("ShellCheck output exceeded its bounded stream limit")
	}
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
			message := strings.TrimSpace(stderr.buffer.String())
			if message == "" {
				message = err.Error()
			}
			return nil, fmt.Errorf("ShellCheck failed: %s", message)
		}
	}
	return stdout.buffer.Bytes(), nil
}

func shellCheckFindings(root string, allowed map[string]bool, data []byte) ([]responseFinding, error) {
	output := shellCheckOutput{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&output); err != nil {
		return nil, fmt.Errorf("decode ShellCheck output: %w", err)
	}
	findings := []responseFinding{}
	for _, diagnostic := range output.Comments {
		name, err := shellCheckPath(root, diagnostic.File)
		if err != nil {
			return nil, err
		}
		if !allowed[name] {
			continue
		}
		if diagnostic.Line < 1 || diagnostic.Column < 1 || diagnostic.Code < 1 || strings.TrimSpace(diagnostic.Message) == "" {
			return nil, errors.New("ShellCheck returned an invalid diagnostic")
		}
		message := diagnostic.Message
		if len(message) > 4096 {
			message = message[:4096]
		}
		code := "SC" + strconv.Itoa(diagnostic.Code)
		findings = append(findings, responseFinding{
			Capability: "lint", Path: name, Line: diagnostic.Line, Column: diagnostic.Column,
			Subject: code, Message: message, Rule: "shellcheck.sc" + strconv.Itoa(diagnostic.Code),
		})
	}
	sortFindings(findings)
	return findings, nil
}

func shellCheckPath(root, name string) (string, error) {
	if !filepath.IsAbs(name) {
		name = filepath.Join(root, name)
	}
	relative, err := filepath.Rel(root, name)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("ShellCheck returned a diagnostic outside its materialized context")
	}
	normalized := filepath.ToSlash(filepath.Clean(relative))
	if err := exactPath(normalized); err != nil {
		return "", err
	}
	return normalized, nil
}
