package policymodule

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestPythonSourceActivatesPinnedRuffCommands(t *testing.T) {
	root := t.TempDir()
	writeModuleFile(t, root, "tools/pyproject.toml", "[tool.ruff]\n")
	writeModuleFile(t, root, "tools/check.py", "print('ok')\n")
	installFakePolicyTool(t, root, "ruff", "ruff 0.16.0")
	pinPolicyTool(t, root, "ruff", "0.16.0")
	repo := repository.Repository{
		Root: root, PolicyRoot: root,
		Config: policy.Config{
			Modules:      []policy.Module{{Name: "tools", Paths: []string{"tools/**"}}},
			ModuleByName: map[string]int{"tools": 0},
		},
	}
	resolution := Resolve(repo, []string{"tools/check.py", "tools/pyproject.toml"})
	if got := activeNames(resolution.Active); !slices.Equal(got, []string{"ruff:tools"}) {
		t.Fatalf("active = %v", got)
	}
	suffix := safeName("tools")
	if got := commandNames(resolution.Commands); !slices.Equal(got, []string{"policy-ruff-format-" + suffix, "policy-ruff-lint-" + suffix, "policy-ruff-write-" + suffix}) {
		t.Fatalf("commands = %v", got)
	}
	for _, command := range resolution.Commands {
		if !command.Managed || !command.PassFiles || !slices.Contains(command.Modules, "tools") {
			t.Fatalf("command is not safely targeted: %+v", command)
		}
	}
}

// A Node stack activates the framework policy its packages declare and
// nothing else. Every generic JavaScript capability now comes from the sealed
// bundle, so the resolution contributes no target command at all: the target
// declares, pins, and installs no analyzer, and a checked-in analyzer
// configuration activates nothing.
func TestNodeStackActivatesElectronReactAndOSVWithoutCommands(t *testing.T) {
	root := t.TempDir()
	packageJSON := `{
  "packageManager": "pnpm@10.23.0",
  "dependencies": {"electron":"41.10.3","react":"19.2.6","react-dom":"19.2.6"}
}
`
	writeModuleFile(t, root, "desktop/package.json", packageJSON)
	writeModuleFile(t, root, "desktop/knip.json", "{}\n")
	writeModuleFile(t, root, "desktop/tsconfig.json", "{}\n")
	writeModuleFile(t, root, "desktop/src/main/index.ts", "export {};\n")
	writeModuleFile(t, root, "desktop/src/preload/index.ts", "export {};\n")
	writeModuleFile(t, root, "desktop/src/renderer/App.tsx", "export const App = () => null;\n")
	installFakePolicyTool(t, root, "osv-scanner", "osv-scanner version: 2.4.0")
	pinPolicyTool(t, root, "osv-scanner", "v2.4.0")
	repo := repository.Repository{
		Root: root, PolicyRoot: root,
		Config: policy.Config{
			Modules:      []policy.Module{{Name: "desktop", Paths: []string{"desktop/**"}}},
			ModuleByName: map[string]int{"desktop": 0},
		},
	}
	files := []string{
		"desktop/knip.json", "desktop/package.json", "desktop/tsconfig.json",
		"desktop/src/main/index.ts", "desktop/src/preload/index.ts", "desktop/src/renderer/App.tsx",
	}
	resolution := Resolve(repo, files)
	wantActive := []string{"electron:desktop", "osv:.", "react:desktop"}
	if got := activeNames(resolution.Active); !slices.Equal(got, wantActive) {
		t.Fatalf("active = %v", got)
	}
	if got := commandNames(resolution.Commands); len(got) != 0 {
		t.Fatalf("commands = %v", got)
	}
	if len(resolution.Findings) != 0 {
		t.Fatalf("findings = %+v", resolution.Findings)
	}
	// Lint is not a target command at all: React activation resolves to rule
	// activation the sealed bundle runs, and the target pins no lint tooling.
	want := []policy.JavaScriptLintScope{{Root: "desktop", ReactHooks: true, JSXAccessibility: true}}
	if !slices.Equal(resolution.LintScopes, want) {
		t.Fatalf("lint scopes = %+v", resolution.LintScopes)
	}
}

// A nested package inherits no tooling from an ancestor, because there is no
// tooling for it to inherit: the sealed bundle owns the formatter, the linter,
// the compiler, and the dead-code analyzer.
func TestNestedPackageInheritsNoPolicyTooling(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeModuleFile(t, root, "package.json", `{
  "packageManager": "pnpm@10.23.0",
  "devDependencies": {
    "knip":"6.13.1",
    "typescript":"6.0.3"
  }
}`+"\n")
	writeModuleFile(t, root, "packages/web/package.json", `{"dependencies":{"react":"19.2.6"}}`+"\n")
	repo := repository.Repository{Root: root, PolicyRoot: root}
	packages, findings := discoverNodePackages(repo, []string{"package.json", "packages/web/package.json"})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	web, exists := packageAt(packages, "packages/web")
	if !exists {
		t.Fatal("nested package was not discovered")
	}
	for _, name := range []string{"knip", "typescript", "eslint", "eslint-plugin-react-hooks", "prettier"} {
		if web.hasDependency(name) {
			t.Errorf("nested package inherited policy-owned tooling %s", name)
		}
	}
}

func TestFrameworkDetectionIncludesOptionalAndPeerDependencies(t *testing.T) {
	t.Parallel()
	peer := nodePackage{Peers: map[string]string{"react": "^19.0.0"}}
	optional := nodePackage{Optional: map[string]string{"electron": "41.10.3"}}
	if !peer.hasDependency("react") || !optional.hasDependency("electron") {
		t.Fatal("framework detection ignored optional or peer dependency evidence")
	}
}

