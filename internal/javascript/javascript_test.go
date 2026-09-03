package javascript

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/release"
)

const wellFormedResult = `{"bundleDigest":"a1b2c3","node":"24.18.0","pnpm":"11.13.0","tools":{"prettier":"3.9.5","eslint":"9.39.5"}}`

func wellFormedResponse(result string) string {
	return `{"protocolVersion":3,"operation":"provenance","result":` + result + `}`
}

func respond(payload string) string {
	return "#!/bin/sh\nprintf '%s\\n' '" + payload + "'\n"
}

func fakeBundle(t *testing.T, runnerScript string) Bundle {
	t.Helper()
	root := t.TempDir()
	host, err := release.Host()
	if err != nil {
		t.Fatalf("resolve the host tuple: %v", err)
	}
	installed := filepath.Join(root, ".tools", "javascript")
	writeScript(t, filepath.Join(installed, host, "node", "bin", "node"), "#!/bin/sh\nexec /bin/sh \"$1\"\n")
	writeScript(t, filepath.Join(installed, "bundle", "runner.mjs"), runnerScript)
	return Bundle{PolicyRoot: root}
}

func writeScript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func expectFailure(t *testing.T, bundle Bundle, fragment string) {
	t.Helper()
	if _, err := bundle.Provenance(context.Background()); err == nil {
		t.Fatal("expected the exchange to fail")
	} else if !strings.Contains(err.Error(), fragment) {
		t.Fatalf("expected a failure mentioning %q, got %q", fragment, err.Error())
	}
}

func TestProvenanceReportsWhatTheBundleAnswered(t *testing.T) {
	reported, err := fakeBundle(t, respond(wellFormedResponse(wellFormedResult))).Provenance(context.Background())
	if err != nil {
		t.Fatalf("exchange provenance: %v", err)
	}
	if reported.BundleDigest != "a1b2c3" || reported.Node != "24.18.0" || reported.Pnpm != "11.13.0" {
		t.Fatalf("unexpected provenance: %+v", reported)
	}
	if reported.Tools["prettier"] != "3.9.5" || reported.Tools["eslint"] != "9.39.5" {
		t.Fatalf("unexpected analyzer versions: %+v", reported.Tools)
	}
}

func TestTheRequestCarriesOnlyTheClosedProtocol(t *testing.T) {
	observed := filepath.Join(t.TempDir(), "request.json")
	bundle := fakeBundle(t, "#!/bin/sh\n/bin/cat >"+observed+"\nprintf '%s\\n' '"+wellFormedResponse(wellFormedResult)+"'\n")
	if _, err := bundle.Provenance(context.Background()); err != nil {
		t.Fatalf("exchange provenance: %v", err)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatalf("read the observed request: %v", err)
	}
	if string(request) != `{"protocolVersion":3,"operation":"provenance"}` {
		t.Fatalf("unexpected request %q", string(request))
	}
}

func TestTheRunnerLaunchesUnderAClosedEnvironment(t *testing.T) {
	t.Setenv("NODE_OPTIONS", "--require=/tmp/injected.js")
	t.Setenv("NPM_TOKEN", "secret")
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid")
	observed := filepath.Join(t.TempDir(), "environment.txt")
	script := "#!/bin/sh\n" +
		"{ echo \"PATH=[${PATH-unset}]\"" +
		"; echo \"NODE_OPTIONS=[${NODE_OPTIONS-unset}]\"" +
		"; echo \"NPM_TOKEN=[${NPM_TOKEN-unset}]\"" +
		"; echo \"HTTPS_PROXY=[${HTTPS_PROXY-unset}]\"" +
		"; echo \"HOME=[${HOME-unset}]\"" +
		"; echo \"PWD=[${PWD}]\"; } >" + observed + "\n" +
		"printf '%s\\n' '" + wellFormedResponse(wellFormedResult) + "'\n"
	bundle := fakeBundle(t, script)
	if _, err := bundle.Provenance(context.Background()); err != nil {
		t.Fatalf("exchange provenance: %v", err)
	}
	data, err := os.ReadFile(observed)
	if err != nil {
		t.Fatalf("read the observed environment: %v", err)
	}
	environment := string(data)
	for _, absent := range []string{"NODE_OPTIONS=[unset]", "NPM_TOKEN=[unset]", "HTTPS_PROXY=[unset]", "PATH=[]"} {
		if !strings.Contains(environment, absent) {
			t.Fatalf("expected %q in the child environment, got %q", absent, environment)
		}
	}
	if strings.Contains(environment, "HOME=["+os.Getenv("HOME")+"]") {
		t.Fatalf("the child inherited the invoking home directory: %q", environment)
	}
	if strings.Contains(environment, "PWD=["+bundle.PolicyRoot) {
		t.Fatalf("the child ran inside the policy checkout: %q", environment)
	}
}

func TestTheScratchDirectoryDoesNotSurviveTheExchange(t *testing.T) {
	observed := filepath.Join(t.TempDir(), "workdir.txt")
	bundle := fakeBundle(t, "#!/bin/sh\necho \"${PWD}\" >"+observed+"\nprintf '%s\\n' '"+wellFormedResponse(wellFormedResult)+"'\n")
	if _, err := bundle.Provenance(context.Background()); err != nil {
		t.Fatalf("exchange provenance: %v", err)
	}
	data, err := os.ReadFile(observed)
	if err != nil {
		t.Fatalf("read the observed working directory: %v", err)
	}
	working := strings.TrimSpace(string(data))
	if _, err := os.Stat(working); !os.IsNotExist(err) {
		t.Fatalf("expected the scratch working directory %s to be removed, got %v", working, err)
	}
}

func TestAStructuredRunnerErrorIsSurfaced(t *testing.T) {
	bundle := fakeBundle(t, "#!/bin/sh\nprintf '%s\\n' '{\"protocolVersion\":3,\"error\":\"the request declares unsupported operation\"}'\nexit 1\n")
	expectFailure(t, bundle, "the request declares unsupported operation")
}

