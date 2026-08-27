package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeTaskSessionPromotesOneContainedCommittedCandidate(t *testing.T) {
	root := contentRepository(t, nil)
	initializeEngineGitRepository(t, root)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODE_POLISHY_TEST_TASK_WORKER", "1")
	result, err := RunTaskSession(context.Background(), TaskSessionOptions{
		RepoRoot: root, PolicyRoot: root, Modules: []string{"content"}, Promote: true,
		Command:    []string{executable, "-test.run=^TestNativeTaskSessionWorker$"},
		Executable: executable,
	})
	if err != nil {
		t.Fatalf("run native task session: %v", err)
	}
	if result.Status != "promoted" || result.ExitStatus != 0 || result.CandidateHead == result.TrustedBase {
		t.Fatalf("result = %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(root, "content", "native-task.txt"))
	if err != nil || string(data) != "native task session\n" {
		t.Fatalf("promoted content = %q, err=%v", data, err)
	}
	if _, err := os.Stat(result.Workspace); !os.IsNotExist(err) {
		t.Fatalf("disposable worktree remained after promotion: %v", err)
	}
	receipt, err := os.ReadFile(filepath.Join(result.OutputDir, "session.txt"))
	if err != nil || !strings.Contains(string(receipt), "status=promoted\n") {
		t.Fatalf("terminal receipt = %q, err=%v", receipt, err)
	}
}

func TestNativeTaskSessionRetainsInterruptedCandidate(t *testing.T) {
	root := contentRepository(t, nil)
	initializeEngineGitRepository(t, root)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODE_POLISHY_TEST_TASK_WORKER", "1")
	t.Setenv("CODE_POLISHY_TEST_TASK_WORKER_MODE", "wait")
	output := filepath.Join(t.TempDir(), "task-artifacts")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() {
		for {
			if _, statErr := os.Stat(filepath.Join(output, "worker-ready")); statErr == nil {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	result, err := RunTaskSession(ctx, TaskSessionOptions{
		RepoRoot: root, PolicyRoot: root, Modules: []string{"content"},
		OutputDir: output, Command: []string{executable, "-test.run=^TestNativeTaskSessionWorker$", "worker"}, Executable: executable,
	})
	if err == nil || result.Status != "interrupted" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if info, statErr := os.Stat(result.Workspace); statErr != nil || !info.IsDir() {
		t.Fatalf("interrupted worktree was not retained: %v", statErr)
	}
	command := exec.Command("git", "worktree", "remove", "--force", result.Workspace)
	command.Dir = root
	if output, removeErr := command.CombinedOutput(); removeErr != nil {
		t.Fatalf("remove retained test worktree: %v\n%s", removeErr, output)
	}
}

func TestNativeTaskSessionRefusesChangedWorkspaceIdentity(t *testing.T) {
	root := contentRepository(t, nil)
	initializeEngineGitRepository(t, root)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODE_POLISHY_TEST_TASK_WORKER", "1")
	t.Setenv("CODE_POLISHY_TEST_TASK_WORKER_MODE", "corrupt-pointer")
	result, err := RunTaskSession(context.Background(), TaskSessionOptions{
		RepoRoot: root, PolicyRoot: root, Modules: []string{"content"},
		Command: []string{executable, "-test.run=^TestNativeTaskSessionWorker$"}, Executable: executable,
	})
	if err == nil || result.Status != "workspace-untrusted" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	pointer := filepath.Join(result.Workspace, ".git")
	if restoreErr := os.Rename(pointer+".original", pointer); restoreErr != nil {
		t.Fatalf("restore test worktree pointer: %v", restoreErr)
	}
	command := exec.Command("git", "worktree", "remove", "--force", result.Workspace)
	command.Dir = root
	if output, removeErr := command.CombinedOutput(); removeErr != nil {
		t.Fatalf("remove retained test worktree: %v\n%s", removeErr, output)
	}
}

func TestNativeTaskSessionWorker(t *testing.T) {
	if os.Getenv("CODE_POLISHY_TEST_TASK_WORKER") != "1" {
		return
	}
	if os.Getenv("CODE_POLISHY_TEST_TASK_WORKER_MODE") == "wait" {
		if err := os.WriteFile(filepath.Join(os.Getenv("CODE_POLISHY_TASK_ARTIFACT_DIR"), "worker-ready"), []byte("ready\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Minute)
		return
	}
	if os.Getenv("CODE_POLISHY_TEST_TASK_WORKER_MODE") == "corrupt-pointer" {
		if err := os.Rename(".git", ".git.original"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(".git", []byte("gitdir: replaced\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	path := filepath.Join("content", "native-task.txt")
	if err := os.WriteFile(path, []byte("native task session\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{{"add", path}, {"commit", "-m", "native task candidate"}} {
		command := exec.Command("git", arguments...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
}
