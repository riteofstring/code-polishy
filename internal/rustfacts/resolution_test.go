package rustfacts

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestCargoResolutionPreservesWorkspaceEdgesWithoutExecutingCode(t *testing.T) {
	t.Parallel()
	for _, offline := range []bool{true, false} {
		t.Run(map[bool]string{true: "offline", false: "online"}[offline], func(t *testing.T) {
			repo, originalLock := resolutionRepository(t)
			plan := PlanResolution(repo, []string{"Cargo.toml"}, offline)
			boundary := &resolutionRunner{}
			result := Resolve(t.Context(), repo, plan, boundary)
			if len(result.Findings) != 0 || len(result.Resolutions) != 1 {
				t.Fatalf("resolution = %+v", result)
			}
			if !reflect.DeepEqual(boundary.commands, plan.Commands) {
				t.Fatal("resolution did not execute its exact structured command plan")
			}
			assertResolutionWorkspace(t, result.Resolutions[0])
			assertResolutionInputs(t, repo, originalLock)
		})
	}
}

func TestCargoResolutionRejectsStaleLocksWithoutRepair(t *testing.T) {
	t.Parallel()
	repo, originalLock := resolutionRepository(t)
	write(t, repo.Root, "utility/Cargo.toml", "[package]\nname='utility'\nversion='2.0.0'\nedition='2021'\n")
	result := Resolve(t.Context(), repo, PlanResolution(repo, []string{"Cargo.toml"}, true), runner.OSRunner{})
	if len(result.Resolutions) != 0 || len(result.Findings) != 1 || !strings.Contains(result.Findings[0].Message, "lock file") {
		t.Fatalf("stale lock resolution = %+v", result)
	}
	assertResolutionInputs(t, repo, originalLock)
}