func TestAFailureWithoutAStructuredErrorReportsItsDiagnostics(t *testing.T) {
	bundle := fakeBundle(t, "#!/bin/sh\necho 'the runtime aborted' >&2\nexit 3\n")
	expectFailure(t, bundle, "the runtime aborted")
}

func TestAnotherProtocolVersionIsRejected(t *testing.T) {
	bundle := fakeBundle(t, respond(`{"protocolVersion":1,"operation":"provenance","result":`+wellFormedResult+`}`))
	expectFailure(t, bundle, "protocol version 1")
}

func TestAnUnknownResponseFieldIsRejected(t *testing.T) {
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"provenance","warnings":[],"result":`+wellFormedResult+`}`))
	expectFailure(t, bundle, "unreadable provenance response")
}

func TestAnUnknownResultFieldIsRejected(t *testing.T) {
	bundle := fakeBundle(t, respond(wellFormedResponse(`{"bundleDigest":"a1","node":"24.18.0","pnpm":"11.13.0","tools":{},"extra":1}`)))
	expectFailure(t, bundle, "unreadable provenance result")
}

func TestTrailingContentIsRejected(t *testing.T) {
	bundle := fakeBundle(t, respond(wellFormedResponse(wellFormedResult)+" {}"))
	expectFailure(t, bundle, "unreadable provenance response")
}

func TestAnAnsweredOperationMustBeTheRequestedOne(t *testing.T) {
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"format","result":`+wellFormedResult+`}`))
	expectFailure(t, bundle, `answered the provenance request with "format"`)
}

func TestIncompleteProvenanceIsRejected(t *testing.T) {
	bundle := fakeBundle(t, respond(wellFormedResponse(`{"bundleDigest":"a1","node":"24.18.0","pnpm":"","tools":{"prettier":"3.9.5"}}`)))
	expectFailure(t, bundle, "incomplete provenance")
}

func TestAMissingResultIsRejected(t *testing.T) {
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"provenance"}`))
	expectFailure(t, bundle, "returned no provenance result")
}

func TestAnOversizedResponseIsRejected(t *testing.T) {
	bundle := fakeBundle(t, "#!/bin/sh\n/bin/dd if=/dev/zero bs=1048576 count=9 2>/dev/null\n")
	expectFailure(t, bundle, "exceeds the 8388608 byte limit")
}

func TestAnUninstalledBundleNamesItsInstaller(t *testing.T) {
	bundle := Bundle{PolicyRoot: t.TempDir()}
	expectFailure(t, bundle, "install-policy-tools.sh")
}

func TestARelativeCheckoutPathIsRejected(t *testing.T) {
	expectFailure(t, Bundle{PolicyRoot: "relative/checkout"}, "is not absolute")
}

func TestAnOperationThatOverrunsItsBoundIsCancelledCompletely(t *testing.T) {
	survivor := filepath.Join(t.TempDir(), "survivor.txt")
	bundle := fakeBundle(t, "#!/bin/sh\n/bin/sleep 1 && echo survived >"+survivor+"\n")
	started := time.Now()
	if _, err := bundle.exchange(context.Background(), request{Operation: OperationProvenance}, 100*time.Millisecond); err == nil {
		t.Fatal("expected the operation to time out")
	} else if !strings.Contains(err.Error(), "timed out after 100ms") {
		t.Fatalf("expected a timeout failure, got %q", err.Error())
	}
	if elapsed := time.Since(started); elapsed >= cleanupDelay {
		t.Fatalf("a descendant held the exchange open for %s", elapsed)
	}
	time.Sleep(2 * time.Second)
	if _, err := os.Stat(survivor); !os.IsNotExist(err) {
		t.Fatalf("a descendant of the cancelled operation kept running: %v", err)
	}
}

func TestACancelledContextStopsTheExchange(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fakeBundle(t, respond(wellFormedResponse(wellFormedResult))).Provenance(ctx); err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestAFormatRequestCarriesTheRootAndTheSelection(t *testing.T) {
	observed := filepath.Join(t.TempDir(), "request.json")
	response := `{"protocolVersion":3,"operation":"format","result":{"changed":[],"unsupported":[]}}`
	bundle := fakeBundle(t, "#!/bin/sh\n/bin/cat >"+observed+"\nprintf '%s\\n' '"+response+"'\n")
	if _, err := bundle.Format(context.Background(), "/target", []string{"src/a.ts"}); err != nil {
		t.Fatalf("exchange format: %v", err)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatalf("read the observed request: %v", err)
	}
	want := `{"protocolVersion":3,"operation":"format","root":"/target","paths":["src/a.ts"]}`
	if string(request) != want {
		t.Fatalf("unexpected request %q", string(request))
	}
}

func TestAFormatResultReportsChangedAndUnsupportedFiles(t *testing.T) {
	result := `{"changed":["src/a.ts"],"unsupported":[{"path":"src/b.zz","reason":"no parser"}]}`
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"format-write","result":`+result+`}`))
	reported, err := bundle.FormatWrite(context.Background(), "/target", []string{"src/a.ts", "src/b.zz"})
	if err != nil {
		t.Fatalf("exchange format-write: %v", err)
	}
	if len(reported.Changed) != 1 || reported.Changed[0] != "src/a.ts" {
		t.Fatalf("unexpected changed files: %+v", reported.Changed)
	}
	if len(reported.Unsupported) != 1 || reported.Unsupported[0].Path != "src/b.zz" || reported.Unsupported[0].Reason != "no parser" {
		t.Fatalf("unexpected unsupported files: %+v", reported.Unsupported)
	}
}

