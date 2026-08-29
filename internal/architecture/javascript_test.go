package architecture

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestJavaScriptImportMustFollowDeclaredModuleDependency(t *testing.T) {
	t.Parallel()
	repo := javascriptRepository(t, false)
	repo.PolicyRoot = fakeImportBundle(t, importFact("web/app.ts", 3, "../domain/model.js", "domain/model.ts", ""))
	findings := Check(t.Context(), repo, []string{"web/app.ts"})
	if len(findings) != 1 || findings[0].Check != "architecture.moduleDependency" ||
		findings[0].Path != "web/app.ts" || findings[0].Subject != "domain" {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Message != `line 3 module "web" imports module "domain" without declaring dependsOn` {
		t.Fatalf("message = %q", findings[0].Message)
	}
}

func TestDeclaredJavaScriptModuleDependencyPasses(t *testing.T) {
	t.Parallel()
	repo := javascriptRepository(t, true)
	repo.PolicyRoot = fakeImportBundle(t, importFact("web/app.ts", 3, "../domain/model.js", "domain/model.ts", ""))
	if findings := Check(t.Context(), repo, []string{"web/app.ts"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestJavaScriptImportOutsideTheGovernedTreeIsNotAModuleEdge(t *testing.T) {
	t.Parallel()
	repo := javascriptRepository(t, false)
	repo.PolicyRoot = fakeImportBundle(t,
		importFact("web/app.ts", 1, "left-pad", "node_modules/left-pad/index.js", "left-pad"),
		importFact("web/app.ts", 2, "@scope/absent", "", "@scope/absent"))
	if findings := Check(t.Context(), repo, []string{"web/app.ts"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestJavaScriptImportOfAnUndeclaredPackageIsReported(t *testing.T) {
	t.Parallel()
	repo := nodePackageRepository(t, `{"name":"app","dependencies":{"react":"19.2.0"}}`)
	repo.PolicyRoot = fakeImportBundle(t,
		importFact("web/app.ts", 4, "left-pad/index.js", "node_modules/left-pad/index.js", "left-pad"))
	findings := Check(t.Context(), repo, []string{"web/app.ts"})
	if len(findings) != 1 || findings[0].Check != "architecture.packageDependency" ||
		findings[0].Path != "web/app.ts" || findings[0].Subject != "left-pad" {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Message != `line 4 package "." imports "left-pad" without declaring it as a dependency` {
		t.Fatalf("message = %q", findings[0].Message)
	}
}

func TestDeclaredJavaScriptPackageDependencyPasses(t *testing.T) {
	t.Parallel()
	repo := nodePackageRepository(t, `{"name":"app","dependencies":{"react":"19.2.0"},`+
		`"peerDependencies":{"zod":"4.1.12"},"optionalDependencies":{"fsevents":"2.3.3"}}`)
	repo.PolicyRoot = fakeImportBundle(t,
		importFact("web/app.ts", 1, "react", "node_modules/react/index.js", "react"),
		importFact("web/app.ts", 2, "zod", "node_modules/zod/index.js", "zod"),
		importFact("web/app.ts", 3, "fsevents", "node_modules/fsevents/index.js", "fsevents"),
		importFact("web/app.ts", 4, "app/helper", "web/helper.ts", "app"))
	if findings := Check(t.Context(), repo, []string{"web/app.ts"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestUnresolvedJavaScriptSpecifierIsNotAnUndeclaredDependency(t *testing.T) {
	t.Parallel()
	repo := nodePackageRepository(t, `{"name":"app","dependencies":{}}`)
	repo.PolicyRoot = fakeImportBundle(t, importFact("web/app.ts", 1, "shared/util", "", "shared"))
	if findings := Check(t.Context(), repo, []string{"web/app.ts"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestJavaScriptDevelopmentDependencyImportedByShippedSourceIsReported(t *testing.T) {
	t.Parallel()
	repo := nodePackageRepository(t, `{"name":"app","devDependencies":{"vitest":"3.2.4"}}`)
	repo.PolicyRoot = fakeImportBundle(t,
		importFact("web/app.ts", 2, "vitest", "node_modules/vitest/index.js", "vitest"))
	findings := Check(t.Context(), repo, []string{"web/app.ts"})
	if len(findings) != 1 || findings[0].Check != "architecture.packageDependency" ||
		findings[0].Subject != "vitest" {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Message != `line 2 package "." imports development dependency "vitest" from source that ships` {
		t.Fatalf("message = %q", findings[0].Message)
	}
}

func TestJavaScriptDevelopmentDependencyImportedByDevelopmentSourcePasses(t *testing.T) {
	t.Parallel()
	repo := nodePackageRepository(t, `{"name":"app","devDependencies":{"vitest":"3.2.4"}}`)
	repo.Config.Scope.Development = []string{"web/*.config.ts"}
	repo.PolicyRoot = fakeImportBundle(t,
		importFact("web/app.test.ts", 1, "vitest", "node_modules/vitest/index.js", "vitest"),
		importFact("web/vitest.config.ts", 1, "vitest", "node_modules/vitest/index.js", "vitest"))
	selected := []string{"web/app.test.ts", "web/vitest.config.ts"}
	if findings := Check(t.Context(), repo, selected); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestNearestJavaScriptManifestOwnsItsDependencies(t *testing.T) {
	t.Parallel()
	repo := nodePackageRepository(t, `{"name":"root","dependencies":{"left-pad":"1.3.0"}}`)
	writeArchitectureFile(t, repo.Root, "web/package.json", `{"name":"@app/web"}`)
	repo.PolicyRoot = fakeImportBundle(t,
		importFact("web/app.ts", 1, "left-pad", "node_modules/left-pad/index.js", "left-pad"))
	findings := Check(t.Context(), repo, []string{"web/app.ts"})
	if len(findings) != 1 || findings[0].Subject != "left-pad" ||
		findings[0].Message != `line 1 package "web" imports "left-pad" without declaring it as a dependency` {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestUnreadableJavaScriptManifestIsMissingCoverage(t *testing.T) {
	t.Parallel()
	repo := nodePackageRepository(t, "{not json")
	repo.PolicyRoot = fakeImportBundle(t,
		importFact("web/app.ts", 1, "left-pad", "node_modules/left-pad/index.js", "left-pad"),
		importFact("web/app.ts", 2, "react", "node_modules/react/index.js", "react"))
	findings := Check(t.Context(), repo, []string{"web/app.ts"})
	if len(findings) != 1 || findings[0].Check != "architecture.importCoverage" ||
		findings[0].Path != "package.json" || findings[0].Subject != "package.json" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestJavaScriptSourceTheReaderCannotParseIsMissingCoverage(t *testing.T) {
	t.Parallel()
	repo := javascriptRepository(t, false)
	repo.PolicyRoot = t.TempDir()
	writeArchitectureFile(t, repo.Root, "web/widget.vue", "<template />\n")
	findings := Check(t.Context(), repo, []string{"web/widget.vue"})
	if len(findings) != 1 || findings[0].Check != "architecture.importCoverage" ||
		findings[0].Path != "web/widget.vue" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestUnreadJavaScriptImportIsMissingCoverage(t *testing.T) {
	t.Parallel()
	repo := javascriptRepository(t, false)
	result := `{"imports":[],"unsupported":[{"path":"web/app.ts","reason":"line 4: a dynamic import whose specifier is computed names no file to resolve"}]}`
	repo.PolicyRoot = installImportBundle(t, result)
	findings := Check(t.Context(), repo, []string{"web/app.ts"})
	if len(findings) != 1 || findings[0].Check != "architecture.importCoverage" ||
		findings[0].Path != "web/app.ts" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGoOnlySelectionNeverLaunchesTheBundle(t *testing.T) {
	t.Parallel()
	repo := architectureRepository(t, true)
	repo.PolicyRoot = t.TempDir()
	if findings := Check(t.Context(), repo, []string{"internal/api/api.go"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestJavaScriptArchitectureFailsClosedWithoutTheBundle(t *testing.T) {
	t.Parallel()
	repo := javascriptRepository(t, false)
	repo.PolicyRoot = t.TempDir()
	findings := Check(t.Context(), repo, []string{"web/app.ts"})
	if len(findings) != 1 || findings[0].Check != "policy.tool" || findings[0].Subject != "javascript-bundle" {
		t.Fatalf("findings = %+v", findings)
	}
}

func javascriptRepository(t *testing.T, allow bool) repository.Repository {
	t.Helper()
	root := t.TempDir()
	writeArchitectureFile(t, root, "domain/model.ts", "export type Model = string;\n")
	writeArchitectureFile(t, root, "web/app.ts", "export {};\n")
	web := policy.Module{Name: "web", Paths: []string{"web/**"}}
	if allow {
		web.DependsOn = []string{"domain"}
	}
	config := policy.Config{
		Modules:      []policy.Module{{Name: "domain", Paths: []string{"domain/**"}}, web},
		ModuleByName: map[string]int{"domain": 0, "web": 1},
	}
	return repository.Repository{Root: root, Config: config}
}

func nodePackageRepository(t *testing.T, manifest string) repository.Repository {
	t.Helper()
	repo := javascriptRepository(t, false)
	writeArchitectureFile(t, repo.Root, "package.json", manifest)
	return repo
}

func importFact(path string, line int, specifier, resolved, imported string) string {
	return `{"path":"` + path + `","line":` + strconv.Itoa(line) + `,"specifier":"` + specifier +
		`","resolved":"` + resolved + `","package":"` + imported + `"}`
}

func fakeImportBundle(t *testing.T, facts ...string) string {
	t.Helper()
	return installImportBundle(t, `{"imports":[`+strings.Join(facts, ",")+`],"unsupported":[]}`)
}

func installImportBundle(t *testing.T, result string) string {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("the sealed JavaScript bundle does not support %s", runtime.GOOS)
	}
	architecture := runtime.GOARCH
	if architecture == "amd64" {
		architecture = "x64"
	}
	root := t.TempDir()
	installed := filepath.Join(root, ".tools", "javascript")
	writeBundleFile(t, filepath.Join(installed, runtime.GOOS+"-"+architecture, "node", "bin", "node"),
		"#!/bin/sh\nexec /bin/sh \"$1\"\n")
	writeBundleFile(t, filepath.Join(installed, "bundle", "runner.mjs"),
		"#!/bin/sh\n/bin/cat >/dev/null\n"+
			`printf '{"protocolVersion":3,"operation":"imports","result":%s}\n' '`+result+"'\n")
	return root
}

func writeBundleFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
