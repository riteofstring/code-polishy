package agents

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	agentsTemplateRelativePath = "templates/AGENTS.md"
	agentsTargetFilename       = "AGENTS.md"
	claudeTargetFilename       = "CLAUDE.md"
	ignoreTargetFilename       = ".gitignore"
	reportsIgnorePattern       = "/.code-polishy-reports/"
	testArtifactsIgnorePattern = "/.code-polishy-artifacts/"
	reportsDirectoryPath       = ".code-polishy-reports/"
	testArtifactsDirectoryPath = ".code-polishy-artifacts/"
	legacyClaudeImport         = "@AGENTS.md\n"
	legacyClaudeRedirect       = "Read and follow `AGENTS.md` in the repository root for all project guidelines and workflows.\n"
)

type Issue struct {
	Check   string
	Path    string
	Subject string
	Message string
}

type Status struct {
	Current bool
	Message string
	Issues  []Issue
}

func Install(repoRoot, policyRoot string) (string, error) {
	return install(repoRoot, policyRoot, os.Rename)
}

func install(repoRoot, policyRoot string, replace replacement) (string, error) {
	guidance, err := canonical(policyRoot)
	if err != nil {
		return "", err
	}
	agentsTarget, ignoreTarget, err := readTargets(repoRoot)
	if err != nil {
		return "", err
	}
	wrapperTargets, err := readWrapperTargets(repoRoot)
	if err != nil {
		return "", err
	}
	agentsMutation, writesAgents, agentsMessage, err := planInstallAgents(agentsTarget, guidance.agents)
	if err != nil {
		return "", err
	}
	wrapperMutations, wrapperMessage, err := planWrappers(wrapperTargets, guidance.wrappers, "installed canonical Code Polishy wrappers")
	if err != nil {
		return "", err
	}
	claudeMutation, removesClaude, claudeMessage := planClaudeCleanup(readClaudeCleanupTarget(repoRoot))
	ignoreCurrent, err := reportArtifactsIgnored(repoRoot, ignoreTarget.contents)
	if err != nil {
		return "", err
	}
	ignoreMutation, writesIgnore, ignoreMessage := planReportIgnore(ignoreTarget, ignoreCurrent)
	mutations := adoptionMutations(
		optionalMutation{agentsMutation, writesAgents},
		optionalMutation{claudeMutation, removesClaude},
		wrapperMutations,
		optionalMutation{ignoreMutation, writesIgnore},
	)
	if err := commitMutations(repoRoot, mutations, replace); err != nil {
		return "", err
	}
	return joinOperationMessages(agentsMessage, claudeMessage, wrapperMessage, ignoreMessage), nil
}

func Sync(repoRoot, policyRoot string) (string, error) {
	return sync(repoRoot, policyRoot, os.Rename)
}

func SyncWithLock(repoRoot, policyRoot string, expectedLock, incomingLock []byte) (string, error) {
	return syncWithLock(repoRoot, policyRoot, expectedLock, incomingLock, os.Rename)
}

func sync(repoRoot, policyRoot string, replace replacement) (string, error) {
	guidance, err := canonical(policyRoot)
	if err != nil {
		return "", err
	}
	wrapperTargets, err := readWrapperTargets(repoRoot)
	if err != nil {
		return "", err
	}
	agentsTarget, ignoreTarget, err := readTargets(repoRoot)
	if err != nil {
		return "", err
	}
	wrapperMutations, wrapperMessage, err := planWrappers(wrapperTargets, guidance.wrappers, "synchronized canonical Code Polishy wrappers")
	if err != nil {
		return "", err
	}
	agentsMutation, writesAgents, agentsMessage, err := planSyncAgents(agentsTarget, guidance.agents)
	if err != nil {
		return "", err
	}
	claudeMutation, removesClaude, claudeMessage := planClaudeCleanup(readClaudeCleanupTarget(repoRoot))
	ignoreCurrent, err := reportArtifactsIgnored(repoRoot, ignoreTarget.contents)
	if err != nil {
		return "", err
	}
	ignoreMutation, writesIgnore, ignoreMessage := planReportIgnore(ignoreTarget, ignoreCurrent)
	mutations := adoptionMutations(
		optionalMutation{agentsMutation, writesAgents},
		optionalMutation{claudeMutation, removesClaude},
		wrapperMutations,
		optionalMutation{ignoreMutation, writesIgnore},
	)
	if err := commitMutations(repoRoot, mutations, replace); err != nil {
		return "", err
	}
	return joinOperationMessages(agentsMessage, claudeMessage, wrapperMessage, ignoreMessage), nil
}