func TestAParseRequestCarriesTheRootAndStructuredDataSelection(t *testing.T) {
	observed := filepath.Join(t.TempDir(), "request.json")
	response := `{"protocolVersion":3,"operation":"parse","result":{"covered":["data/catalog.json","data/layout.yaml"],"unsupported":[]}}`
	bundle := fakeBundle(t, "#!/bin/sh\n/bin/cat >"+observed+"\nprintf '%s\\n' '"+response+"'\n")
	if _, err := bundle.Parse(context.Background(), "/target", []string{"data/catalog.json", "data/layout.yaml"}); err != nil {
		t.Fatalf("exchange parse: %v", err)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatalf("read the observed request: %v", err)
	}
	want := `{"protocolVersion":3,"operation":"parse","root":"/target","paths":["data/catalog.json","data/layout.yaml"]}`
	if string(request) != want {
		t.Fatalf("unexpected request %q", string(request))
	}
}

func TestAParseResultReportsInvalidStructuredData(t *testing.T) {
	result := `{"covered":[],"unsupported":[{"path":"data/broken.json","reason":"unexpected token"}]}`
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"parse","result":`+result+`}`))
	reported, err := bundle.Parse(context.Background(), "/target", []string{"data/broken.json"})
	if err != nil {
		t.Fatalf("exchange parse: %v", err)
	}
	if len(reported.Unsupported) != 1 || reported.Unsupported[0].Path != "data/broken.json" || reported.Unsupported[0].Reason != "unexpected token" {
		t.Fatalf("unsupported = %+v", reported.Unsupported)
	}
}

func TestAnUnknownParseResultFieldIsRejected(t *testing.T) {
	result := `{"covered":["data/catalog.json"],"unsupported":[],"durationMs":12}`
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"parse","result":`+result+`}`))
	if _, err := bundle.Parse(context.Background(), "/target", []string{"data/catalog.json"}); err == nil {
		t.Fatal("expected an unknown result field to be rejected")
	} else if !strings.Contains(err.Error(), "unreadable parse result") {
		t.Fatalf("unexpected failure %q", err.Error())
	}
}

func TestAParseResultMustAccountForEveryRequestedPath(t *testing.T) {
	t.Parallel()
	for name, result := range map[string]string{
		"missing fields":       `{}`,
		"null fields":          `{"covered":null,"unsupported":null}`,
		"omitted path":         `{"covered":[],"unsupported":[]}`,
		"unrequested path":     `{"covered":["data/other.json"],"unsupported":[]}`,
		"duplicate path":       `{"covered":["data/catalog.json"],"unsupported":[{"path":"data/catalog.json","reason":"bad"}]}`,
		"empty refusal reason": `{"covered":[],"unsupported":[{"path":"data/catalog.json","reason":""}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"parse","result":`+result+`}`))
			if _, err := bundle.Parse(context.Background(), "/target", []string{"data/catalog.json"}); err == nil {
				t.Fatal("invalid parse result was accepted")
			}
		})
	}
}

func TestAGitLabRequestCarriesDistinctRootConfigurations(t *testing.T) {
	observed := filepath.Join(t.TempDir(), "request.json")
	result := `{"controls":[".gitlab-ci.yml","ci/common.yml"],"images":[],"includes":[],"unsupported":[]}`
	script := "#!/bin/sh\n/bin/cat >" + observed + "\nprintf '%s\\n' '" +
		`{"protocolVersion":3,"operation":"gitlab","result":` + result + `}'` + "\n"
	bundle := fakeBundle(t, script)
	reported, err := bundle.GitLab(context.Background(), "/target", []string{".gitlab-ci.yml"}, []string{".gitlab-ci.yml", "ci/common.yml"})
	if err != nil {
		t.Fatalf("exchange gitlab: %v", err)
	}
	if !slices.Equal(reported.Controls, []string{".gitlab-ci.yml", "ci/common.yml"}) {
		t.Fatalf("controls = %v", reported.Controls)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatalf("read the observed request: %v", err)
	}
	want := `{"protocolVersion":3,"operation":"gitlab","root":"/target","paths":[".gitlab-ci.yml"],"governedPaths":[".gitlab-ci.yml","ci/common.yml"]}`
	if string(request) != want {
		t.Fatalf("unexpected request %q", string(request))
	}
	for _, paths := range [][]string{nil, {"ci/release.yml"}, {".gitlab-ci.yml", ".gitlab-ci.yml"}} {
		if _, err := bundle.GitLab(context.Background(), "/target", paths, []string{".gitlab-ci.yml"}); err == nil {
			t.Fatalf("GitLab(%v) accepted invalid roots", paths)
		}
	}
}

