package rustfacts

import (
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRustLintCollectionCoversCompilerWarningsWithoutLoweringErrors(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	_, output, err := runner.RunCaptured(t.Context(), runner.OSRunner{}, repo.Root, Command(repo, "rust-lint-interface", "rustc", "-W", "help"), runner.CaptureStructured)
	if err != nil {
		t.Fatalf("compiler lint interface: %v; %s", err, output.Stderr)
	}
	requested := map[string]bool{}
	arguments := compilationCommand(repo, "Cargo.toml").Argv
	for index, argument := range arguments[:len(arguments)-1] {
		if argument == "--warn" {
			requested[arguments[index+1]] = true
		}
	}
	warnings, errors := 0, 0
	for _, line := range strings.Split(string(output.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || slices.Contains([]string{"warnings", "tail-call-track-caller"}, fields[0]) || !slices.Contains([]string{"warn", "deny"}, fields[1]) {
			continue
		}
		if fields[1] == "warn" {
			warnings++
		} else {
			errors++
		}
		if requested[fields[0]] != (fields[1] == "warn") {
			t.Errorf("policy lint collection changes the compiler's %s contract for %s", fields[1], fields[0])
		}
	}
	if warnings == 0 || errors == 0 {
		t.Fatal("compiler lint interface omitted its default warning/error inventory")
	}
}

func TestRustCompilerTailCallWarningRequiresAnUnsupportedFeature(t *testing.T) {
	t.Parallel()
	repo := compilationRepository(t, "pub fn answer() {}\n")
	command := Command(repo, "rust-unstable-lint", "rustc", "src/lib.rs", "--crate-type=lib", "--emit=metadata", "--out-dir", repo.Root, "--force-warn", "tail-call-track-caller")
	_, output, err := runner.RunCaptured(t.Context(), runner.OSRunner{}, repo.Root, command, runner.CaptureStructured)
	if err != nil || !strings.Contains(string(output.Stderr), "the `tail_call_track_caller` lint is unstable") {
		t.Fatalf("excluded lint is no longer feature-gated: %v; %s", err, output.Stderr)
	}
}
