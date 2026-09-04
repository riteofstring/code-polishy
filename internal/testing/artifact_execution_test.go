package testing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/testartifact"
)

const artifactHelperMode = "CODE_POLISHY_ARTIFACT_HELPER_MODE"

func TestExecuteSuiteCapturesManagedJUnitArtifact(t *testing.T) {
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(artifactHelperMode, "write")
	suite := policy.TestSuite{
		Name: "artifact-helper", Kind: "unit", Scope: "repository", Cwd: ".", TimeoutSeconds: 30,
		Argv: []string{executable, "-test.run=^TestManagedArtifactHelper$"}, Environment: []string{artifactHelperMode},
		Artifacts: []policy.TestArtifact{{Path: "junit.xml", Type: "junit", Required: true}},
	}
	execution := ExecuteSuite(t.Context(), root, runner.OSRunner{}, suite, 1)
	if execution.Failed() || len(execution.Artifacts) != 1 || execution.Artifacts[0].SHA256 == "" ||
		execution.Artifacts[0].Path != "artifact-helper/junit.xml" {
		t.Fatalf("execution = %+v", execution)
	}
	if _, err := os.Stat(filepath.Join(root, testartifact.RootName, executionArtifactID(t, root), "artifact-helper", "junit.xml")); err != nil {
		t.Fatalf("retained JUnit artifact is unavailable: %v", err)
	}
}

func TestExecuteSuiteFailsWhenRequiredArtifactIsMissing(t *testing.T) {
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(artifactHelperMode, "omit")
	suite := policy.TestSuite{
		Name: "artifact-helper", Kind: "unit", Scope: "repository", Cwd: ".", TimeoutSeconds: 30,
		Argv: []string{executable, "-test.run=^TestManagedArtifactHelper$"}, Environment: []string{artifactHelperMode},
		Artifacts: []policy.TestArtifact{{Path: "junit.xml", Type: "junit", Required: true}},
	}
	execution := ExecuteSuite(t.Context(), root, runner.OSRunner{}, suite, 1)
	if !execution.Failed() || execution.FailureCategory != runner.FailureOperational || !strings.Contains(execution.FailureMessage, "required junit artifact") {
		t.Fatalf("execution = %+v", execution)
	}
}

func TestExecuteSuiteKeepsCommandFailureAndCapturesArtifact(t *testing.T) {
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(artifactHelperMode, "write-fail")
	suite := policy.TestSuite{
		Name: "artifact-helper", Kind: "unit", Scope: "repository", Cwd: ".", TimeoutSeconds: 30,
		Argv: []string{executable, "-test.run=^TestManagedArtifactHelper$"}, Environment: []string{artifactHelperMode},
		Artifacts: []policy.TestArtifact{{Path: "junit.xml", Type: "junit", Required: true}},
	}
	execution := ExecuteSuite(t.Context(), root, runner.OSRunner{}, suite, 1)
	if !execution.Failed() || execution.FailureCategory != runner.FailureCommandExit || len(execution.Artifacts) != 1 {
		t.Fatalf("execution = %+v", execution)
	}
}

func TestManagedArtifactHelper(t *testing.T) {
	mode := os.Getenv(artifactHelperMode)
	if mode == "" {
		return
	}
	directory := os.Getenv(testartifact.EnvironmentDirectory)
	executionID := os.Getenv(testartifact.EnvironmentID)
	if directory == "" || executionID == "" || !strings.HasPrefix(filepath.Base(filepath.Dir(directory)), "run-") {
		os.Exit(91)
	}
	if mode == "write" || mode == "write-fail" {
		if err := os.WriteFile(filepath.Join(directory, "junit.xml"), []byte("<testsuite tests=\"1\"/>\n"), 0o600); err != nil {
			os.Exit(92)
		}
	}
	if mode == "write-fail" {
		os.Exit(17)
	}
}

func executionArtifactID(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, testartifact.RootName))
	if err != nil || len(entries) != 1 {
		t.Fatalf("artifact executions = %v, error = %v", entries, err)
	}
	return entries[0].Name()
}
