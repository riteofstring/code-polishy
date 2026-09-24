package rustfacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestCargoConfigurationRejectsExecutionOverridesBeforeDiscovery(t *testing.T) {
	t.Parallel()
	for name, configuration := range map[string]string{
		"compiler": "[build]\nrustc='unapproved'\n", "wrapper": "[build]\nrustc-wrapper='unapproved'\n",
		"flags": "[build]\nrustflags=['--cap-lints=allow']\n", "environment": "[env]\nRUSTC='unapproved'\n",
		"replacement": "[source.crates-io]\nreplace-with='local'\n", "include": "include='other.toml'\n",
		"malformed": "[build\n",
	} {
		t.Run(name, func(t *testing.T) {
			repo := repository.Repository{Root: t.TempDir()}
			write(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\n")
			write(t, repo.Root, ".cargo/config.toml", configuration)
			plan := PlanDiscovery(repo, []string{"Cargo.toml"}, []string{"Cargo.toml", ".cargo/config.toml"})
			if len(plan.Commands) != 0 || len(plan.Findings) != 1 || plan.Findings[0].Subject != "configuration" {
				t.Fatalf("unsafe configuration reached execution: %+v", plan)
			}
		})
	}
}

func TestCargoConfigurationAcceptsNonExecutableSettings(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Root: t.TempDir()}
	write(t, repo.Root, ".cargo/config.toml", "[alias]\nci='check --workspace'\n[build]\njobs=2\ntarget='x86_64-unknown-linux-gnu'\n[net]\noffline=true\n[term]\ncolor='never'\n")
	if err := configurationProblem(repo); err != nil {
		t.Fatal(err)
	}
}

func TestCargoConfigurationRejectsAncestorAndCargoHomeInputs(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"ancestor", "cargo-home"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			repo := repository.Repository{Root: filepath.Join(parent, "repo")}
			if err := os.Mkdir(repo.Root, 0o755); err != nil {
				t.Fatal(err)
			}
			if kind == "ancestor" {
				write(t, parent, ".cargo/config", "[env]\nRUSTC='external'\n")
			} else {
				write(t, repo.Root, ".tools/cargo-home/config.toml", "[env]\nRUSTC='external'\n")
			}
			if err := configurationProblem(repo); err == nil {
				t.Fatalf("accepted %s configuration", kind)
			}
		})
	}
}

func TestCargoConfigurationRejectsSymlinkedCache(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Root: t.TempDir()}
	if err := os.Symlink(t.TempDir(), filepath.Join(repo.Root, ".tools")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := configurationProblem(repo); err == nil || !strings.Contains(err.Error(), "contained regular directory") {
		t.Fatalf("linked execution directory accepted: %v", err)
	}
}

func TestCargoManifestWorkspacePathsStayWithinRepository(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Root: t.TempDir()}
	for _, value := range []string{"../..", "../sibling", "."} {
		manifest := repository.CargoManifest{Root: "crates/app", Package: &repository.CargoPackageManifest{Workspace: value}}
		if err := manifestPathProblem(repo, manifest); err != nil {
			t.Fatalf("contained workspace %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"../../..", "/outside", "C:\\outside"} {
		manifest := repository.CargoManifest{Root: "crates/app", Package: &repository.CargoPackageManifest{Workspace: value}}
		if err := manifestPathProblem(repo, manifest); err == nil {
			t.Fatalf("escaping workspace %q accepted", value)
		}
	}
}