func TestAGitLabResultReportsBoundedFactsAndCoverage(t *testing.T) {
	commit := "0123456789abcdef0123456789abcdef01234567"
	result := `{"controls":[".gitlab-ci.yml","ci/common.yml"],` +
		`"images":[{"path":".gitlab-ci.yml","scope":"global:image","image":"registry.example/app@sha256:` + strings.Repeat("a", 64) + `"}],` +
		`"includes":[` +
		`{"path":".gitlab-ci.yml","kind":"local","local":"ci/common.yml","project":"","file":"","ref":"","remote":"","integrity":"","component":"","template":""},` +
		`{"path":"ci/common.yml","kind":"project","local":"","project":"group/templates","file":"release.yml","ref":"` + commit + `","remote":"","integrity":"","component":"","template":""},` +
		`{"path":".gitlab-ci.yml","kind":"template","local":"","project":"","file":"","ref":"","remote":"","integrity":"","component":"","template":"Jobs/SAST.gitlab-ci.yml"}],` +
		`"unsupported":[{"path":"ci/common.yml","reason":"a conditional include cannot be statically resolved"}]}`
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"gitlab","result":`+result+`}`))
	reported, err := bundle.GitLab(context.Background(), "/target", []string{".gitlab-ci.yml"}, []string{".gitlab-ci.yml", "ci/common.yml"})
	if err != nil {
		t.Fatalf("exchange gitlab: %v", err)
	}
	if len(reported.Images) != 1 || reported.Images[0].Scope != "global:image" || len(reported.Includes) != 3 || reported.Includes[2].Template != "Jobs/SAST.gitlab-ci.yml" || len(reported.Unsupported) != 1 {
		t.Fatalf("result = %+v", reported)
	}
}

func TestAGitLabResultRejectsUntrustedOrIncompleteFacts(t *testing.T) {
	for name, result := range map[string]string{
		"missing fields":      `{}`,
		"missing root":        `{"controls":["ci/common.yml"],"images":[],"includes":[],"unsupported":[]}`,
		"unguarded control":   `{"controls":[".gitlab-ci.yml",".git/config"],"images":[],"includes":[],"unsupported":[]}`,
		"outside image":       `{"controls":[".gitlab-ci.yml"],"images":[{"path":"ci/common.yml","scope":"global:image","image":"registry.example/app"}],"includes":[],"unsupported":[]}`,
		"incomplete include":  `{"controls":[".gitlab-ci.yml"],"images":[],"includes":[{"path":".gitlab-ci.yml","kind":"local"}],"unsupported":[]}`,
		"unowned unsupported": `{"controls":[".gitlab-ci.yml"],"images":[],"includes":[],"unsupported":[{"path":"ci/common.yml","reason":"missing"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"gitlab","result":`+result+`}`))
			if _, err := bundle.GitLab(context.Background(), "/target", []string{".gitlab-ci.yml"}, []string{".gitlab-ci.yml"}); err == nil || !strings.Contains(err.Error(), "unreadable gitlab result") {
				t.Fatalf("result was accepted: %v", err)
			}
		})
	}
}

func TestAFileOperationRefusesAnUncontainedSelection(t *testing.T) {
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"format","result":{"changed":[],"unsupported":[]}}`))
	for _, test := range []struct {
		name     string
		root     string
		paths    []string
		fragment string
	}{
		{"a relative root", "target", []string{"a.ts"}, "not a normal absolute path"},
		{"an unnormal root", "/target/../target", []string{"a.ts"}, "not a normal absolute path"},
		{"an empty selection", "/target", nil, "selects no files"},
		{"an escaping path", "/target", []string{"../secret.ts"}, "not a contained repository-relative path"},
		{"an absolute path", "/target", []string{"/etc/passwd"}, "not a contained repository-relative path"},
		{"an unnormal path", "/target", []string{"src/./a.ts"}, "not a contained repository-relative path"},
		{"an empty path", "/target", []string{""}, "not a contained repository-relative path"},
	} {
		if _, err := bundle.Format(context.Background(), test.root, test.paths); err == nil {
			t.Errorf("%s was accepted", test.name)
		} else if !strings.Contains(err.Error(), test.fragment) {
			t.Errorf("%s failed with %q, want %q", test.name, err.Error(), test.fragment)
		}
	}
}

func TestAnUnboundedSelectionIsRefused(t *testing.T) {
	paths := make([]string, maximumOperationPaths+1)
	for index := range paths {
		paths[index] = "src/file.ts"
	}
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"format","result":{"changed":[],"unsupported":[]}}`))
	if _, err := bundle.Format(context.Background(), "/target", paths); err == nil {
		t.Fatal("expected an unbounded selection to be refused")
	} else if !strings.Contains(err.Error(), "more than the 4096 limit") {
		t.Fatalf("unexpected failure %q", err.Error())
	}
}

func TestAnUnknownFormatResultFieldIsRejected(t *testing.T) {
	result := `{"changed":[],"unsupported":[],"durationMs":12}`
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"format","result":`+result+`}`))
	if _, err := bundle.Format(context.Background(), "/target", []string{"a.ts"}); err == nil {
		t.Fatal("expected an unknown result field to be rejected")
	} else if !strings.Contains(err.Error(), "unreadable format result") {
		t.Fatalf("unexpected failure %q", err.Error())
	}
}

func TestALintRequestCarriesTheBudgetsAndActivation(t *testing.T) {
	observed := filepath.Join(t.TempDir(), "request.json")
	response := `{"protocolVersion":3,"operation":"lint","result":{"findings":[],"comments":[],"unsupported":[]}}`
	bundle := fakeBundle(t, "#!/bin/sh\n/bin/cat >"+observed+"\nprintf '%s\\n' '"+response+"'\n")
	limits := LintLimits{Complexity: 9, Depth: 4, Parameters: 5}
	activation := LintActivation{ReactHooks: true}
	if _, err := bundle.Lint(context.Background(), "/target", []string{"src/a.tsx"}, limits, activation); err != nil {
		t.Fatalf("exchange lint: %v", err)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatalf("read the observed request: %v", err)
	}
	want := `{"protocolVersion":3,"operation":"lint","root":"/target","paths":["src/a.tsx"],` +
		`"limits":{"complexity":9,"depth":4,"parameters":5},` +
		`"activation":{"reactHooks":true,"jsxAccessibility":false}}`
	if string(request) != want {
		t.Fatalf("unexpected request %q", string(request))
	}
}

func TestALintRequestRefusesAnUnusableBudget(t *testing.T) {
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"lint","result":{"findings":[],"comments":[],"unsupported":[]}}`))
	for _, limits := range []LintLimits{
		{Complexity: 0, Depth: 4, Parameters: 5},
		{Complexity: 9, Depth: -1, Parameters: 5},
		{Complexity: 9, Depth: 4, Parameters: 0},
	} {
		if _, err := bundle.Lint(context.Background(), "/target", []string{"a.ts"}, limits, LintActivation{}); err == nil {
			t.Errorf("%+v was accepted", limits)
		} else if !strings.Contains(err.Error(), "not positive allowed maximums") {
			t.Errorf("%+v failed with %q", limits, err.Error())
		}
	}
}

func TestALintResultReportsViolationsAndUnsupportedFiles(t *testing.T) {
	result := `{"findings":[{"path":"a.ts","line":3,"column":7,"rule":"complexity","message":"too complex"}],` +
		`"comments":[{"path":"a.ts","kind":"Line","raw":"// prose","complete":true,"line":1,"column":1,"beforeCode":true,"preamble":true,"byteZero":true}],` +
		`"unsupported":[{"path":"b.ts","reason":"line 2: Unexpected token"}]}`
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"lint","result":`+result+`}`))
	reported, err := bundle.Lint(context.Background(), "/target", []string{"a.ts", "b.ts"},
		LintLimits{Complexity: 9, Depth: 4, Parameters: 5}, LintActivation{})
	if err != nil {
		t.Fatalf("exchange lint: %v", err)
	}
	if len(reported.Findings) != 1 || reported.Findings[0].Rule != "complexity" || reported.Findings[0].Line != 3 {
		t.Fatalf("unexpected findings: %+v", reported.Findings)
	}
	if len(reported.Comments) != 1 || reported.Comments[0].Kind != "Line" || reported.Comments[0].Raw != "// prose" {
		t.Fatalf("unexpected comments: %+v", reported.Comments)
	}
	if len(reported.Unsupported) != 1 || reported.Unsupported[0].Path != "b.ts" {
		t.Fatalf("unexpected unsupported files: %+v", reported.Unsupported)
	}
}

