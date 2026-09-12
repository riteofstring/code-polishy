package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestSizeBuildsHumanAndSchemaValidatedMachineEvidence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeEngineFile(t, root, "src/app.go", string(make([]byte, 2048)), 0o600)
	writeEngineFile(t, root, "docs/guide.md", string(make([]byte, 1024)), 0o600)
	policyEngine := &Engine{Repository: repository.Repository{
		Root:   root,
		Config: policy.Config{Modules: []policy.Module{{Name: "application", Paths: []string{"src/**"}}}},
	}}

	report, err := policyEngine.Size("")
	if err != nil {
		t.Fatal(err)
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
	if got := formatSizeBytes(1536); got != "1.5 KiB" {
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
