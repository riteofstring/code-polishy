package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/engine"
)

func TestTaskStartOptionalDirtyPacketPreservesFilesAndPlannedPaths(t *testing.T) {
	root, policyRoot, intent := newTaskStartCLIRepository(t)
	writeBehaviorReviewCLIFile(t, root, "value.go", "package application\nconst Staged = 1\n")
	gitBehaviorReviewCLI(t, root, "add", "value.go")
	writeBehaviorReviewCLIFile(t, root, "value.go", "package application\nconst Pending = 2\n")
	staged := contextGitOutput(t, root, "diff", "--cached")
	unstaged := contextGitOutput(t, root, "diff")
	before := contextGitOutput(t, root, "status", "--porcelain")
	status, stdout, stderr := captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, ""), "--files", "value.go", "--new-path", "retrospective/README.md"))
	var packet engine.TaskStartPacket
	if err := json.Unmarshal([]byte(stdout), &packet); err != nil || status != 0 || stderr != "" || packet.Intent.Captured || packet.TaskBase != gitRevision(t, root, "HEAD") {
		t.Fatalf("dirty optional packet: status=%d packet=%+v error=%v stderr=%q", status, packet, err, stderr)
	}
	if !slices.Equal(packet.RequestedSelection.Planned, []string{"retrospective/README.md"}) || !slices.Equal(packet.RequestedSelection.Expanded, []string{"value.go"}) || before != contextGitOutput(t, root, "status", "--porcelain") {
		t.Fatalf("planned paths or dirty files changed: %+v", packet.RequestedSelection)
	}
	if staged != contextGitOutput(t, root, "diff", "--cached") || unstaged != contextGitOutput(t, root, "diff") {
		t.Fatal("optional context altered staged or unstaged contents")
	}

	if _, err := os.Stat(filepath.Join(root, "retrospective")); !os.IsNotExist(err) {
		t.Fatalf("created planned directory: %v", err)
	}
	status, stdout, stderr = captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, intent), "--files", "value.go", "--feature", "checkout"))
	if status != 2 || stdout != "" || !strings.Contains(stderr, "initial request boundary") || strings.Contains(stderr, "correction") {
		t.Fatalf("dirty required review: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".code-polishy-reports", "behavior-review")); !os.IsNotExist(err) {
		t.Fatalf("captured invalid boundary: %v", err)
	}
}

func contextGitOutput(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

func TestDesignContextAndTaskStartAcceptOnlyPlannedPaths(t *testing.T) {
	root, policyRoot, _ := newTaskStartCLIRepository(t)
	for _, command := range []string{"design-context", "task-start"} {
		status, stdout, stderr := captureRunOutput(t, []string{"--repo-root", root, "--policy-root", policyRoot, command, "--new-path", "retrospective/README.md", "--format", "json"})
		if status != 0 || stderr != "" || !strings.Contains(stdout, `"planned"`) || !strings.Contains(stdout, "retrospective/README.md") {
			t.Fatalf("%s: status=%d stdout=%q stderr=%q", command, status, stdout, stderr)
		}
	}
	status, stdout, stderr := captureRunOutput(t, []string{"--repo-root", root, "--policy-root", policyRoot, "design-context", "--new-path", "planned.go", "--feature", "checkout", "--format", "json"})
	var report engine.Report
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || status != 0 || stderr != "" || report.RepositoryContext == nil || !report.RepositoryContext.Review.Required || !slices.Equal(report.RepositoryContext.Review.SelectedFeatures, []string{"checkout"}) {
		t.Fatalf("review discovery: status=%d report=%+v error=%v stderr=%q", status, report, err, stderr)
	}
}
