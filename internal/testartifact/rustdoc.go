package testartifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/strictjson"
)

type rustdocCase struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Outcome string `json:"outcome"`
}

type rustdocBatch struct {
	Cases   []rustdocCase `json:"cases"`
	Passed  int           `json:"passed"`
	Failed  int           `json:"failed"`
	Ignored int           `json:"ignored"`
}

type rustdocReport struct {
	Protocol       string         `json:"protocol"`
	RustVersion    string         `json:"rustVersion"`
	Suite          string         `json:"suite"`
	BuildSucceeded bool           `json:"buildSucceeded"`
	Batches        []rustdocBatch `json:"batches"`
}

func NeedsCommandOutput(command policy.Command) bool {
	for _, artifact := range command.TestArtifacts {
		if artifact.Type == policy.RustdocArtifact {
			return true
		}
	}
	return false
}

func CaptureCommandOutput(command policy.Command, stdout []byte) error {
	if !NeedsCommandOutput(command) {
		return nil
	}
	if command.TestArtifactDirectory == "" || command.TestArtifactSuite == "" {
		return errors.New("doctest evidence has no managed artifact directory or suite")
	}
	report, err := parseRustdocOutput(stdout, command.TestArtifactSuite)
	if err != nil {
		return fmt.Errorf("invalid doctest output: %w", err)
	}
	data, err := json.Marshal(report)
	if err != nil || len(data) > MaximumArtifactBytes {
		return errors.New("doctest report exceeds its bounded JSON contract")
	}
	root, err := os.OpenRoot(command.TestArtifactDirectory)
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.OpenFile("rustdoc.json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Close())
}

func validateRustdoc(data []byte, suite string, exitStatus int) error {
	var report rustdocReport
	if err := strictjson.Decode(data, &report); err != nil {
		return fmt.Errorf("invalid doctest evidence: %w", err)
	}
	if !report.matches(suite) {
		return errors.New("doctest evidence lacks the current suite, compiler or successful Cargo build")
	}
	passed, failed := 0, 0
	for _, batch := range report.Batches {
		if err := validateRustdocBatch(batch); err != nil {
			return err
		}
		passed += batch.Passed
		failed += batch.Failed
	}
	if passed+failed == 0 {
		return errors.New("doctest suite selected no checked examples")
	}
	if exitStatus == 0 && failed == 0 || exitStatus == 101 && failed > 0 {
		return nil
	}
	return fmt.Errorf("cargo exit %d disagrees with doctest evidence or indicates an operational failure", exitStatus)
}

func (report rustdocReport) matches(suite string) bool {
	return report.Protocol == "rustdoc/v1" && report.RustVersion == policy.RustdocToolVersion && report.Suite == suite && report.BuildSucceeded
}

func validateRustdocBatch(batch rustdocBatch) error {
	seen := map[string]bool{}
	counts := rustdocBatch{}
	for _, test := range batch.Cases {
		if strings.TrimSpace(test.Name) == "" || seen[test.Name] || test.Kind != rustdocCaseKind(test.Name) {
			return errors.New("doctest report has missing, duplicate or inconsistent case identities")
		}
		seen[test.Name] = true
		if err := countRustdocOutcome(&counts, test.Outcome); err != nil {
			return err
		}
	}
	if batch.Cases == nil || batch.Passed != counts.Passed || batch.Failed != counts.Failed || batch.Ignored != counts.Ignored {
		return errors.New("doctest report counts disagree with its complete cases")
	}
	return nil
}

func countRustdocOutcome(batch *rustdocBatch, outcome string) error {
	switch outcome {
	case "passed":
		batch.Passed++
	case "failed":
		batch.Failed++
	case "ignored":
		batch.Ignored++
	default:
		return errors.New("unknown doctest outcome")
	}
	return nil
}

func rustdocCaseKind(name string) string {
	for suffix, kind := range map[string]string{" - compile fail": "compile-fail", " - compile": "compile-only", " - should panic": "should-panic"} {
		if strings.HasSuffix(name, suffix) {
			return kind
		}
	}
	return "run"
}
