package architecture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStylesheetImportsRetainExistenceAndModuleChecks(t *testing.T) {
	t.Parallel()
	policyRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		allowed     bool
		missing     bool
		escaped     bool
		declaration bool
		check       string
	}{
		"declared module":            {allowed: true},
		"forbidden module":           {check: "architecture.moduleDependency"},
		"missing target":             {allowed: true, missing: true, check: "architecture.importCoverage"},
		"declaration without target": {allowed: true, missing: true, declaration: true, check: "architecture.importCoverage"},
		"escaped target":             {allowed: true, escaped: true, check: "architecture.importCoverage"},
	} {
		t.Run(name, func(t *testing.T) {
			repo := javascriptRepository(t, test.allowed)
			repo.PolicyRoot = policyRoot
			writeArchitectureFile(t, repo.Root, "web/app.ts", "import '../domain/style.css';\n")
			if test.declaration {
				writeArchitectureFile(t, repo.Root, "domain/style.css.d.ts", "export {};\n")
				writeArchitectureFile(t, repo.Root, "domain/style.d.css.ts", "export {};\n")
			}
			if !test.missing {
				writeArchitectureFile(t, repo.Root, "domain/style.css", "body { color: red; }\n")
			}
			if test.escaped {
				outside := t.TempDir()
				writeArchitectureFile(t, outside, "style.css", "body { color: blue; }\n")
				target := filepath.Join(repo.Root, "domain/style.css")
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(outside, "style.css"), target); err != nil {
					t.Fatal(err)
				}
			}
			findings := Check(t.Context(), repo, []string{"web/app.ts"})
			if test.check == "" {
				if len(findings) != 0 {
					t.Fatalf("valid stylesheet import rejected: %+v", findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Check != test.check {
				t.Fatalf("wanted %s, got %+v", test.check, findings)
			}
		})
	}
}
