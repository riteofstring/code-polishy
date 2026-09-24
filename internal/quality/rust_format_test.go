package quality

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRustFormatWritesOnlySelectedHandwrittenSource(t *testing.T) {
	t.Parallel()
	repo := rustFormatRepository(t)
	dirty := "pub fn value()->u32{42}\n"
	writeQualityFile(t, repo.Root, "src/lib.rs", "mod child;\n"+dirty)
	writeQualityFile(t, repo.Root, "src/child.rs", dirty)
	writeQualityFile(t, repo.Root, "src/schema.generated.rs", dirty)
	repo.Config.Scope.Generated = []string{"**/*.generated.rs"}
	selection := repository.Selection{Files: []string{"src/lib.rs", "src/schema.generated.rs"}}
	findings := Format(t.Context(), repo, selection, runner.OSRunner{})
	if len(findings) != 0 {
		t.Fatalf("format findings: %+v", findings)
	}
	want := "mod child;\npub fn value() -> u32 {\n    42\n}\n"
	if got := rustFormatFileText(t, repo, "src/lib.rs"); got != want {
		t.Fatalf("formatted source = %q, want %q", got, want)
	}
	for _, path := range []string{"src/child.rs", "src/schema.generated.rs"} {
		if got := rustFormatFileText(t, repo, path); got != dirty {
			t.Errorf("unselected or generated source %s changed: %q", path, got)
		}
	}
}

func TestRustFormatUsesCargoEditionAndProjectStyle(t *testing.T) {
	t.Parallel()
	repo := rustFormatRepository(t)
	writeQualityFile(t, repo.Root, "rustfmt.toml", "hard_tabs=true\nstyle_edition='2021'\n")
	writeQualityFile(t, repo.Root, "src/lib.rs", "pub async fn answer()->u32{42}\n")
	findings := rustFormatWrite(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{})
	if len(findings) != 0 {
		t.Fatalf("format findings: %+v", findings)
	}
	if got := rustFormatFileText(t, repo, "src/lib.rs"); got != "pub async fn answer() -> u32 {\n\t42\n}\n" {
		t.Fatalf("edition/style formatting = %q", got)
	}
}

func TestRustFormatCheckReportsWithoutWriting(t *testing.T) {
	t.Parallel()
	repo := rustFormatRepository(t)
	dirty := "pub fn value()->u32{42}\n"
	writeQualityFile(t, repo.Root, "src/lib.rs", dirty)
	findings := cargoDiscoveryFindings(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{})
	if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Check == "quality.rustfmt" && finding.Path == "src/lib.rs" && strings.Contains(finding.Message, "not rustfmt formatted")
	}) {
		t.Fatalf("missing formatting finding: %+v", findings)
	}
	if got := rustFormatFileText(t, repo, "src/lib.rs"); got != dirty {
		t.Fatalf("read-only check wrote %q", got)
	}
}

func TestRustFormatRejectsInvalidSourceAndWeakenedConfiguration(t *testing.T) {
	t.Parallel()
	for name, configuration := range map[string]string{
		"edition mismatch": "edition='2015'\n", "disabled": "disable_all_formatting=true\n",
		"ignored": "ignore=['src/lib.rs']\n", "unknown": "imaginary_option=true\n", "malformed": "bad=[\n",
	} {
		t.Run(name, func(t *testing.T) {
			repo := rustFormatRepository(t)
			dirty := "pub fn value()->u32{42}\n"
			writeQualityFile(t, repo.Root, "src/lib.rs", dirty)
			writeQualityFile(t, repo.Root, ".rustfmt.toml", configuration)
			if findings := rustFormatWrite(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{}); len(findings) == 0 {
				t.Fatal("invalid configuration was accepted")
			}
			if got := rustFormatFileText(t, repo, "src/lib.rs"); got != dirty {
				t.Fatalf("rejected formatting changed source: %q", got)
			}
		})
	}
	repo := rustFormatRepository(t)
	invalid := "pub fn unfinished(\n"
	writeQualityFile(t, repo.Root, "src/lib.rs", invalid)
	if findings := rustFormatWrite(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{}); len(findings) == 0 {
		t.Fatal("invalid Rust source was accepted")
	}
	if got := rustFormatFileText(t, repo, "src/lib.rs"); got != invalid {
		t.Fatalf("invalid source was overwritten: %q", got)
	}
}

func TestRustFormatPreservesConcurrentSourceEdit(t *testing.T) {
	t.Parallel()
	repo := rustFormatRepository(t)
	writeQualityFile(t, repo.Root, "src/lib.rs", "pub fn value()->u32{42}\n")
	concurrent := "pub fn value() -> u32 { 99 }\n"
	boundary := rustEditingRunner{OSRunner: runner.OSRunner{}, edit: func() { writeQualityFile(t, repo.Root, "src/lib.rs", concurrent) }}
	findings := rustFormatWrite(t.Context(), repo, []string{"src/lib.rs"}, boundary)
	if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return strings.Contains(finding.Message, "changed during formatting")
	}) {
		t.Fatalf("concurrent edit was not reported: %+v", findings)
	}
	if got := rustFormatFileText(t, repo, "src/lib.rs"); got != concurrent {
		t.Fatalf("concurrent source edit was overwritten: %q", got)
	}
}

type rustEditingRunner struct {
	runner.OSRunner
	edit func()
}

func (boundary rustEditingRunner) RunStructured(ctx context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	if strings.HasPrefix(command.Name, "policy-rustfmt-") {
		boundary.edit()
	}
	return boundary.OSRunner.RunStructured(ctx, root, command)
}

func rustFormatRepository(t *testing.T) repository.Repository {
	t.Helper()
	repo := cargoQualityRepository(t)
	writeQualityFile(t, repo.Root, "Cargo.toml", "[package]\nname='format-fixture'\nversion='1.0.0'\nedition='2024'\n")
	return repo
}

func rustFormatFileText(t *testing.T, repo repository.Repository, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo.Root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
