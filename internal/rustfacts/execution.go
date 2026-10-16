package rustfacts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func CargoHome(repo repository.Repository) string {
	return filepath.Join(repo.Root, ".tools", "cargo-home")
}

func TargetDirectory(repo repository.Repository) string {
	return filepath.Join(repo.Root, ".tools", "rust-target")
}

func Command(repo repository.Repository, name, tool string, arguments ...string) policy.Command {
	return policy.Command{
		Name: name, Argv: append([]string{repo.RustTool(tool)}, arguments...), Cwd: ".",
		Managed: true, SealedEnvironment: true, TimeoutSeconds: 300,
		Environment: []string{"SYSTEMROOT", "WINDIR", "ProgramFiles", "ProgramFiles(x86)"},
		EnvironmentOverrides: []string{
			"RUSTC=" + repo.RustTool("rustc"), "RUSTDOC=" + repo.RustTool("rustdoc"),
			"CARGO_HOME=" + CargoHome(repo), "CARGO_TARGET_DIR=" + TargetDirectory(repo),
			"CARGO_NET_OFFLINE=true", "CARGO_TERM_COLOR=never", "CARGO_INCREMENTAL=0",
		},
	}
}

func ToolProblem(repo repository.Repository) error {
	if err := release.VerifyRustTools(repo.PolicyRoot); err != nil {
		return err
	}
	pin := repo.ToolPin("rust")
	if pin == "" {
		return errors.New("pinned Rust version is unavailable")
	}
	for _, name := range []string{"rustc", "rustdoc", "cargo", "rustfmt", "cargo-fmt", "cargo-clippy", "clippy-driver"} {
		info, err := os.Stat(repo.RustTool(name))
		if err != nil || !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			return fmt.Errorf("pinned Rust tool %s is unavailable; install the policy-owned Rust toolchain", name)
		}
	}
	marker := filepath.Join(filepath.Dir(filepath.Dir(repo.RustTool("rustc"))), ".code-polishy-rust-release")
	data, err := os.ReadFile(marker)
	if err != nil || strings.TrimSpace(string(data)) != pin {
		return errors.New("rust toolchain installation does not match tools/rust-version.txt")
	}
	return nil
}

func executionDirectories(repo repository.Repository) error {
	for _, relative := range []string{".tools", ".tools/cargo-home", ".tools/rust-target"} {
		info, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(relative)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("rust execution directory %q must be a contained regular directory", relative)
		}
	}
	for _, name := range []string{"config", "config.toml"} {
		path := filepath.Join(CargoHome(repo), name)
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("governed Cargo home must not contain %s", path)
		}
	}
	return nil
}
