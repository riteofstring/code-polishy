package pack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestProviderContextIncludesProtectedLinkIdentityAndRejectsChanges(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "images/picture.png", "original", 0o600)
	writeTestFile(t, root, "public/keep.txt", "text", 0o600)
	if err := os.Symlink("../images", filepath.Join(root, "public/images")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	config := policy.Config{Modules: []policy.Module{{Name: "application", Paths: []string{"images/**", "public/**"}}}, ModuleByName: map[string]int{"application": 0}}
	repo, err := repository.Open(root, root, config)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Files: []string{}, Capability: "format"}
	if err := prepareInputs(repo, &request); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, input := range request.Context {
		if input.Path == "public/images" {
			found = true
		}
	}
	if !found {
		t.Fatal("provider context omitted the asset link")
	}
	response := Response{Status: "pass", Inputs: request.Context}
	if err := verifyAnalysisInputs(repo, request, response); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "images/picture.png", "changed", 0o600)
	if err := verifyAnalysisInputs(repo, request, response); err == nil {
		t.Fatal("changed asset received valid provider evidence")
	}
}

func TestProviderEditsRejectAllLinkedTargetsBeforeWriting(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/first.js", "first", 0o600)
	writeTestFile(t, root, "target.js", "original", 0o600)
	if err := os.Symlink("../target.js", filepath.Join(root, "src/linked.js")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	repo := repository.Repository{Root: root}
	request := Request{Capability: "format", Mode: "write", Files: []string{"src/first.js", "src/linked.js"}}
	response := Response{Status: "pass", Edits: []Edit{{Path: "src/first.js", Content: "new first"}, {Path: "src/linked.js", Content: "bad"}}}
	if err := applyEdits(repo, request, response); err == nil {
		t.Fatal("linked write target accepted")
	}
	for path, want := range map[string]string{"src/first.js": "first", "target.js": "original"} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || string(data) != want {
			t.Fatalf("rejected edits changed %s: %q %v", path, data, err)
		}
	}
}

func TestProviderNotesRemainVisibleWithoutCreatingASeverityFailure(t *testing.T) {
	response := Response{Status: "pass", Notes: []string{"external browser script was not fetched"}}
	findings := analysisFindings(repository.Repository{}, &policy.PackAdapter{PackName: "javascript", Capability: "architecture"}, response)
	if len(findings) != 1 || findings[0].Severity != policy.FindingInformation || findings[0].Message != response.Notes[0] {
		t.Fatalf("boundary findings = %+v", findings)
	}
}