func TestALintResultRequiresParserCommentFacts(t *testing.T) {
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"lint","result":{"findings":[],"unsupported":[]}}`))
	if _, err := bundle.Lint(context.Background(), "/target", []string{"a.ts"}, LintLimits{Complexity: 9, Depth: 4, Parameters: 5}, LintActivation{}); err == nil {
		t.Fatal("expected missing comment facts to be rejected")
	} else if !strings.Contains(err.Error(), "unreadable lint result") {
		t.Fatalf("unexpected failure %q", err.Error())
	}
}

func TestALintResultRejectsMalformedParserCommentFacts(t *testing.T) {
	for _, test := range []struct {
		name    string
		comment string
	}{
		{name: "missing completeness", comment: `{"path":"a.ts","kind":"Line","raw":"// prose","line":1,"column":1,"beforeCode":true,"preamble":true,"byteZero":true}`},
		{name: "unknown kind", comment: `{"path":"a.ts","kind":"Directive","raw":"// prose","complete":true,"line":1,"column":1,"beforeCode":true,"preamble":true,"byteZero":true}`},
		{name: "empty raw", comment: `{"path":"a.ts","kind":"Line","raw":"","complete":true,"line":1,"column":1,"beforeCode":true,"preamble":true,"byteZero":true}`},
		{name: "nonpositive location", comment: `{"path":"a.ts","kind":"Line","raw":"// prose","complete":true,"line":0,"column":1,"beforeCode":true,"preamble":true,"byteZero":true}`},
		{name: "uncontained path", comment: `{"path":"../a.ts","kind":"Line","raw":"// prose","complete":true,"line":1,"column":1,"beforeCode":true,"preamble":true,"byteZero":true}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := `{"protocolVersion":3,"operation":"lint","result":{"findings":[],"comments":[` + test.comment + `],"unsupported":[]}}`
			bundle := fakeBundle(t, respond(result))
			if _, err := bundle.Lint(context.Background(), "/target", []string{"a.ts"}, LintLimits{Complexity: 9, Depth: 4, Parameters: 5}, LintActivation{}); err == nil {
				t.Fatal("expected malformed parser comment fact to be rejected")
			} else if !strings.Contains(err.Error(), "unreadable lint result") {
				t.Fatalf("unexpected failure %q", err.Error())
			}
		})
	}
}

func TestALintResultRejectsLegacyDirectiveFields(t *testing.T) {
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"lint","result":{"findings":[],"directives":[],"comments":[],"unsupported":[]}}`))
	if _, err := bundle.Lint(context.Background(), "/target", []string{"a.ts"}, LintLimits{Complexity: 9, Depth: 4, Parameters: 5}, LintActivation{}); err == nil {
		t.Fatal("expected the legacy directive field to be rejected")
	} else if !strings.Contains(err.Error(), "unreadable lint result") {
		t.Fatalf("unexpected failure %q", err.Error())
	}
}

func TestATypeCheckRequestCarriesItsProject(t *testing.T) {
	observed := filepath.Join(t.TempDir(), "request.json")
	response := `{"protocolVersion":3,"operation":"typecheck","result":{"diagnostics":[],"covered":[],"unsupported":[]}}`
	bundle := fakeBundle(t, "#!/bin/sh\n/bin/cat >"+observed+"\nprintf '%s\\n' '"+response+"'\n")
	if _, err := bundle.TypeCheck(context.Background(), "/target", "app/tsconfig.json", []string{"app/src/a.ts"}); err != nil {
		t.Fatalf("exchange typecheck: %v", err)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatalf("read the observed request: %v", err)
	}
	want := `{"protocolVersion":3,"operation":"typecheck","root":"/target","paths":["app/src/a.ts"],` +
		`"project":"app/tsconfig.json"}`
	if string(request) != want {
		t.Fatalf("unexpected request %q", string(request))
	}
}

func TestATypeCheckRequestRefusesAnUncontainedProject(t *testing.T) {
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"typecheck","result":{"diagnostics":[],"covered":[],"unsupported":[]}}`))
	for _, project := range []string{"../tsconfig.json", "/etc/tsconfig.json", "app/./tsconfig.json", ""} {
		if _, err := bundle.TypeCheck(context.Background(), "/target", project, []string{"a.ts"}); err == nil {
			t.Errorf("project %q was accepted", project)
		} else if !strings.Contains(err.Error(), "not a contained repository-relative path") {
			t.Errorf("project %q failed with %q", project, err.Error())
		}
	}
}

