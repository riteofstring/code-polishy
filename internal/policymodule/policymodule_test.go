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

func TestPythonSourceActivatesPinnedRuffAndTyCommands(t *testing.T) {
	root := t.TempDir()
	writeModuleFile(t, root, "tools/pyproject.toml", "[tool.ruff]\n")
	writeModuleFile(t, root, "tools/check.py", "print('ok')\n")
	installFakePolicyTool(t, root, "ruff", "ruff 0.16.0")
	pinPolicyTool(t, root, "ruff", "0.16.0")
	installFakePolicyTool(t, root, "ty", "ty 0.0.65 (87de836df 2026-07-29)")
	pinPolicyTool(t, root, "ty", "0.0.65")
	repo := repository.Repository{
		Root: root, PolicyRoot: root,
		Config: policy.Config{
			Modules:      []policy.Module{{Name: "tools", Paths: []string{"tools/**"}}},
			ModuleByName: map[string]int{"tools": 0},
		},
	}
	resolution := Resolve(repo, []string{"tools/check.py", "tools/pyproject.toml"})
	if got := activeNames(resolution.Active); len(got) != 2 || !slices.Contains(got, "ruff:tools") || !slices.Contains(got, "ty:tools") {
		t.Fatalf("active = %v", got)
	}
	if hasToolFinding(resolution, "ty") {
		t.Fatalf("ty finding = %+v", resolution.Findings)
	}
	for _, command := range resolution.Commands {
		if !command.Managed || !command.PassFiles || !slices.Contains(command.Modules, "tools") {
			t.Fatalf("command is not safely targeted: %+v", command)
		}
	}
	complexity, found := commandProviding(resolution.Commands, "complexity")
	if !found {
		t.Fatalf("missing Ruff complexity command: %+v", resolution.Commands)
	}
	wantArguments := []string{
		repo.PolicyTool("ruff"), "check", "--no-fix", "--isolated", "--select", "C901", "--ignore-noqa",
		"--config", "lint.mccabe.max-complexity = 9", "--",
	}
	if !slices.Equal(complexity.Provides, []string{"complexity"}) || !slices.Equal(complexity.Argv, wantArguments) {
		t.Fatalf("complexity command = %+v", complexity)
	}
	typecheck, found := commandProviding(resolution.Commands, "typecheck")
	if !found {
		t.Fatalf("missing ty command: %+v", resolution.Commands)
	}
	wantTypecheck := []string{repo.PolicyTool("ty"), "check", "--config-file", filepath.Join(root, "tools", "ty.toml"), "--project", ".", "--"}
	if !slices.Equal(typecheck.Provides, []string{"typecheck"}) || !slices.Equal(typecheck.Argv, wantTypecheck) ||
		!slices.Equal(typecheck.RunOn, []string{"check", "gate"}) {
		t.Fatalf("typecheck command = %+v", typecheck)
	}
}

func TestTyUsesNearestProjectBoundaryWithoutChangingRuffRoot(t *testing.T) {
	root := t.TempDir()
	writeModuleFile(t, root, "pyproject.toml", "[tool.ruff]\n")
	writeModuleFile(t, root, "apps/pyproject.toml", "[tool.ty]\n")
	writeModuleFile(t, root, "apps/service/ty.toml", "[rules]\n")
	writeModuleFile(t, root, "apps/service/check.py", "print('ok')\n")
	installFakePolicyTool(t, root, "ruff", "ruff 0.16.0")
	pinPolicyTool(t, root, "ruff", "0.16.0")
	installFakePolicyTool(t, root, "ty", "ty 0.0.65 (87de836df 2026-07-29)")
	pinPolicyTool(t, root, "ty", "0.0.65")
	repo := repository.Repository{Root: root, PolicyRoot: root}
	files := []string{"pyproject.toml", "apps/pyproject.toml", "apps/service/ty.toml", "apps/service/check.py"}
	resolution := Resolve(repo, files)
	if got := activeNames(resolution.Active); len(got) != 2 || !slices.Contains(got, "ruff:.") || !slices.Contains(got, "ty:apps/service") {
		t.Fatalf("active = %v", got)
	}
	typecheck, found := commandProviding(resolution.Commands, "typecheck")
	if !found {
		t.Fatalf("commands = %+v", resolution.Commands)
	}
	want := []string{repo.PolicyTool("ty"), "check", "--config-file", filepath.Join(root, "tools", "ty.toml"), "--project", ".", "--"}
	if typecheck.Cwd != "apps/service" || !typecheck.PassFiles || !slices.Equal(typecheck.Argv, want) {
		t.Fatalf("typecheck command = %+v", typecheck)
	}
}