func syncWithLock(repoRoot, policyRoot string, expectedLock, incomingLock []byte, replace replacement) (string, error) {
	guidance, err := canonical(policyRoot)
	if err != nil {
		return "", err
	}
	wrapperTargets, err := readWrapperTargets(repoRoot)
	if err != nil {
		return "", err
	}
	agentsTarget, ignoreTarget, err := readTargets(repoRoot)
	if err != nil {
		return "", err
	}
	lockMutation, err := planUpgradeLock(repoRoot, expectedLock, incomingLock)
	if err != nil {
		return "", err
	}
	wrapperMutations, wrapperMessage, err := planWrappers(wrapperTargets, guidance.wrappers, "synchronized canonical Code Polishy wrappers")
	if err != nil {
		return "", err
	}
	agentsMutation, writesAgents, agentsMessage, err := planSyncAgents(agentsTarget, guidance.agents)
	if err != nil {
		return "", err
	}
	claudeMutation, removesClaude, claudeMessage := planClaudeCleanup(readClaudeCleanupTarget(repoRoot))
	ignoreCurrent, err := reportArtifactsIgnored(repoRoot, ignoreTarget.contents)
	if err != nil {
		return "", err
	}
	ignoreMutation, writesIgnore, ignoreMessage := planReportIgnore(ignoreTarget, ignoreCurrent)
	mutations := adoptionMutations(
		optionalMutation{agentsMutation, writesAgents},
		optionalMutation{claudeMutation, removesClaude},
		wrapperMutations,
		optionalMutation{ignoreMutation, writesIgnore},
	)
	mutations = append(mutations, lockMutation)
	if err := commitMutations(repoRoot, mutations, replace); err != nil {
		return "", err
	}
	return joinOperationMessages(agentsMessage, claudeMessage, wrapperMessage, ignoreMessage, "updated repository lock"), nil
}

func planUpgradeLock(repoRoot string, expected, incoming []byte) (mutation, error) {
	path := ".code-polishy.lock.json"
	target, err := readTarget(filepath.Join(repoRoot, path), path)
	if err != nil {
		return mutation{}, err
	}
	if !target.exists || !bytes.Equal(target.contents, expected) {
		return mutation{}, errors.New("repository lock changed after upgrade planning")
	}
	if len(incoming) == 0 || bytes.Equal(incoming, expected) {
		return mutation{}, errors.New("incoming repository lock must be distinct and nonempty")
	}
	return mutation{path: path, contents: append([]byte{}, incoming...), mode: target.mode, previous: target}, nil
}

func Check(repoRoot, policyRoot string) Status {
	guidance, err := canonical(policyRoot)
	if err != nil {
		message := err.Error()
		return Status{Message: message, Issues: []Issue{{
			Check: "policy.agentGuidance", Path: agentsTargetFilename, Subject: "canonical-guidance", Message: message,
		}}}
	}
	agentsTarget, agentsErr := readTarget(filepath.Join(repoRoot, agentsTargetFilename), agentsTargetFilename)
	ignoreTarget, ignoreErr := readTarget(filepath.Join(repoRoot, ignoreTargetFilename), ignoreTargetFilename)
	wrapperTargets, wrapperErrors := checkWrapperTargets(repoRoot)
	agentsStatus := checkAgents(agentsTarget, agentsErr, guidance.agents)
	ignoreCurrent, ignoreMatchErr := reportArtifactsIgnored(repoRoot, ignoreTarget.contents)
	ignoreStatus := checkReportIgnore(ignoreTarget, ignoreErr, ignoreCurrent, ignoreMatchErr)
	statuses := []checkStatus{agentsStatus}
	for index, template := range guidance.wrappers {
		statuses = append(statuses, checkWrapper(wrapperTargets[index], wrapperErrors[index], template))
	}
	statuses = append(statuses, ignoreStatus)
	allCurrent := true
	for _, status := range statuses {
		allCurrent = allCurrent && status.current
	}
	if allCurrent {
		return Status{Current: true, Message: joinStatusMessages(statuses)}
	}
	issues := make([]Issue, 0, len(statuses))
	for _, status := range statuses {
		if !status.current {
			issues = append(issues, status.issue)
		}
	}
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	return Status{Message: strings.Join(messages, "; "), Issues: issues}
}

type canonicalGuidance struct {
	agents   []byte
	wrappers []wrapperTemplate
}

