package architecture

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestFocusedGoGraphDoesNotAnalyzeUnrelatedProjects(t *testing.T) {
	t.Parallel()
	for _, manifest := range []string{"module example.test/unrelated\n\ngo 1.26.5\n", "invalid manifest"} {
		t.Run(manifest, func(t *testing.T) {
			repo := architectureRepository(t, true)
			writeArchitectureFile(t, repo.Root, "unrelated/go.mod", manifest)
			writeArchitectureFile(t, repo.Root, "unrelated/broken.go", "not valid Go")
			writeArchitectureFile(t, repo.Root, "unrelated/go.work", "invalid workspace")
			for _, selected := range []string{"internal/api/api.go", "go.mod"} {
				analysis := AnalyzeWithRunner(t.Context(), repo, []string{selected}, &pythonGraphRunner{})
				if analysis.Graph == nil || len(analysis.Findings) != 0 || len(analysis.Graph.Nodes) != 2 {
					t.Fatalf("focused analysis of %s = %+v", selected, analysis)
				}
			}
			analysis := AnalyzeWithRunner(t.Context(), repo, []string{"unrelated/broken.go"}, &pythonGraphRunner{})
			if analysis.Graph != nil || len(analysis.Findings) == 0 {
				t.Fatalf("selected invalid project passed: %+v", analysis)
			}
		})
	}
}

func TestFocusedGoGraphRetainsTransitivelyConnectedProjects(t *testing.T) {
	t.Parallel()
	repo := architectureRepository(t, true)
	repo.Config.Modules[0].DependsOn = []string{"api"}
	writeArchitectureFile(t, repo.Root, "internal/domain/go.mod", "module example.test/domain\n\ngo 1.26.5\n")
	writeArchitectureFile(t, repo.Root, "internal/api/api.go", "package api\nimport _ \"example.test/domain\"\n")
	writeArchitectureFile(t, repo.Root, "internal/domain/domain.go", "package domain\nimport _ \"example.test/app/internal/api\"\n")
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"internal/api/api.go"}, &pythonGraphRunner{})
	finding := onlyCycleFinding(t, analysis.Findings, "architecture.fileCycle")
	if analysis.Graph == nil || len(finding.DependencyComponent.Members) != 2 {
		t.Fatalf("connected project cycle = %+v", analysis)
	}
}

func TestGoWorkspaceSelectionDoesNotIncludeUnlistedProjects(t *testing.T) {
	t.Parallel()
	repo := architectureRepository(t, true)
	writeArchitectureFile(t, repo.Root, "go.work", "go 1.26.5\nuse .\n")
	writeArchitectureFile(t, repo.Root, "unrelated/go.mod", "module example.test/unrelated\n\ngo 1.26.5\n")
	writeArchitectureFile(t, repo.Root, "unrelated/broken.go", "not valid Go")
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"go.work"}, &pythonGraphRunner{})
	if analysis.Graph == nil || len(analysis.Findings) != 0 || len(analysis.Graph.Nodes) != 2 {
		t.Fatalf("workspace analysis = %+v", analysis)
	}
}

func TestFocusedJavaScriptGraphDoesNotAnalyzeUnrelatedProjects(t *testing.T) {
	t.Parallel()
	for _, control := range []string{"package.json", "tsconfig.json"} {
		t.Run(control, func(t *testing.T) {
			repo := javascriptRepository(t, true)
			writeArchitectureFile(t, repo.Root, "web/"+control, "{}\n")
			writeArchitectureFile(t, repo.Root, "domain/"+control, "invalid JSON")
			writeArchitectureFile(t, repo.Root, "domain/model.vue", "unsupported source")
			repo.PolicyRoot = installImportBundle(t, `{"analyzed":["web/app.ts"],"imports":[],"unsupported":[]}`)
			for _, selected := range []string{"web/app.ts", "web/" + control} {
				analysis := AnalyzeWithRunner(t.Context(), repo, []string{selected}, &pythonGraphRunner{})
				if analysis.Graph == nil || len(analysis.Findings) != 0 || len(analysis.Graph.Nodes) != 1 || analysis.Graph.Nodes[0].Path != "web/app.ts" {
					t.Fatalf("focused analysis of %s = %+v", selected, analysis)
				}
			}
			analysis := AnalyzeWithRunner(t.Context(), repo, []string{"domain/model.vue"}, &pythonGraphRunner{})
			if analysis.Graph != nil || len(analysis.Findings) == 0 {
				t.Fatalf("selected unsupported project passed: %+v", analysis)
			}
		})
	}
}

