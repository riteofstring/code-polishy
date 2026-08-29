package agents

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	agentsTemplateRelativePath = "templates/AGENTS.md"
	claudeTemplateRelativePath = "templates/CLAUDE.md"
	agentsTargetFilename       = "AGENTS.md"
	claudeTargetFilename       = "CLAUDE.md"
	claudeRedirect             = "Read and follow `AGENTS.md` in the repository root for all project guidelines and workflows.\n"
)

type Status struct {
	Current bool
	Message string
}

func Install(repoRoot, policyRoot string) (string, error) {
	return install(repoRoot, policyRoot, os.Rename)
}

func install(repoRoot, policyRoot string, replace replacement) (string, error) {
	guidance, err := canonical(policyRoot)
	if err != nil {
		return "", err
	}
	agentsTarget, claudeTarget, err := readTargets(repoRoot)
	if err != nil {
		return "", err
	}
	agentsMutation, writesAgents, agentsMessage, err := planInstallAgents(agentsTarget, guidance.agents)
	if err != nil {
		return "", err
	}
	claudeMutation, writesClaude, claudeMessage, err := planClaude(claudeTarget, guidance.claude)
	if err != nil {
		return "", err
	}
	mutations := make([]mutation, 0, 2)
	if writesAgents {
		mutations = append(mutations, agentsMutation)
	}
	if writesClaude {
		mutations = append(mutations, claudeMutation)
	}
	if err := commitMutations(repoRoot, mutations, replace); err != nil {
		return "", err
	}
	return agentsMessage + "; " + claudeMessage, nil
}

func Sync(repoRoot, policyRoot string) (string, error) {
	return sync(repoRoot, policyRoot, os.Rename)
}

func sync(repoRoot, policyRoot string, replace replacement) (string, error) {
	guidance, err := canonical(policyRoot)
	if err != nil {
		return "", err
	}
	agentsTarget, claudeTarget, err := readTargets(repoRoot)
	if err != nil {
		return "", err
	}
	agentsMutation, writesAgents, agentsMessage, err := planSyncAgents(agentsTarget, guidance.agents)
	if err != nil {
		return "", err
	}
	claudeMutation, writesClaude, claudeMessage, err := planClaude(claudeTarget, guidance.claude)
	if err != nil {
		return "", err
	}
	mutations := make([]mutation, 0, 2)
	if writesAgents {
		mutations = append(mutations, agentsMutation)
	}
	if writesClaude {
		mutations = append(mutations, claudeMutation)
	}
	if err := commitMutations(repoRoot, mutations, replace); err != nil {
		return "", err
	}
	return agentsMessage + "; " + claudeMessage, nil
}

func Check(repoRoot, policyRoot string) Status {
	guidance, err := canonical(policyRoot)
	if err != nil {
		return Status{Message: err.Error()}
	}
	agentsTarget, agentsErr := readTarget(filepath.Join(repoRoot, agentsTargetFilename), agentsTargetFilename)
	claudeTarget, claudeErr := readTarget(filepath.Join(repoRoot, claudeTargetFilename), claudeTargetFilename)
	agentsStatus := checkAgents(agentsTarget, agentsErr, guidance.agents)
	claudeStatus := checkClaude(claudeTarget, claudeErr, guidance.claude)
	if agentsStatus.current && claudeStatus.current {
		return Status{Current: true, Message: agentsStatus.message + "; " + claudeStatus.message}
	}
	issues := make([]string, 0, 2)
	if !agentsStatus.current {
		issues = append(issues, agentsStatus.message)
	}
	if !claudeStatus.current {
		issues = append(issues, claudeStatus.message)
	}
	return Status{Message: strings.Join(issues, "; ")}
}

type canonicalGuidance struct {
	agents []byte
	claude []byte
}

