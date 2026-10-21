package quality

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"io"
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
	for _, before := range []string{"policy-rustfmt-", "policy-rust-source-facts"} {
		t.Run(before, func(t *testing.T) {
			for _, source := range []string{"pub fn value()->u32{42}\n", "pub fn value() -> u32 {\n    42\n}\n"} {
				repo := rustFormatRepository(t)
				writeQualityFile(t, repo.Root, "src/lib.rs", source)
				concurrent := "#[rustfmt::skip]\npub fn value() -> u32 { 99 }\n"
				boundary := rustEditingRunner{OSRunner: runner.OSRunner{}, before: before, edit: func() { writeQualityFile(t, repo.Root, "src/lib.rs", concurrent) }}
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
		})
	}
}

func TestRustFormatRejectsParsedExclusionsWithoutRewritingThem(t *testing.T) {
	t.Parallel()
	for _, attribute := range []string{
		"#[rustfmt::skip]", "#[cfg_attr(any(), rustfmt::skip)]", "#[cfg_attr(all(), cfg_attr(any(), rustfmt::skip))]",
		"#[rustfmt::skip::macros(println)]", "#![rustfmt::skip::attributes(custom)]", "#[r#rustfmt::r#skip]",
		"#[cfg_attr(any(), rustfmt::skip::macros(println))]",
	} {
		t.Run(attribute, func(t *testing.T) {
			repo := rustFormatRepository(t)
			dirty := attribute + "\npub fn value()->u32{42}\n"
			writeQualityFile(t, repo.Root, "src/lib.rs", dirty)
			findings := Format(t.Context(), repo, repository.Selection{Files: []string{"src/lib.rs"}}, runner.OSRunner{})
			if len(findings) != 1 || findings[0].Check != "policy.rustAttribute" || findings[0].Path != "src/lib.rs" || findings[0].Line != 1 {
				t.Fatalf("format exclusion did not fail at its original attribute: %+v", findings)
			}
			if got := rustFormatFileText(t, repo, "src/lib.rs"); got != dirty {
				t.Fatalf("format exclusion was rewritten or removed: %q", got)
			}
		})
	}
}

func TestRustFormatDoesNotTreatRawStringContentsAsAnExclusion(t *testing.T) {
	t.Parallel()
	repo := rustFormatRepository(t)
	writeQualityFile(t, repo.Root, "src/lib.rs", "pub fn text()->&'static str{r#\"#[rustfmt::skip]\"#}\n")
	findings := Format(t.Context(), repo, repository.Selection{Files: []string{"src/lib.rs"}}, runner.OSRunner{})
	wanted := "pub fn text() -> &'static str {\n    r#\"#[rustfmt::skip]\"#\n}\n"
	if len(findings) != 0 || rustFormatFileText(t, repo, "src/lib.rs") != wanted {
		t.Fatalf("ordinary string data blocked formatting: %+v", findings)
	}
}

func TestRustFormatPreservesExcludedSourceAndFormatsValidSelectedSiblings(t *testing.T) {
	t.Parallel()
	repo := rustFormatRepository(t)
	excluded := "mod child;\n#[rustfmt::skip]\npub fn value()->u32{42}\n"
	writeQualityFile(t, repo.Root, "src/lib.rs", excluded)
	writeQualityFile(t, repo.Root, "src/child.rs", "pub fn value()->u32{99}\n")
	findings := Format(t.Context(), repo, repository.Selection{Files: []string{"src/lib.rs", "src/child.rs"}}, runner.OSRunner{})
	if len(findings) != 1 || findings[0].Path != "src/lib.rs" || findings[0].Check != "policy.rustAttribute" {
		t.Fatalf("format exclusion lost selected-file scope: %+v", findings)
	}
	if rustFormatFileText(t, repo, "src/lib.rs") != excluded || rustFormatFileText(t, repo, "src/child.rs") != "pub fn value() -> u32 {\n    99\n}\n" {
		t.Fatal("rejected formatting changed excluded source or skipped a valid selected sibling")
	}
}

