package testing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestSourceFindingsRejectEmptyAndUnconditionalSkipGoTests(t *testing.T) {
	t.Parallel()
	repo := testQualityRepository(t)
	writeTestQualityFile(t, repo.Root, "sample_test.go", `package sample
import "testing"
func TestEmpty(t *testing.T) {}
func TestSkipped(t *testing.T) { t.Helper(); t.Skip("later") }
`)
	findings := SourceFindings(repo, []string{"sample_test.go"})
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestSourceFindingsAcceptObservableGoAssertion(t *testing.T) {
	t.Parallel()
	repo := testQualityRepository(t)
	writeTestQualityFile(t, repo.Root, "sample_test.go", `package sample
import "testing"
func TestValue(t *testing.T) {
	got := 2 + 2
	if got != 4 { t.Fatalf("got %d", got) }
}
`)
	if findings := SourceFindings(repo, []string{"sample_test.go"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestSourceFindingsRejectEmptyGoSubtest(t *testing.T) {
	t.Parallel()
	repo := testQualityRepository(t)
	writeTestQualityFile(t, repo.Root, "sample_test.go", `package sample
import "testing"
func TestCases(t *testing.T) {
	t.Run("empty", func(t *testing.T) {})
}
`)
	findings := SourceFindings(repo, []string{"sample_test.go"})
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "subtest") || !strings.Contains(findings[0].Message, "empty") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestSourceFindingsRecognizeBareTestEntryName(t *testing.T) {
	t.Parallel()
	repo := testQualityRepository(t)
	writeTestQualityFile(t, repo.Root, "sample_test.go", `package sample
import "testing"
func Test(t *testing.T) {}
`)
	if findings := SourceFindings(repo, []string{"sample_test.go"}); len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
}

func testQualityRepository(t *testing.T) repository.Repository {
	t.Helper()
	return repository.Repository{Root: t.TempDir(), Config: policy.Config{}}
}

func writeTestQualityFile(t *testing.T, root, path, contents string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
