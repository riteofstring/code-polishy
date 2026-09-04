package testartifact

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestExecutionExposesContainedSuiteDirectoryAndCapturesXML(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	execution, err := Start(root, "")
	if err != nil {
		t.Fatal(err)
	}
	suite := policy.TestSuite{
		Name: "unit", Artifacts: []policy.TestArtifact{
			{Path: "junit.xml", Type: "junit", Required: true},
			{Path: "coverage/cobertura.xml", Type: "cobertura"},
		},
	}
	command, err := execution.PrepareCommand(suite, policy.Command{Argv: []string{"test", "--junit=" + DirectoryToken + "/junit.xml", ExecutionIDToken}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(command.Argv[1], filepath.Join(execution.Directory(), "unit")) || command.Argv[2] != execution.ID() ||
		!slices.Contains(command.EnvironmentOverrides, EnvironmentID+"="+execution.ID()) {
		t.Fatalf("prepared command = %+v", command)
	}
	writeArtifactFile(t, command.TestArtifactDirectory, "junit.xml", "<testsuites><testsuite/></testsuites>\n")
	writeArtifactFile(t, command.TestArtifactDirectory, "coverage/cobertura.xml", "<coverage/>\n")
	records, err := execution.ValidateCommand(command)
	if err != nil || len(records) != 2 || records[0].SHA256 == "" || records[1].Path != "unit/coverage/cobertura.xml" {
		t.Fatalf("records = %+v, error = %v", records, err)
	}
	if err := execution.Complete(); err != nil {
		t.Fatal(err)
	}
	owned, err := readManifest(execution.Directory())
	if err != nil || owned.Status != "completed" {
		t.Fatalf("manifest = %+v, error = %v", owned, err)
	}
}

func TestArtifactValidationFailsClosed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, content, artifactType, want string
	}{
		{name: "malformed", content: "<testsuite>", artifactType: "junit", want: "malformed XML"},
		{name: "doctype", content: "<!DOCTYPE testsuite><testsuite/>", artifactType: "junit", want: "forbidden"},
		{name: "junit shape", content: "<coverage/>", artifactType: "junit", want: "JUnit root"},
		{name: "cobertura shape", content: "<testsuite/>", artifactType: "cobertura", want: "Cobertura root"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			execution, err := Start(root, "")
			if err != nil {
				t.Fatal(err)
			}
			suite := policy.TestSuite{Name: "unit", Artifacts: []policy.TestArtifact{{Path: "result.xml", Type: test.artifactType, Required: true}}}
			command, err := execution.PrepareCommand(suite, policy.Command{})
			if err != nil {
				t.Fatal(err)
			}
			writeArtifactFile(t, command.TestArtifactDirectory, "result.xml", test.content)
			if _, err := execution.ValidateCommand(command); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestArtifactValidationRejectsMissingLinksAndOversize(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	execution, err := Start(root, "")
	if err != nil {
		t.Fatal(err)
	}
	suite := policy.TestSuite{Name: "unit", Artifacts: []policy.TestArtifact{{Path: "result.xml", Type: "junit", Required: true}}}
	command, err := execution.PrepareCommand(suite, policy.Command{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execution.ValidateCommand(command); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing artifact error = %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.xml")
	if err := os.WriteFile(outside, []byte("<testsuite/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(command.TestArtifactDirectory, "result.xml")
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	if _, err := execution.ValidateCommand(command); err == nil || !strings.Contains(err.Error(), "linked") {
		t.Fatalf("linked artifact error = %v", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(target, MaximumArtifactBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := execution.ValidateCommand(command); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized artifact error = %v", err)
	}
}

func TestOptionalArtifactMayOmitItsParentDirectory(t *testing.T) {
	t.Parallel()
	execution, err := Start(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	suite := policy.TestSuite{Name: "unit", Artifacts: []policy.TestArtifact{{Path: "coverage/result.xml", Type: "cobertura"}}}
	command, err := execution.PrepareCommand(suite, policy.Command{})
	if err != nil {
		t.Fatal(err)
	}
	records, err := execution.ValidateCommand(command)
	if err != nil || len(records) != 0 {
		t.Fatalf("records = %+v, error = %v", records, err)
	}
}

func TestPruneRetainsCurrentActiveUnknownAndLinkedTrees(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for index := 0; index < 5; index++ {
		execution, err := Start(root, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := execution.Complete(); err != nil {
			t.Fatal(err)
		}
	}
	artifactRoot := filepath.Join(root, RootName)
	unknown := filepath.Join(artifactRoot, "unknown")
	if err := os.Mkdir(unknown, 0o700); err != nil {
		t.Fatal(err)
	}
	active, err := Start(root, "")
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	linked := filepath.Join(artifactRoot, "linked")
	if err := os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	current, err := Start(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := current.Complete(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{unknown, active.Directory(), linked, current.Directory(), outside} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("protected path %s was removed: %v", path, err)
		}
	}
	completed := 0
	entries, err := os.ReadDir(artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if owned, err := readManifest(filepath.Join(artifactRoot, entry.Name())); err == nil && owned.Status == "completed" {
				completed++
			}
		}
	}
	if completed != retainedExecutions {
		t.Fatalf("retained completed executions = %d, want %d", completed, retainedExecutions)
	}
}

func TestArtifactRootRejectsSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, RootName)); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(root, ""); err == nil || !strings.Contains(err.Error(), "regular directory") {
		t.Fatalf("symlink root error = %v", err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("outside artifact root changed: %v, %v", entries, err)
	}
}

func writeArtifactFile(t *testing.T, root, relative, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAbandonLeavesAnActiveExecutionForInspection(t *testing.T) {
	t.Parallel()
	execution, err := Start(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.Abandon(); err != nil {
		t.Fatal(err)
	}
	owned, err := readManifest(execution.Directory())
	if err != nil || owned.Status != "active" {
		t.Fatalf("abandoned manifest = %+v, error = %v", owned, err)
	}
	if _, err := os.Stat(filepath.Join(execution.Directory(), leaseFilename)); err != nil {
		t.Fatalf("abandoned execution lost its lease: %v", err)
	}
}
