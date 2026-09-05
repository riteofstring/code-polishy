package architecture

import (
	"path/filepath"
	"testing"
)

func TestAstroRouteImportsEnforceModuleDirection(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		repo := javascriptRepository(t, allowed)
		var err error
		repo.PolicyRoot, err = filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			t.Fatal(err)
		}
		writeArchitectureFile(t, repo.Root, "web/[slug].astro", "---\nimport type { Model } from '../domain/model.js';\nconst value: Model = 'example';\n---\n<p>{value}</p>\n")
		findings := Check(t.Context(), repo, []string{"web/[slug].astro"})
		if allowed {
			if len(findings) != 0 {
				t.Fatalf("declared Astro dependency failed: %+v", findings)
			}
		} else if len(findings) != 1 || findings[0].Check != "architecture.moduleDependency" || findings[0].Path != "web/[slug].astro" || findings[0].Subject != "domain" {
			t.Fatalf("undeclared Astro dependency was not reported: %+v", findings)
		}
	}
}
