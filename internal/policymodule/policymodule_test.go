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

func TestPythonSourceActivatesPinnedRuffTyAndVulturePolicyModules(t *testing.T) {
	root := t.TempDir()
	writeModuleFile(t, root, "tools/pyproject.toml", "[project]\nname = \"tools\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeModuleFile(t, root, "tools/check.py", "print('ok')\n")
	installFakePolicyTool(t, root, "ruff", "ruff 0.16.0")
	pinPolicyTool(t, root, "ruff", "0.16.0")
	installFakePolicyTool(t, root, "ty", "ty 0.0.65 (87de836df 2026-07-29)")
	pinPolicyTool(t, root, "ty", "0.0.65")
	installPinnedPythonVulture(t, root)
	repo := repository.Repository{
		Root: root, PolicyRoot: root,
		Config: policy.Config{
			Modules:      []policy.Module{{Name: "tools", Paths: []string{"tools/**"}}},
			ModuleByName: map[string]int{"tools": 0},
		},
	}
	resolution := Resolve(repo, []string{"tools/check.py", "tools/pyproject.toml"})
	if got := activeNames(resolution.Active); !slices.Contains(got, "ruff:tools") || !slices.Contains(got, "ty:tools") || !slices.Contains(got, "vulture:tools") {
		t.Fatalf("active = %v", got)
	}
	if hasToolFinding(resolution, "python") || hasToolFinding(resolution, "ty") || hasToolFinding(resolution, "vulture") {
		t.Fatalf("tool findings = %+v", resolution.Findings)
	}
	for _, command := range resolution.Commands {
		if !command.Managed || !command.PassFiles || !slices.Contains(command.Modules, "tools") {
			t.Fatalf("command is not safely targeted: %+v", command)
		}
	}
	if len(resolution.Commands) != 2 {
		t.Fatalf("commands = %+v", resolution.Commands)
	}
	for _, command := range resolution.Commands {
		if !slices.Equal(command.Provides, []string{"format"}) || command.Cwd != "tools" ||
			!slices.Equal(command.PassFilePaths, []string{"tools/check.py"}) {
			t.Fatalf("format command = %+v", command)
		}
		arguments := strings.Join(command.Argv, "\x00")
		for _, expected := range []string{
			"--target-version\x00py312",
			"--config\x00line-length = 88",
			"--config\x00lint.pycodestyle.max-line-length = 88",
			`--config` + "\x00" + `src = ["."]`,
		} {
			if !strings.Contains(arguments, expected) {
				t.Fatalf("format command lacks %q: %+v", expected, command)
			}
		}
	}
}

func TestRuffConfigurationConflictBlocksFormatting(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeModuleFile(t, root, "pyproject.toml", "[project]\nrequires-python = \"==3.12.*\"\n")
	writeModuleFile(t, root, "ruff.toml", "line-length = 100\n")
	writeModuleFile(t, root, "app.py", "value = 1\n")
	installFakePolicyTool(t, root, "ruff", "ruff 0.16.0")
	pinPolicyTool(t, root, "ruff", "0.16.0")
	installFakePolicyTool(t, root, "ty", "ty 0.0.65 (87de836df 2026-07-29)")
	pinPolicyTool(t, root, "ty", "0.0.65")
	installPinnedPythonVulture(t, root)
	resolution := Resolve(repository.Repository{Root: root, PolicyRoot: root}, []string{"pyproject.toml", "ruff.toml", "app.py"})
	if len(resolution.Commands) != 0 || !slices.ContainsFunc(resolution.Findings, func(finding policy.Finding) bool {
		return finding.Check == "policy.pythonRuffConfiguration" && finding.Path == "ruff.toml" &&
			finding.Line == 1 && strings.Contains(finding.Message, "policy-owned line length 88")
	}) {
		t.Fatalf("resolution = %+v", resolution)
	}
}

func TestPythonPolicyModulesUseProjectBoundaries(t *testing.T) {
	root := t.TempDir()
	writeModuleFile(t, root, "pyproject.toml", "[project]\nname = \"root\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeModuleFile(t, root, "root.py", "print('ok')\n")
	writeModuleFile(t, root, "apps/pyproject.toml", "[project]\nname = \"apps\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeModuleFile(t, root, "apps/service/check.py", "print('ok')\n")
	installFakePolicyTool(t, root, "ruff", "ruff 0.16.0")
	pinPolicyTool(t, root, "ruff", "0.16.0")
	installFakePolicyTool(t, root, "ty", "ty 0.0.65 (87de836df 2026-07-29)")
	pinPolicyTool(t, root, "ty", "0.0.65")
	installPinnedPythonVulture(t, root)
	repo := repository.Repository{Root: root, PolicyRoot: root}
	files := []string{"pyproject.toml", "root.py", "apps/pyproject.toml", "apps/service/check.py"}
	resolution := Resolve(repo, files)
	if got := activeNames(resolution.Active); !slices.Contains(got, "ruff:.") || !slices.Contains(got, "ty:.") ||
		!slices.Contains(got, "ruff:apps") || !slices.Contains(got, "ty:apps") ||
		!slices.Contains(got, "vulture:.") || !slices.Contains(got, "vulture:apps") {
		t.Fatalf("active = %v", got)
	}
	if len(resolution.Commands) != 4 {
		t.Fatalf("commands = %+v", resolution.Commands)
	}
	for _, command := range resolution.Commands {
		if command.Cwd != "." && command.Cwd != "apps" {
			t.Fatalf("format command has wrong project root: %+v", command)
		}
	}
}

func TestTyPolicyModuleDoesNotRegisterGenericTypecheckCommand(t *testing.T) {
	targetRoot := t.TempDir()
	policyRoot := t.TempDir()
	writeModuleFile(t, targetRoot, "app/pyproject.toml", "[project]\nname = \"app\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeModuleFile(t, targetRoot, "app/check.py", "print('ok')\n")
	installFakePolicyTool(t, policyRoot, "ty", "ty 0.0.65 (87de836df 2026-07-29)")
	pinPolicyTool(t, policyRoot, "ty", "0.0.65")
	installPinnedPythonVulture(t, policyRoot)
	repo := repository.Repository{
		Root: targetRoot, PolicyRoot: policyRoot,
		Config: policy.Config{
			Modules:      []policy.Module{{Name: "app", Paths: []string{"app/**"}}},
			ModuleByName: map[string]int{"app": 0},
		},
	}
	resolution := Resolve(repo, []string{"app/pyproject.toml", "app/check.py"})
	if got := activeNames(resolution.Active); !slices.Contains(got, "ty:app") || !slices.Contains(got, "vulture:app") {
		t.Fatalf("active = %v", got)
	}
	if _, found := commandProviding(resolution.Commands, "typecheck"); found {
		t.Fatalf("generic ty command = %+v", resolution.Commands)
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
			writeModuleFile(t, root, "app/pyproject.toml", "[project]\nname = \"app\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
			writeModuleFile(t, root, "app/check.py", "print('ok')\n")
			installFakePolicyTool(t, root, "ty", test.version)
			if test.pin != "" {
				pinPolicyTool(t, root, "ty", test.pin)
			}
			installPinnedPythonVulture(t, root)
			resolution := Resolve(repository.Repository{Root: root, PolicyRoot: root}, []string{"app/pyproject.toml", "app/check.py"})
			if !hasToolFinding(resolution, "ty") {
				t.Fatalf("ty was accepted: %+v", resolution.Findings)
			}
		})
	}
}

func TestVultureRequiresExactPolicyRuntimeAndPackage(t *testing.T) {
	cases := []struct {
		name, pythonVersion, pythonPin, vultureVersion, vulturePin, subject string
	}{
		{name: "missing Python pin", pythonVersion: "3.12.13", vultureVersion: "2.16", vulturePin: "2.16", subject: "python"},
		{name: "neighboring Python version", pythonVersion: "3.12.14", pythonPin: "3.12.13+20260728", vultureVersion: "2.16", vulturePin: "2.16", subject: "python"},
		{name: "missing Vulture pin", pythonVersion: "3.12.13", pythonPin: "3.12.13+20260728", vultureVersion: "2.16", subject: "vulture"},
		{name: "neighboring Vulture version", pythonVersion: "3.12.13", pythonPin: "3.12.13+20260728", vultureVersion: "2.160", vulturePin: "2.16", subject: "vulture"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeModuleFile(t, root, "app/pyproject.toml", "[project]\nname = \"app\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
			writeModuleFile(t, root, "app/check.py", "print('ok')\n")
			installFakePythonRuntime(t, root, test.pythonVersion, test.vultureVersion)
			if test.pythonPin != "" {
				pinPolicyTool(t, root, "python", test.pythonPin)
			}
			if test.vulturePin != "" {
				pinPolicyTool(t, root, "vulture", test.vulturePin)
			}
			resolution := Resolve(repository.Repository{Root: root, PolicyRoot: root}, []string{"app/check.py", "app/pyproject.toml"})
			if !hasToolFinding(resolution, test.subject) {
				t.Fatalf("%s was accepted: %+v", test.subject, resolution.Findings)
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
		writeModuleFile(t, root, "app/pyproject.toml", "[project]\nrequires-python = \"==3.12.*\"\n\n[tool.ruff]\n")
		writeModuleFile(t, root, "app/check.py", "print('ok')\n")
		installFakePolicyTool(t, root, "ruff", "ruff 0.16.0")
		installPinnedPythonVulture(t, root)
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

func installPinnedPythonVulture(t *testing.T, root string) {
	t.Helper()
	installFakePythonRuntime(t, root, "3.12.13", "2.16")
	pinPolicyTool(t, root, "python", "3.12.13+20260728")
	pinPolicyTool(t, root, "vulture", "2.16")
}

func installFakePythonRuntime(t *testing.T, root, pythonVersion, vultureVersion string) {
	t.Helper()
	path := (repository.Repository{PolicyRoot: root}).PythonTool()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "#!/usr/bin/env sh\nif [ \"$1\" != \"-I\" ] || [ \"$2\" != \"-c\" ]; then\n  exit 1\nfi\ncase \"$3\" in\n  *sys.version_info*) printf '%s\\n' '" + pythonVersion + "' ;;\n  *importlib.metadata*) printf '%s\\n' '" + vultureVersion + "' ;;\n  *) exit 1 ;;\nesac\n"
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