func TestRustFormatRequiresCompleteSyntaxEvidenceBeforeWriting(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{errors.New("syntax tool unavailable"), nil} {
		repo := rustFormatRepository(t)
		dirty := "pub fn value()->u32{42}\n"
		writeQualityFile(t, repo.Root, "src/lib.rs", dirty)
		boundary := &rustSyntaxFailureRunner{failure: failure}
		findings := Format(t.Context(), repo, repository.Selection{Files: []string{"src/lib.rs"}}, boundary)
		if len(findings) != 1 || findings[0].Check != "policy.rustSource" || boundary.formatted != 0 {
			t.Fatalf("missing or invalid syntax evidence authorized rustfmt: %+v, %d formatter calls", findings, boundary.formatted)
		}
		if rustFormatFileText(t, repo, "src/lib.rs") != dirty {
			t.Fatal("missing syntax evidence authorized a source write")
		}
	}
}

type rustSyntaxFailureRunner struct {
	runner.OSRunner
	failure   error
	formatted int
}

func (boundary *rustSyntaxFailureRunner) runStructured(ctx context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	if strings.HasPrefix(command.Name, "policy-rustfmt-") {
		boundary.formatted++
	}
	return runner.RunCaptured(ctx, boundary.OSRunner, root, command, runner.CaptureStructured)
}

func (boundary *rustSyntaxFailureRunner) runStreamed(ctx context.Context, root string, command policy.Command, stdout, stderr io.Writer) (runner.Result, error) {
	if command.Name == "policy-rust-source-facts" {
		_, err := io.WriteString(stdout, "{}\n")
		return runner.Result{}, errors.Join(err, boundary.failure)
	}
	return runner.RunStreamed(ctx, boundary.OSRunner, root, command, stdout, stderr)
}

type rustEditingRunner struct {
	runner.OSRunner
	before string
	edit   func()
}

func (boundary rustEditingRunner) runStructured(ctx context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	if strings.HasPrefix(command.Name, boundary.before) {
		boundary.edit()
	}
	return runner.RunCaptured(ctx, boundary.OSRunner, root, command, runner.CaptureStructured)
}

func (boundary rustEditingRunner) runStreamed(ctx context.Context, root string, command policy.Command, stdout, stderr io.Writer) (runner.Result, error) {
	if strings.HasPrefix(command.Name, boundary.before) {
		boundary.edit()
	}
	return runner.RunStreamed(ctx, boundary.OSRunner, root, command, stdout, stderr)
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

func (double *rustSyntaxFailureRunner) Execute(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	if invocation.Stdout != nil || invocation.Stderr != nil {
		var stdout, stderr bytes.Buffer
		result, err := double.runStreamed(ctx, invocation.Root, invocation.Command, io.MultiWriter(&stdout, cmp.Or[io.Writer](invocation.Stdout, io.Discard)), io.MultiWriter(&stderr, cmp.Or[io.Writer](invocation.Stderr, io.Discard)))
		return result, runner.Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
	}
	if invocation.Capture == runner.CaptureStructured {
		return double.runStructured(ctx, invocation.Root, invocation.Command)
	}
	return double.OSRunner.Execute(ctx, invocation)
}

func (double rustEditingRunner) Execute(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	if invocation.Stdout != nil || invocation.Stderr != nil {
		var stdout, stderr bytes.Buffer
		result, err := double.runStreamed(ctx, invocation.Root, invocation.Command, io.MultiWriter(&stdout, cmp.Or[io.Writer](invocation.Stdout, io.Discard)), io.MultiWriter(&stderr, cmp.Or[io.Writer](invocation.Stderr, io.Discard)))
		return result, runner.Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
	}
	if invocation.Capture == runner.CaptureStructured {
		return double.runStructured(ctx, invocation.Root, invocation.Command)
	}
	return double.OSRunner.Execute(ctx, invocation)
}
