package supplychain

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestCargoSupplyChainResolvesThroughItsExactCommandPlan(t *testing.T) {
	t.Parallel()
	repo := cargoSupplyRepository(t)
	commands, err := OnlineCommands(repo, []string{"Cargo.toml", "Cargo.lock"})
	if err != nil {
		t.Fatal(err)
	}
	boundary := &cargoSupplyRunner{}
	findings := OnlineWithCommands(t.Context(), repo, []string{"Cargo.toml", "Cargo.lock"}, commands, boundary)
	if len(findings) != 0 || len(boundary.commands) != 4 || !reflect.DeepEqual(commands, boundary.commands) {
		t.Fatalf("Cargo online supply-chain = %+v, commands = %+v", findings, boundary.commands)
	}
	command := commands[len(commands)-1]
	if !slices.Contains(command.Argv, "--locked") || !command.SealedEnvironment || !slices.Contains(command.EnvironmentOverrides, "CARGO_NET_OFFLINE=false") {
		t.Fatalf("online lock verification = %+v", command)
	}
	boundary.commands = nil
	findings = CheckCargoResolution(t.Context(), repo, []string{"Cargo.toml", "Cargo.lock"}, true, boundary)
	if len(findings) != 0 || len(boundary.commands) != 4 || !slices.Contains(boundary.commands[3].Argv, "--frozen") {
		t.Fatalf("Cargo offline supply-chain = %+v, commands = %+v", findings, boundary.commands)
	}
}

func TestCargoSupplyChainRejectsAlteredExecutionEnvironments(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*policy.Command){
		"unsealed": func(command *policy.Command) { command.SealedEnvironment = false },
		"compiler override": func(command *policy.Command) {
			command.EnvironmentOverrides = append(command.EnvironmentOverrides, "RUSTC=untrusted")
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := cargoSupplyRepository(t)
			commands, err := OnlineCommands(repo, []string{"Cargo.toml"})
			if err != nil {
				t.Fatal(err)
			}
			mutate(&commands[len(commands)-1])
			boundary := &cargoSupplyRunner{}
			findings := OnlineWithCommands(t.Context(), repo, []string{"Cargo.toml"}, commands, boundary)
			if len(boundary.commands) != 0 || len(findings) != 1 || !strings.Contains(findings[0].Message, "differs from policy-derived") {
				t.Fatalf("altered resolution plan accepted: %+v", findings)
			}
		})
	}
}

func cargoSupplyRepository(t *testing.T) repository.Repository {
	t.Helper()
	repo := supplyRepository(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	repo.PolicyRoot = root
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\nedition='2021'\n")
	writeSupplyFile(t, repo.Root, "Cargo.lock", "version=4\n[[package]]\nname='app'\nversion='1.0.0'\n")
	writeSupplyFile(t, repo.Root, "src/lib.rs", "pub fn app() {}\n")
	return repo
}

type cargoSupplyRunner struct {
	commands []policy.Command
}

func (boundary *cargoSupplyRunner) runPlain(context.Context, string, policy.Command) error {
	panic("Cargo must use the structured execution boundary")
}

func (boundary *cargoSupplyRunner) runStructured(ctx context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	boundary.commands = append(boundary.commands, command)
	return runner.RunCaptured(ctx, runner.OSRunner{}, root, command, runner.CaptureStructured)
}

func (double *cargoSupplyRunner) Execute(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	if invocation.Capture == runner.CaptureStructured {
		return double.runStructured(ctx, invocation.Root, invocation.Command)
	}
	return runner.Result{}, runner.Output{}, double.runPlain(ctx, invocation.Root, invocation.Command)
}
