package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fakeShellCheck struct {
	findings []responseFinding
	err      error
}

func (checker fakeShellCheck) check(context.Context, map[string][]byte, []shellSource) ([]responseFinding, error) {
	return checker.findings, checker.err
}

func TestParseSourceClassifiesDirectivesCommentsAndPortabilityFacts(t *testing.T) {
	t.Parallel()
	source := []byte("#!/usr/bin/env bash\n#SBATCH --time=00:05:00\n# shellcheck source=lib/shared.sh\nsource \"$dynamic\"\n# prose\nresolve \"$PROJECT_ROOT\" \"../catalog\"\n")
	inventory := []inventoryEntry{{Path: "scripts/main.sh", Language: "shell", Source: true}, {Path: "lib/shared.sh", Language: "shell", Source: true}}
	parsed, err := parseSource("scripts/main.sh", source, dialectBash, inventory)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Comments) != 4 {
		t.Fatalf("comments = %+v", parsed.Comments)
	}
	for index := 0; index < 3; index++ {
		if !parsed.Comments[index].MachineDirective {
			t.Fatalf("directive %d = %+v", index, parsed.Comments[index])
		}
	}
	if parsed.Comments[3].MachineDirective || parsed.Comments[3].Line != 5 || parsed.Comments[3].Column != 1 {
		t.Fatalf("prose = %+v", parsed.Comments[3])
	}
	if !slices.ContainsFunc(parsed.Literals, func(fact literalFact) bool {
		return fact.Value == "../catalog" && fact.Line == 6 && fact.RootContext
	}) {
		t.Fatalf("literals = %+v", parsed.Literals)
	}
}

func TestParseSourceUsesPOSIXAndBashDialects(t *testing.T) {
	t.Parallel()
	source := []byte("#!/usr/bin/env bash\nvalues=(one two)\n")
	if _, err := parseSource("tool.bash", source, dialectBash, nil); err != nil {
		t.Fatalf("bash source failed: %v", err)
	}
	if _, err := parseSource("tool.sh", source, dialectPOSIX, nil); err == nil {
		t.Fatal("POSIX accepted a Bash conditional")
	}
	if dialectFor("tool.sh", []byte("#!/bin/sh\n")) != dialectPOSIX || dialectFor("tool", source) != dialectBash {
		t.Fatal("shebang dialect classification changed")
	}
}

func TestParseSourceFindsCommentsInsideHeredocSubstitutions(t *testing.T) {
	t.Parallel()
	source := []byte("cat <<EOF\n$(printf value # prose\n)\nEOF\n")
	parsed, err := parseSource("scripts/main.sh", source, dialectPOSIX, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Comments) != 1 || parsed.Comments[0].Line != 2 || parsed.Comments[0].Column != 16 || parsed.Comments[0].MachineDirective {
		t.Fatalf("comments = %+v", parsed.Comments)
	}
}

func TestDiscoveryAccountsForSelectedShellSource(t *testing.T) {
	t.Parallel()
	result := discover(request{
		Provider: "pack.shell.analyze.lint", Files: []string{"scripts/main.sh"},
		Inventory: []inventoryEntry{
			{Path: "scripts/main.sh", Language: "shell", Owner: "pack.shell.analyze.lint", Source: true},
			{Path: "README.md", Owner: "", Source: false},
		},
	})
	if result.Status != "pass" || result.Discovery == nil || len(result.Discovery.Scopes) != 1 {
		t.Fatalf("result = %+v", result)
	}
	scope := result.Discovery.Scopes[0]
	if !slices.Equal(scope.Members, []string{"scripts/main.sh"}) || !slices.Equal(scope.Selected, scope.Members) || !slices.Equal(scope.Context, scope.Members) {
		t.Fatalf("scope = %+v", scope)
	}
}

func TestLintAnalysisReturnsFactsShellCheckAndVerifiedInputs(t *testing.T) {
	root := t.TempDir()
	source := []byte("#!/usr/bin/env bash\nvalue=$1\necho $value\n")
	writeShellFile(t, root, "scripts/main.sh", source)
	digest := sha256.Sum256(source)
	t.Setenv("CODE_POLISHY_TOOL_SHELLCHECK", filepath.Join(root, "shellcheck"))
	request := request{
		ProtocolVersion: protocolVersion, Operation: "check", Capability: "lint", ProjectRoot: root,
		Files: []string{"scripts/main.sh"}, DiagnosticFiles: []string{"scripts/main.sh"},
		Scopes:    []analysisScope{{Handle: "scope-1", Language: "shell", Root: ".", Members: []string{"scripts/main.sh"}, Context: []string{"scripts/main.sh"}, Data: json.RawMessage(`{}`)}},
		Context:   []inputFile{{Path: "scripts/main.sh", SHA256: hex.EncodeToString(digest[:])}},
		Inventory: []inventoryEntry{{Path: "scripts/main.sh", Language: "shell", Source: true}},
		Tools:     []toolIdentity{{ID: "shellcheck", Name: "shellcheck", Version: "0.11.0", SHA256: strings.Repeat("a", 64)}},
	}
	want := responseFinding{Capability: "lint", Path: "scripts/main.sh", Line: 3, Column: 6, Subject: "SC2086", Message: "quote it", Rule: "shellcheck.sc2086"}
	result := (adapter{checker: fakeShellCheck{findings: []responseFinding{want}}}).run(context.Background(), request)
	if result.Status != "findings" || !slices.Equal(result.ScopeHandles, []string{"scope-1"}) || len(result.Inputs) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.Facts == nil || result.Facts.Comments == nil || result.Facts.Literals == nil || len(*result.Facts.Comments) != 1 {
		t.Fatalf("facts = %+v", result.Facts)
	}
	if !slices.Contains(result.Findings, want) {
		t.Fatalf("findings = %+v", result.Findings)
	}
}

