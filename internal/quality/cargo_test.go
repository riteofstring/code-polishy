package quality

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
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
		return finding.Check == "policy.rustProject" && finding.Subject == "policy-cargo-metadata-app/Cargo.toml"
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
	for _, name := range []string{"policy-rust-version", "policy-cargo-version", "policy-cargo-metadata-Cargo.toml"} {
		if !slices.ContainsFunc(commands, func(command policy.Command) bool { return command.Name == name && command.SealedEnvironment }) {
			t.Errorf("Cargo command %s is missing from check planning: %+v", name, commands)
		}
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

type cargoRecordingRunner struct {
	runner.OSRunner
	names []string
}

func (boundary *cargoRecordingRunner) Run(ctx context.Context, root string, command policy.Command) error {
	boundary.names = append(boundary.names, command.Name)
	return boundary.OSRunner.Run(ctx, root, command)
}

func (boundary *cargoRecordingRunner) RunStructured(ctx context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	boundary.names = append(boundary.names, command.Name)
	return boundary.OSRunner.RunStructured(ctx, root, command)
}
