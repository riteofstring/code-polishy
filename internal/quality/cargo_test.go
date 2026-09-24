package quality

import (
	"path/filepath"
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
