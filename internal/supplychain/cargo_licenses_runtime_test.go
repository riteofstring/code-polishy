package supplychain

import (
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/rustfacts"
)

func TestCargoLicensesReadPinnedCargoPublishedWorkspaceMetadata(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	repo.PolicyRoot = root
	if err := rustfacts.ToolProblem(repo); err != nil {
		t.Fatal(err)
	}
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[workspace]\nmembers=['crates/package']\nresolver='3'\n[workspace.package]\nlicense='MIT OR Apache-2.0'\n")
	writeSupplyFile(t, repo.Root, "crates/package/Cargo.toml", "[package]\nname='crate'\nversion='1.0.0'\nedition='2024'\nlicense.workspace=true\n")
	writeSupplyFile(t, repo.Root, "crates/package/src/lib.rs", "pub fn answer() -> i32 { 42 }\n")
	writeSupplyFile(t, repo.Root, "crates/package/build.rs", "compile_error!(\"package verification must not execute build scripts\");\n")
	writeSupplyFile(t, repo.Root, "Cargo.lock", "version=4\n[[package]]\nname='crate'\nversion='1.0.0'\n")
	command := rustfacts.Command(repo, "package-license-fixture", "cargo", "package", "--manifest-path", "crates/package/Cargo.toml", "--frozen", "--allow-dirty", "--no-verify")
	if _, output, err := runner.RunCaptured(t.Context(), runner.OSRunner{}, repo.Root, command, runner.CaptureStructured); err != nil {
		t.Fatalf("pinned Cargo packaging failed: %v, %s", err, output.Stderr)
	}
	data, err := repo.ReadContainedFile(".tools/rust-target/package/crate-1.0.0.crate", 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	pkg := writeCargoTestArchive(t, repo, "opaque-registry-id", "crate", "1.0.0", data)
	writeSupplyFile(t, repo.Root, "Cargo.lock", cargoLicenseTestLock(pkg))
	repo.Config.SupplyChain.AllowedLicenses = []string{"MIT"}
	if findings := cargoLicenseFindings(t.Context(), repo, []string{"Cargo.lock"}); len(findings) != 0 {
		t.Fatalf("published inherited license was not resolved by Cargo: %+v", findings)
	}
	repo.Config.SupplyChain.AllowedLicenses = []string{"BSD-3-Clause"}
	findings := cargoLicenseFindings(t.Context(), repo, []string{"Cargo.lock"})
	if len(findings) != 1 || findings[0].Check != "supplyChain.dependencyLicense" || findings[0].Subject != "crate@1.0.0" {
		t.Fatalf("published Cargo license did not reach shared policy: %+v", findings)
	}
}