func TestCargoResolutionRequiresLockAndOfflineSources(t *testing.T) {
	t.Parallel()
	for _, lock := range []string{"", "version=4\n[[package]]\nname='app'\nversion='1.0.0'\n"} {
		t.Run(map[bool]string{true: "missing lock", false: "missing sources"}[lock == ""], func(t *testing.T) {
			repo := rustRepository(t)
			write(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\nedition='2021'\n[dependencies]\nserde='=1.0.228'\n")
			write(t, repo.Root, "src/lib.rs", "pub fn app() {}\n")
			if lock != "" {
				write(t, repo.Root, "Cargo.lock", lock)
			}
			result := Resolve(t.Context(), repo, PlanResolution(repo, []string{"Cargo.toml"}, true), runner.OSRunner{})
			if len(result.Resolutions) != 0 || len(result.Findings) != 1 || result.Findings[0].Path != "Cargo.lock" {
				t.Fatalf("unprepared resolution = %+v", result)
			}
			if _, err := os.Stat(filepath.Join(CargoHome(repo), "registry", "src")); !os.IsNotExist(err) {
				t.Fatalf("offline resolution materialized registry sources: %v", err)
			}
		})
	}
}

func TestCargoResolutionPreservesValidIndependentProjects(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	write(t, repo.Root, "bad/Cargo.toml", "[package\n")
	write(t, repo.Root, "good/Cargo.toml", "[package]\nname='good'\nversion='1.0.0'\nedition='2021'\n")
	write(t, repo.Root, "good/Cargo.lock", "version=4\n[[package]]\nname='good'\nversion='1.0.0'\n")
	write(t, repo.Root, "good/src/lib.rs", "pub fn good() {}\n")
	result := Resolve(t.Context(), repo, PlanResolution(repo, []string{"bad/Cargo.toml", "good/Cargo.toml"}, true), runner.OSRunner{})
	if len(result.Findings) != 1 || len(result.Resolutions) != 1 || result.Resolutions[0].Lock.Path != "good/Cargo.lock" {
		t.Fatalf("independent resolution = %+v", result)
	}
}

func resolutionRepository(t *testing.T) (repository.Repository, string) {
	t.Helper()
	repo := rustRepository(t)
	write(t, repo.Root, "Cargo.toml", "[workspace]\nmembers=['app','utility','windows-support']\nexclude=['optional']\ndefault-members=['app']\nresolver='3'\n")
	write(t, repo.Root, "app/Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\nedition='2021'\n[features]\ndefault=[]\nextra=['dep:optional']\n[dependencies]\nrenamed={package='utility',path='../utility'}\noptional={path='../optional',optional=true}\n[build-dependencies]\nrenamed={package='utility',path='../utility'}\n[target.'cfg(windows)'.dependencies]\nwindows-support={path='../windows-support'}\n")
	write(t, repo.Root, "app/src/lib.rs", "compile_error!(\"metadata must not compile project code\");\n")
	write(t, repo.Root, "app/build.rs", "fn main() { std::fs::write(\"executed\", \"bad\").unwrap(); panic!(\"must not run\"); }\n")
	for _, name := range []string{"utility", "windows-support", "optional"} {
		write(t, repo.Root, name+"/Cargo.toml", "[package]\nname='"+name+"'\nversion='1.0.0'\nedition='2021'\n")
		write(t, repo.Root, name+"/src/lib.rs", "pub fn value() -> u32 { 42 }\n")
	}
	lock := "version=4\n[[package]]\nname='app'\nversion='1.0.0'\ndependencies=['optional','utility','windows-support']\n"
	for _, name := range []string{"optional", "utility", "windows-support"} {
		lock += "[[package]]\nname='" + name + "'\nversion='1.0.0'\n"
	}
	write(t, repo.Root, "Cargo.lock", lock)
	return repo, lock
}

func assertResolutionWorkspace(t *testing.T, resolution Resolution) {
	t.Helper()
	if resolution.Graph.Root != nil || len(resolution.Packages) != 3 || len(resolution.Lock.Packages) != 4 {
		t.Fatalf("inactive optional dependency confused lock and active graph coverage: %+v", resolution)
	}
	packageIndex := slices.IndexFunc(resolution.Packages, func(pkg Package) bool { return pkg.Name == "app" })
	nodeIndex := slices.IndexFunc(resolution.Graph.Nodes, func(node ResolvedNode) bool { return node.ID == resolution.Packages[packageIndex].ID })
	node := resolution.Graph.Nodes[nodeIndex]
	if len(node.Deps) != 2 || !slices.Equal(node.Features, []string{"default"}) {
		t.Fatalf("resolved application = %+v", node)
	}
	for _, edge := range node.Deps {
		switch edge.Name {
		case "renamed":
			if len(edge.Kinds) != 2 || !slices.Contains(edge.Kinds, ResolvedDependencyKind{Kind: "build"}) {
				t.Fatalf("renamed build dependency = %+v", edge)
			}
		case "windows_support":
			if !slices.Equal(edge.Kinds, []ResolvedDependencyKind{{Target: "cfg(windows)"}}) {
				t.Fatalf("platform dependency = %+v", edge)
			}
		default:
			t.Fatalf("unexpected edge = %+v", edge)
		}
	}
}

func assertResolutionInputs(t *testing.T, repo repository.Repository, originalLock string) {
	t.Helper()
	lock, err := os.ReadFile(filepath.Join(repo.Root, "Cargo.lock"))
	if err != nil || string(lock) != originalLock {
		t.Fatalf("resolution changed the reviewed lock: %s, %v", lock, err)
	}
	for _, path := range []string{"app/executed", "executed", ".tools/rust-target"} {
		if _, err := os.Stat(filepath.Join(repo.Root, filepath.FromSlash(path))); !os.IsNotExist(err) {
			t.Errorf("resolution compiled or executed project code at %s: %v", path, err)
		}
	}
}

type resolutionRunner struct {
	commands []policy.Command
}

func (boundary *resolutionRunner) runPlain(context.Context, string, policy.Command) error {
	panic("Cargo resolution must use bounded structured output")
}

func (boundary *resolutionRunner) runStructured(ctx context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	boundary.commands = append(boundary.commands, command)
	return runner.RunCaptured(ctx, runner.OSRunner{}, root, command, runner.CaptureStructured)
}

func (double *resolutionRunner) Execute(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	if invocation.Capture == runner.CaptureStructured {
		return double.runStructured(ctx, invocation.Root, invocation.Command)
	}
	return runner.Result{}, runner.Output{}, double.runPlain(ctx, invocation.Root, invocation.Command)
}
