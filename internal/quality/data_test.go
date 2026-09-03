package quality

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestDataSyntaxValidationUsesTheParseOnlyBundleForAllStructuredFormats(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Data = []string{"data/**/*.json", "data/**/*.jsonc", "data/**/*.yaml", "data/**/*.yml"}
	policyRoot, observed := fakeFileBundle(t, `{"covered":["data/catalog.json","data/catalog.jsonc","data/catalog.yaml","data/catalog.yml"],"unsupported":[]}`)
	repo.PolicyRoot = policyRoot
	files := map[string]string{
		"data/catalog.json":  "{  \"name\": \"catalog\"  }\n",
		"data/catalog.jsonc": "{\n  // comments are valid JSONC\n  \"name\": \"catalog\",\n}\n",
		"data/catalog.yaml":  "name: catalog\nitems:\n  - one\n",
		"data/catalog.yml":   "name: catalog\nitems:\n  - one\n",
	}
	for path, contents := range files {
		writeQualityFile(t, repo.Root, path, contents)
	}
	paths := []string{"data/catalog.json", "data/catalog.jsonc", "data/catalog.yaml", "data/catalog.yml"}
	before := map[string][]byte{}
	for _, path := range paths {
		contents, err := os.ReadFile(filepath.Join(repo.Root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		before[path] = contents
	}

	if findings := DataSyntaxFindings(t.Context(), repo, paths); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	requests := observedRequestLines(t, observed)
	if len(requests) != 1 || !strings.Contains(requests[0], `"operation":"parse"`) ||
		!strings.Contains(requests[0], `"paths":["data/catalog.json","data/catalog.jsonc","data/catalog.yaml","data/catalog.yml"]`) {
		t.Fatalf("requests = %v", requests)
	}
	for _, path := range paths {
		after, err := os.ReadFile(filepath.Join(repo.Root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(after, before[path]) {
			t.Fatalf("parse changed %s: before %q, after %q", path, before[path], after)
		}
	}
}

func TestDataSyntaxValidationReportsMalformedStructuredData(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Data = []string{"data/**/*.json"}
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"covered":[],"unsupported":[{"path":"data/broken.json","reason":"unexpected token"}]}`)
	writeQualityFile(t, repo.Root, "data/broken.json", "{\n")

	findings := DataSyntaxFindings(t.Context(), repo, []string{"data/broken.json"})
	if len(findings) != 1 || findings[0].Check != "quality.dataSyntax" || findings[0].Path != "data/broken.json" ||
		findings[0].Subject != "json" || !strings.Contains(findings[0].Message, "unexpected token") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDataSyntaxValidationRejectsNonTextAndOversizedData(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		contents []byte
		check    string
	}{
		"non-UTF-8": {contents: []byte{0xff, 0xfe}, check: "quality.dataText"},
		"oversized": {contents: []byte(strings.Repeat(" ", int(structuredDataMaximumBytes)+1)), check: "quality.dataSize"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			repo := qualityRepository(t)
			repo.Config.Scope.Data = []string{"data/**/*.json"}
			path := filepath.Join(repo.Root, "data", "identity.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, test.contents, 0o600); err != nil {
				t.Fatal(err)
			}
			findings := DataSyntaxFindings(t.Context(), repo, []string{"data/identity.json"})
			if len(findings) != 1 || findings[0].Check != test.check {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestCheckValidatesDataAndKeepsItVisibleToNonFormattingProviders(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Data = []string{"data/**/*.yaml"}
	repo.Config.Checks = []policy.Command{
		{Name: "schema", Provides: []string{"schema"}, Argv: []string{"schema-check"}, Paths: []string{"data/**/*.yaml"}, PassFiles: true, RunOn: []string{"check"}},
		{Name: "formatter", Provides: []string{"format"}, Argv: []string{"format"}, Paths: []string{"data/**/*.yaml"}, PassFiles: true, RunOn: []string{"format"}},
	}
	policyRoot, observed := fakeFileBundle(t, `{"covered":["data/identity.yaml"],"unsupported":[]}`)
	repo.PolicyRoot = policyRoot
	path := "data/identity.yaml"
	contents := "identity:   keep these bytes\n"
	writeQualityFile(t, repo.Root, path, contents)
	selection := repository.Selection{Files: []string{path}, Candidate: repository.CandidateDelta{AddedOrModified: []string{path}}}
	checkRunner := &recordingQualityRunner{}

	if findings := Check(t.Context(), repo, selection, checkRunner, "check"); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	_, schema, found := checkRunner.command("schema")
	if !found || !slices.Equal(schema.Argv, []string{"schema-check", path}) {
		t.Fatalf("schema command = %+v, found = %t", schema, found)
	}
	if requests := observedRequestLines(t, observed); len(requests) != 1 || !strings.Contains(requests[0], `"operation":"parse"`) {
		t.Fatalf("requests = %v", requests)
	}

	formatRunner := &recordingQualityRunner{}
	if findings := Format(t.Context(), repo, selection, formatRunner); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	if _, _, found := formatRunner.command("formatter"); found {
		t.Fatal("format provider received declared data")
	}
	preserved, err := os.ReadFile(filepath.Join(repo.Root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(preserved) != contents {
		t.Fatalf("format changed data bytes: %q", preserved)
	}
}
