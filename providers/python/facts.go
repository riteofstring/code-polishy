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
	"strings"
)

type factExecutor interface {
	comments(context.Context, string, []string) (factResult, error)
}

type osPythonFacts struct {
	executable string
	script     string
}

type factResult struct {
	Comments []commentFact
	Failures map[string]string
}

type factRequest struct {
	Files []string `json:"files"`
}

type factResponse struct {
	Comments []commentFact `json:"comments"`
	Failures []factFailure `json:"failures"`
}

type factFailure struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

func (runner osPythonFacts) comments(ctx context.Context, workspace string, files []string) (factResult, error) {
	if err := runner.validate(); err != nil {
		return factResult{}, err
	}
	input, err := json.Marshal(factRequest{Files: files})
	if err != nil {
		return factResult{}, err
	}
	command := exec.CommandContext(ctx, runner.executable, "-I", "-B", runner.script)
	command.Dir = workspace
	command.Env = os.Environ()
	command.Stdin = bytes.NewReader(input)
	stdout := &boundedBuffer{limit: maximumToolOutput}
	stderr := &boundedBuffer{limit: maximumToolError}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.buffer.String())
		if message == "" {
			message = err.Error()
		}
		return factResult{}, fmt.Errorf("python source-fact adapter failed: %s", message)
	}
	if stdout.exceeded || stderr.exceeded {
		return factResult{}, errors.New("python source-fact output exceeded its bounded stream limit")
	}
	return parseFactResponse(stdout.buffer.Bytes(), files)
}

func (runner osPythonFacts) validate() error {
	if strings.TrimSpace(runner.executable) == "" || !filepath.IsAbs(runner.executable) {
		return errors.New("sealed CPython path must be absolute")
	}
	if strings.TrimSpace(runner.script) == "" || !filepath.IsAbs(runner.script) {
		return errors.New("python source-fact script path must be absolute")
	}
	info, err := os.Stat(runner.script)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("python source-fact script is unavailable")
	}
	return nil
}

func parseFactResponse(data []byte, files []string) (factResult, error) {
	value, err := decodeFactResponse(data)
	if err != nil {
		return factResult{}, err
	}
	allowed := factAllowedPaths(files)
	failures, err := validatedFactFailures(value.Failures, allowed)
	if err != nil {
		return factResult{}, err
	}
	comments, err := validatedFactComments(value.Comments, allowed, failures)
	if err != nil {
		return factResult{}, err
	}
	return factResult{Comments: comments, Failures: failures}, nil
}

func decodeFactResponse(data []byte) (factResponse, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	value := factResponse{}
	if err := decoder.Decode(&value); err != nil {
		return factResponse{}, fmt.Errorf("decode python source facts: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return factResponse{}, errors.New("python source facts contain more than one JSON value")
	}
	return value, nil
}

func factAllowedPaths(files []string) map[string]bool {
	allowed := map[string]bool{}
	for _, file := range files {
		allowed[file] = true
	}
	return allowed
}

func validatedFactFailures(values []factFailure, allowed map[string]bool) (map[string]string, error) {
	failures := map[string]string{}
	for _, failure := range values {
		if !allowed[failure.Path] || strings.TrimSpace(failure.Reason) == "" || len(failure.Reason) > 4096 || failures[failure.Path] != "" {
			return nil, errors.New("python source facts contain an invalid failure")
		}
		failures[failure.Path] = failure.Reason
	}
	return failures, nil
}

func validatedFactComments(values []commentFact, allowed map[string]bool, failures map[string]string) ([]commentFact, error) {
	comments := []commentFact{}
	for _, comment := range values {
		if !allowed[comment.Path] || failures[comment.Path] != "" {
			continue
		}
		if comment.Line < 1 || comment.Column < 1 || comment.Raw == "" || len(comment.Raw) > 65536 {
			return nil, errors.New("python source facts contain an invalid comment")
		}
		comments = append(comments, comment)
	}
	return comments, nil
}
