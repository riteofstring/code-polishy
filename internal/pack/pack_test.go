package pack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/schema"
)

const testEngineVersion = "0.25.0"

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
		{"legacy manifest", func(value map[string]any) { value["manifestVersion"] = float64(2) }, "manifestVersion"},
		{"legacy protocol", func(value map[string]any) { value["protocolVersion"] = float64(3) }, "protocolVersion"},
		{"missing engine version", func(value map[string]any) { delete(value, "engineVersion") }, "engineVersion"},
		{"missing discovery mode", func(value map[string]any) {
			delete(value["languages"].([]any)[0].(map[string]any), "discoveryMode")
		}, "discoveryMode"},
		{"missing language detector", func(value map[string]any) {
			delete(value["languages"].([]any)[0].(map[string]any), "sourcePatterns")
		}, "sourcePatterns or shebangs"},
		{"invalid shebang", func(value map[string]any) {
			value["languages"].([]any)[0].(map[string]any)["shebangs"] = []any{"#! /usr/bin/fixture"}
		}, "canonical shebang"},
		{"invalid unsupported capability", func(value map[string]any) {
			value["languages"].([]any)[0].(map[string]any)["unsupportedCapabilities"] = []any{map[string]any{"capability": "unknown", "reason": "not implemented"}}
		}, "unique standard capability"},
		{"empty unsupported reason", func(value map[string]any) {
			value["languages"].([]any)[0].(map[string]any)["unsupportedCapabilities"] = []any{map[string]any{"capability": "format", "reason": " "}}
		}, "non-whitespace"},
		{"provided capability marked unsupported", func(value map[string]any) {
			value["languages"].([]any)[0].(map[string]any)["unsupportedCapabilities"] = []any{map[string]any{"capability": "lint", "reason": "not implemented"}}
		}, "not provided"},
		{"ambiguous shebang", func(value map[string]any) {
			language := value["languages"].([]any)[0].(map[string]any)
			language["shebangs"] = []any{"#!/usr/bin/env"}
			value["languages"] = append(value["languages"].([]any), map[string]any{"id": "other", "shebangs": []any{"#!/usr/bin/env fixture"}, "discoveryMode": "file-scoped"})
		}, "owned by both"},
		{"file-scoped metadata", func(value map[string]any) {
			value["languages"].([]any)[0].(map[string]any)["metadataPatterns"] = []any{"package.json"}
		}, "metadataPatterns"},
		{"unsafe executable", func(value map[string]any) {
			value["commands"].([]any)[0].(map[string]any)["argv"] = []any{"../adapter"}
		}, "contained relative path"},
		{"unsafe runtime executable", func(value map[string]any) {
			value["executables"] = []any{"../helper"}
		}, "executables[0]"},
		{"unknown command language", func(value map[string]any) {
			value["commands"].([]any)[0].(map[string]any)["languages"] = []any{"other"}
		}, "languages"},
		{"missing execution", func(value map[string]any) {
			delete(value["commands"].([]any)[0].(map[string]any), "execution")
		}, "execution.type"},
		{"self-contained tools", func(value map[string]any) {
			value["commands"].([]any)[0].(map[string]any)["execution"].(map[string]any)["tools"] = []any{map[string]any{"id": "node", "name": "node", "version": "24.18.0", "launcher": true}}
		}, "empty"},
		{"host toolchain without tools", func(value map[string]any) {
			value["commands"].([]any)[0].(map[string]any)["execution"].(map[string]any)["type"] = "host-toolchain"
		}, "at least one exact host tool identity"},
		{"multiple launchers", func(value map[string]any) {
			execution := value["commands"].([]any)[0].(map[string]any)["execution"].(map[string]any)
			execution["type"] = "host-toolchain"
			execution["tools"] = []any{map[string]any{"id": "python", "name": "python", "version": "3.13.11", "launcher": true}, map[string]any{"id": "shellcheck", "name": "shellcheck", "version": "0.11.0", "launcher": true}}
		}, "at most one launcher"},
		{"colliding tool environment", func(value map[string]any) {
			execution := value["commands"].([]any)[0].(map[string]any)["execution"].(map[string]any)
			execution["type"] = "host-toolchain"
			execution["tools"] = []any{map[string]any{"id": "fixture-tool", "name": "python", "version": "3.13.11"}, map[string]any{"id": "fixture.tool", "name": "shellcheck", "version": "0.11.0"}}
		}, "unique lowercase identifier"},
		{"network authority", func(value map[string]any) {
			value["commands"].([]any)[0].(map[string]any)["execution"].(map[string]any)["network"] = "ambient"
		}, "none"},
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

