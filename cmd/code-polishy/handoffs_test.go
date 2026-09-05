package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestDesignContextCLISelectsOperationalHandoffsWithoutExecutingProcedures(t *testing.T) {
	root := newHandoffCLIRepository(t)
	agentPath := filepath.Join(root, "AGENTS.md")
	before, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	arguments := []string{"--repo-root", root, "--policy-root", behaviorReviewCLIPolicyRoot(t), "design-context"}
	status, stdout, stderr := captureRunOutput(t, append(append([]string{}, arguments...), "--situation", "release", "--format", "json"))
	report := engine.Report{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || status != 0 || report.RepositoryContext == nil {
		t.Fatalf("status=%d error=%v stdout=%q stderr=%q", status, err, stdout, stderr)
	}
	if len(report.RepositoryContext.Handoffs) != 1 || report.RepositoryContext.Handoffs[0].Name != "release" ||
		!strings.Contains(report.RepositoryContext.Handoffs[0].Document.Content, "procedure-must-not-run") {
		t.Fatalf("handoffs=%+v", report.RepositoryContext.Handoffs)
	}
	if _, err := os.Stat(filepath.Join(root, "procedure-must-not-run")); !os.IsNotExist(err) {
		t.Fatalf("procedure command was executed: %v", err)
	}
	after, err := os.ReadFile(agentPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("managed guidance was changed: %v", err)
	}
	status, stdout, stderr = captureRunOutput(t, append(append([]string{}, arguments...), "--files", "value.go"))
	if status != 0 || !strings.Contains(stdout, "HANDOFF release: Release procedure.") || !strings.Contains(stdout, "SELECTED BY: source value.go") {
		t.Fatalf("automatic source context: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	status, stdout, stderr = captureRunOutput(t, append(append([]string{}, arguments...), "--situation", "authentication", "--format", "json"))
	report = engine.Report{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || status != 1 || report.RepositoryContext != nil || len(report.Findings) != 1 || report.Findings[0].Check != "policy.operationalHandoff" {
		t.Fatalf("selected invalid handoff: status=%d error=%v stdout=%q stderr=%q", status, err, stdout, stderr)
	}
}

func TestDesignContextCLIUsesTheActualCommandSituation(t *testing.T) {
	root := newHandoffCLIRepository(t)
	configPath := filepath.Join(root, policy.ConfigFilename)
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorReviewCLIFile(t, root, policy.ConfigFilename, strings.Replace(string(config), `"release"]`, `"design-context"]`, 1))
	status, stdout, stderr := captureRunOutput(t, []string{
		"--repo-root", root, "--policy-root", behaviorReviewCLIPolicyRoot(t), "design-context", "--files", "AGENTS.md", "--format", "json",
	})
	report := engine.Report{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || status != 0 || report.RepositoryContext == nil || len(report.RepositoryContext.Handoffs) != 1 {
		t.Fatalf("workflow context: status=%d error=%v stdout=%q stderr=%q", status, err, stdout, stderr)
	}
	reasons := report.RepositoryContext.Handoffs[0].Reasons
	if len(reasons) != 1 || reasons[0].Kind != "situation" || reasons[0].Value != "design-context" {
		t.Fatalf("workflow selection reasons = %+v", reasons)
	}
}

func TestDesignContextOptionsKeepSituationsExactAndIndependentOfFileSelection(t *testing.T) {
	t.Parallel()
	options, err := parseDesignContextOptions([]string{"--situation", "release", "--files", "src/app.go", "--situation=authentication"})
	if err != nil || options.mode != "files" || len(options.files) != 1 || len(options.situations) != 2 {
		t.Fatalf("options=%+v error=%v", options, err)
	}
	options, err = parseDesignContextOptions([]string{"--situation", "release"})
	if err != nil || options.mode != "none" {
		t.Fatalf("situations-only options=%+v error=%v", options, err)
	}
	for _, arguments := range [][]string{{"--situation"}, {"--situation="}, {"--situation", "please release"}, {"--situation", "release", "--situation=release"}} {
		if _, err := parseDesignContextOptions(arguments); err == nil {
			t.Fatalf("accepted ambiguous situation selection %v", arguments)
		}
	}
}

func newHandoffCLIRepository(t *testing.T) string {
	t.Helper()
	root, _ := newBehaviorReviewCLIBaseRepository(t)
	config, err := os.ReadFile(filepath.Join(root, policy.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}
	declaration := `"documentation":{"handoffs":[
  {"name":"release","description":"Release procedure.","path":"docs/operations/release.md","situations":["release"],"sourcePaths":["value.go"]},
  {"name":"authentication","description":"Authentication procedure.","path":"docs/operations/missing.md","situations":["authentication"]}
]},"scope": {}`
	writeBehaviorReviewCLIFile(t, root, policy.ConfigFilename, strings.Replace(string(config), `"scope": {}`, declaration, 1))
	writeBehaviorReviewCLIFile(t, root, "docs/operations/release.md", "# Release\n\n```sh\nprintf executed > procedure-must-not-run\n```\n")
	return root
}
