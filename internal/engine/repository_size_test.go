package engine

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestSizePublishesIncompleteWorkspaceWithoutLosingLineBudget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeEngineFile(t, root, "src/app.go", "package app\n\nfunc Run() {}\n", 0o600)
	writeEngineFile(t, root, "node_modules/private/output", "not measured\n", 0o600)
	blocked := filepath.Join(root, "node_modules", "private")
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })
	if _, err := os.ReadDir(blocked); err == nil {
		t.Skip("this host does not enforce directory read permissions for the test user")
	} else if !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
	policyEngine := &Engine{Repository: repository.Repository{Root: root, Config: policy.Config{
		Quality: policy.Quality{LineBudget: &policy.LineBudget{Production: 10}},
	}}}
	report, err := policyEngine.Size("")
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Status != "passed" || report.RepositorySize.SourceLines.Production != 2 {
		t.Fatalf("partial workspace discarded source evidence: %+v", report)
	}
	if !strings.Contains(strings.Join(report.Notes, " "), "workspace totals are incomplete") {
		t.Fatalf("missing incomplete note: %v", report.Notes)
	}
	for title, row := range map[string][]string{
		"UNREADABLE WORKSPACE PATHS": {"node_modules/private"},
		"SOURCE LINES":               {"all", "2", "0"},
		"LINE BUDGET":                {"production", "2", "10", "8"},
	} {
		if !slices.ContainsFunc(report.Tables, func(table Table) bool {
			return table.Title == title && len(table.Rows) > 0 && slices.Equal(table.Rows[0], row)
		}) {
			t.Fatalf("missing %s evidence: %+v", title, report.Tables)
		}
	}
	if report.Tables[0].Rows[0][0] != "workspace (incomplete)" {
		t.Fatalf("workspace total not labeled incomplete: %+v", report.Tables[0])
	}
	finalized, err := policyEngine.FinalizeReport("size", report)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(finalized.ReportPath)))
	if err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	workspace := decoded.RepositorySize.Workspace
	if !workspace.Incomplete || workspace.UnreadablePathCount != 1 || !slices.Equal(workspace.UnreadablePaths, []string{"node_modules/private"}) {
		t.Fatalf("machine evidence lost unreadable paths: %+v", workspace)
	}
}

func TestSizeBuildsHumanAndSchemaValidatedMachineEvidence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeEngineFile(t, root, "src/app.go", string(make([]byte, 2048)), 0o600)
	writeEngineFile(t, root, "docs/guide.md", string(make([]byte, 1024)), 0o600)
	policyEngine := &Engine{Repository: repository.Repository{
		Root: root,
		Config: policy.Config{
			Modules:    []policy.Module{{Name: "application", Paths: []string{"src/**"}}},
			Exceptions: []policy.Exception{expiredEngineException()},
		},
	}}

	report, err := policyEngine.Size("")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 || report.Summary.Status != "passed" {
		t.Fatalf("unrelated policy status changed the size outcome: %+v", report.Findings)
	}
	if report.RepositorySize == nil || report.RepositorySize.Governed.Files != 2 || report.RepositorySize.Governed.Bytes != 3072 {
		t.Fatalf("repository size = %+v", report.RepositorySize)
	}
	titles := make([]string, 0, len(report.Tables))
	for _, table := range report.Tables {
		titles = append(titles, table.Title)
	}
	for _, title := range []string{"REPOSITORY SIZE", "WORKSPACE CATEGORIES", "GOVERNED CONTENT", "GOVERNED MODULES", "LARGEST GOVERNED FILES"} {
		if !slices.Contains(titles, title) {
			t.Errorf("tables = %v, missing %s", titles, title)
		}
	}
	finalized, err := policyEngine.FinalizeReport("size", report)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(finalized.ReportPath)))
	if err != nil {
		t.Fatal(err)
	}
	decoded := Report{}
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.RepositorySize == nil || decoded.RepositorySize.Governed.Bytes != 3072 {
		t.Fatalf("decoded report = %+v, error = %v", decoded.RepositorySize, err)
	}
}

func TestFormatSizeBytesUsesSignedIECUnits(t *testing.T) {
	t.Parallel()
	if got := FormatSizeBytes(1536); got != "1.5 KiB" {
		t.Fatalf("formatted size = %q", got)
	}
	if got := formatSignedSizeBytes(-2 << 20); got != "-2.0 MiB" {
		t.Fatalf("formatted signed size = %q", got)
	}
	if got := formatSignedSizeBytes(7); got != "+7 B" {
		t.Fatalf("formatted positive size = %q", got)
	}
}

func TestRepositorySizeSummaryDistinguishesWorkingTreeAndGitBlobTotals(t *testing.T) {
	t.Parallel()
	analysis := repository.SizeAnalysis{
		Workspace: repository.SizeWorkspace{Files: 4, Bytes: 400},
		Governed:  repository.SizeGoverned{Files: 3, Bytes: 300},
		Comparison: &repository.SizeComparison{
			Base: repository.SizeTotal{Files: 1, Bytes: 100}, Current: repository.SizeTotal{Files: 2, Bytes: 200},
			DeltaFiles: 1, DeltaBytes: 100,
		},
	}
	table := repositorySizeSummaryTable(analysis)
	labels := make([]string, 0, len(table.Rows))
	for _, row := range table.Rows {
		labels = append(labels, row[0])
	}
	want := []string{"workspace", "governed working tree", "base Git blobs", "current Git blobs", "change from base"}
	if !slices.Equal(labels, want) {
		t.Fatalf("summary labels = %v", labels)
	}
}