func TestManifestOwnedShebangAndTestRulesDriveRepositoryClassification(t *testing.T) {
	manifest, err := ParseManifest(testManifest(t), "manifest")
	if err != nil {
		t.Fatal(err)
	}
	manifest.Languages[0].SourcePatterns = nil
	manifest.Languages[0].Shebangs = []string{"#!/usr/bin/env fixture"}
	manifest.Languages[0].TestPatterns = []string{"verification/**"}
	manifest.Languages[0].Unsupported = []UnsupportedCapability{{Capability: "format", Reason: "this language has no specified formatter"}}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewValidator(schema.ConfigurationBase + "code-polishy-pack.schema.json").Validate(encoded); err != nil {
		t.Fatalf("shebang-only manifest schema: %v", err)
	}
	manifest, err = ParseManifest(encoded, "manifest")
	if err != nil {
		t.Fatalf("shebang-only manifest decoder: %v", err)
	}
	resolution := Resolution{}
	compileManifest("/packs/fixture", policy.PackSelection{Name: manifest.Name, Version: manifest.Version, Digest: strings.Repeat("a", 64)}, manifest, &resolution)
	config := policy.Config{}
	Apply(&config, resolution)
	root := t.TempDir()
	writeTestFile(t, root, "verification/tool", "#!/usr/bin/env fixture\n", 0o755)
	repo, err := repository.Open(root, root, config)
	if err != nil {
		t.Fatal(err)
	}
	if language := repo.Language("verification/tool"); language != "fixture" {
		t.Fatalf("language = %q, want fixture", language)
	}
	if !repo.IsTest("verification/tool") {
		t.Fatal("manifest-owned test pattern did not classify source")
	}
	if len(resolution.Commands) != 1 || !repo.CommandOwnsPath(resolution.Commands[0], "verification/tool") {
		t.Fatal("shebang-only source was not owned by its pack command")
	}
	owner := repo.AnalysisOwner("verification/tool", "format", "format")
	if !owner.Unsupported || owner.Pack != manifest.Name || owner.Problem != "this language has no specified formatter" {
		t.Fatalf("explicit capability absence was lost: %+v", owner)
	}
}

func TestManifestCommandOwnsPatternsAndShebangsWithoutBroadeningEitherClaim(t *testing.T) {
	manifest, err := ParseManifest(testManifest(t), "manifest")
	if err != nil {
		t.Fatal(err)
	}
	manifest.Languages[0].Shebangs = []string{"#!/usr/bin/env fixture"}
	resolution := Resolution{}
	compileManifest("/packs/fixture", policy.PackSelection{Name: manifest.Name, Version: manifest.Version, Digest: strings.Repeat("a", 64)}, manifest, &resolution)
	root := t.TempDir()
	writeTestFile(t, root, "src/pattern.fixture", "value\n", 0o600)
	writeTestFile(t, root, "scripts/tool", "#!/usr/bin/env fixture\n", 0o700)
	writeTestFile(t, root, "scripts/other", "#!/usr/bin/env other\n", 0o700)
	config := policy.Config{}
	Apply(&config, resolution)
	repo, err := repository.Open(root, root, config)
	if err != nil {
		t.Fatal(err)
	}
	command := resolution.Commands[0]
	if !repo.CommandOwnsPath(command, "src/pattern.fixture") || !repo.CommandOwnsPath(command, "scripts/tool") {
		t.Fatal("manifest command did not retain both source recognition forms")
	}
	if repo.CommandOwnsPath(command, "scripts/other") {
		t.Fatal("manifest command claimed an undeclared shebang")
	}
	inventory := inventoryByPath(governedInventory(repo, command, []string{"src/pattern.fixture", "scripts/tool", "scripts/other"}, "check"))
	if !inventory["scripts/tool"].Source || inventory["scripts/tool"].Language != "fixture" || inventory["scripts/tool"].Owner != command.Name {
		t.Fatalf("shebang inventory = %+v", inventory["scripts/tool"])
	}
	if inventory["scripts/other"].Source {
		t.Fatalf("unclaimed shebang inventory = %+v", inventory["scripts/other"])
	}
}

