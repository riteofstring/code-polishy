package behaviorreview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchitectureReviewIncludesUnchangedCandidateSourceDuringAdoption(t *testing.T) {
	t.Parallel()
	repo, input := newArchitectureReviewRepository(t)
	gitBehavior(t, repo.Root, "branch", "-f", "main", "HEAD")
	writeBehaviorFile(t, repo.Root, ".code-polishy.json", "{\"modules\":[{\"name\":\"app\",\"paths\":[\"app/**\"]}]}\n")
	gitBehavior(t, repo.Root, "add", ".code-polishy.json")
	gitBehavior(t, repo.Root, "commit", "-m", "adopt architecture ownership")
	prepared, packet := prepareArchitectureForTest(t, repo, input)
	expected := "package app\n\nfunc Value() int { return 2 }\n"
	if strings.Contains(packet.Patch, "func Value") || len(packet.Sources) != 1 {
		t.Fatalf("adoption evidence = %+v", packet)
	}
	source := packet.Sources[0]
	if source.Path != "app/value.go" || source.Content != expected || source.SHA256 != sha256Hex([]byte(expected)) {
		t.Fatalf("source evidence = %+v", source)
	}
	result := architectureAcceptance(packet)
	result.Evidence = []ArchitectureCitation{{Pointer: "/sources/0/content", Quote: "func Value() int { return 2 }", Rationale: "The module implements value calculation directly without forwarding to another module."}}
	writeBehaviorBytes(t, repo.Root, prepared.ResultPath, architectureTestJSON(t, result))
	accepted, err := FinalizeArchitectureReview(context.Background(), repo, "main", input)
	if err != nil || accepted.State != "passed" {
		t.Fatalf("source-backed acceptance = %+v, %v", accepted, err)
	}
}

func TestArchitectureReviewRejectsUnavailableCandidateSource(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"missing", "symlink", "non-UTF8", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			repo, input := newArchitectureReviewRepository(t)
			path := filepath.Join(repo.Root, "app/value.go")
			switch kind {
			case "missing", "symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					if err := os.Symlink("../docs/design.md", path); err != nil {
						t.Fatal(err)
					}
				}
			case "non-UTF8":
				writeBehaviorBytes(t, repo.Root, "app/value.go", []byte{0xff, 0xfe})
			case "oversized":
				writeBehaviorFile(t, repo.Root, "app/value.go", strings.Repeat("x", maximumArchitectureSource+1))
			}
			gitBehavior(t, repo.Root, "add", "-A")
			gitBehavior(t, repo.Root, "commit", "-m", "replace source")
			gitBehavior(t, repo.Root, "branch", "-f", "main", "HEAD")
			if _, err := PrepareArchitectureReview(context.Background(), repo, "main", input); !errors.Is(err, ErrArchitectureReview) {
				t.Fatalf("unavailable source error = %v", err)
			}
			if _, err := os.Stat(filepath.Join(repo.Root, architectureDirectory)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed preparation created artifacts: %v", err)
			}
		})
	}
}