func TestATypeCheckResultReportsDiagnosticsCoverageAndRefusals(t *testing.T) {
	result := `{"diagnostics":[{"path":"a.ts","line":3,"column":7,"code":2322,"message":"Type 'string' is not assignable to type 'number'."}],` +
		`"covered":["a.ts"],` +
		`"unsupported":[{"path":"tsconfig.json","reason":"the project declares references, which the policy-owned type checker does not resolve"}]}`
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"typecheck","result":`+result+`}`))
	reported, err := bundle.TypeCheck(context.Background(), "/target", "tsconfig.json", []string{"a.ts", "b.ts"})
	if err != nil {
		t.Fatalf("exchange typecheck: %v", err)
	}
	if len(reported.Diagnostics) != 1 || reported.Diagnostics[0].Code != 2322 || reported.Diagnostics[0].Line != 3 {
		t.Fatalf("unexpected diagnostics: %+v", reported.Diagnostics)
	}
	if len(reported.Covered) != 1 || reported.Covered[0] != "a.ts" {
		t.Fatalf("unexpected coverage: %+v", reported.Covered)
	}
	if len(reported.Unsupported) != 1 || reported.Unsupported[0].Path != "tsconfig.json" {
		t.Fatalf("unexpected refusals: %+v", reported.Unsupported)
	}
}

const deadCodeResponse = `{"protocolVersion":3,"operation":"deadcode","result":` +
	`{"unusedFiles":[],"unusedExports":[],"covered":[],"unsupported":[]}}`

func deadCodePackages() []DeadCodeWorkspace {
	return []DeadCodeWorkspace{
		{Root: ".", Entry: []string{"src/index.ts"}, Project: []string{"src/index.ts", "src/helper.ts"}},
		{Root: "packages/web", Entry: []string{}, Project: []string{"packages/web/src/lib.ts"}},
	}
}

func TestADeadCodeRequestCarriesTheTreeAndItsPackages(t *testing.T) {
	observed := filepath.Join(t.TempDir(), "request.json")
	bundle := fakeBundle(t, "#!/bin/sh\n/bin/cat >"+observed+"\nprintf '%s\\n' '"+deadCodeResponse+"'\n")
	if _, err := bundle.DeadCode(context.Background(), "/target", ".", deadCodePackages()); err != nil {
		t.Fatalf("exchange deadcode: %v", err)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatalf("read the observed request: %v", err)
	}
	want := `{"protocolVersion":3,"operation":"deadcode","root":"/target","directory":".","workspaces":[` +
		`{"root":".","entry":["src/index.ts"],"project":["src/index.ts","src/helper.ts"]},` +
		`{"root":"packages/web","entry":[],"project":["packages/web/src/lib.ts"]}]}`
	if string(request) != want {
		t.Fatalf("unexpected request %q", string(request))
	}
}

func TestADeadCodeRequestDeclaresEveryPackagesEntryPoints(t *testing.T) {
	observed := filepath.Join(t.TempDir(), "request.json")
	bundle := fakeBundle(t, "#!/bin/sh\n/bin/cat >"+observed+"\nprintf '%s\\n' '"+deadCodeResponse+"'\n")
	packages := []DeadCodeWorkspace{{Root: ".", Project: []string{"a.ts"}}}
	if _, err := bundle.DeadCode(context.Background(), "/target", ".", packages); err != nil {
		t.Fatalf("exchange deadcode: %v", err)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatalf("read the observed request: %v", err)
	}
	if !strings.Contains(string(request), `"entry":[]`) {
		t.Fatalf("unexpected request %q", string(request))
	}
}

func TestADeadCodeRequestRefusesAnIncompleteTree(t *testing.T) {
	bundle := fakeBundle(t, respond(deadCodeResponse))
	tests := []struct {
		description string
		directory   string
		packages    []DeadCodeWorkspace
		fragment    string
	}{
		{"no packages", ".", nil, "declares no packages"},
		{"an uncontained directory", "../elsewhere",
			[]DeadCodeWorkspace{{Root: ".", Project: []string{"a.ts"}}},
			"not a contained repository-relative path"},
		{"a package outside the tree", "packages/web",
			[]DeadCodeWorkspace{{Root: "packages/api", Project: []string{"packages/api/a.ts"}}},
			"outside"},
		{"a package that selects nothing", ".",
			[]DeadCodeWorkspace{{Root: ".", Project: []string{}}}, "selects no files"},
		{"a file another package owns", ".",
			[]DeadCodeWorkspace{{Root: "packages/web", Project: []string{"src/a.ts"}}},
			"which it does not contain"},
		{"an uncontained entry point", ".",
			[]DeadCodeWorkspace{{Root: ".", Entry: []string{"../a.ts"}, Project: []string{"a.ts"}}},
			"which it does not contain"},
	}
	for _, test := range tests {
		_, err := bundle.DeadCode(context.Background(), "/target", test.directory, test.packages)
		if err == nil {
			t.Errorf("%s was accepted", test.description)
		} else if !strings.Contains(err.Error(), test.fragment) {
			t.Errorf("%s failed with %q", test.description, err.Error())
		}
	}
}

func TestADeadCodeRequestIsBoundedAcrossItsPackages(t *testing.T) {
	bundle := fakeBundle(t, respond(deadCodeResponse))
	packages := []DeadCodeWorkspace{{Root: "apps/one"}, {Root: "apps/two"}}
	for index := range packages {
		for file := 0; file <= maximumOperationPaths/2; file++ {
			packages[index].Project = append(packages[index].Project,
				fmt.Sprintf("%s/file%d.ts", packages[index].Root, file))
		}
	}
	if _, err := bundle.DeadCode(context.Background(), "/target", "apps", packages); err == nil {
		t.Fatal("an unbounded selection was accepted")
	} else if !strings.Contains(err.Error(), "more than the") {
		t.Fatalf("unexpected failure %q", err.Error())
	}
}

func TestADeadCodeResultReportsUnusedFilesExportsAndCoverage(t *testing.T) {
	result := `{"unusedFiles":["src/orphan.ts"],` +
		`"unusedExports":[{"path":"src/helper.ts","line":2,"column":14,"symbol":"unused","kind":"exports"}],` +
		`"covered":["src/helper.ts","src/orphan.ts"],` +
		`"unsupported":[{"path":"src/absent.ts","reason":"the file is unreadable"}]}`
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"deadcode","result":`+result+`}`))
	reported, err := bundle.DeadCode(context.Background(), "/target", ".",
		[]DeadCodeWorkspace{{Root: ".", Project: []string{"src/helper.ts", "src/orphan.ts", "src/absent.ts"}}})
	if err != nil {
		t.Fatalf("exchange deadcode: %v", err)
	}
	if len(reported.UnusedFiles) != 1 || reported.UnusedFiles[0] != "src/orphan.ts" {
		t.Fatalf("unexpected unused files: %+v", reported.UnusedFiles)
	}
	unused := reported.UnusedExports
	if len(unused) != 1 || unused[0].Symbol != "unused" || unused[0].Kind != "exports" || unused[0].Line != 2 {
		t.Fatalf("unexpected unused exports: %+v", unused)
	}
	if len(reported.Covered) != 2 || len(reported.Unsupported) != 1 {
		t.Fatalf("unexpected coverage: %+v, %+v", reported.Covered, reported.Unsupported)
	}
}

