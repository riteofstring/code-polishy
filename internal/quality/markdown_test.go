package quality

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestDocumentationFindingsFormatsChangedMarkdownAndValidatesTheCorpus(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	policyRoot, observed := fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	repo.PolicyRoot = policyRoot
	writeQualityFile(t, repo.Root, "README.MD", "# Root\n\n[Guide](docs/guide.markdown#details)\n")
	writeQualityFile(t, repo.Root, "docs/guide.markdown", "# Guide\n\n## Details\n")
	findings := DocumentationFindings(t.Context(), repo, []string{"README.MD"}, nil)
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(request), `"paths":["README.MD"]`) {
		t.Fatalf("formatter selection = %s", request)
	}
}

func TestDocumentationFindingsRejectsChangedMarkdownFormatting(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":["README.md"],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "README.md", "# Root\n")
	findings := DocumentationFindings(t.Context(), repo, []string{"README.md"}, nil)
	if len(findings) != 1 || findings[0].Check != "quality.format" || findings[0].Path != "README.md" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsDetectsInboundLinksAfterDeletion(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	writeQualityFile(t, repo.Root, "docs/index.md", "# Index\n\n[Guide](guide.md)\n")
	findings := DocumentationFindings(t.Context(), repo, nil, []string{"docs/guide.md"})
	if len(findings) != 1 || findings[0].Check != "quality.documentationLink" ||
		findings[0].Path != "docs/index.md" || findings[0].Subject != "3:1" ||
		!strings.Contains(findings[0].Message, "local target is missing: docs/guide.md") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsNormalizesDuplicateHeadingAnchors(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "README.md", "# Root\n\n[First](docs/target.md#same)\n[Second](docs/target.md#same-1)\n")
	writeQualityFile(t, repo.Root, "docs/target.md", "# Same\n\n# Same\n")
	findings := DocumentationFindings(t.Context(), repo, []string{"README.md"}, nil)
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsValidatesReferenceDefinitionsContinuedOnTheNextLine(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "README.md", "# Root\n\n[Guide][guide]\n\n[guide]:\n  docs/guide.md#details\n")
	writeQualityFile(t, repo.Root, "docs/guide.md", "# Guide\n\n## Details\n")
	findings := DocumentationFindings(t.Context(), repo, []string{"README.md"}, nil)
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsParsesMarkdownGrammarBoundaries(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "README.md", strings.Join([]string{
		"# Root",
		"",
		"[Nested [label]](docs/a(b).md)",
		"[Explicit][guide] [Collapsed][] [Shortcut]",
		"![Image](assets/logo.png)",
		"\\[Escaped](missing.md) and `[Inline](missing.md)`",
		"",
		"```markdown",
		"[Fenced](missing.md)",
		"```",
		"",
		"~~~",
		"[Tilde fenced](missing.md)",
		"~~~",
		"",
		"[guide]: <docs/guide.md>",
		"[collapsed]: docs/guide.md optional title",
		"[shortcut]: docs/guide.md",
		"",
		"   ###### Six hashes ##",
		"Setext heading",
		"===",
		"",
	}, "\n"))
	writeQualityFile(t, repo.Root, "docs/a(b).md", "# Nested target\n")
	writeQualityFile(t, repo.Root, "docs/guide.md", "# Guide\n")
	writeQualityFile(t, repo.Root, "assets/logo.png", "image\n")
	if findings := DocumentationFindings(t.Context(), repo, []string{"README.md"}, nil); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsAcceptsEmptyMarkdownDocuments(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "README.md", "")
	findings := DocumentationFindings(t.Context(), repo, []string{"README.md"}, nil)
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsRejectsBrokenFragmentWithStableLocation(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "README.md", "# Root\n\n[Missing](docs/target.md#missing)\n")
	writeQualityFile(t, repo.Root, "docs/target.md", "# Target\n")
	findings := DocumentationFindings(t.Context(), repo, []string{"README.md"}, nil)
	if len(findings) != 1 || findings[0].Check != "quality.documentationLink" ||
		findings[0].Path != "README.md" || findings[0].Subject != "3:1" ||
		!strings.Contains(findings[0].Message, "Markdown fragment target is missing: #missing") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsValidatesImagesWithoutHeadingRequirements(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "README.md", "# Root\n\n![Logo](assets/logo.png#preview)\n")
	writeQualityFile(t, repo.Root, "assets/logo.png", "image\n")
	findings := DocumentationFindings(t.Context(), repo, []string{"README.md"}, nil)
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsRejectsTextHygieneWithLocations(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "README.md", "# Root  ")
	findings := DocumentationFindings(t.Context(), repo, []string{"README.md"}, nil)
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Check != "quality.documentationFinalNewline" || findings[0].Subject != "1:9" {
		t.Fatalf("newline finding = %+v", findings[0])
	}
	if findings[1].Check != "quality.documentationWhitespace" || findings[1].Subject != "1:7" {
		t.Fatalf("whitespace finding = %+v", findings[1])
	}
}

func TestDocumentationFindingsRejectsInvalidMarkdownInputs(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	invalid := filepath.Join(repo.Root, "docs", "broken.md")
	if err := os.MkdirAll(filepath.Dir(invalid), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalid, []byte{'#', ' ', 0xff, '\n'}, 0o600); err != nil {
		t.Fatal(err)
	}
	findings := DocumentationFindings(t.Context(), repo, []string{"docs/broken.md", "docs/other.txt"}, nil)
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	checks := map[string]policy.Finding{}
	for _, finding := range findings {
		checks[finding.Check] = finding
	}
	if checks["quality.documentationText"].Subject != "UTF-8" || checks["quality.documentationPath"].Path != "docs/other.txt" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsRejectsOversizedMarkdown(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	path := filepath.Join(repo.Root, "docs", "large.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, int(documentationMaximumBytes)+1), 0o600); err != nil {
		t.Fatal(err)
	}
	findings := DocumentationFindings(t.Context(), repo, []string{"docs/large.md"}, nil)
	if len(findings) != 1 || findings[0].Check != "quality.documentationSize" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsAcceptsExternalAndFragmentOnlyLinks(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "README.md", "# Root\n\n[External](https://example.invalid/missing.md)\n[Root](#root)\n")
	findings := DocumentationFindings(t.Context(), repo, []string{"README.md"}, nil)
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsRecognizesExternalSchemeBoundaries(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "README.md", "# Root\n\n[a](a:x) [z](z:x) [A](A:x) [Z](Z:x) [zero](0:x) [nine](9:x) [plus](a+:x) [minus](a-:x) [dot](a.:x)\n")
	if findings := DocumentationFindings(t.Context(), repo, []string{"README.md"}, nil); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsRejectsMalformedAndPlatformAmbiguousTargets(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "README.md", "# Root\n\n[Encoding](docs/%zz.md)\n[Backslash](docs%5Cguide.md)\n[Drive](C:/outside.md)\n")
	findings := DocumentationFindings(t.Context(), repo, []string{"README.md"}, nil)
	if len(findings) != 3 || findings[0].Subject != "3:1" || findings[1].Subject != "4:1" || findings[2].Subject != "5:1" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsRejectsDriveLetterBoundaries(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "README.md", "# Root\n\n[a](a:/outside) [z](z:\\outside) [A](A:/outside) [Z](Z:\\outside)\n")
	findings := DocumentationFindings(t.Context(), repo, []string{"README.md"}, nil)
	if len(findings) != 4 {
		t.Fatalf("findings = %+v", findings)
	}
	for _, finding := range findings {
		if finding.Check != "quality.documentationLink" || !strings.Contains(finding.Message, "escapes the repository") {
			t.Fatalf("finding = %+v", finding)
		}
	}
}

func TestMarkdownLocalTargetRejectsCaseAmbiguity(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	corpus := markdownCorpus{
		files:  map[string]bool{"docs/Guide.md": true},
		folded: map[string][]string{"docs/guide.md": {"docs/Guide.md", "docs/guide.md"}},
	}
	if _, problem := markdownLocalTarget(repo, corpus, "README.md", "docs/Guide.md"); problem != "local target is ambiguous across case-sensitive filesystems" {
		t.Fatalf("problem = %q", problem)
	}
}

func TestDocumentationFindingsRejectsEscapedAndNonRegularTargets(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "docs/index.md", "# Index\n\n[Escape](../../outside.md)\n[Directory](assets)\n")
	if err := os.MkdirAll(filepath.Join(repo.Root, "docs", "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	findings := DocumentationFindings(t.Context(), repo, []string{"docs/index.md"}, nil)
	if len(findings) != 2 || findings[0].Subject != "3:1" || findings[1].Subject != "4:1" ||
		!strings.Contains(findings[0].Message, "escapes") || !strings.Contains(findings[1].Message, "not a regular file") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsRejectsMarkdownSymlinks(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	writeQualityFile(t, repo.Root, "docs/target.md", "# Target\n")
	if err := os.Symlink("target.md", filepath.Join(repo.Root, "docs", "linked.md")); err != nil {
		t.Skipf("create documentation symlink: %v", err)
	}
	findings := DocumentationFindings(t.Context(), repo, []string{"docs/linked.md"}, nil)
	if len(findings) != 1 || findings[0].Check != "quality.documentationPath" || findings[0].Subject != "regular file" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsScansRawMarkdownEntriesInsideGovernedScope(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "README.md", "# Root\n")
	writeQualityFile(t, repo.Root, "docs/target.md", "# Target\n")
	if err := os.Symlink("target.md", filepath.Join(repo.Root, "docs", "linked.md")); err != nil {
		t.Skipf("create documentation symlink: %v", err)
	}
	findings := DocumentationFindings(t.Context(), repo, []string{"README.md"}, nil)
	if len(findings) != 1 || findings[0].Check != "quality.documentationPath" || findings[0].Path != "docs/linked.md" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDocumentationFindingsRespectsExcludedMarkdownPaths(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Exclude = []string{"private/**"}
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "README.md", "# Root\n")
	writeQualityFile(t, repo.Root, "private/broken.md", "# Broken\n\n[Missing](missing.md)\n")
	findings := DocumentationFindings(t.Context(), repo, []string{"README.md"}, nil)
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestFormatWritesOnlyChangedMarkdownForDocumentationCandidates(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	policyRoot, observed := fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	repo.PolicyRoot = policyRoot
	writeQualityFile(t, repo.Root, "README.md", "# Root\n")
	writeQualityFile(t, repo.Root, "internal/sample/file.go", "package sample\n")
	selection := repository.Selection{
		Candidate: repository.CandidateDelta{AddedOrModified: []string{"README.md"}, Deleted: []string{"docs/removed.md"}},
		Files:     []string{"README.md", "internal/sample/file.go"},
		All:       true,
	}
	runner := &recordingQualityRunner{}
	if findings := Format(t.Context(), repo, selection, runner); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("format commands = %+v", runner.commands)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(request), `"paths":["README.md"]`) {
		t.Fatalf("formatter selection = %s", request)
	}
}
