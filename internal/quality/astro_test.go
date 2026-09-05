package quality

import (
	"path/filepath"
	"testing"
)

func TestAstroOnlyChangeRunsDeadCodeAnalysis(t *testing.T) {
	repo := qualityRepository(t)
	var err error
	repo.PolicyRoot, err = filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repo.Config.Scope.EntryPoints = []string{"page.astro"}
	for path, content := range map[string]string{
		"package.json": `{"name":"fixture","type":"module"}`,
		"page.astro":   "---\nimport Card from './Card.astro';\n---\n<Card />\n",
		"Card.astro":   "<div>Used</div>\n",
		"orphan.astro": "<div>Unused</div>\n",
	} {
		writeQualityFile(t, repo.Root, path, content)
	}
	findings := JavaScriptDeadCodeFindings(t.Context(), repo, []string{"page.astro"})
	if len(findings) != 1 || findings[0].Check != "quality.deadCode" || findings[0].Path != "orphan.astro" {
		t.Fatalf("Astro-only change findings = %+v", findings)
	}
}