func TestAnImportsRequestCarriesTheRootAndTheSelection(t *testing.T) {
	observed := filepath.Join(t.TempDir(), "request.json")
	response := `{"protocolVersion":3,"operation":"imports","result":{"imports":[],"unsupported":[]}}`
	bundle := fakeBundle(t, "#!/bin/sh\n/bin/cat >"+observed+"\nprintf '%s\\n' '"+response+"'\n")
	if _, err := bundle.Imports(context.Background(), "/target", []string{"web/app.ts"}); err != nil {
		t.Fatalf("exchange imports: %v", err)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatalf("read the observed request: %v", err)
	}
	want := `{"protocolVersion":3,"operation":"imports","root":"/target","paths":["web/app.ts"]}`
	if string(request) != want {
		t.Fatalf("unexpected request %q", string(request))
	}
}

func TestAnImportsResultReportsResolvedSpecifiersAndUnreadFiles(t *testing.T) {
	result := `{"imports":[{"path":"web/app.ts","line":3,"specifier":"../domain/model.js","resolved":"domain/model.ts"},` +
		`{"path":"web/app.ts","line":4,"specifier":"@scope/absent","resolved":""}],` +
		`"unsupported":[{"path":"web/widget.tsx","reason":"the file is unreadable"}]}`
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"imports","result":`+result+`}`))
	reported, err := bundle.Imports(context.Background(), "/target", []string{"web/app.ts", "web/widget.tsx"})
	if err != nil {
		t.Fatalf("exchange imports: %v", err)
	}
	if len(reported.Imports) != 2 {
		t.Fatalf("unexpected imports: %+v", reported.Imports)
	}
	if reported.Imports[0].Line != 3 || reported.Imports[0].Resolved != "domain/model.ts" ||
		reported.Imports[0].Specifier != "../domain/model.js" {
		t.Fatalf("unexpected resolved import: %+v", reported.Imports[0])
	}
	if reported.Imports[1].Resolved != "" {
		t.Fatalf("unexpected external import: %+v", reported.Imports[1])
	}
	if len(reported.Unsupported) != 1 || reported.Unsupported[0].Path != "web/widget.tsx" {
		t.Fatalf("unexpected unread files: %+v", reported.Unsupported)
	}
}

func TestAPackagesResultReportsClosedLicenseMetadata(t *testing.T) {
	result := `{"lockfileVersion":"9.0","importers":[],"packages":[` +
		`{"name":"foreign","version":"1.2.3","source":"registry","licenseMetadata":"platform-excluded"},` +
		`{"name":"current","version":"2.3.4","source":"registry","licenseMetadata":"required"},` +
		`{"name":"uncertain","version":"3.4.5","source":"registry","licenseMetadata":"unknown"}` +
		`],"unsupported":[]}`
	reported, err := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"packages","result":`+result+`}`)).Packages(context.Background(), "/target", ".")
	if err != nil {
		t.Fatalf("exchange packages: %v", err)
	}
	if len(reported.Packages) != 3 ||
		reported.Packages[0].LicenseMetadata != LicenseMetadataPlatformExcluded ||
		reported.Packages[1].LicenseMetadata != LicenseMetadataRequired ||
		reported.Packages[2].LicenseMetadata != LicenseMetadataUnknown {
		t.Fatalf("unexpected packages: %+v", reported.Packages)
	}
}

func TestAPackagesResultRefusesAnUnclosedLicenseMetadataState(t *testing.T) {
	for _, test := range []struct {
		name  string
		entry string
	}{
		{
			name:  "missing",
			entry: `{"name":"example","version":"1.2.3","source":"registry"}`,
		},
		{
			name:  "unknown",
			entry: `{"name":"example","version":"1.2.3","source":"registry","licenseMetadata":"host-compatible"}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := `{"lockfileVersion":"9.0","importers":[],"packages":[` +
				test.entry + `],"unsupported":[]}`
			_, err := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"packages","result":`+result+`}`)).Packages(context.Background(), "/target", ".")
			if err == nil || !strings.Contains(err.Error(), "unreadable packages result") {
				t.Fatalf("license metadata was accepted: %v", err)
			}
		})
	}
}

func TestAWorkspaceResultReportsEverySettingAsWrittenText(t *testing.T) {
	result := `{"files":[{"path":"pnpm-workspace.yaml","settings":[` +
		`{"name":"minimumReleaseAge","values":["43200"]},` +
		`{"name":"minimumReleaseAgeExclude","values":["example@1.2.3","other@2.0.0"]},` +
		`{"name":"onlyBuiltDependencies","values":[]}]}],"unsupported":[]}`
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"workspace","result":`+result+`}`))
	reported, err := bundle.Workspace(context.Background(), "/target", []string{"pnpm-workspace.yaml"})
	if err != nil {
		t.Fatalf("exchange workspace: %v", err)
	}
	if len(reported.Files) != 1 || reported.Files[0].Path != "pnpm-workspace.yaml" {
		t.Fatalf("unexpected files: %+v", reported.Files)
	}
	settings := reported.Files[0].Settings
	if len(settings) != 3 || !slices.Equal(settings[0].Values, []string{"43200"}) {
		t.Fatalf("unexpected settings: %+v", settings)
	}
	if !slices.Equal(settings[1].Values, []string{"example@1.2.3", "other@2.0.0"}) {
		t.Fatalf("unexpected exclusions: %+v", settings[1])
	}
	if len(settings[2].Values) != 0 {
		t.Fatalf("unexpected allowlist: %+v", settings[2])
	}
}

func TestAWorkspaceResultReportsAnUnreadableFileAsUnsupported(t *testing.T) {
	result := `{"files":[],"unsupported":[{"path":"pnpm-workspace.yaml","reason":"end of the stream"}]}`
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"workspace","result":`+result+`}`))
	reported, err := bundle.Workspace(context.Background(), "/target", []string{"pnpm-workspace.yaml"})
	if err != nil {
		t.Fatalf("exchange workspace: %v", err)
	}
	if len(reported.Files) != 0 || len(reported.Unsupported) != 1 {
		t.Fatalf("unexpected result: %+v", reported)
	}
}

