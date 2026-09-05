package javascript

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAstroImportsIncludeFrontmatterClientScriptsAndExpressions(t *testing.T) {
	bundle, root := astroFixture(t)
	source := "---\nimport Card from './Card.astro';\nimport { used } from './values.js';\n---\n<Card value={used} />\n<script>\nimport { play } from './client.js';\nplay();\n</script>\n{await import('./late.js')}\n"
	writeAstroFixture(t, root, "page.astro", source)
	reported, err := bundle.Imports(t.Context(), root, []string{"page.astro"})
	if err != nil {
		t.Fatal(err)
	}
	if len(reported.Unsupported) != 0 || len(reported.Imports) != 4 {
		t.Fatalf("Astro import coverage: %+v", reported)
	}
	for _, expected := range []struct {
		specifier string
		line      int
		resolved  string
	}{
		{"./Card.astro", 2, "Card.astro"}, {"./values.js", 3, "values.js"}, {"./client.js", 7, "client.js"}, {"./late.js", 10, "late.js"},
	} {
		if !slices.ContainsFunc(reported.Imports, func(entry ImportFact) bool {
			return entry.Specifier == expected.specifier && entry.Line == expected.line && entry.Resolved == expected.resolved
		}) {
			t.Errorf("missing original import %+v in %+v", expected, reported.Imports)
		}
	}
}

func TestAstroDeadCodeTracksMarkupUsageAndUnusedExports(t *testing.T) {
	bundle, root := astroFixture(t)
	writeAstroFixture(t, root, "page.astro", "---\nimport Card from './Card.astro';\nimport { used } from './values.js';\n---\n<Card value={used} />\n")
	paths := []string{"page.astro", "Card.astro", "values.js", "orphan.astro"}
	reported, err := bundle.DeadCode(t.Context(), root, ".", []DeadCodeWorkspace{{Root: ".", Entry: []string{"page.astro"}, Project: paths}})
	if err != nil {
		t.Fatal(err)
	}
	if len(reported.Unsupported) != 0 || len(reported.Covered) != len(paths) {
		t.Fatalf("Astro dead-code coverage: %+v", reported)
	}
	if !slices.Contains(reported.UnusedFiles, "orphan.astro") || slices.Contains(reported.UnusedFiles, "Card.astro") {
		t.Fatalf("incorrect component reachability: %+v", reported.UnusedFiles)
	}
	if !slices.ContainsFunc(reported.UnusedExports, func(entry UnusedExport) bool { return entry.Path == "values.js" && entry.Symbol == "unused" }) {
		t.Fatalf("unused export was missed: %+v", reported.UnusedExports)
	}
	if slices.ContainsFunc(reported.UnusedExports, func(entry UnusedExport) bool { return entry.Symbol == "used" }) {
		t.Fatalf("markup reference was missed: %+v", reported.UnusedExports)
	}
}

func TestAstroComputedImportsRemainVisible(t *testing.T) {
	bundle, root := astroFixture(t)
	writeAstroFixture(t, root, "page.astro", "---\nconst path = Astro.props.path;\n---\n{await import(path)}\n")
	reported, err := bundle.Imports(t.Context(), root, []string{"page.astro"})
	if err != nil {
		t.Fatal(err)
	}
	if len(reported.Unsupported) != 1 || !strings.Contains(reported.Unsupported[0].Reason, "line 4:") {
		t.Fatalf("computed import lost its original location: %+v", reported)
	}
}

func TestAstroClientScriptUsageReachesItsModule(t *testing.T) {
	bundle, root := astroFixture(t)
	writeAstroFixture(t, root, "page.astro", "<script>\nimport { play } from './client.js';\nplay();\n</script>\n")
	reported, err := bundle.DeadCode(t.Context(), root, ".", []DeadCodeWorkspace{{Root: ".", Entry: []string{"page.astro"}, Project: []string{"page.astro", "client.js"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(reported.Unsupported) != 0 || len(reported.UnusedFiles) != 0 || len(reported.UnusedExports) != 0 {
		t.Fatalf("client script dependency was lost: %+v", reported)
	}
}

func TestAstroMalformedCodeCannotClaimImportCoverage(t *testing.T) {
	bundle, root := astroFixture(t)
	writeAstroFixture(t, root, "page.astro", "---\nconst = ;\n---\n<div />\n")
	reported, err := bundle.Imports(t.Context(), root, []string{"page.astro"})
	if err != nil {
		t.Fatal(err)
	}
	if len(reported.Unsupported) != 1 {
		t.Fatalf("malformed Astro source was accepted: %+v", reported)
	}
}

func astroFixture(t *testing.T) (Bundle, string) {
	t.Helper()
	policyRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for name, source := range map[string]string{
		"package.json": `{"name":"fixture","type":"module","dependencies":{"astro":"7.1.0"}}`,
		"Card.astro":   "<div>{Astro.props.value}</div>\n",
		"orphan.astro": "<div>Unused</div>\n",
		"values.js":    "export const used = 1;\nexport const unused = 2;\n",
		"client.js":    "export function play() {}\n",
		"late.js":      "export const detail = 1;\n",
	} {
		writeAstroFixture(t, root, name, source)
	}
	return Bundle{PolicyRoot: policyRoot}, root
}

func writeAstroFixture(t *testing.T, root, name, source string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}