func canonical(policyRoot string) (canonicalGuidance, error) {
	agentsPath := filepath.Join(policyRoot, filepath.FromSlash(agentsTemplateRelativePath))
	agentsTemplate, agentsErr := os.ReadFile(agentsPath)
	if agentsErr != nil {
		return canonicalGuidance{}, fmt.Errorf("read canonical AGENTS.md: %w", agentsErr)
	}
	if err := validateCanonicalAgents(agentsTemplate); err != nil {
		return canonicalGuidance{}, err
	}
	wrappers, err := canonicalWrappers(policyRoot)
	if err != nil {
		return canonicalGuidance{}, err
	}
	return canonicalGuidance{
		agents:   append([]byte{}, agentsTemplate...),
		wrappers: wrappers,
	}, nil
}

type targetState struct {
	exists   bool
	contents []byte
	mode     os.FileMode
}

func readTargets(repoRoot string) (targetState, targetState, error) {
	agentsTarget, agentsErr := readTarget(filepath.Join(repoRoot, agentsTargetFilename), agentsTargetFilename)
	ignoreTarget, ignoreErr := readTarget(filepath.Join(repoRoot, ignoreTargetFilename), ignoreTargetFilename)
	if agentsErr != nil {
		return targetState{}, targetState{}, agentsErr
	}
	if ignoreErr != nil {
		return targetState{}, targetState{}, ignoreErr
	}
	return agentsTarget, ignoreTarget, nil
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
	remove   bool
}

type optionalMutation struct {
	mutation mutation
	write    bool
}

func adoptionMutations(agents, claude optionalMutation, wrappers []mutation, ignore optionalMutation) []mutation {
	mutations := make([]mutation, 0, 5)
	for _, planned := range []optionalMutation{agents, claude} {
		if planned.write {
			mutations = append(mutations, planned.mutation)
		}
	}
	mutations = append(mutations, wrappers...)
	if ignore.write {
		mutations = append(mutations, ignore.mutation)
	}
	return mutations
}

func planInstallAgents(existing targetState, template []byte) (mutation, bool, string, error) {
	path := agentsTargetFilename
	if !existing.exists {
		return mutation{path: path, contents: template, mode: 0o644, previous: existing}, true, "installed canonical AGENTS.md", nil
	}
	document, err := splitAgentsDocument(existing.contents)
	if err != nil {
		return mutation{}, false, "", fmt.Errorf("AGENTS.md project principles are invalid: %w; its bytes were preserved", err)
	}
	managed := document.managed
	if !document.hasBoundary {
		managed = existing.contents
	}
	if matchesCanonicalGuidance(managed, template) {
		return mutation{}, false, "AGENTS.md canonical guidance is already current", nil
	}
	return mutation{}, false, "", errors.New("AGENTS.md conflicts with canonical guidance; its bytes were preserved")
}

func planSyncAgents(existing targetState, template []byte) (mutation, bool, string, error) {
	if !existing.exists {
		return mutation{}, false, "", fmt.Errorf("read AGENTS.md: %w", os.ErrNotExist)
	}
	updated, changed, err := renderSynchronizedAgents(existing.contents, template)
	if err != nil {
		return mutation{}, false, "", fmt.Errorf("AGENTS.md project principles are invalid: %w; its bytes were preserved", err)
	}
	if !changed {
		return mutation{}, false, "AGENTS.md canonical guidance is already current", nil
	}
	return mutation{
		path: agentsTargetFilename, contents: updated, mode: existing.mode, previous: existing,
	}, true, "synchronized AGENTS.md canonical guidance", nil
}

func readClaudeCleanupTarget(repoRoot string) targetState {
	target, err := readTarget(filepath.Join(repoRoot, claudeTargetFilename), claudeTargetFilename)
	if err != nil {
		return targetState{}
	}
	return target
}

func planClaudeCleanup(existing targetState) (mutation, bool, string) {
	managed := matchesCanonicalGuidance(existing.contents, []byte(legacyClaudeImport)) ||
		matchesCanonicalGuidance(existing.contents, []byte(legacyClaudeRedirect))
	if !existing.exists || !managed {
		return mutation{}, false, ""
	}
	return mutation{path: claudeTargetFilename, previous: existing, remove: true}, true, "removed obsolete managed CLAUDE.md"
}

func planReportIgnore(existing targetState, current bool) (mutation, bool, string) {
	if current {
		return mutation{}, false, ".gitignore report-artifact rule is already current"
	}
	mode := existing.mode
	if !existing.exists {
		mode = 0o644
	}
	return mutation{
		path: ignoreTargetFilename, contents: appendReportIgnore(existing.contents), mode: mode, previous: existing,
	}, true, "installed .gitignore report-artifact rule"
}

