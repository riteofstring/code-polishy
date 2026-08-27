package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskSessionReceiptWritesCanonicalArtifact(t *testing.T) {
	output := t.TempDir()
	arguments := []string{
		"--output-dir", output,
		"--status", "promoted",
		"--source-root", "/source root",
		"--source-branch", "main",
		"--trusted-base", strings.Repeat("a", 40),
		"--candidate-head", strings.Repeat("b", 40),
		"--config", ".code-polishy.json",
		"--promote", "1",
		"--policy-binary", "/policy binary",
		"--policy-digest", strings.Repeat("c", 64),
		"--environment-receipt", "/environment.json",
		"--environment-digest", strings.Repeat("d", 64),
		"--environment-paths", "/environment-paths.txt",
		"--environment-paths-digest", strings.Repeat("e", 64),
		"--scope-digest", strings.Repeat("f", 64),
		"--module", "cli",
		"--module", "tooling",
		"--exact-path", "plan with spaces.md",
		"--new-path", "result.json",
		"--workspace", "/workspace",
		"--command-digest", strings.Repeat("1", 64),
	}
	options, err := parseTaskSessionReceiptOptions(arguments)
	if err != nil {
		t.Fatalf("parse receipt options: %v", err)
	}
	if err := writeTaskSessionReceipt(options); err != nil {
		t.Fatalf("write receipt: %v", err)
	}
	receiptPath := filepath.Join(output, taskSessionReceiptArtifact)
	receipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	want := "version=5\n" +
		"status=promoted\n" +
		"sourceRoot=/source root\n" +
		"sourceBranch=main\n" +
		"trustedBase=" + strings.Repeat("a", 40) + "\n" +
		"candidateHead=" + strings.Repeat("b", 40) + "\n" +
		"config=.code-polishy.json\n" +
		"promote=1\n" +
		"policyBinary=/policy binary\n" +
		"policyDigest=" + strings.Repeat("c", 64) + "\n" +
		"environmentReceipt=/environment.json\n" +
		"environmentDigest=" + strings.Repeat("d", 64) + "\n" +
		"environmentPaths=/environment-paths.txt\n" +
		"environmentPathsDigest=" + strings.Repeat("e", 64) + "\n" +
		"scopeDigest=" + strings.Repeat("f", 64) + "\n" +
		"modules=cli,tooling\n" +
		"exactPaths=plan with spaces.md\n" +
		"newPaths=result.json\n" +
		"workspace=/workspace\n" +
		"commandDigest=" + strings.Repeat("1", 64) + "\n"
	if string(receipt) != want {
		t.Fatalf("receipt = %q, want %q", receipt, want)
	}
	info, err := os.Stat(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("receipt mode = %o, want 600", info.Mode().Perm())
	}
}

func TestTaskSessionReceiptRejectsAmbiguousValues(t *testing.T) {
	output := t.TempDir()
	base := []string{
		"--output-dir", output, "--status", "interrupted", "--source-root", "/source",
		"--source-branch", "main", "--trusted-base", "base", "--candidate-head", "head",
		"--config", "config", "--promote", "0", "--policy-binary", "policy",
		"--policy-digest", "policy-digest", "--environment-receipt", "environment",
		"--environment-digest", "environment-digest", "--environment-paths", "paths",
		"--environment-paths-digest", "paths-digest", "--scope-digest", "scope-digest",
		"--workspace", "workspace", "--command-digest", "command-digest", "--module", "cli",
	}
	for name, extra := range map[string][]string{
		"duplicate field": {"--status", "failed"},
		"multiline field": {"--candidate-head", "head\nforged=value"},
		"ambiguous list":  {"--exact-path", "one,two"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseTaskSessionReceiptOptions(append(append([]string{}, base...), extra...)); err == nil {
				t.Fatal("ambiguous receipt input was accepted")
			}
		})
	}
}

func TestTaskSessionReceiptRejectsMissingModule(t *testing.T) {
	output := t.TempDir()
	arguments := []string{
		"--output-dir", output, "--status", "interrupted", "--source-root", "/source",
		"--source-branch", "main", "--trusted-base", "base", "--candidate-head", "head",
		"--config", "config", "--promote", "0", "--policy-binary", "policy",
		"--policy-digest", "policy-digest", "--environment-receipt", "environment",
		"--environment-digest", "environment-digest", "--environment-paths", "paths",
		"--environment-paths-digest", "paths-digest", "--scope-digest", "scope-digest",
		"--workspace", "workspace", "--command-digest", "command-digest",
	}
	if _, err := parseTaskSessionReceiptOptions(arguments); err == nil {
		t.Fatal("receipt input without a module was accepted")
	}
}