func TestPublishedPackSchemasAndExamplesMatchProductionContracts(t *testing.T) {
	root := filepath.Join("..", "..")
	manifestData, err := os.ReadFile(filepath.Join(root, "tools", "fixtures", "language-pack", ManifestFilename))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewValidator(schema.ConfigurationBase + "code-polishy-pack.schema.json").Validate(manifestData); err != nil {
		t.Fatalf("manifest schema: %v", err)
	}
	if _, err := ParseManifest(manifestData, ManifestFilename); err != nil {
		t.Fatalf("manifest decoder: %v", err)
	}
	requestData, err := os.ReadFile(filepath.Join(root, "tools", "fixtures", "language-pack", "examples", "request-v4.json"))
	if err != nil {
		t.Fatal(err)
	}
	requestValidator := schema.NewValidator(schema.ConfigurationBase + "code-polishy-pack-request-v4.schema.json")
	if err := requestValidator.Validate(requestData); err != nil {
		t.Fatalf("request schema: %v", err)
	}
	requestDecoder := json.NewDecoder(bytes.NewReader(requestData))
	requestDecoder.DisallowUnknownFields()
	request := Request{}
	if err := requestDecoder.Decode(&request); err != nil {
		t.Fatalf("request decoder: %v", err)
	}
	encodedRequest, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := requestValidator.Validate(encodedRequest); err != nil {
		t.Fatalf("production request encoder: %v", err)
	}
	responseData, err := os.ReadFile(filepath.Join(root, "tools", "fixtures", "language-pack", "examples", "response-v4.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewValidator(schema.ConfigurationBase + "code-polishy-pack-response-v4.schema.json").Validate(responseData); err != nil {
		t.Fatalf("response schema: %v", err)
	}
	response, err := decodeResponse(responseData)
	if err != nil {
		t.Fatalf("response decoder: %v", err)
	}
	if err := validateResponse(response, request); err != nil {
		t.Fatalf("response contract: %v", err)
	}
}

func TestInstallPublishesExactContentAddressedTreeAndDetectsTampering(t *testing.T) {
	source := writePackSource(t)
	dataRoot := filepath.Join(t.TempDir(), "packs")
	t.Cleanup(func() { makeWritable(dataRoot) })
	identity, installed, err := Install(source, dataRoot, testEngineVersion)
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
	second, secondRoot, err := Install(source, dataRoot, testEngineVersion)
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

func TestInstallPreservesDeclaredRuntimeExecutables(t *testing.T) {
	source := writePackSource(t)
	manifest, err := ParseManifest(testManifest(t), "manifest")
	if err != nil {
		t.Fatal(err)
	}
	manifest.Executables = []string{"bin/runtime-helper"}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, source, ManifestFilename, string(data), 0o644)
	writeTestFile(t, source, "bin/runtime-helper", "helper\n", 0o755)
	dataRoot := filepath.Join(t.TempDir(), "packs")
	t.Cleanup(func() { makeWritable(dataRoot) })
	_, installed, err := Install(source, dataRoot, testEngineVersion)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(installed, "bin", "runtime-helper"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o555 {
		t.Fatalf("runtime helper mode = %s", info.Mode().Perm())
	}
	manifest.Executables = []string{"bin/missing-helper"}
	data, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, source, ManifestFilename, string(data), 0o644)
	if _, _, err := Install(source, dataRoot, testEngineVersion); err == nil || !strings.Contains(err.Error(), "executables[0]") {
		t.Fatalf("missing runtime executable passed: %v", err)
	}
}

func TestTreeDigestIsDeterministicAndDifferentBytesNeverReplaceAnIdentity(t *testing.T) {
	firstSource := writePackSource(t)
	secondSource := writePackSource(t)
	dataRoot := filepath.Join(t.TempDir(), "packs")
	t.Cleanup(func() { makeWritable(dataRoot) })
	first, _, err := Install(firstSource, dataRoot, testEngineVersion)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := Install(secondSource, dataRoot, testEngineVersion)
	if err != nil || first != second {
		t.Fatalf("identical trees did not share an identity: %+v %+v %v", first, second, err)
	}
	writeTestFile(t, secondSource, "README.md", "# Different bytes\n", 0o644)
	changed, changedRoot, err := Install(secondSource, dataRoot, testEngineVersion)
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
	if _, _, err := Install(source, dataRoot, testEngineVersion); err == nil || !strings.Contains(err.Error(), "symlink") {
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
		{"fake success", `{"protocolVersion":4,"status":"pass"}`},
		{"extra response", `{"protocolVersion":4,"status":"pass","evidence":["lint ran"],"coverage":{"analyzed":["src/main.fixture"],"unsupported":[]}}{}`},
		{"escaping finding", `{"protocolVersion":4,"status":"findings","coverage":{"analyzed":["src/main.fixture"],"unsupported":[]},"findings":[{"capability":"lint","rule":"invalid-source","path":"../secret","subject":"bad","message":"bad"}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseResponse([]byte(test.data), Request{Capability: "lint", Files: []string{"src/main.fixture"}, DiagnosticFiles: []string{"src/main.fixture"}, WriteFiles: []string{"src/main.fixture"}}); err == nil {
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
			if !slices.Equal(request.Files, sortedUnique(test.want)) {
				t.Fatalf("files = %v, want %v", request.Files, test.want)
			}
		})
	}
}

func TestResolveCompilesExactPackProvidersIntoManagedProfiles(t *testing.T) {
	source := writePackSource(t)
	dataRoot := filepath.Join(t.TempDir(), "packs")
	t.Cleanup(func() { makeWritable(dataRoot) })
	identity, _, err := Install(source, dataRoot, testEngineVersion)
	if err != nil {
		t.Fatal(err)
	}
	selected := policy.PackSelection{Name: identity.Name, Version: identity.Version, Digest: identity.Digest}
	resolution := Resolve([]policy.PackSelection{selected}, dataRoot, testEngineVersion)
	if len(resolution.Findings) != 0 || len(resolution.Commands) != 1 {
		t.Fatalf("unexpected resolution: %+v", resolution)
	}
	command := resolution.Commands[0]
	if command.Adapter == nil || !command.Managed || !command.SealedEnvironment || !slices.Equal(command.Provides, []string{"lint"}) || !slices.Equal(command.RunOn, []string{"check", "gate"}) {
		t.Fatalf("pack command did not compile into the managed model: %+v", command)
	}
}

func TestEngineUpgradeRequiresAnExactPackCutover(t *testing.T) {
	source := writePackSource(t)
	dataRoot := filepath.Join(t.TempDir(), "packs")
	t.Cleanup(func() { makeWritable(dataRoot) })
	if _, _, err := Install(source, dataRoot, "0.26.0"); err == nil || !strings.Contains(err.Error(), "requires Code Polishy "+testEngineVersion) {
		t.Fatalf("different engine installed pack: %v", err)
	}
	identity, _, err := Install(source, dataRoot, testEngineVersion)
	if err != nil {
		t.Fatal(err)
	}
	selected := []policy.PackSelection{{Name: identity.Name, Version: identity.Version, Digest: identity.Digest}}
	resolution := Resolve(selected, dataRoot, "0.26.0")
	if len(resolution.Findings) != 1 || len(resolution.Commands) == 0 || !strings.Contains(resolution.Findings[0].Message, "requires Code Polishy") {
		t.Fatalf("different engine resolved pack: %+v", resolution)
	}
	config := policy.Config{}
	Apply(&config, resolution)
	repo := repository.Repository{Config: config}
	if owner := repo.AnalysisOwner("main.fixture", "lint", "check"); owner.Native || !strings.Contains(owner.Problem, "unavailable") {
		t.Fatalf("different engine enabled fallback: %+v", owner)
	}
	statuses, err := List(dataRoot, selected, "0.26.0")
	if err != nil || len(statuses) != 1 || statuses[0].State != "incompatible" {
		t.Fatalf("different engine status = %+v: %v", statuses, err)
	}
	if _, err := VerifySource(context.Background(), source, source, "0.26.0", &responseRunner{}); err == nil || !strings.Contains(err.Error(), "requires Code Polishy") {
		t.Fatalf("different engine verified pack: %v", err)
	}
}

func TestAdapterExecutionProducesNormalFindingsAndDetectsConcurrentTampering(t *testing.T) {
	source := writePackSource(t)
	dataRoot := filepath.Join(t.TempDir(), "packs")
	t.Cleanup(func() { makeWritable(dataRoot) })
	identity, installed, err := Install(source, dataRoot, testEngineVersion)
	if err != nil {
		t.Fatal(err)
	}
	resolution := Resolve([]policy.PackSelection{{Name: identity.Name, Version: identity.Version, Digest: identity.Digest}}, dataRoot, testEngineVersion)
	root := t.TempDir()
	writeTestFile(t, root, "src/main.fixture", "bad\n", 0o644)
	config := policy.Config{Modules: []policy.Module{{Name: "app", Paths: []string{"src/**"}}}, ModuleByName: map[string]int{"app": 0}}
	repo, err := repository.Open(root, root, config)
	if err != nil {
		t.Fatal(err)
	}
	boundary := &responseRunner{responses: [][]byte{[]byte(`{"protocolVersion":4,"status":"findings","coverage":{"analyzed":["src/main.fixture"],"unsupported":[]},"findings":[{"capability":"lint","rule":"invalid-source","path":"src/main.fixture","line":1,"column":1,"subject":"bad","message":"bad source"}]}`)}}
	result := RunAdapter(t.Context(), repo, repository.Selection{Files: []string{"src/main.fixture"}}, resolution.Commands[0], boundary, "check")
	findings := result.Findings
	if len(findings) != 1 || findings[0].Check != "pack.fixture-language.invalid-source" || findings[0].Line != 1 || findings[0].Subject != "bad" {
		t.Fatalf("unexpected findings: %+v", findings)
	}
	boundary.mutate = func() {
		adapter := filepath.Join(installed, "bin", "adapter")
		_ = os.Chmod(adapter, 0o755)
		_ = os.WriteFile(adapter, []byte("changed"), 0o755)
	}
	boundary.responses = [][]byte{[]byte(`{"protocolVersion":4,"status":"pass","evidence":["lint ran"],"coverage":{"analyzed":["src/main.fixture"],"unsupported":[]}}`)}
	result = RunAdapter(t.Context(), repo, repository.Selection{Files: []string{"src/main.fixture"}}, resolution.Commands[0], boundary, "check")
	findings = result.Findings
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "changed during execution") {
		t.Fatalf("concurrent tampering was not visible: %+v", findings)
	}
}

func TestRunAdapterValidatesStaticDiscoveryBeforeCapabilityExecution(t *testing.T) {
	source := writePackSource(t)
	manifest, err := ParseManifest(testManifest(t), "manifest")
	if err != nil {
		t.Fatal(err)
	}
	manifest.Languages[0].DiscoveryMode = "static"
	manifest.Languages[0].MetadataPatterns = []string{"**/project.fixture.json"}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, source, ManifestFilename, string(data), 0o644)
	store := t.TempDir()
	t.Cleanup(func() { makeWritable(store) })
	identity, _, err := Install(source, store, testEngineVersion)
	if err != nil {
		t.Fatal(err)
	}
	resolution := Resolve([]policy.PackSelection{{Name: identity.Name, Version: identity.Version, Digest: identity.Digest}}, store, testEngineVersion)
	root := t.TempDir()
	writeTestFile(t, root, "project.fixture.json", "{}\n", 0o644)
	writeTestFile(t, root, "src/main.fixture", "good\n", 0o644)
	config := policy.Config{Modules: []policy.Module{{Name: "app", Paths: []string{"src/**", "project.fixture.json"}}}, ModuleByName: map[string]int{"app": 0}}
	Apply(&config, resolution)
	repo, err := repository.Open(root, root, config)
	if err != nil {
		t.Fatal(err)
	}
	discovery := `{"protocolVersion":4,"status":"pass","evidence":["static fixture discovery"],"discovery":{"scopes":[{"id":"project","language":"fixture","root":".","members":["src/main.fixture"],"entryFiles":["src/main.fixture"],"context":["project.fixture.json"],"selected":["src/main.fixture"],"data":{"manifest":"project.fixture.json"}}]}}`
	analysis := `{"protocolVersion":4,"status":"pass","evidence":["lint ran"],"coverage":{"analyzed":["src/main.fixture"],"unsupported":[]}}`
	boundary := &responseRunner{responses: [][]byte{[]byte(discovery), []byte(analysis)}}
	result := RunAdapter(t.Context(), repo, repository.Selection{Files: []string{"src/main.fixture"}}, resolution.Commands[0], boundary, "check")
	if len(result.Findings) != 0 || len(boundary.requests) != 2 {
		t.Fatalf("static execution = %+v, requests = %+v", result, boundary.requests)
	}
	if boundary.requests[0].Operation != "discover" || boundary.requests[1].Operation != "check" || len(boundary.requests[1].Scopes) != 1 || boundary.requests[1].Scopes[0].Handle != "scope-1" {
		t.Fatalf("protocol sequence = %+v", boundary.requests)
	}
}

func TestVerifySourceRunsEveryDeclaredFixture(t *testing.T) {
	source := writePackSource(t)
	boundary := &responseRunner{responses: [][]byte{
		[]byte(`{"protocolVersion":4,"status":"pass","evidence":["lint ran"],"coverage":{"analyzed":["src/main.fixture"],"unsupported":[]}}`),
		[]byte(`{"protocolVersion":4,"status":"findings","coverage":{"analyzed":["src/main.fixture"],"unsupported":[]},"findings":[{"capability":"lint","rule":"invalid-source","path":"src/main.fixture","subject":"bad","message":"bad source"}]}`),
	}}
	result, err := VerifySource(context.Background(), source, source, testEngineVersion, boundary)
	if err != nil || result.Fixtures != 2 || len(boundary.requests) != 2 {
		t.Fatalf("fixture verification failed: %+v %v", result, err)
	}
}

func TestVerifySourceBindsSensitiveControlWithoutTreatingItAsPackMetadata(t *testing.T) {
	source := writePackSource(t)
	writeTestFile(t, source, "fixtures/pass/package.json", "{}\n", 0o644)
	writeTestFile(t, source, "fixtures/fail/package.json", "{}\n", 0o644)
	writeTestFile(t, source, ".gitignore", "fixtures/\n", 0o644)
	if output, err := exec.Command("git", "-C", source, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("initialize source checkout: %s %v", output, err)
	}
	boundary := &responseRunner{responses: [][]byte{
		[]byte(`{"protocolVersion":4,"status":"pass","evidence":["lint ran"],"coverage":{"analyzed":["src/main.fixture"],"unsupported":[]}}`),
		[]byte(`{"protocolVersion":4,"status":"findings","coverage":{"analyzed":["src/main.fixture"],"unsupported":[]},"findings":[{"capability":"lint","rule":"invalid-source","path":"src/main.fixture","subject":"bad","message":"bad source"}]}`),
	}}
	result, err := VerifySource(context.Background(), source, source, testEngineVersion, boundary)
	if err != nil || result.Fixtures != 2 || len(boundary.requests) != 2 {
		t.Fatalf("fixture verification failed: %+v %v", result, err)
	}
	for _, request := range boundary.requests {
		if !slices.ContainsFunc(request.Context, func(input InputFile) bool { return input.Path == "package.json" }) {
			t.Fatalf("file-scoped fixture omitted sensitive control: %+v", request.Context)
		}
		entry := inventoryByPath(request.Inventory)["package.json"]
		if !entry.Control || entry.Metadata || entry.Dependency || entry.Source {
			t.Fatalf("sensitive control classification = %+v", entry)
		}
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
	var value map[string]any
	if err := json.Unmarshal(response, &value); err != nil {
		return runner.Result{}, runner.Output{}, err
	}
	value["inputs"] = request.Context
	if request.Operation != "discover" {
		handles := make([]string, 0, len(request.Scopes))
		for _, scope := range request.Scopes {
			handles = append(handles, scope.Handle)
		}
		value["scopeHandles"] = handles
	}
	response, _ = json.Marshal(value)
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
		Name: "fixture-language", Version: "1.0.0", EngineVersion: testEngineVersion, ProtocolVersion: ProtocolVersion, Platforms: []string{CurrentPlatform()},
		Languages: []Language{{ID: "fixture", SourcePatterns: []string{"**/*.fixture"}, DiscoveryMode: "file-scoped"}},
		Commands:  []Command{{Name: "adapter", Argv: []string{"bin/adapter"}, Languages: []string{"fixture"}, Capabilities: []string{"lint"}, Profiles: []string{"check", "gate"}, TimeoutSeconds: 30, Execution: CommandExecution{Type: "self-contained", Network: "none"}}},
		Fixtures: []Fixture{
			{Name: "lint-pass", Command: "adapter", Capability: "lint", Project: "fixtures/pass", Files: []string{"src/main.fixture"}, ExpectedStatus: "pass"},
			{Name: "lint-fail", Command: "adapter", Capability: "lint", Project: "fixtures/fail", Files: []string{"src/main.fixture"}, ExpectedStatus: "findings", ExpectedRules: []string{"invalid-source"}},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFixtureExpectationAllowsIncompleteCoverageEvidence(t *testing.T) {
	fixture := Fixture{ExpectedStatus: "incomplete", ExpectedRules: []string{"project.configuration"}}
	if err := validateFixtureExpectation(fixture, "fixtures[0]"); err != nil {
		t.Fatal(err)
	}
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

func TestUnavailablePackRetainsOnlyAuthenticatedClaims(t *testing.T) {
	source, store := writePackSource(t), t.TempDir()
	t.Cleanup(func() { makeWritable(store) })
	identity, installed, err := Install(source, store, testEngineVersion)
	if err != nil {
		t.Fatal(err)
	}
	selected := []policy.PackSelection{{Name: identity.Name, Version: identity.Version, Digest: identity.Digest}}
	makeWritable(installed)
	if err := os.WriteFile(filepath.Join(installed, "bin/adapter"), []byte("changed"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolution := Resolve(selected, store, testEngineVersion)
	if len(resolution.Findings) != 1 || len(resolution.Commands) == 0 {
		t.Fatalf("failed pack lost authenticated claims: %+v", resolution)
	}
	config := policy.Config{}
	Apply(&config, resolution)
	repo := repository.Repository{Config: config}
	if owner := repo.AnalysisOwner("src/main.fixture", "lint", "check"); owner.Native || !strings.Contains(owner.Problem, "unavailable") {
		t.Fatalf("failed claim became analyzable: %+v", owner)
	}
	if !repo.NativeAnalysis("main.py", "lint") {
		t.Fatal("unrelated Python was suppressed")
	}
	if err := os.WriteFile(filepath.Join(installed, ManifestFilename), []byte("forged"), 0o644); err != nil {
		t.Fatal(err)
	}
	if forged := Resolve(selected, store, testEngineVersion); len(forged.Findings) != 1 || len(forged.Commands) != 0 {
		t.Fatalf("unauthenticated manifest established claims: %+v", forged)
	}
}