func TestDetectedModuleCanOnlyBeDisabledAtExactGovernedRoot(t *testing.T) {
	root := t.TempDir()
	writeModuleFile(t, root, "web/package.json", `{"packageManager":"npm@11.0.0","dependencies":{"react":"19.2.6"}}`+"\n")
	writeModuleFile(t, root, "web/App.tsx", "export const App = () => null;\n")
	installFakePolicyTool(t, root, "osv-scanner", "osv-scanner version: 2.4.0")
	pinPolicyTool(t, root, "osv-scanner", "v2.4.0")
	repo := repository.Repository{Root: root, PolicyRoot: root, Config: policy.Config{
		PolicyModules: policy.PolicyModules{Overrides: []policy.PolicyModuleOverride{{Name: "react", Root: "web", Mode: "disabled"}}},
	}}
	resolution := Resolve(repo, []string{"web/package.json", "web/App.tsx"})
	if slices.Contains(activeNames(resolution.Active), "react:web") {
		t.Fatalf("disabled activation remained: %+v", resolution.Active)
	}
	if !slices.Contains(activeNames(resolution.Active), "osv:.") {
		t.Fatalf("unrelated activation was removed: %+v", resolution.Active)
	}
}

// A React package that renders no DOM gets the Hooks rules and not the JSX
// accessibility baseline, and neither activation asks the target for a plug-in.
func TestReactWithoutDOMActivatesOnlyTheHooksRules(t *testing.T) {
	root := t.TempDir()
	writeModuleFile(t, root, "package.json", `{"packageManager":"npm@11.0.0","dependencies":{"react":"19.2.6"}}`+"\n")
	writeModuleFile(t, root, "App.tsx", "export const App = () => null;\n")
	installFakePolicyTool(t, root, "osv-scanner", "osv-scanner version: 2.4.0")
	pinPolicyTool(t, root, "osv-scanner", "v2.4.0")
	resolution := Resolve(repository.Repository{Root: root, PolicyRoot: root}, []string{"App.tsx", "package.json"})
	want := []policy.JavaScriptLintScope{{Root: ".", ReactHooks: true, JSXAccessibility: false}}
	if !slices.Equal(resolution.LintScopes, want) {
		t.Fatalf("lint scopes = %+v", resolution.LintScopes)
	}
	for _, item := range resolution.Findings {
		if strings.Contains(item.Subject, "eslint") {
			t.Errorf("React policy still demands target lint tooling: %+v", item)
		}
	}
}

// The policy root's checked-in pin decides which version of a conditional tool
// is required, because a release carries that file beside the tool it names. A
// tool that reports another version is refused, and so is one in a policy root
// that pins nothing: neither is the release's own record of what it carries.
func TestConditionalToolMustBeTheVersionThePolicyRootPins(t *testing.T) {
	t.Parallel()
	for name, pin := range map[string]string{"another version": "0.17.0", "no pin at all": ""} {
		root := t.TempDir()
		writeModuleFile(t, root, "app/pyproject.toml", "[tool.ruff]\n")
		writeModuleFile(t, root, "app/check.py", "print('ok')\n")
		installFakePolicyTool(t, root, "ruff", "ruff 0.16.0")
		if pin != "" {
			pinPolicyTool(t, root, "ruff", pin)
		}
		repo := repository.Repository{
			Root: root, PolicyRoot: root,
			Config: policy.Config{
				Modules:      []policy.Module{{Name: "app", Paths: []string{"app/**"}}},
				ModuleByName: map[string]int{"app": 0},
			},
		}
		resolution := Resolve(repo, []string{"app/check.py", "app/pyproject.toml"})
		if !hasToolFinding(resolution, "ruff") {
			t.Fatalf("%s: Ruff was accepted as the pinned one", name)
		}
	}
}

func TestPolicyToolVersionMatchRequiresAnExactLine(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	installFakePolicyTool(t, root, "ruff", "ruff 0.16.01")
	path := filepath.Join(root, ".tools", "bin", "ruff")
	if executableVersion(path, "ruff 0.16.0") {
		t.Fatal("neighboring Ruff version was accepted")
	}
}

func TestGeneratedCommandRootNamesCannotCollide(t *testing.T) {
	t.Parallel()
	if safeName("foo/bar") == safeName("foo.bar") || safeName("foo/bar") == safeName("foo-bar") {
		t.Fatal("distinct roots generated the same command suffix")
	}
}

func activeNames(active []policy.ActivePolicyModule) []string {
	result := make([]string, 0, len(active))
	for _, item := range active {
		result = append(result, item.Name+":"+item.Root)
	}
	return result
}

func commandNames(commands []policy.Command) []string {
	result := make([]string, 0, len(commands))
	for _, command := range commands {
		result = append(result, command.Name)
	}
	slices.Sort(result)
	return result
}

// pinPolicyTool checks the version a policy root requires of one conditional
// tool in beside it, the way a release carries the pin of every tool it does.
func pinPolicyTool(t *testing.T, root, name, pin string) {
	t.Helper()
	writeModuleFile(t, root, "tools/"+name+"-version.txt", pin+"\n")
}

func hasToolFinding(resolution Resolution, subject string) bool {
	return slices.ContainsFunc(resolution.Findings, func(item policy.Finding) bool {
		return item.Check == "policy.tool" && item.Subject == subject
	})
}

func installFakePolicyTool(t *testing.T, root, name, version string) {
	t.Helper()
	path := filepath.Join(root, ".tools", "bin", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "#!/usr/bin/env sh\nprintf '%s\\n' '" + version + "'\n"
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
}

func writeModuleFile(t *testing.T, root, path, contents string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
