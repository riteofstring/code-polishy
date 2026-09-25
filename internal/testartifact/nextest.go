package testartifact

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type nextestCounts struct {
	Tests    string `xml:"tests,attr"`
	Failures string `xml:"failures,attr"`
	Errors   string `xml:"errors,attr"`
}

type nextestReport struct {
	Name string `xml:"name,attr"`
	UUID string `xml:"uuid,attr"`
	nextestCounts
	Binaries []nextestBinary `xml:"testsuite"`
}

type nextestBinary struct {
	Name     string `xml:"name,attr"`
	Disabled string `xml:"disabled,attr"`
	nextestCounts
	Cases []nextestCase `xml:"testcase"`
}

type nextestCase struct {
	Name     string     `xml:"name,attr"`
	Class    string     `xml:"classname,attr"`
	Failures []struct{} `xml:"failure"`
	Errors   []struct{} `xml:"error"`
	Skipped  []struct{} `xml:"skipped"`
}

type nextestTotals struct{ tests, failures, errors, skipped int }

type nextestXMLState struct {
	stack []string
	root  bool
}

func validateNextest(data []byte, suite string, exitStatus int) error {
	if err := nextestXMLShape(data); err != nil {
		return fmt.Errorf("invalid Nextest evidence: %w", err)
	}
	var report nextestReport
	if err := xml.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("invalid Nextest evidence: %w", err)
	}
	if report.Name != suite || strings.TrimSpace(report.UUID) == "" {
		return errors.New("nextest report must identify the current suite and run")
	}
	totals, err := nextestReportTotals(report)
	if err != nil {
		return err
	}
	if err := report.nextestCounts.matches(totals); err != nil {
		return err
	}
	return nextestOutcome(totals, exitStatus)
}

func nextestReportTotals(report nextestReport) (nextestTotals, error) {
	total := nextestTotals{}
	seen := map[string]bool{}
	for _, binary := range report.Binaries {
		if strings.TrimSpace(binary.Name) == "" || seen[binary.Name] {
			return total, errors.New("nextest report has missing or duplicate binary identities")
		}
		seen[binary.Name] = true
		counts, err := nextestBinaryTotals(binary)
		if err != nil {
			return total, err
		}
		total.tests += counts.tests
		total.failures += counts.failures
		total.errors += counts.errors
		total.skipped += counts.skipped
	}
	return total, nil
}

func nextestBinaryTotals(binary nextestBinary) (nextestTotals, error) {
	totals := nextestTotals{tests: len(binary.Cases)}
	seen := map[string]bool{}
	for _, test := range binary.Cases {
		if strings.TrimSpace(test.Name) == "" || test.Class != binary.Name || seen[test.Name] {
			return totals, errors.New("nextest report has missing or duplicate test identities")
		}
		seen[test.Name] = true
		if len(test.Failures)+len(test.Errors)+len(test.Skipped) > 1 {
			return totals, errors.New("nextest test has conflicting or repeated outcomes")
		}
		totals.failures += len(test.Failures)
		totals.errors += len(test.Errors)
		totals.skipped += len(test.Skipped)
	}
	if binary.Disabled != strconv.Itoa(totals.skipped) {
		return totals, errors.New("nextest disabled count disagrees with its cases")
	}
	return totals, binary.nextestCounts.matches(totals)
}

func (counts nextestCounts) matches(totals nextestTotals) error {
	if counts.Tests != strconv.Itoa(totals.tests) || counts.Failures != strconv.Itoa(totals.failures) || counts.Errors != strconv.Itoa(totals.errors) {
		return errors.New("nextest declared counts disagree with complete test cases")
	}
	return nil
}

func nextestOutcome(totals nextestTotals, exitStatus int) error {
	if totals.tests == totals.skipped {
		return errors.New("nextest selected no executed tests")
	}
	failures := totals.failures + totals.errors
	if exitStatus == 0 && failures == 0 || exitStatus == 100 && failures > 0 {
		return nil
	}
	return fmt.Errorf("nextest exit %d disagrees with test evidence or indicates an operational failure", exitStatus)
}

func nextestXMLShape(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	state := nextestXMLState{}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if err := state.consume(token); err != nil {
			return err
		}
	}
	if !state.root || len(state.stack) != 0 {
		return errors.New("incomplete Nextest report")
	}
	return nil
}

func (state *nextestXMLState) consume(token xml.Token) error {
	switch value := token.(type) {
	case xml.StartElement:
		if err := nextestElement(state.stack, value, state.root); err != nil {
			return err
		}
		state.root = true
		state.stack = append(state.stack, value.Name.Local)
	case xml.EndElement:
		state.stack = state.stack[:len(state.stack)-1]
	case xml.Directive:
		return errors.New("XML directives are forbidden")
	case xml.CharData:
		if len(state.stack) < 4 && strings.TrimSpace(string(value)) != "" {
			return errors.New("unexpected text outside a test outcome")
		}
	}
	return nil
}

func nextestAttributes(element xml.StartElement) error {
	if element.Name.Space != "" {
		return errors.New("nextest XML namespaces are unsupported")
	}
	seen := map[xml.Name]bool{}
	for _, attribute := range element.Attr {
		if attribute.Name.Space != "" || seen[attribute.Name] {
			return errors.New("duplicate or namespaced Nextest attribute")
		}
		seen[attribute.Name] = true
	}
	return nil
}

func nextestElement(stack []string, element xml.StartElement, root bool) error {
	if err := nextestAttributes(element); err != nil {
		return err
	}
	path := strings.Join(append(append([]string{}, stack...), element.Name.Local), "/")
	if path == "testsuites" && !root || path == "testsuites/testsuite" || path == "testsuites/testsuite/testcase" {
		return nil
	}
	if len(stack) == 3 && stack[2] == "testcase" {
		switch element.Name.Local {
		case "failure", "error", "skipped", "system-out", "system-err":
			return nil
		}
	}
	return fmt.Errorf("unsupported Nextest element %q", path)
}
