package rustfacts

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRustCompilationThroughRepositoryAlias(t *testing.T) {
	t.Parallel()
	repo := compilationRepository(t, "pub fn value() -> u32 { 42 }\n")
	repo.Root = repositoryAlias(t, repo.Root)
	plan, discovery := compilationDiscovery(t, repo, "src/lib.rs")
	if findings := Compile(t.Context(), repo, plan, discovery, runner.OSRunner{}); len(findings) != 0 {
		t.Fatalf("compilation through root alias = %+v", findings)
	}
	write(t, repo.Root, "src/lib.rs", "pub fn value() -> u32 { \"wrong\" }\n")
	findings := Compile(t.Context(), repo, plan, discovery, runner.OSRunner{})
	if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Check == "quality.rustCompiler" && finding.Subject == "E0308" && finding.Path == "src/lib.rs" && finding.Line == 1
	}) {
		t.Fatalf("compiler diagnostic through root alias = %+v", findings)
	}
}

func TestCargoDiscoveryThroughRepositoryAlias(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	write(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\nedition='2024'\n")
	write(t, repo.Root, "src/lib.rs", "pub fn value() -> u32 { 42 }\n")
	repo.Root = repositoryAlias(t, repo.Root)
	plan := PlanDiscovery(repo, []string{"src/lib.rs"}, []string{"Cargo.toml", "src/lib.rs"})
	result := Discover(t.Context(), repo, plan, runner.OSRunner{})
	if len(result.Findings) != 0 || len(result.Workspaces) != 1 {
		t.Fatalf("discovery through root alias = %+v", result)
	}
	pkg := result.Workspaces[0].Packages[0]
	if pkg.Manifest != "Cargo.toml" || pkg.Targets[0].Path != "src/lib.rs" {
		t.Fatalf("discovered source paths = %+v", pkg)
	}
}

func TestCargoInputContainmentThroughRepositoryAlias(t *testing.T) {
	t.Parallel()
	repo, _ := metadataFixture(t)
	physical, err := filepath.EvalSymlinks(repo.Root)
	if err != nil {
		t.Fatal(err)
	}
	repo.Root = repositoryAlias(t, physical)
	for _, root := range []string{repo.Root, physical} {
		path, err := containedPath(repo, filepath.Join(root, "src", "lib.rs"))
		if err != nil || path != "src/lib.rs" {
			t.Fatalf("contained input under %s = %q, %v", root, path, err)
		}
		path, err = containedPath(repo, filepath.Join(root, "examples", "absent.rs"))
		if path != "examples/absent.rs" || !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("absent input under %s = %q, %v", root, path, err)
		}
	}
	outside := t.TempDir()
	write(t, outside, "lib.rs", "pub fn outside() {}\n")
	if _, err := containedPath(repo, filepath.Join(outside, "lib.rs")); err == nil {
		t.Fatal("outside input was accepted")
	}
	for name, target := range map[string]string{"internal": filepath.Join(physical, "src"), "external": outside} {
		if err := os.Symlink(target, filepath.Join(physical, name)); err != nil {
			t.Fatal(err)
		}
		for _, root := range []string{repo.Root, physical} {
			if _, err := containedPath(repo, filepath.Join(root, name, "lib.rs")); err == nil || !strings.Contains(err.Error(), "symbolic link") {
				t.Fatalf("linked input under %s: %v", root, err)
			}
		}
	}
}

func repositoryAlias(t *testing.T, root string) string {
	t.Helper()
	alias := filepath.Join(t.TempDir(), "checkout")
	if err := os.Symlink(filepath.Dir(root), alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	return filepath.Join(alias, filepath.Base(root))
}