func reportArtifactsIgnored(repoRoot string, contents []byte) (bool, error) {
	if !containsArtifactIgnoreRules(contents) {
		return false, nil
	}
	if _, err := os.Lstat(filepath.Join(repoRoot, ".git")); errors.Is(err, os.ErrNotExist) {
		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("inspect Git repository metadata: %w", err)
	}
	for _, path := range []string{reportsDirectoryPath, testArtifactsDirectoryPath} {
		command := exec.Command("git", "-C", repoRoot, "check-ignore", "--no-index", "--quiet", "--", path)
		if err := command.Run(); err != nil {
			var exitError *exec.ExitError
			if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
				return false, nil
			}
			return false, fmt.Errorf("verify report-artifact ignore behavior with Git: %w", err)
		}
	}
	return true, nil
}

func containsArtifactIgnoreRules(contents []byte) bool {
	return containsExactIgnoreRule(contents, reportsIgnorePattern) && containsExactIgnoreRule(contents, testArtifactsIgnorePattern)
}

func containsExactIgnoreRule(contents []byte, pattern string) bool {
	for _, line := range bytes.Split(contents, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if bytes.Equal(line, []byte(pattern)) {
			return true
		}
	}
	return false
}

func appendReportIgnore(contents []byte) []byte {
	lineEnding := []byte("\n")
	if bytes.Contains(contents, []byte("\r\n")) {
		lineEnding = []byte("\r\n")
	}
	updated := append([]byte{}, contents...)
	if len(updated) > 0 && !bytes.HasSuffix(updated, []byte("\n")) {
		updated = append(updated, lineEnding...)
	}
	for _, pattern := range []string{reportsIgnorePattern, testArtifactsIgnorePattern} {
		updated = append(updated, []byte(pattern)...)
		updated = append(updated, lineEnding...)
	}
	return updated
}

type checkStatus struct {
	current bool
	message string
	issue   Issue
}

func checkAgents(existing targetState, readErr error, template []byte) checkStatus {
	if readErr != nil || !existing.exists {
		return failedStatus("policy.agentGuidance", agentsTargetFilename, "canonical-guidance", "AGENTS.md is missing or unreadable; run `code-polishy agents install`")
	}
	document, err := splitAgentsDocument(existing.contents)
	if err != nil {
		return failedStatus("policy.agentGuidance", agentsTargetFilename, "project-principles", "AGENTS.md project principles are invalid: "+err.Error())
	}
	managed := document.managed
	if !document.hasBoundary {
		managed = existing.contents
	}
	if !matchesCanonicalGuidance(managed, template) {
		return failedStatus("policy.agentGuidance", agentsTargetFilename, "canonical-guidance", "AGENTS.md canonical guidance is stale; run `code-polishy agents sync`")
	}
	return checkStatus{current: true, message: "AGENTS.md canonical guidance is current"}
}

func checkReportIgnore(existing targetState, readErr error, current bool, matchErr error) checkStatus {
	if readErr != nil {
		return failedStatus("policy.reportArtifacts", ignoreTargetFilename, "workspace-ignore", ".gitignore is unreadable or non-regular; preserve its bytes and resolve the conflict")
	}
	if matchErr != nil {
		return failedStatus("policy.reportArtifacts", ignoreTargetFilename, "workspace-ignore", matchErr.Error())
	}
	if !existing.exists || !current {
		return failedStatus("policy.reportArtifacts", ignoreTargetFilename, "workspace-ignore", "Code Polishy report artifacts are not ignored; run `code-polishy agents sync`")
	}
	return checkStatus{current: true, message: ".gitignore report-artifact rule is current"}
}

func failedStatus(check, path, subject, message string) checkStatus {
	return checkStatus{message: message, issue: Issue{Check: check, Path: path, Subject: subject, Message: message}}
}

func joinStatusMessages(statuses []checkStatus) string {
	messages := make([]string, 0, len(statuses))
	for _, status := range statuses {
		messages = append(messages, status.message)
	}
	return strings.Join(messages, "; ")
}

func joinOperationMessages(messages ...string) string {
	selected := make([]string, 0, len(messages))
	for _, message := range messages {
		if message != "" {
			selected = append(selected, message)
		}
	}
	return strings.Join(selected, "; ")
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
		incoming := ""
		if !planned.remove {
			var err error
			incoming, err = stageTemporary(repoRoot, planned.contents, planned.mode)
			if err != nil {
				cleanStaged(staged)
				return fmt.Errorf("stage %s: %w", planned.path, err)
			}
		}
		item := stagedMutation{mutation: planned, incoming: incoming}
		if planned.previous.exists {
			backup, err := stageTemporary(repoRoot, planned.previous.contents, planned.previous.mode)
			if err != nil {
				if incoming != "" {
					_ = os.Remove(incoming)
				}
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
		var err error
		if item.mutation.remove {
			err = os.Remove(target)
		} else {
			err = replace(item.incoming, target)
		}
		if err != nil {
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
