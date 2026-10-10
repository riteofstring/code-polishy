package javascript

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func installedLintBundle(t *testing.T) Bundle {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return Bundle{PolicyRoot: root}
}

func TestLintPagesPreserveSingleFileDiagnosticsAndSelectionEquivalence(t *testing.T) {
	root := t.TempDir()
	var source strings.Builder
	for index := range 5101 {
		fmt.Fprintf(&source, "// prose %d\nexport function value%d() { return 1; return 2; }\n", index, index)
	}
	writeScript(t, filepath.Join(root, "large.js"), source.String())
	writeScript(t, filepath.Join(root, "small.js"), "export function small() { return 1; return 2; }\n")
	bundle := installedLintBundle(t)
	limits := LintLimits{Complexity: 9, Depth: 4, Parameters: 5}
	activation := LintActivation{Complexity: true, Comments: true}
	combined, err := bundle.Lint(t.Context(), root, []string{"large.js", "small.js"}, limits, activation)
	if err != nil {
		t.Fatal(err)
	}
	if len(combined.Findings) != 5102 || len(combined.Comments) != 5101 || len(combined.Unsupported) != 0 {
		t.Fatalf("findings=%d comments=%d unsupported=%v", len(combined.Findings), len(combined.Comments), combined.Unsupported)
	}
	for index := range 5101 {
		finding, comment := combined.Findings[index], combined.Comments[index]
		if finding.Path != "large.js" || finding.Rule != "no-unreachable" || finding.Line != index*2+2 || comment.Raw != fmt.Sprintf("// prose %d", index) {
			t.Fatalf("lost or reordered result %d: %+v %+v", index, finding, comment)
		}
	}
	var separate LintResult
	for _, path := range []string{"large.js", "small.js"} {
		result, err := bundle.Lint(t.Context(), root, []string{path}, limits, activation)
		if err != nil {
			t.Fatal(err)
		}
		separate.Findings = append(separate.Findings, result.Findings...)
		separate.Comments = append(separate.Comments, result.Comments...)
		separate.Unsupported = append(separate.Unsupported, result.Unsupported...)
	}
	if !reflect.DeepEqual(combined, separate) {
		t.Fatal("splitting the file selection changed applicable lint results")
	}
}

func TestLintPagesPreserveCommentsAcrossTheByteLimit(t *testing.T) {
	root := t.TempDir()
	path := strings.Repeat(strings.Repeat("a", 160)+"/", 4) + "comments.js"
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	comment := "// " + strings.Repeat("界", 190)
	if err := os.WriteFile(absolute, []byte(strings.Repeat(comment+"\n", 3400)), 0o600); err != nil {
		t.Fatal(err)
	}
	bundle := installedLintBundle(t)
	limits := LintLimits{9, 4, 5}
	activation := LintActivation{Comments: true}
	payload := request{Operation: OperationLint, Root: root, Paths: []string{path}, Limits: &limits, Activation: &activation, Cursor: &lintCursor{}}
	first, err := bundle.lintPage(t.Context(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if first.next == nil || len(first.Comments) == 0 || len(first.Comments) >= 3400 {
		t.Fatal("serialized comment bytes did not produce a bounded continuation page")
	}
	result, err := bundle.Lint(t.Context(), root, []string{path}, limits, activation)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Comments) != 3400 || len(result.Findings) != 0 || len(result.Unsupported) != 0 {
		t.Fatalf("comments=%d findings=%d unsupported=%v", len(result.Comments), len(result.Findings), result.Unsupported)
	}
	for index, fact := range result.Comments {
		if fact.Line != index+1 || fact.Raw != comment || !fact.Complete {
			t.Fatalf("comment %d was lost or truncated: %+v", index, fact)
		}
	}
}

func TestLintRejectsPartialResultsWhenALaterPageFails(t *testing.T) {
	first := `{"protocolVersion":3,"operation":"lint","result":{"next":{"pathIndex":0,"resultIndex":1},"findings":[{"path":"a.js","line":1,"column":1,"rule":"no-unreachable","message":"unreachable"}],"comments":[],"unsupported":[]}}`
	last := `{"protocolVersion":3,"error":"analysis interrupted"}`
	script := "#!/bin/sh\nrequest=$(/bin/cat)\ncase \"$request\" in\n" +
		"*'\"resultIndex\":0'*) printf '%s\\n' '" + first + "';;\n" +
		"*) printf '%s\\n' '" + last + "'; exit 1;;\nesac\n"
	result, err := fakeBundle(t, script).Lint(t.Context(), "/target", []string{"a.js"}, LintLimits{9, 4, 5}, LintActivation{})
	if err == nil || !strings.Contains(err.Error(), "analysis interrupted") || len(result.Findings) != 0 {
		t.Fatalf("partial lint result accepted: %+v, %v", result, err)
	}
}

func TestLintRejectsInvalidContinuationAndMissingCompletion(t *testing.T) {
	for _, next := range []string{
		`"next":{"pathIndex":0,"resultIndex":0},`,
		`"next":{"pathIndex":1,"resultIndex":0},`,
		`"next":{"pathIndex":0,"resultIndex":-1},`,
		`"next":{"pathIndex":0,"resultIndex":1.5},`,
		``,
	} {
		t.Run(next, func(t *testing.T) {
			response := `{"protocolVersion":3,"operation":"lint","result":{` + next + `"findings":[],"comments":[],"unsupported":[]}}`
			_, err := fakeBundle(t, respond(response)).Lint(context.Background(), "/target", []string{"a.js"}, LintLimits{9, 4, 5}, LintActivation{})
			if err == nil {
				t.Fatal("accepted incomplete or nonadvancing lint results")
			}
		})
	}
}