func canonical(policyRoot string) (canonicalGuidance, error) {
	agentsPath := filepath.Join(policyRoot, filepath.FromSlash(agentsTemplateRelativePath))
	claudePath := filepath.Join(policyRoot, filepath.FromSlash(claudeTemplateRelativePath))
	agentsTemplate, agentsErr := os.ReadFile(agentsPath)
	claudeTemplate, claudeErr := os.ReadFile(claudePath)
	if agentsErr != nil {
		return canonicalGuidance{}, fmt.Errorf("read canonical AGENTS.md: %w", agentsErr)
	}
	if claudeErr != nil {
		return canonicalGuidance{}, fmt.Errorf("read canonical CLAUDE.md: %w", claudeErr)
	}
	if len(agentsTemplate) == 0 {
		return canonicalGuidance{}, errors.New("canonical AGENTS.md must not be empty")
	}
	if !bytes.Equal(claudeTemplate, []byte(claudeRedirect)) {
		return canonicalGuidance{}, errors.New("canonical CLAUDE.md must contain exactly the required one-line redirect")
	}
	return canonicalGuidance{
		agents: append([]byte{}, agentsTemplate...),
		claude: append([]byte{}, claudeTemplate...),
	}, nil
}

type targetState struct {
	exists   bool
	contents []byte
	mode     os.FileMode
}

func readTargets(repoRoot string) (targetState, targetState, error) {
	agentsTarget, agentsErr := readTarget(filepath.Join(repoRoot, agentsTargetFilename), agentsTargetFilename)
	claudeTarget, claudeErr := readTarget(filepath.Join(repoRoot, claudeTargetFilename), claudeTargetFilename)
	if agentsErr != nil {
		return targetState{}, targetState{}, agentsErr
	}
	if claudeErr != nil {
		return targetState{}, targetState{}, claudeErr
	}
	return agentsTarget, claudeTarget, nil
}

func readTarget(path, name string) (targetState, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return targetState{}, nil
	}
	if err != nil {
		return targetState{}, fmt.Errorf("read %s: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return targetState{}, fmt.Errorf("%s is not a regular file", name)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return targetState{}, fmt.Errorf("read %s: %w", name, err)
	}
	return targetState{exists: true, contents: contents, mode: info.Mode().Perm()}, nil
}

type mutation struct {
	path     string
	contents []byte
	mode     os.FileMode
	previous targetState
}

func planInstallAgents(existing targetState, template []byte) (mutation, bool, string, error) {
	path := agentsTargetFilename
	if !existing.exists {
		return mutation{path: path, contents: template, mode: 0o644, previous: existing}, true, "installed canonical AGENTS.md", nil
	}
	if matchesCanonicalGuidance(existing.contents, template) {
		return mutation{}, false, "AGENTS.md canonical guidance is already current", nil
	}
	return mutation{}, false, "", errors.New("AGENTS.md conflicts with canonical guidance; its bytes were preserved")
}

func planSyncAgents(existing targetState, template []byte) (mutation, bool, string, error) {
	if !existing.exists {
		return mutation{}, false, "", fmt.Errorf("read AGENTS.md: %w", os.ErrNotExist)
	}
	if matchesCanonicalGuidance(existing.contents, template) {
		return mutation{}, false, "AGENTS.md canonical guidance is already current", nil
	}
	return mutation{
		path: agentsTargetFilename, contents: template, mode: existing.mode, previous: existing,
	}, true, "synchronized AGENTS.md canonical guidance", nil
}

func planClaude(existing targetState, template []byte) (mutation, bool, string, error) {
	if !existing.exists {
		return mutation{
			path: claudeTargetFilename, contents: template, mode: 0o644, previous: existing,
		}, true, "installed canonical CLAUDE.md redirect", nil
	}
	if matchesCanonicalGuidance(existing.contents, template) {
		return mutation{}, false, "CLAUDE.md redirect is already current", nil
	}
	return mutation{}, false, "", errors.New("CLAUDE.md conflicts with the canonical redirect; its bytes were preserved")
}

type checkStatus struct {
	current bool
	message string
}

