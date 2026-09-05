package pack

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestManifestRequiresExactSafeCompleteContract(t *testing.T) {
	valid := testManifest(t)
	if _, err := ParseManifest(valid, "manifest"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{"unknown field", func(value map[string]any) { value["unknown"] = true }, "unknown field"},
		{"unsafe executable", func(value map[string]any) {
			value["commands"].([]any)[0].(map[string]any)["argv"] = []any{"../adapter"}
		}, "contained relative path"},
		{"unsupported protocol", func(value map[string]any) { value["protocolVersion"] = float64(2) }, "protocolVersion"},
		{"missing failing fixture", func(value map[string]any) { value["fixtures"] = value["fixtures"].([]any)[:1] }, "deliberately failing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(valid, &value); err != nil {
				t.Fatal(err)
			}
			test.edit(value)
			data, _ := json.Marshal(value)
			if _, err := ParseManifest(data, "manifest"); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, received %v", test.want, err)
			}
		})
	}
}

func TestInstallPublishesExactContentAddressedTreeAndDetectsTampering(t *testing.T) {
	source := writePackSource(t)
	dataRoot := filepath.Join(t.TempDir(), "packs")
	t.Cleanup(func() { makeWritable(dataRoot) })
	identity, installed, err := Install(source, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if installed != InstalledRoot(dataRoot, identity.Name, identity.Version, identity.Digest) {
		t.Fatalf("unexpected installed root %s", installed)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(installed)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o555 {
			t.Fatalf("installed pack root remained writable: %s", info.Mode().Perm())
		}
	}
	second, secondRoot, err := Install(source, dataRoot)
	if err != nil || second != identity || secondRoot != installed {
		t.Fatalf("idempotent install failed: %+v %s %v", second, secondRoot, err)
	}
	if _, err := VerifyInstalled(installed); err != nil {
		t.Fatal(err)
	}
	adapter := filepath.Join(installed, "bin", "adapter")
	if err := os.Chmod(adapter, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(adapter, []byte("changed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyInstalled(installed); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("tampered adapter passed: %v", err)
	}
}

func TestTreeDigestIsDeterministicAndDifferentBytesNeverReplaceAnIdentity(t *testing.T) {
	firstSource := writePackSource(t)
	secondSource := writePackSource(t)
	dataRoot := filepath.Join(t.TempDir(), "packs")
	t.Cleanup(func() { makeWritable(dataRoot) })
	first, _, err := Install(firstSource, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := Install(secondSource, dataRoot)
	if err != nil || first != second {
		t.Fatalf("identical trees did not share an identity: %+v %+v %v", first, second, err)
	}
	writeTestFile(t, secondSource, "README.md", "# Different bytes\n", 0o644)
	changed, changedRoot, err := Install(secondSource, dataRoot)
	if err != nil || changed.Name != first.Name || changed.Version != first.Version || changed.Digest == first.Digest || changedRoot == InstalledRoot(dataRoot, first.Name, first.Version, first.Digest) {
		t.Fatalf("changed bytes replaced an identity: %+v %s %v", changed, changedRoot, err)
	}
}

func TestSourceValidationRejectsLinksBeforeInstallation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows link creation is privilege-dependent")
	}
	source := writePackSource(t)
	if err := os.Symlink("README.md", filepath.Join(source, "linked")); err != nil {
		t.Fatal(err)
	}
	dataRoot := filepath.Join(t.TempDir(), "packs")
	t.Cleanup(func() { makeWritable(dataRoot) })
	if _, _, err := Install(source, dataRoot); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("unsafe source passed: %v", err)
	}
	if _, err := os.Stat(dataRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("installation wrote before validation: %v", err)
	}
}

func TestUserDataResolutionIsPlatformSpecificAndExact(t *testing.T) {
	unix, err := userDataRoot("linux", func(name string) string {
		if name == "XDG_DATA_HOME" {
			return "/data"
		}
		return ""
	}, func() (string, error) { return "/home/user", nil })
	if err != nil || unix != filepath.Join("/data", "code-polishy", "packs") {
		t.Fatalf("unexpected Unix root %s: %v", unix, err)
	}
	windows, err := userDataRoot("windows", func(name string) string { return `C:\Users\example\AppData\Local` }, func() (string, error) { return "", nil })
	if err != nil || windows != filepath.Join(`C:\Users\example\AppData\Local`, "CodePolishy", "packs") {
		t.Fatalf("unexpected Windows root %s: %v", windows, err)
	}
	if _, err := userDataRoot("linux", func(string) string { return "relative" }, func() (string, error) { return "", nil }); err == nil {
		t.Fatal("relative XDG_DATA_HOME passed")
	}
}

func TestProtocolRejectsFakeSuccessExtraJSONAndEscapingFindings(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"fake success", `{"protocolVersion":1,"status":"pass"}`},
		{"extra response", `{"protocolVersion":1,"status":"pass","evidence":["lint ran"]}{}`},
		{"escaping finding", `{"protocolVersion":1,"status":"findings","findings":[{"capability":"lint","path":"../secret","subject":"bad","message":"bad"}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseResponse([]byte(test.data), Request{Capability: "lint", Files: []string{"src/main.fixture"}}); err == nil {
				t.Fatal("invalid response passed")
			}
		})
	}
}

func TestFormattingRequestSeparatesCheckAndWriteModes(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/main.fixture", "good\n", 0o644)
	repo, err := repository.Open(root, root, policy.Config{Modules: []policy.Module{{Name: "app", Paths: []string{"src/**"}}}, ModuleByName: map[string]int{"app": 0}})
	if err != nil {
		t.Fatal(err)
	}
	command := policy.Command{Paths: []string{"**/*.fixture"}, Adapter: &policy.PackAdapter{Capability: "format"}}
	selection := repository.Selection{Files: []string{"src/main.fixture"}}
	check := requestFor(repo, selection, command, "check")
	write := requestFor(repo, selection, command, "format")
	if check.Operation != "format" || check.Mode != "check" || write.Operation != "format" || write.Mode != "write" {
		t.Fatalf("format modes were not explicit: check=%+v write=%+v", check, write)
	}
}

func TestPackRequestsKeepGeneratedExecutableSourceAndProtectDeclaredData(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"src/main.ts", "src/client.generated.ts", "data/identity.json"} {
		writeTestFile(t, root, path, "value\n", 0o644)
	}
	repo, err := repository.Open(root, root, policy.Config{
		Scope: policy.Scope{
			Generated: []string{"src/client.generated.ts"},
			Data:      []string{"data/**/*.json"},
		},
		Modules:      []policy.Module{{Name: "app", Paths: []string{"src/**", "data/**"}}},
		ModuleByName: map[string]int{"app": 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	selection := repository.Selection{Files: []string{"src/main.ts", "src/client.generated.ts", "data/identity.json"}}
	adapter := func(capability string) *policy.PackAdapter { return &policy.PackAdapter{Capability: capability} }
	cases := []struct {
		name       string
		capability string
		profile    string
		paths      []string
		want       []string
	}{
		{name: "format check", capability: "format", profile: "check", paths: []string{"src/**", "data/**"}, want: []string{"src/main.ts"}},
		{name: "format gate", capability: "format", profile: "gate", paths: []string{"src/**", "data/**"}, want: []string{"src/main.ts"}},
		{name: "format write", capability: "format", profile: "format", paths: []string{"src/**", "data/**"}, want: []string{"src/main.ts"}},
		{name: "lint", capability: "lint", profile: "check", paths: []string{"src/**"}, want: []string{"src/main.ts", "src/client.generated.ts"}},
		{name: "typecheck", capability: "typecheck", profile: "check", paths: []string{"src/**"}, want: []string{"src/main.ts", "src/client.generated.ts"}},
		{name: "dead code", capability: "dead-code", profile: "check", paths: []string{"src/**"}, want: []string{"src/main.ts", "src/client.generated.ts"}},
		{name: "architecture", capability: "architecture", profile: "check", paths: []string{"src/**"}, want: []string{"src/main.ts", "src/client.generated.ts"}},
		{name: "complexity", capability: "complexity", profile: "check", paths: []string{"src/**"}, want: []string{"src/main.ts"}},
		{name: "schema provider", capability: "schema", profile: "check", paths: []string{"data/**"}, want: []string{"data/identity.json"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := requestFor(repo, selection, policy.Command{Paths: test.paths, Adapter: adapter(test.capability)}, test.profile)
			if !slices.Equal(request.Files, test.want) {
				t.Fatalf("files = %v, want %v", request.Files, test.want)
			}
		})
	}
}

func TestResolveCompilesExactPackProvidersIntoManagedProfiles(t *testing.T) {
	source := writePackSource(t)
	dataRoot := filepath.Join(t.TempDir(), "packs")
	t.Cleanup(func() { makeWritable(dataRoot) })
	identity, _, err := Install(source, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	selected := policy.PackSelection{Name: identity.Name, Version: identity.Version, Digest: identity.Digest}
	resolution := Resolve([]policy.PackSelection{selected}, dataRoot)
	if len(resolution.Findings) != 0 || len(resolution.Commands) != 1 {
		t.Fatalf("unexpected resolution: %+v", resolution)
	}
	command := resolution.Commands[0]
	if command.Adapter == nil || !command.Managed || !command.SealedEnvironment || !slices.Equal(command.Provides, []string{"lint"}) || !slices.Equal(command.RunOn, []string{"check", "gate"}) {
		t.Fatalf("pack command did not compile into the managed model: %+v", command)
	}
}

func TestAdapterExecutionProducesNormalFindingsAndDetectsConcurrentTampering(t *testing.T) {
	source := writePackSource(t)
	dataRoot := filepath.Join(t.TempDir(), "packs")
	t.Cleanup(func() { makeWritable(dataRoot) })
	identity, installed, err := Install(source, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	resolution := Resolve([]policy.PackSelection{{Name: identity.Name, Version: identity.Version, Digest: identity.Digest}}, dataRoot)
	root := t.TempDir()
	writeTestFile(t, root, "src/main.fixture", "bad\n", 0o644)
	config := policy.Config{Modules: []policy.Module{{Name: "app", Paths: []string{"src/**"}}}, ModuleByName: map[string]int{"app": 0}}
	repo, err := repository.Open(root, root, config)
	if err != nil {
		t.Fatal(err)
	}
	boundary := &responseRunner{responses: [][]byte{[]byte(`{"protocolVersion":1,"status":"findings","findings":[{"capability":"lint","path":"src/main.fixture","line":2,"subject":"bad","message":"bad source"}]}`)}}
	findings := RunAdapter(t.Context(), repo, repository.Selection{Files: []string{"src/main.fixture"}}, resolution.Commands[0], boundary, "check")
	if len(findings) != 1 || findings[0].Check != "pack.lint" || findings[0].Line != 2 || findings[0].Subject != "bad" {
		t.Fatalf("unexpected findings: %+v", findings)
	}
	boundary.mutate = func() {
		adapter := filepath.Join(installed, "bin", "adapter")
		_ = os.Chmod(adapter, 0o755)
		_ = os.WriteFile(adapter, []byte("changed"), 0o755)
	}
	boundary.responses = [][]byte{[]byte(`{"protocolVersion":1,"status":"pass","evidence":["lint ran"]}`)}
	findings = RunAdapter(t.Context(), repo, repository.Selection{Files: []string{"src/main.fixture"}}, resolution.Commands[0], boundary, "check")
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "changed during execution") {
		t.Fatalf("concurrent tampering was not visible: %+v", findings)
	}
}

func TestVerifySourceRunsEveryDeclaredFixture(t *testing.T) {
	source := writePackSource(t)
	boundary := &responseRunner{responses: [][]byte{
		[]byte(`{"protocolVersion":1,"status":"pass","evidence":["lint ran"]}`),
		[]byte(`{"protocolVersion":1,"status":"findings","findings":[{"capability":"lint","path":"src/main.fixture","subject":"bad","message":"bad source"}]}`),
	}}
	result, err := VerifySource(context.Background(), source, boundary)
	if err != nil || result.Fixtures != 2 || len(boundary.requests) != 2 {
		t.Fatalf("fixture verification failed: %+v %v", result, err)
	}
}

type responseRunner struct {
	responses [][]byte
	requests  []Request
	mutate    func()
}

func (boundary *responseRunner) Run(context.Context, string, policy.Command) error { return nil }

func (boundary *responseRunner) RunWithOutput(_ context.Context, _ string, command policy.Command) (runner.Result, runner.Output, error) {
	return boundary.RunStructured(context.Background(), "", command)
}

func (boundary *responseRunner) RunStructured(_ context.Context, _ string, command policy.Command) (runner.Result, runner.Output, error) {
	request := Request{}
	if err := json.Unmarshal(command.Stdin, &request); err != nil {
		return runner.Result{}, runner.Output{}, err
	}
	boundary.requests = append(boundary.requests, request)
	if boundary.mutate != nil {
		boundary.mutate()
		boundary.mutate = nil
	}
	response := boundary.responses[0]
	boundary.responses = boundary.responses[1:]
	return runner.Result{}, runner.Output{Stdout: response}, nil
}

func writePackSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, root, ManifestFilename, string(testManifest(t)), 0o644)
	writeTestFile(t, root, "README.md", "# Fixture pack\n", 0o644)
	writeTestFile(t, root, "bin/adapter", "adapter\n", 0o755)
	writeTestFile(t, root, "fixtures/pass/src/main.fixture", "good\n", 0o644)
	writeTestFile(t, root, "fixtures/fail/src/main.fixture", "bad\n", 0o644)
	return root
}

func testManifest(t *testing.T) []byte {
	t.Helper()
	manifest := Manifest{
		Schema: "../../schema/code-polishy-pack.schema.json", ManifestVersion: ManifestVersion,
		Name: "fixture-language", Version: "1.0.0", ProtocolVersion: ProtocolVersion, Platforms: []string{CurrentPlatform()},
		Languages: []Language{{ID: "fixture", SourcePatterns: []string{"**/*.fixture"}}},
		Commands:  []Command{{Name: "adapter", Argv: []string{"bin/adapter"}, Capabilities: []string{"lint"}, Profiles: []string{"check", "gate"}, TimeoutSeconds: 30}},
		Fixtures: []Fixture{
			{Name: "lint-pass", Command: "adapter", Capability: "lint", Project: "fixtures/pass", Files: []string{"src/main.fixture"}, ExpectedStatus: "pass"},
			{Name: "lint-fail", Command: "adapter", Capability: "lint", Project: "fixtures/fail", Files: []string{"src/main.fixture"}, ExpectedStatus: "findings"},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeTestFile(t *testing.T, root, relative, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