func TestFocusedJavaScriptGraphRetainsTransitivelyConnectedProjects(t *testing.T) {
	t.Parallel()
	repo := javascriptRepository(t, true)
	repo.Config.Modules[0].DependsOn = []string{"web"}
	writeArchitectureFile(t, repo.Root, "web/package.json", "{}\n")
	writeArchitectureFile(t, repo.Root, "domain/package.json", "{}\n")
	web := `{"analyzed":["web/app.ts"],"imports":[` + importFact("web/app.ts", 1, "../domain/model.js", "domain/model.ts", "") + `],"unsupported":[]}`
	domain := `{"analyzed":["domain/model.ts"],"imports":[` + importFact("domain/model.ts", 1, "../web/app.js", "web/app.ts", "") + `],"unsupported":[]}`
	repo.PolicyRoot = installImportBundle(t, web)
	script := "#!/bin/sh\nrequest=$(/bin/cat)\ncase \"$request\" in\n" +
		`*'"paths":["web/app.ts"]'*) result='` + web + "';;\n" +
		`*'"paths":["domain/model.ts"]'*) result='` + domain + "';;\n" +
		"*) exit 1;;\nesac\n" + `printf '{"protocolVersion":3,"operation":"imports","result":%s}\n' "$result"` + "\n"
	writeBundleFile(t, filepath.Join(repo.PolicyRoot, ".tools", "javascript", "bundle", "runner.mjs"), script)
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"web/app.ts"}, &pythonGraphRunner{})
	finding := onlyCycleFinding(t, analysis.Findings, "architecture.fileCycle")
	if analysis.Graph == nil || len(finding.DependencyComponent.Members) != 2 {
		t.Fatalf("connected project cycle = %+v", analysis)
	}
}

func TestJavaScriptManifestSelectionIncludesItsTypeScriptProjects(t *testing.T) {
	t.Parallel()
	repo := javascriptRepository(t, true)
	writeArchitectureFile(t, repo.Root, "web/package.json", "{}\n")
	writeArchitectureFile(t, repo.Root, "web/server/tsconfig.json", "{}\n")
	writeArchitectureFile(t, repo.Root, "web/server/main.ts", "export {};\n")
	writeArchitectureFile(t, repo.Root, "web/unrelated/package.json", "{}\n")
	writeArchitectureFile(t, repo.Root, "web/unrelated/broken.vue", "unsupported source")
	repo.PolicyRoot = installImportBundle(t, `{"analyzed":["web/app.ts","web/server/main.ts"],"imports":[],"unsupported":[]}`)
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"web/package.json"}, &pythonGraphRunner{})
	if analysis.Graph == nil || len(analysis.Findings) != 0 || len(analysis.Graph.Nodes) != 2 {
		t.Fatalf("package manifest analysis = %+v", analysis)
	}
}

func TestGeneratedJavaScriptGraphSelectionUsesDeclaredSourceProject(t *testing.T) {
	t.Parallel()
	repo := javascriptRepository(t, true)
	writeArchitectureFile(t, repo.Root, "web/package.json", "{}\n")
	writeArchitectureFile(t, repo.Root, "domain/package.json", "{}\n")
	writeArchitectureFile(t, repo.Root, "domain/model.vue", "unsupported source")
	writeArchitectureFile(t, repo.Root, "python_pkg/client.js", "export {};\n")
	repo.Config.Modules[1].Paths = append(repo.Config.Modules[1].Paths, "python_pkg/**")
	repo.Config.Scope.Generated = []string{"python_pkg/**"}
	repo.Config.Scope.GeneratedJavaScript = []policy.GeneratedJavaScript{{Paths: []string{"python_pkg/**"}, SourcePackage: "web/package.json"}}
	repo.PolicyRoot = installImportBundle(t, `{"analyzed":["python_pkg/client.js","web/app.ts"],"imports":[],"unsupported":[]}`)
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"python_pkg/client.js"}, &pythonGraphRunner{})
	if analysis.Graph == nil || len(analysis.Findings) != 0 {
		t.Fatalf("generated graph = %+v", analysis)
	}
	paths := []string{}
	for _, node := range analysis.Graph.Nodes {
		paths = append(paths, node.Path)
	}
	if !slices.Equal(paths, []string{"python_pkg/client.js", "web/app.ts"}) {
		t.Fatalf("generated project scope = %s", strings.Join(paths, ", "))
	}
}