func TestALicensesRequestCarriesTheProjectDirectory(t *testing.T) {
	observed := filepath.Join(t.TempDir(), "request.json")
	result := `{"packages":[],"unsupported":[]}`
	bundle := fakeBundle(t, "#!/bin/sh\n/bin/cat >"+observed+"\nprintf '%s\\n' '"+
		`{"protocolVersion":3,"operation":"licenses","result":`+result+`}`+"'\n")
	if _, err := bundle.Licenses(context.Background(), "/target", "desktop"); err != nil {
		t.Fatalf("exchange licenses: %v", err)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatalf("read the observed request: %v", err)
	}
	want := `{"protocolVersion":3,"operation":"licenses","root":"/target","directory":"desktop"}`
	if string(request) != want {
		t.Fatalf("unexpected request %q", string(request))
	}
	if _, err := bundle.Licenses(context.Background(), "/target", "../elsewhere"); err == nil {
		t.Fatal("an uncontained project directory was accepted")
	}
}

func TestALicensesResultReportsDeclaredExpressionsAndUnreadableMetadata(t *testing.T) {
	result := `{"packages":[{"name":"example","version":"1.2.3","license":"MIT OR Apache-2.0"},` +
		`{"name":"@scope/quiet","version":"0.1.0","license":""}],` +
		`"unsupported":[{"path":"node_modules/.pnpm","reason":"the dependencies of this project are not installed"}]}`
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"licenses","result":`+result+`}`))
	reported, err := bundle.Licenses(context.Background(), "/target", ".")
	if err != nil {
		t.Fatalf("exchange licenses: %v", err)
	}
	if len(reported.Packages) != 2 || len(reported.Unsupported) != 1 {
		t.Fatalf("unexpected result: %+v", reported)
	}
	if reported.Packages[0].Name != "example" || reported.Packages[0].License != "MIT OR Apache-2.0" {
		t.Fatalf("unexpected package: %+v", reported.Packages[0])
	}
	if reported.Packages[1].Name != "@scope/quiet" || reported.Packages[1].License != "" {
		t.Fatalf("unexpected package: %+v", reported.Packages[1])
	}
}

func TestAuditCommandCarriesTheProjectDirectory(t *testing.T) {
	bundle := fakeBundle(t, respond(`{"protocolVersion":3,"operation":"audit","result":{"advisories":[],"unsupported":[]}}`))
	command, err := bundle.AuditCommand("/target", "desktop")
	if err != nil {
		t.Fatal(err)
	}
	if len(command.Argv) != 4 || command.Argv[2] != "--request-json" || command.Cwd != "." || !command.SealedEnvironment || command.TimeoutSeconds != 900 {
		t.Fatalf("audit command = %+v", command)
	}
	want := `{"protocolVersion":3,"operation":"audit","root":"/target","directory":"desktop"}`
	if command.Argv[3] != want {
		t.Fatalf("unexpected request %q", command.Argv[3])
	}
	if _, err := bundle.AuditCommand("/target", "../elsewhere"); err == nil {
		t.Fatal("an uncontained project directory was accepted")
	}
}

func TestAnAuditResultReportsAdvisoriesAndUnreadableOnes(t *testing.T) {
	result := `{"advisories":[{"id":"GHSA-abcd-1234-5678","aliases":["npm:1100"],"package":"example",` +
		`"severity":"high","title":"unsafe example path","versions":["1.2.3","1.2.4"]}],` +
		`"unsupported":[{"path":"pnpm-lock.yaml","reason":"advisory '1200' omitted the package"}]}`
	reported, err := ParseAuditOutput([]byte(`{"protocolVersion":3,"operation":"audit","result":` + result + `}`))
	if err != nil {
		t.Fatalf("exchange audit: %v", err)
	}
	if len(reported.Advisories) != 1 || len(reported.Unsupported) != 1 {
		t.Fatalf("unexpected result: %+v", reported)
	}
	advisory := reported.Advisories[0]
	if advisory.ID != "GHSA-abcd-1234-5678" || advisory.Package != "example" || advisory.Severity != "high" {
		t.Fatalf("unexpected advisory: %+v", advisory)
	}
	if !slices.Equal(advisory.Aliases, []string{"npm:1100"}) || !slices.Equal(advisory.Versions, []string{"1.2.3", "1.2.4"}) {
		t.Fatalf("unexpected advisory identity: %+v", advisory)
	}
}

func TestParseAuditOutputRejectsAnOversizedResponse(t *testing.T) {
	output := bytes.Repeat([]byte("x"), maximumResponseBytes+1)
	if _, err := ParseAuditOutput(output); err == nil || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("oversized audit response error = %v", err)
	}
}

func TestABoundedWriterKeepsOnlyItsLimit(t *testing.T) {
	bounded := &boundedWriter{limit: 4}
	written, err := bounded.Write([]byte("abcdefgh"))
	if written != 8 || err != nil {
		t.Fatalf("expected a complete accepted write, got %d, %v", written, err)
	}
	if bounded.buffer.String() != "abcd" || !bounded.truncated {
		t.Fatalf("expected a truncated %q, got %q truncated=%v", "abcd", bounded.buffer.String(), bounded.truncated)
	}
}
