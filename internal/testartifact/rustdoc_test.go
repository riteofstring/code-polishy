package testartifact

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestRustdocOutputPreservesNativeCaseKinds(t *testing.T) {
	t.Parallel()
	input := rustdocBuild + "\nrunning 5 tests\n" +
		"test src/lib.rs - answer (line 1) ... ok\n" +
		"test src/lib.rs - answer (line 4) - compile fail ... ok\n" +
		"test src/lib.rs - answer (line 7) - compile ... ok\n" +
		"test src/lib.rs - answer (line 10) ... ignored, needs a live provider\n" +
		"test src/lib.rs - answer (line 13) - should panic ... ok\n\n" +
		"test result: ok. 4 passed; 0 failed; 1 ignored; 0 measured; 0 filtered out; finished in 0.04s\n\n" +
		"all doctests ran in 0.50s; merged doctests compilation took 0.40s\n"
	report, err := parseRustdocOutput([]byte(input), "docs")
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []string{"run", "compile-fail", "compile-only", "run", "should-panic"} {
		if got := report.Batches[0].Cases[index].Kind; got != want {
			t.Fatalf("case %d kind = %q, want %q", index, got, want)
		}
	}
	data, err := json.Marshal(report)
	if err != nil || validateRustdoc(data, "docs", 0) != nil {
		t.Fatalf("complete doctest evidence rejected: %s, %v", data, err)
	}
}

func TestRustdocOutputRejectsPartialOrInconsistentEvidence(t *testing.T) {
	t.Parallel()
	pass := rustdocBuild + rustdocPassBatch
	for name, input := range map[string]string{
		"empty": "", "build only": rustdocBuild,
		"without build":         rustdocPassBatch,
		"failed build":          strings.Replace(pass, `"success":true`, `"success":false`, 1),
		"missing build outcome": strings.Replace(pass, `,"success":true`, "", 1),
		"unknown Cargo event":   strings.Replace(pass, "build-finished", "unknown", 1),
		"partial test":          strings.Split(pass, "test result:")[0],
		"missing case":          strings.Replace(pass, "running 1 test", "running 2 tests", 1),
		"duplicate case":        strings.Replace(strings.Replace(pass, "running 1 test", "running 2 tests", 1), "\ntest result:", "\ntest src/lib.rs - answer (line 1) ... ok\ntest result:", 1),
		"wrong summary":         strings.Replace(pass, "1 passed", "2 passed", 1),
		"wrong outcome":         strings.Replace(pass, "test result: ok", "test result: FAILED", 1),
		"filtered tests":        strings.Replace(pass, "0 filtered out", "1 filtered out", 1),
		"benchmark":             strings.Replace(pass, "0 measured", "1 measured", 1),
		"surplus case":          pass + "test src/lib.rs - extra (line 10) ... ok\n",
		"surplus summary":       pass + "test result: ok. 1 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.01s\n",
		"too many tests":        rustdocBuild + "running 100001 tests\n",
		"oversized":             strings.Repeat(" ", 8<<20+1),
		"invalid UTF8":          pass + "\xff",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseRustdocOutput([]byte(input), "docs"); err == nil {
				t.Fatal("invalid doctest output established evidence")
			}
		})
	}
}

func TestRustdocEvidenceRequiresRealCasesAndMatchingExit(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		batch string
		exit  int
		valid bool
	}{
		"pass":                       {rustdocPassBatch, 0, true},
		"failure":                    {rustdocFailedBatch, 101, true},
		"failed result exit zero":    {rustdocFailedBatch, 0, false},
		"passing result failed exit": {rustdocPassBatch, 101, false},
		"operational exit":           {rustdocFailedBatch, 1, false},
		"empty":                      {"running 0 tests\ntest result: ok. 0 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s\n", 0, false},
		"all ignored":                {strings.ReplaceAll(strings.Replace(rustdocPassBatch, " ... ok", " ... ignored", 1), "1 passed; 0 failed; 0 ignored", "0 passed; 0 failed; 1 ignored"), 0, false},
		"multiple batches":           {rustdocPassBatch + rustdocPassBatch, 0, true},
	} {
		t.Run(name, func(t *testing.T) {
			report, err := parseRustdocOutput([]byte(rustdocBuild+test.batch), "docs")
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateRustdoc(data, "docs", test.exit); (err == nil) != test.valid {
				t.Fatalf("validation = %v, want valid %v", err, test.valid)
			}
		})
	}
}

func TestRustdocArtifactCannotReuseAnEarlierAttempt(t *testing.T) {
	t.Parallel()
	execution, err := Start(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer execution.Complete()
	suite := policy.TestSuite{Name: "docs", Rust: &policy.RustTestSuite{Manifest: "Cargo.toml", Doctests: true}}
	command, err := execution.PrepareCommand(suite, policy.Command{})
	if err != nil {
		t.Fatal(err)
	}
	if err := CaptureCommandOutput(command, []byte(rustdocBuild+rustdocPassBatch)); err != nil {
		t.Fatal(err)
	}
	if err := CaptureCommandOutput(command, []byte(rustdocBuild+rustdocFailedBatch)); err == nil {
		t.Fatal("a later result replaced existing doctest evidence")
	}
	records, err := execution.ValidateCommand(command, 0)
	if err != nil || len(records) != 1 || records[0].Type != policy.RustdocArtifact || records[0].SHA256 == "" {
		t.Fatalf("doctest artifact = %+v, %v", records, err)
	}
	retry, err := execution.PrepareCommand(suite, policy.Command{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execution.ValidateCommand(retry, 0); err == nil {
		t.Fatal("retry reused an earlier doctest report")
	}
	data, err := os.ReadFile(filepath.Join(command.TestArtifactDirectory, "rustdoc.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]byte{
		[]byte(strings.Replace(string(data), `"passed":1`, `"passed":2`, 1)),
		[]byte(strings.Replace(string(data), `"suite":"docs"`, `"suite":"other"`, 1)),
		[]byte(strings.Replace(string(data), policy.RustdocToolVersion, "1.96.0", 1)),
		[]byte(strings.Replace(string(data), `"kind":"run"`, `"kind":"compile-only"`, 1)),
		append(append([]byte{}, data...), []byte("{}")...),
	} {
		if err := validateRustdoc(invalid, "docs", 0); err == nil {
			t.Fatal("changed doctest evidence was accepted")
		}
	}
}

const rustdocBuild = "{\"reason\":\"build-finished\",\"success\":true}\n"
const rustdocPassBatch = "\nrunning 1 test\ntest src/lib.rs - answer (line 1) ... ok\n\ntest result: ok. 1 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.01s\n"
const rustdocFailedBatch = "\nrunning 1 test\ntest src/lib.rs - answer (line 1) ... FAILED\n\nfailures:\n---- src/lib.rs - answer (line 1) stdout ----\nassertion failed\nfailures:\n    src/lib.rs - answer (line 1)\n\ntest result: FAILED. 0 passed; 1 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.01s\n"
