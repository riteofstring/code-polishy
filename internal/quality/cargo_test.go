package quality

import (
	"bytes"
	"cmp"
	"context"
	"io"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestCargoCheckDiscoversInvalidMemberFromSelectedRustSource(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	writeQualityFile(t, repo.Root, "Cargo.toml", "[workspace]\nmembers=['app', 'missing']\n")
	writeQualityFile(t, repo.Root, "app/Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\n")
	writeQualityFile(t, repo.Root, "app/src/lib.rs", "pub fn app() {}\n")
	findings := cargoDiscoveryFindings(t.Context(), repo, []string{"app/src/lib.rs"}, runner.OSRunner{})
	if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Check == "policy.rustProject" && finding.Path == "app/Cargo.toml" && strings.HasPrefix(finding.Subject, "policy-cargo-metadata-") && strings.Contains(finding.Message, "missing")
	}) {
		t.Fatalf("missing workspace failure from selected source: %+v", findings)
	}
}

func TestCargoCheckCommandsExposeActualDiscoveryPlan(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	writeQualityFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\n")
	writeQualityFile(t, repo.Root, "src/lib.rs", "pub fn app() {}\n")
	commands := CheckCommands(repo, repository.Selection{Files: []string{"src/lib.rs"}}, "check")
	for _, name := range []string{"policy-rust-version", "policy-cargo-version"} {
		if !slices.ContainsFunc(commands, func(command policy.Command) bool { return command.Name == name && command.SealedEnvironment }) {
			t.Errorf("Cargo command %s is missing from check planning: %+v", name, commands)
		}
	}
	if !slices.ContainsFunc(commands, func(command policy.Command) bool {
		return strings.HasPrefix(command.Name, "policy-cargo-metadata-") && command.Argv[len(command.Argv)-1] == "Cargo.toml" && command.SealedEnvironment
	}) {
		t.Fatalf("Cargo manifest discovery is missing from check planning: %+v", commands)
	}
}

func cargoQualityRepository(t *testing.T) repository.Repository {
	t.Helper()
	repo := qualityRepository(t)
	var err error
	repo.PolicyRoot, err = filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestCargoAndGoCheckPlanMatchesObservedCommandOrder(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	writeQualityFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\n")
	writeQualityFile(t, repo.Root, "Cargo.lock", "version=4\n[[package]]\nname='app'\nversion='1.0.0'\n")
	writeQualityFile(t, repo.Root, "src/lib.rs", "pub fn app() {}\n")
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/mixed\n\ngo 1.26.6\n")
	writeQualityFile(t, repo.Root, "main.go", "package main\n\nfunc main() {}\n")
	selection := repository.Selection{Files: []string{"main.go", "src/lib.rs"}}
	planned := CheckCommands(repo, selection, "check")
	boundary := &cargoRecordingRunner{OSRunner: runner.OSRunner{}}
	if findings := Check(t.Context(), repo, selection, boundary, "check"); len(findings) != 0 {
		t.Fatalf("mixed language check: %+v", findings)
	}
	want := []string{}
	for _, command := range planned {
		want = append(want, command.Name)
	}
	if !reflect.DeepEqual(boundary.names, want) || len(want) < 5 {
		t.Fatalf("executed commands = %v, planned = %v", boundary.names, want)
	}
}

func TestCargoNativeDeadCodePreservesPublicAPIAndChecksSelectedFeatures(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	manifest := "[package]\nname='app'\nversion='1.0.0'\nedition='2024'\n[features]\nactive=[]\ndefault=[]\n"
	writeQualityFile(t, repo.Root, "Cargo.toml", manifest)
	writeQualityFile(t, repo.Root, "Cargo.lock", "version=4\n[[package]]\nname='app'\nversion='1.0.0'\n")
	writeQualityFile(t, repo.Root, "src/lib.rs", "fn feature_helper() {}\n#[cfg(feature = \"active\")]\npub fn entry() {\n    feature_helper();\n}\npub fn public_api() {}\n")
	selection := repository.Selection{Files: []string{"src/lib.rs"}}
	findings := Check(t.Context(), repo, selection, runner.OSRunner{}, "check")
	if !repo.NativeAnalysis("src/lib.rs", "dead-code") || len(findings) == 0 || slices.ContainsFunc(findings, func(finding policy.Finding) bool { return finding.Subject != "dead_code" || finding.Line != 1 }) {
		t.Fatalf("native dead-code coverage did not reflect the checked configuration: %+v", findings)
	}
	writeQualityFile(t, repo.Root, "Cargo.toml", strings.Replace(manifest, "default=[]", "default=['active']", 1))
	if findings := Check(t.Context(), repo, selection, runner.OSRunner{}, "check"); len(findings) != 0 {
		t.Fatalf("enabled private helper or legitimate public API was treated as dead: %+v", findings)
	}
}

type cargoRecordingRunner struct {
	runner.OSRunner
	names []string
}

func (boundary *cargoRecordingRunner) runPlain(ctx context.Context, root string, command policy.Command) error {
	boundary.names = append(boundary.names, command.Name)
	return runner.RunCommand(ctx, boundary.OSRunner, root, command)
}

func (boundary *cargoRecordingRunner) runStructured(ctx context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	boundary.names = append(boundary.names, command.Name)
	return runner.RunCaptured(ctx, boundary.OSRunner, root, command, runner.CaptureStructured)
}

func (boundary *cargoRecordingRunner) runStreamed(ctx context.Context, root string, command policy.Command, stdout, stderr io.Writer) (runner.Result, error) {
	boundary.names = append(boundary.names, command.Name)
	return runner.RunStreamed(ctx, boundary.OSRunner, root, command, stdout, stderr)
}

func (double *cargoRecordingRunner) Execute(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	if invocation.Stdout != nil || invocation.Stderr != nil {
		var stdout, stderr bytes.Buffer
		result, err := double.runStreamed(ctx, invocation.Root, invocation.Command, io.MultiWriter(&stdout, cmp.Or[io.Writer](invocation.Stdout, io.Discard)), io.MultiWriter(&stderr, cmp.Or[io.Writer](invocation.Stderr, io.Discard)))
		return result, runner.Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
	}
	if invocation.Capture == runner.CaptureStructured {
		return double.runStructured(ctx, invocation.Root, invocation.Command)
	}
	return runner.Result{}, runner.Output{}, double.runPlain(ctx, invocation.Root, invocation.Command)
}
