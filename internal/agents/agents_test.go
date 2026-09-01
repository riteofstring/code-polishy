package agents

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	canonicalAgentsText          = "# Canonical agent guidance\n\n- Verify observable behavior.\n"
	expectedClaudeImport         = "@AGENTS.md\n"
	expectedLegacyClaudeRedirect = "Read and follow `AGENTS.md` in the repository root for all project guidelines and workflows.\n"
	installedIgnoreMessage       = "installed .gitignore report-artifact rule"
	currentIgnoreMessage         = ".gitignore report-artifact rule is already current"
)

func TestInstallCreatesMissingCanonicalFiles(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()

	message, err := Install(repoRoot, policyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if message != "installed canonical AGENTS.md; installed canonical CLAUDE.md import; "+installedIgnoreMessage {
		t.Fatalf("install message = %q", message)
	}
	assertFile(t, filepath.Join(repoRoot, agentsTargetFilename), []byte(canonicalAgentsText), 0o644)
	assertFile(t, filepath.Join(repoRoot, claudeTargetFilename), []byte(expectedClaudeImport), 0o644)
	assertFile(t, filepath.Join(repoRoot, ignoreTargetFilename), []byte(reportsIgnorePattern+"\n"), 0o644)
}

func TestInstallAcceptsExactCanonicalFilesWithoutReplacingThem(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	agentsPath := filepath.Join(repoRoot, agentsTargetFilename)
	claudePath := filepath.Join(repoRoot, claudeTargetFilename)
	ignorePath := filepath.Join(repoRoot, ignoreTargetFilename)
	writeFile(t, agentsPath, []byte(canonicalAgentsText), 0o600)
	writeFile(t, claudePath, []byte(expectedClaudeImport), 0o640)
	writeFile(t, ignorePath, []byte(reportsIgnorePattern+"\n"), 0o640)

	message, err := install(repoRoot, policyRoot, func(_, _ string) error {
		return errors.New("idempotent install attempted a replacement")
	})
	if err != nil {
		t.Fatal(err)
	}
	if message != "AGENTS.md canonical guidance is already current; CLAUDE.md import is already current; "+currentIgnoreMessage {
		t.Fatalf("install message = %q", message)
	}
	assertFile(t, agentsPath, []byte(canonicalAgentsText), 0o600)
	assertFile(t, claudePath, []byte(expectedClaudeImport), 0o640)
	assertFile(t, ignorePath, []byte(reportsIgnorePattern+"\n"), 0o640)
}

func TestInstallAddsMissingClaudeForExactCanonicalAgents(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	agentsPath := filepath.Join(repoRoot, agentsTargetFilename)
	writeFile(t, agentsPath, []byte(canonicalAgentsText), 0o600)

	message, err := Install(repoRoot, policyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if message != "AGENTS.md canonical guidance is already current; installed canonical CLAUDE.md import; "+installedIgnoreMessage {
		t.Fatalf("install message = %q", message)
	}
	assertFile(t, agentsPath, []byte(canonicalAgentsText), 0o600)
	assertFile(t, filepath.Join(repoRoot, claudeTargetFilename), []byte(expectedClaudeImport), 0o644)
	assertFile(t, filepath.Join(repoRoot, ignoreTargetFilename), []byte(reportsIgnorePattern+"\n"), 0o644)
}

func TestSyncUpgradesTheExactLegacyClaudeRedirect(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	agentsPath := filepath.Join(repoRoot, agentsTargetFilename)
	claudePath := filepath.Join(repoRoot, claudeTargetFilename)
	ignorePath := filepath.Join(repoRoot, ignoreTargetFilename)
	writeFile(t, agentsPath, []byte(canonicalAgentsText), 0o600)
	writeFile(t, claudePath, []byte(strings.ReplaceAll(expectedLegacyClaudeRedirect, "\n", "\r\n")), 0o640)
	writeFile(t, ignorePath, []byte(reportsIgnorePattern+"\n"), 0o600)

	status := Check(repoRoot, policyRoot)
	if status.Current || !strings.Contains(status.Message, "CLAUDE.md canonical import is stale") {
		t.Fatalf("legacy Claude redirect status = %+v", status)
	}
	message, err := Sync(repoRoot, policyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if message != "AGENTS.md canonical guidance is already current; updated canonical CLAUDE.md import; "+currentIgnoreMessage {
		t.Fatalf("sync message = %q", message)
	}
	assertFile(t, claudePath, []byte(expectedClaudeImport), 0o640)
}

func TestInstallAppendsReportIgnoreRuleWithoutReplacingProjectRules(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	agentsPath := filepath.Join(repoRoot, agentsTargetFilename)
	claudePath := filepath.Join(repoRoot, claudeTargetFilename)
	ignorePath := filepath.Join(repoRoot, ignoreTargetFilename)
	writeFile(t, agentsPath, []byte(canonicalAgentsText), 0o600)
	writeFile(t, claudePath, []byte(expectedClaudeImport), 0o640)
	writeFile(t, ignorePath, []byte("dist/\r\n.env"), 0o640)

	message, err := Install(repoRoot, policyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if message != "AGENTS.md canonical guidance is already current; CLAUDE.md import is already current; "+installedIgnoreMessage {
		t.Fatalf("install message = %q", message)
	}
	assertFile(t, ignorePath, []byte("dist/\r\n.env\r\n"+reportsIgnorePattern+"\r\n"), 0o640)
}

func TestInstallPreservesNoncanonicalAgentsAndReportsConflict(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	agentsPath := filepath.Join(repoRoot, agentsTargetFilename)
	existing := []byte("# Repository-specific instructions\n\nKeep these bytes.\n")
	writeFile(t, agentsPath, existing, 0o640)

	if _, err := Install(repoRoot, policyRoot); err == nil || !strings.Contains(err.Error(), "AGENTS.md conflicts with canonical guidance") {
		t.Fatalf("noncanonical AGENTS.md did not report a conflict: %v", err)
	}
	assertFile(t, agentsPath, existing, 0o640)
	assertMissing(t, filepath.Join(repoRoot, claudeTargetFilename))
	assertMissing(t, filepath.Join(repoRoot, ignoreTargetFilename))
	assertNoTemporaryFiles(t, repoRoot)
}

func TestInstallPreservesAllTargetsWhenClaudeConflicts(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	claudePath := filepath.Join(repoRoot, claudeTargetFilename)
	conflictingClaude := []byte("# Tool-specific instructions\n")
	writeFile(t, claudePath, conflictingClaude, 0o600)

	if _, err := Install(repoRoot, policyRoot); err == nil || !strings.Contains(err.Error(), "CLAUDE.md conflicts") {
		t.Fatalf("conflicting CLAUDE.md did not block installation: %v", err)
	}
	assertMissing(t, filepath.Join(repoRoot, agentsTargetFilename))
	assertFile(t, claudePath, conflictingClaude, 0o600)
	assertMissing(t, filepath.Join(repoRoot, ignoreTargetFilename))
	assertNoTemporaryFiles(t, repoRoot)
}

func TestInstallValidatesCanonicalFilesBeforeTargetMutation(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		corrupt func(*testing.T, string)
		wantErr string
	}{
		"missing AGENTS template": {
			corrupt: func(t *testing.T, policyRoot string) {
				t.Helper()
				if err := os.Remove(filepath.Join(policyRoot, filepath.FromSlash(agentsTemplateRelativePath))); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "read canonical AGENTS.md",
		},
		"empty AGENTS template": {
			corrupt: func(t *testing.T, policyRoot string) {
				t.Helper()
				writeFile(t, filepath.Join(policyRoot, filepath.FromSlash(agentsTemplateRelativePath)), nil, 0o600)
			},
			wantErr: "canonical AGENTS.md must not be empty",
		},
		"missing CLAUDE template": {
			corrupt: func(t *testing.T, policyRoot string) {
				t.Helper()
				if err := os.Remove(filepath.Join(policyRoot, filepath.FromSlash(claudeTemplateRelativePath))); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "read canonical CLAUDE.md",
		},
		"noncanonical CLAUDE template": {
			corrupt: func(t *testing.T, policyRoot string) {
				t.Helper()
				writeFile(t, filepath.Join(policyRoot, filepath.FromSlash(claudeTemplateRelativePath)), []byte("Read this instead.\n"), 0o600)
			},
			wantErr: "canonical CLAUDE.md must contain exactly",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			policyRoot := policyFixture(t, canonicalAgentsText)
			repoRoot := t.TempDir()
			agentsPath := filepath.Join(repoRoot, agentsTargetFilename)
			existing := []byte("# Existing instructions\n")
			writeFile(t, agentsPath, existing, 0o640)
			test.corrupt(t, policyRoot)

			if _, err := Install(repoRoot, policyRoot); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("invalid canonical files were accepted: %v", err)
			}
			assertFile(t, agentsPath, existing, 0o640)
			assertMissing(t, filepath.Join(repoRoot, claudeTargetFilename))
			assertMissing(t, filepath.Join(repoRoot, ignoreTargetFilename))
			assertNoTemporaryFiles(t, repoRoot)
		})
	}
}

func TestSyncRequiresExistingAgentsBeforeCreatingClaude(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()

	if _, err := Sync(repoRoot, policyRoot); err == nil || !strings.Contains(err.Error(), "read AGENTS.md") {
		t.Fatalf("missing AGENTS.md did not block sync: %v", err)
	}
	assertMissing(t, filepath.Join(repoRoot, agentsTargetFilename))
	assertMissing(t, filepath.Join(repoRoot, claudeTargetFilename))
	assertMissing(t, filepath.Join(repoRoot, ignoreTargetFilename))
}

func TestSyncReplacesTheEntireStaleAgentsFileAndPreservesItsMode(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	agentsPath := filepath.Join(repoRoot, agentsTargetFilename)
	stale := []byte("prefix that resembles policy\n\n# Extra stale suffix\n")
	writeFile(t, agentsPath, stale, 0o640)

	message, err := Sync(repoRoot, policyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if message != "synchronized AGENTS.md canonical guidance; installed canonical CLAUDE.md import; "+installedIgnoreMessage {
		t.Fatalf("sync message = %q", message)
	}
	assertFile(t, agentsPath, []byte(canonicalAgentsText), 0o640)
	assertFile(t, filepath.Join(repoRoot, claudeTargetFilename), []byte(expectedClaudeImport), 0o644)
	assertFile(t, filepath.Join(repoRoot, ignoreTargetFilename), []byte(reportsIgnorePattern+"\n"), 0o644)
	status := Check(repoRoot, policyRoot)
	if !status.Current {
		t.Fatalf("synchronized guidance is not current: %+v", status)
	}
}

func TestSyncAcceptsExactCanonicalFilesWithoutReplacingThem(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	agentsPath := filepath.Join(repoRoot, agentsTargetFilename)
	claudePath := filepath.Join(repoRoot, claudeTargetFilename)
	ignorePath := filepath.Join(repoRoot, ignoreTargetFilename)
	writeFile(t, agentsPath, []byte(canonicalAgentsText), 0o600)
	writeFile(t, claudePath, []byte(expectedClaudeImport), 0o640)
	writeFile(t, ignorePath, []byte(reportsIgnorePattern+"\n"), 0o640)

	message, err := sync(repoRoot, policyRoot, func(_, _ string) error {
		return errors.New("idempotent sync attempted a replacement")
	})
	if err != nil {
		t.Fatal(err)
	}
	if message != "AGENTS.md canonical guidance is already current; CLAUDE.md import is already current; "+currentIgnoreMessage {
		t.Fatalf("sync message = %q", message)
	}
	assertFile(t, agentsPath, []byte(canonicalAgentsText), 0o600)
	assertFile(t, claudePath, []byte(expectedClaudeImport), 0o640)
	assertFile(t, ignorePath, []byte(reportsIgnorePattern+"\n"), 0o640)
}

func TestInstallSyncAndCheckAcceptWholeFileCRLFCanonicalGuidance(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	agentsPath := filepath.Join(repoRoot, agentsTargetFilename)
	claudePath := filepath.Join(repoRoot, claudeTargetFilename)
	ignorePath := filepath.Join(repoRoot, ignoreTargetFilename)
	crlfAgents := []byte(strings.ReplaceAll(canonicalAgentsText, "\n", "\r\n"))
	crlfClaude := []byte(strings.ReplaceAll(expectedClaudeImport, "\n", "\r\n"))
	writeFile(t, agentsPath, crlfAgents, 0o600)
	writeFile(t, claudePath, crlfClaude, 0o640)
	writeFile(t, ignorePath, []byte(reportsIgnorePattern+"\r\n"), 0o640)
	replace := func(_, _ string) error { return errors.New("canonical CRLF guidance attempted a replacement") }

	message, err := install(repoRoot, policyRoot, replace)
	if err != nil {
		t.Fatal(err)
	}
	if message != "AGENTS.md canonical guidance is already current; CLAUDE.md import is already current; "+currentIgnoreMessage {
		t.Fatalf("install message = %q", message)
	}
	message, err = sync(repoRoot, policyRoot, replace)
	if err != nil {
		t.Fatal(err)
	}
	if message != "AGENTS.md canonical guidance is already current; CLAUDE.md import is already current; "+currentIgnoreMessage {
		t.Fatalf("sync message = %q", message)
	}
	status := Check(repoRoot, policyRoot)
	if !status.Current {
		t.Fatalf("CRLF guidance is not current: %+v", status)
	}
	assertFile(t, agentsPath, crlfAgents, 0o600)
	assertFile(t, claudePath, crlfClaude, 0o640)
	assertFile(t, ignorePath, []byte(reportsIgnorePattern+"\r\n"), 0o640)

	mixedAgents := []byte(strings.Replace(canonicalAgentsText, "\n", "\r\n", 1))
	writeFile(t, agentsPath, mixedAgents, 0o600)
	if status = Check(repoRoot, policyRoot); status.Current || !strings.Contains(status.Message, "AGENTS.md canonical guidance is stale") {
		t.Fatalf("mixed line endings were accepted: %+v", status)
	}
}

func TestSyncPreservesAllTargetsWhenClaudeConflicts(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	agentsPath := filepath.Join(repoRoot, agentsTargetFilename)
	claudePath := filepath.Join(repoRoot, claudeTargetFilename)
	staleAgents := []byte("# Stale guidance\n")
	conflictingClaude := []byte("# Tool-specific instructions\n")
	writeFile(t, agentsPath, staleAgents, 0o640)
	writeFile(t, claudePath, conflictingClaude, 0o600)

	if _, err := Sync(repoRoot, policyRoot); err == nil || !strings.Contains(err.Error(), "CLAUDE.md conflicts") {
		t.Fatalf("conflicting CLAUDE.md did not block sync: %v", err)
	}
	assertFile(t, agentsPath, staleAgents, 0o640)
	assertFile(t, claudePath, conflictingClaude, 0o600)
	assertMissing(t, filepath.Join(repoRoot, ignoreTargetFilename))
	assertNoTemporaryFiles(t, repoRoot)
}

func TestCheckComparesTheEntireCanonicalAgentsFile(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()

	status := Check(repoRoot, policyRoot)
	if status.Current || !strings.Contains(status.Message, "AGENTS.md is missing") || !strings.Contains(status.Message, "CLAUDE.md is missing") {
		t.Fatalf("missing files status = %+v", status)
	}

	agentsPath := filepath.Join(repoRoot, agentsTargetFilename)
	claudePath := filepath.Join(repoRoot, claudeTargetFilename)
	writeFile(t, agentsPath, []byte(canonicalAgentsText+"extra project bytes\n"), 0o600)
	writeFile(t, claudePath, []byte("# Conflicting import\n"), 0o600)
	status = Check(repoRoot, policyRoot)
	if status.Current || !strings.Contains(status.Message, "AGENTS.md canonical guidance is stale") || !strings.Contains(status.Message, "CLAUDE.md conflicts") {
		t.Fatalf("stale and conflicting status = %+v", status)
	}

	writeFile(t, agentsPath, []byte(canonicalAgentsText), 0o600)
	writeFile(t, claudePath, []byte(expectedClaudeImport), 0o600)
	status = Check(repoRoot, policyRoot)
	if status.Current || len(status.Issues) != 1 || status.Issues[0].Check != "policy.reportArtifacts" ||
		status.Issues[0].Path != ignoreTargetFilename || status.Issues[0].Subject != "workspace-ignore" {
		t.Fatalf("missing report ignore status = %+v", status)
	}
	writeFile(t, filepath.Join(repoRoot, ignoreTargetFilename), []byte(reportsIgnorePattern+"\n"), 0o600)
	status = Check(repoRoot, policyRoot)
	if !status.Current || !strings.Contains(status.Message, "AGENTS.md canonical guidance is current") ||
		!strings.Contains(status.Message, "CLAUDE.md import is current") ||
		!strings.Contains(status.Message, ".gitignore report-artifact rule is current") {
		t.Fatalf("current status = %+v", status)
	}
}

func TestSyncRollsBackAgentsWhenTheClaudeReplacementFails(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	agentsPath := filepath.Join(repoRoot, agentsTargetFilename)
	claudePath := filepath.Join(repoRoot, claudeTargetFilename)
	staleAgents := []byte("# Stale guidance that must be restored\n")
	writeFile(t, agentsPath, staleAgents, 0o640)

	_, err := sync(repoRoot, policyRoot, func(oldPath, newPath string) error {
		if newPath == claudePath {
			return errors.New("injected second replacement failure")
		}
		return os.Rename(oldPath, newPath)
	})
	if err == nil || !strings.Contains(err.Error(), "injected second replacement failure") {
		t.Fatalf("second replacement failure was not returned: %v", err)
	}
	assertFile(t, agentsPath, staleAgents, 0o640)
	assertMissing(t, claudePath)
	assertMissing(t, filepath.Join(repoRoot, ignoreTargetFilename))
	assertNoTemporaryFiles(t, repoRoot)
}

func TestSyncRollsBackGuidanceWhenTheGitignoreReplacementFails(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	agentsPath := filepath.Join(repoRoot, agentsTargetFilename)
	claudePath := filepath.Join(repoRoot, claudeTargetFilename)
	ignorePath := filepath.Join(repoRoot, ignoreTargetFilename)
	staleAgents := []byte("# Stale guidance that must be restored\n")
	existingIgnore := []byte("dist/\n")
	writeFile(t, agentsPath, staleAgents, 0o640)
	writeFile(t, claudePath, []byte(expectedClaudeImport), 0o600)
	writeFile(t, ignorePath, existingIgnore, 0o640)

	_, err := sync(repoRoot, policyRoot, func(oldPath, newPath string) error {
		if newPath == ignorePath {
			return errors.New("injected report-ignore replacement failure")
		}
		return os.Rename(oldPath, newPath)
	})
	if err == nil || !strings.Contains(err.Error(), "injected report-ignore replacement failure") {
		t.Fatalf("report-ignore replacement failure was not returned: %v", err)
	}
	assertFile(t, agentsPath, staleAgents, 0o640)
	assertFile(t, claudePath, []byte(expectedClaudeImport), 0o600)
	assertFile(t, ignorePath, existingIgnore, 0o640)
	assertNoTemporaryFiles(t, repoRoot)
}

func assertFile(t *testing.T, path string, want []byte, wantMode os.FileMode) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s contents = %q, want %q", path, got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != wantMode {
		t.Fatalf("%s mode = %v, want %v", path, info.Mode().Perm(), wantMode)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s exists or returned an unexpected error: %v", path, err)
	}
}

func assertNoTemporaryFiles(t *testing.T, repoRoot string) {
	t.Helper()
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".code-polishy-agents-") {
			t.Fatalf("temporary transaction file remains: %s", entry.Name())
		}
	}
}

func writeFile(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
}

func policyFixture(t *testing.T, agents string) string {
	t.Helper()
	root := t.TempDir()
	agentsPath := filepath.Join(root, filepath.FromSlash(agentsTemplateRelativePath))
	if err := os.MkdirAll(filepath.Dir(agentsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, agentsPath, []byte(agents), 0o600)
	if claudeImport != expectedClaudeImport {
		t.Fatalf("claude import contract = %q", claudeImport)
	}
	if legacyClaudeRedirect != expectedLegacyClaudeRedirect {
		t.Fatalf("legacy claude redirect contract = %q", legacyClaudeRedirect)
	}
	claudePath := filepath.Join(root, filepath.FromSlash(claudeTemplateRelativePath))
	writeFile(t, claudePath, []byte(expectedClaudeImport), 0o600)
	return root
}