func checkAgents(existing targetState, readErr error, template []byte) checkStatus {
	if readErr != nil || !existing.exists {
		return checkStatus{message: "AGENTS.md is missing or unreadable; run `code-polishy agents install`"}
	}
	if !matchesCanonicalGuidance(existing.contents, template) {
		return checkStatus{message: "AGENTS.md canonical guidance is stale; run `code-polishy agents sync`"}
	}
	return checkStatus{current: true, message: "AGENTS.md canonical guidance is current"}
}

func checkClaude(existing targetState, readErr error, template []byte) checkStatus {
	if readErr != nil {
		return checkStatus{message: "CLAUDE.md conflicts with the canonical redirect or is unreadable; preserve its bytes and resolve the conflict"}
	}
	if !existing.exists {
		return checkStatus{message: "CLAUDE.md is missing; run `code-polishy agents sync` after AGENTS.md is current"}
	}
	if !matchesCanonicalGuidance(existing.contents, template) {
		return checkStatus{message: "CLAUDE.md conflicts with the canonical redirect; preserve its bytes and resolve the conflict"}
	}
	return checkStatus{current: true, message: "CLAUDE.md redirect is current"}
}

func matchesCanonicalGuidance(existing, template []byte) bool {
	if bytes.Equal(existing, template) {
		return true
	}
	if bytes.Contains(template, []byte("\r")) {
		return false
	}
	return bytes.Equal(existing, bytes.ReplaceAll(template, []byte("\n"), []byte("\r\n")))
}

type replacement func(oldPath, newPath string) error

type stagedMutation struct {
	mutation mutation
	incoming string
	backup   string
}

func commitMutations(repoRoot string, mutations []mutation, replace replacement) error {
	staged := make([]stagedMutation, 0, len(mutations))
	for _, planned := range mutations {
		incoming, err := stageTemporary(repoRoot, planned.contents, planned.mode)
		if err != nil {
			cleanStaged(staged)
			return fmt.Errorf("stage %s: %w", planned.path, err)
		}
		item := stagedMutation{mutation: planned, incoming: incoming}
		if planned.previous.exists {
			backup, err := stageTemporary(repoRoot, planned.previous.contents, planned.previous.mode)
			if err != nil {
				_ = os.Remove(incoming)
				cleanStaged(staged)
				return fmt.Errorf("stage rollback for %s: %w", planned.path, err)
			}
			item.backup = backup
		}
		staged = append(staged, item)
	}
	defer cleanStaged(staged)
	for index := range staged {
		item := &staged[index]
		target := filepath.Join(repoRoot, item.mutation.path)
		if err := replace(item.incoming, target); err != nil {
			if rollbackErr := rollback(staged[:index], repoRoot, replace); rollbackErr != nil {
				return fmt.Errorf("replace %s: %w; rollback: %v", item.mutation.path, err, rollbackErr)
			}
			return fmt.Errorf("replace %s: %w", item.mutation.path, err)
		}
		item.incoming = ""
	}
	return nil
}

func stageTemporary(repoRoot string, contents []byte, mode os.FileMode) (string, error) {
	temporary, err := os.CreateTemp(repoRoot, ".code-polishy-agents-*")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	remove = false
	return temporaryPath, nil
}

func rollback(staged []stagedMutation, repoRoot string, replace replacement) error {
	for index := len(staged) - 1; index >= 0; index-- {
		item := &staged[index]
		target := filepath.Join(repoRoot, item.mutation.path)
		if item.mutation.previous.exists {
			if err := replace(item.backup, target); err != nil {
				return fmt.Errorf("restore %s: %w", item.mutation.path, err)
			}
			item.backup = ""
			continue
		}
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", item.mutation.path, err)
		}
	}
	return nil
}

func cleanStaged(staged []stagedMutation) {
	for _, item := range staged {
		if item.incoming != "" {
			_ = os.Remove(item.incoming)
		}
		if item.backup != "" {
			_ = os.Remove(item.backup)
		}
	}
}
