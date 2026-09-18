package agents

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	posixWrapperTemplateRelativePath      = "templates/code-polishyw"
	powerShellWrapperTemplateRelativePath = "templates/code-polishyw.ps1"
	posixWrapperTargetFilename            = "code-polishyw"
	powerShellWrapperTargetFilename       = "code-polishyw.ps1"
)

type wrapperTemplate struct {
	path     string
	contents []byte
	mode     os.FileMode
	marker   []byte
}

func canonicalWrappers(policyRoot string) ([]wrapperTemplate, error) {
	definitions := []wrapperTemplate{
		{
			path:   posixWrapperTargetFilename,
			mode:   0o755,
			marker: []byte("#!/usr/bin/env bash\nCODE_POLISHY_MANAGED_WRAPPER=1\n"),
		},
		{
			path:   powerShellWrapperTargetFilename,
			mode:   0o644,
			marker: []byte("$CodePolishyManagedWrapper = $true\n"),
		},
	}
	relativePaths := []string{posixWrapperTemplateRelativePath, powerShellWrapperTemplateRelativePath}
	for index, relativePath := range relativePaths {
		contents, err := os.ReadFile(filepath.Join(policyRoot, filepath.FromSlash(relativePath)))
		if err != nil {
			return nil, fmt.Errorf("read canonical %s: %w", definitions[index].path, err)
		}
		if !bytes.HasPrefix(contents, definitions[index].marker) {
			return nil, fmt.Errorf("canonical %s has no managed-wrapper marker", definitions[index].path)
		}
		definitions[index].contents = append([]byte{}, contents...)
	}
	return definitions, nil
}

func readWrapperTargets(repoRoot string) ([]targetState, error) {
	targets := make([]targetState, 0, 2)
	for _, name := range []string{posixWrapperTargetFilename, powerShellWrapperTargetFilename} {
		target, err := readTarget(filepath.Join(repoRoot, name), name)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func checkWrapperTargets(repoRoot string) ([]targetState, []error) {
	targets := make([]targetState, 0, 2)
	errorsByTarget := make([]error, 0, 2)
	for _, name := range []string{posixWrapperTargetFilename, powerShellWrapperTargetFilename} {
		target, err := readTarget(filepath.Join(repoRoot, name), name)
		targets = append(targets, target)
		errorsByTarget = append(errorsByTarget, err)
	}
	return targets, errorsByTarget
}

func planWrappers(existing []targetState, templates []wrapperTemplate, changedMessage string) ([]mutation, string, error) {
	mutations := make([]mutation, 0, len(templates))
	for index, template := range templates {
		current := existing[index]
		if wrapperCurrent(current, template) {
			continue
		}
		if current.exists && !managedWrapper(current.contents, template.marker) {
			return nil, "", fmt.Errorf("%s conflicts with the canonical wrapper; its bytes were preserved", template.path)
		}
		mutations = append(mutations, mutation{
			path: template.path, contents: template.contents, mode: template.mode, previous: current,
		})
	}
	if len(mutations) == 0 {
		return nil, "Code Polishy wrappers are already current", nil
	}
	return mutations, changedMessage, nil
}

func checkWrapper(existing targetState, readErr error, template wrapperTemplate) checkStatus {
	if readErr != nil {
		return failedStatus("policy.bootstrapWrapper", template.path, "managed-wrapper", fmt.Sprintf("%s is unreadable or non-regular; preserve its bytes and resolve the conflict", template.path))
	}
	if !existing.exists {
		return failedStatus("policy.bootstrapWrapper", template.path, "managed-wrapper", fmt.Sprintf("%s is missing; run `code-polishy agents sync`", template.path))
	}
	if wrapperCurrent(existing, template) {
		return checkStatus{current: true, message: template.path + " is current"}
	}
	if managedWrapper(existing.contents, template.marker) {
		return failedStatus("policy.bootstrapWrapper", template.path, "managed-wrapper", fmt.Sprintf("%s is stale; run `code-polishy agents sync`", template.path))
	}
	return failedStatus("policy.bootstrapWrapper", template.path, "managed-wrapper", fmt.Sprintf("%s conflicts with the canonical wrapper; preserve its bytes and resolve the conflict", template.path))
}

func wrapperCurrent(existing targetState, template wrapperTemplate) bool {
	modeCurrent := runtime.GOOS == "windows" || existing.mode == template.mode
	return existing.exists && modeCurrent && bytes.Equal(existing.contents, template.contents)
}

func managedWrapper(contents, marker []byte) bool {
	return bytes.HasPrefix(bytes.ReplaceAll(contents, []byte("\r\n"), []byte("\n")), marker)
}