func TestTypecheckReportsDialectSyntaxAtItsSource(t *testing.T) {
	root := t.TempDir()
	source := []byte("#!/bin/sh\nvalues=(one two)\n")
	writeShellFile(t, root, "scripts/main.sh", source)
	digest := sha256.Sum256(source)
	request := request{
		ProtocolVersion: protocolVersion, Operation: "check", Capability: "typecheck", ProjectRoot: root,
		Files: []string{"scripts/main.sh"}, DiagnosticFiles: []string{"scripts/main.sh"},
		Scopes:  []analysisScope{{Handle: "scope-1", Members: []string{"scripts/main.sh"}, Context: []string{"scripts/main.sh"}}},
		Context: []inputFile{{Path: "scripts/main.sh", SHA256: hex.EncodeToString(digest[:])}},
	}
	result := (adapter{checker: fakeShellCheck{}}).run(context.Background(), request)
	if result.Status != "findings" || len(result.Findings) != 1 || result.Findings[0].Rule != "syntax" || result.Findings[0].Line != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestLintReportsToolBindingAndExecutionFailures(t *testing.T) {
	for _, test := range []struct {
		name  string
		tools []toolIdentity
	}{
		{name: "missing identity", tools: []toolIdentity{}},
		{name: "wrong version", tools: []toolIdentity{{ID: "shellcheck", Name: "shellcheck", Version: "0.10.0", SHA256: strings.Repeat("a", 64)}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := (adapter{checker: fakeShellCheck{}}).run(context.Background(), request{Operation: "check", Capability: "lint", Tools: test.tools})
			if result.Status != "operational-failure" || !strings.Contains(result.Failure, "does not bind shellcheck 0.11.0") {
				t.Fatalf("result = %+v", result)
			}
		})
	}

	t.Run("missing executable", func(t *testing.T) {
		t.Setenv("CODE_POLISHY_TOOL_SHELLCHECK", "")
		result := (adapter{checker: fakeShellCheck{}}).run(context.Background(), request{
			Operation: "check", Capability: "lint",
			Tools: []toolIdentity{{ID: "shellcheck", Name: "shellcheck", Version: "0.11.0", SHA256: strings.Repeat("a", 64)}},
		})
		if result.Status != "operational-failure" || !strings.Contains(result.Failure, "CODE_POLISHY_TOOL_SHELLCHECK is unavailable") {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("execution failure", func(t *testing.T) {
		root := t.TempDir()
		source := []byte("#!/bin/sh\nprintf '%s\\n' \"$1\"\n")
		writeShellFile(t, root, "scripts/main.sh", source)
		digest := sha256.Sum256(source)
		t.Setenv("CODE_POLISHY_TOOL_SHELLCHECK", filepath.Join(root, "shellcheck"))
		result := (adapter{checker: fakeShellCheck{err: errors.New("shellcheck stopped")}}).run(context.Background(), request{
			Operation: "check", Capability: "lint", ProjectRoot: root, Files: []string{"scripts/main.sh"},
			Scopes:  []analysisScope{{Handle: "scope-1", Members: []string{"scripts/main.sh"}, Context: []string{"scripts/main.sh"}}},
			Context: []inputFile{{Path: "scripts/main.sh", SHA256: hex.EncodeToString(digest[:])}},
			Tools:   []toolIdentity{{ID: "shellcheck", Name: "shellcheck", Version: "0.11.0", SHA256: strings.Repeat("a", 64)}},
		})
		if result.Status != "operational-failure" || result.Failure != "shellcheck stopped" || !slices.Equal(result.ScopeHandles, []string{"scope-1"}) {
			t.Fatalf("result = %+v", result)
		}
	})
}

func TestDecodeRequestRejectsUnknownFieldsAndTrailingDocuments(t *testing.T) {
	t.Parallel()
	if _, err := decodeRequest(strings.NewReader(`{"protocolVersion":4,"operation":"discover","unknown":true}`)); err == nil {
		t.Fatal("unknown request field passed")
	}
	if _, err := decodeRequest(strings.NewReader(`{"protocolVersion":4,"operation":"discover"}{}`)); err == nil {
		t.Fatal("trailing request document passed")
	}
}

func TestShellCheckPathRejectsEscapes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	inside := filepath.Join(root, "scripts", "main.sh")
	if got, err := shellCheckPath(root, inside); err != nil || got != "scripts/main.sh" {
		t.Fatalf("inside = %q, %v", got, err)
	}
	if _, err := shellCheckPath(root, filepath.Join(filepath.Dir(root), "outside.sh")); err == nil {
		t.Fatal("escaping ShellCheck path passed")
	}
}

func writeShellFile(t *testing.T, root, name string, data []byte) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