func TestTyUsesPolicyOwnedConfig(t *testing.T) {
	targetRoot := t.TempDir()
	policyRoot := t.TempDir()
	writeModuleFile(t, targetRoot, "app/ty.toml", "[rules]\n")
	writeModuleFile(t, targetRoot, "app/check.py", "print('ok')\n")
	writeModuleFile(t, policyRoot, "tools/ty.toml", "[rules]\n")
	installFakePolicyTool(t, policyRoot, "ty", "ty 0.0.65 (87de836df 2026-07-29)")
	pinPolicyTool(t, policyRoot, "ty", "0.0.65")
	repo := repository.Repository{
		Root: targetRoot, PolicyRoot: policyRoot,
		Config: policy.Config{
			Modules:      []policy.Module{{Name: "app", Paths: []string{"app/**"}}},
			ModuleByName: map[string]int{"app": 0},
		},
	}
	resolution := Resolve(repo, []string{"app/ty.toml", "app/check.py"})
	typecheck, found := commandProviding(resolution.Commands, "typecheck")
	if !found {
		t.Fatalf("commands = %+v", resolution.Commands)
	}
	want := []string{repo.PolicyTool("ty"), "check", "--config-file", filepath.Join(policyRoot, "tools", "ty.toml"), "--project", ".", "--"}
	if !slices.Equal(typecheck.Argv, want) || slices.Contains(typecheck.Argv, filepath.Join(targetRoot, "app", "ty.toml")) {
		t.Fatalf("typecheck argv = %v", typecheck.Argv)
	}
}

func TestTyRequiresAnExactPolicyPin(t *testing.T) {
	cases := []struct {
		name, version, pin string
	}{
		{name: "missing pin", version: "ty 0.0.65 (87de836df 2026-07-29)"},
		{name: "wrong pin", version: "ty 0.0.65 (87de836df 2026-07-29)", pin: "0.0.64"},
		{name: "neighboring version", version: "ty 0.0.650 (87de836df 2026-07-29)", pin: "0.0.65"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeModuleFile(t, root, "app/ty.toml", "[rules]\n")
			writeModuleFile(t, root, "app/check.py", "print('ok')\n")
			installFakePolicyTool(t, root, "ty", test.version)
			if test.pin != "" {
				pinPolicyTool(t, root, "ty", test.pin)
			}
			resolution := Resolve(repository.Repository{Root: root, PolicyRoot: root}, []string{"app/ty.toml", "app/check.py"})
			if !hasToolFinding(resolution, "ty") {
				t.Fatalf("ty was accepted: %+v", resolution.Findings)
			}
		})
	}
}

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
	active := activeNames(resolution.Active)
	if len(active) != 3 || !slices.Contains(active, "electron:desktop") || !slices.Contains(active, "osv:.") || !slices.Contains(active, "react:desktop") {
		t.Fatalf("active = %v", active)
	}
	if len(resolution.Commands) != 0 {
		t.Fatalf("commands = %v", resolution.Commands)
	}
	if len(resolution.Findings) != 0 {
		t.Fatalf("findings = %+v", resolution.Findings)
	}

	want := []policy.JavaScriptLintScope{{Root: "desktop", ReactHooks: true, JSXAccessibility: true}}
	if !slices.Equal(resolution.LintScopes, want) {
		t.Fatalf("lint scopes = %+v", resolution.LintScopes)
	}
}

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

func commandProviding(commands []policy.Command, capability string) (policy.Command, bool) {
	index := slices.IndexFunc(commands, func(command policy.Command) bool {
		return slices.Contains(command.Provides, capability)
	})
	if index < 0 {
		return policy.Command{}, false
	}
	return commands[index], true
}

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
