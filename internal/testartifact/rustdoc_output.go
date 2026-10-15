package testartifact

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/policy"
)

var rustdocRunStart = regexp.MustCompile(`^running ([0-9]+) (test|tests)$`)
var rustdocRunEnd = regexp.MustCompile(`^test result: (ok|FAILED)\. ([0-9]+) passed; ([0-9]+) failed; ([0-9]+) ignored; 0 measured; 0 filtered out; finished in [0-9]+\.[0-9]+s$`)
var rustdocRunTiming = regexp.MustCompile(`^all doctests ran in [0-9]+\.[0-9]+s; merged doctests compilation took [0-9]+\.[0-9]+s$`)

type rustdocOutput struct {
	report   rustdocReport
	active   *rustdocBatch
	expected int
	seen     map[string]bool
}

func parseRustdocOutput(data []byte, suite string) (rustdocReport, error) {
	state := rustdocOutput{report: rustdocReport{Protocol: "rustdoc/v1", RustVersion: policy.RustdocToolVersion, Suite: suite}}
	if len(data) > 8<<20 || !utf8.Valid(data) {
		return state.report, errors.New("doctest output exceeds 8388608 bytes or is not UTF-8")
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		if err := state.line(scanner.Text()); err != nil {
			return state.report, err
		}
	}
	if err := scanner.Err(); err != nil {
		return state.report, err
	}
	if !state.report.BuildSucceeded || state.active != nil || len(state.report.Batches) == 0 {
		return state.report, errors.New("doctest output has no complete Cargo build and test run")
	}
	return state.report, nil
}

func (state *rustdocOutput) line(line string) error {
	if line == "" {
		return nil
	}
	if !state.report.BuildSucceeded {
		return state.cargo(line)
	}
	if state.active == nil {
		return state.start(line)
	}
	if len(state.active.Cases) < state.expected {
		return state.test(line)
	}
	return state.finish(line)
}

func (state *rustdocOutput) cargo(line string) error {
	var message struct {
		Reason  string `json:"reason"`
		Success *bool  `json:"success"`
	}
	if err := json.Unmarshal([]byte(line), &message); err != nil {
		return errors.New("doctest output precedes a complete Cargo JSON build")
	}
	switch message.Reason {
	case "compiler-artifact", "compiler-message", "build-script-executed":
		return nil
	case "build-finished":
		if message.Success != nil && *message.Success {
			state.report.BuildSucceeded = true
			return nil
		}
	}
	return errors.New("doctest output contains a failed or unsupported Cargo build event")
}

func (state *rustdocOutput) start(line string) error {
	if len(state.report.Batches) > 0 && rustdocRunTiming.MatchString(line) {
		return nil
	}
	match := rustdocRunStart.FindStringSubmatch(line)
	if match == nil {
		return errors.New("unexpected output outside a doctest run")
	}
	count, err := strconv.Atoi(match[1])
	if err != nil || count > 100000 || count == 1 && match[2] != "test" || count != 1 && match[2] != "tests" {
		return errors.New("doctest run has an invalid case count")
	}
	state.active = &rustdocBatch{Cases: []rustdocCase{}}
	state.expected = count
	state.seen = map[string]bool{}
	return nil
}

func (state *rustdocOutput) test(line string) error {
	name, outcome, found := strings.Cut(strings.TrimPrefix(line, "test "), " ... ")
	if !strings.HasPrefix(line, "test ") || !found || name == "" || state.seen[name] {
		return errors.New("doctest run has missing or duplicate case records")
	}
	status := ""
	switch {
	case outcome == "ok":
		status = "passed"
	case outcome == "FAILED":
		status = "failed"
	case outcome == "ignored" || strings.HasPrefix(outcome, "ignored, "):
		status = "ignored"
	default:
		return errors.New("doctest case has an unsupported result")
	}
	state.seen[name] = true
	state.active.Cases = append(state.active.Cases, rustdocCase{Name: name, Kind: rustdocCaseKind(name), Outcome: status})
	return countRustdocOutcome(state.active, status)
}

func (state *rustdocOutput) finish(line string) error {
	match := rustdocRunEnd.FindStringSubmatch(line)
	if match == nil {
		if state.active.Failed > 0 && !strings.HasPrefix(line, "test result:") {
			return nil
		}
		return errors.New("doctest run omitted its complete result summary")
	}
	for index, count := range []int{state.active.Passed, state.active.Failed, state.active.Ignored} {
		if match[index+2] != strconv.Itoa(count) {
			return errors.New("doctest summary disagrees with its recorded cases")
		}
	}
	if (match[1] == "ok") != (state.active.Failed == 0) {
		return fmt.Errorf("doctest result %q disagrees with its failures", match[1])
	}
	state.report.Batches = append(state.report.Batches, *state.active)
	state.active = nil
	return nil
}
