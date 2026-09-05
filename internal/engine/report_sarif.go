package engine

import (
	"encoding/json"
	"sort"

	"github.com/riteofstring/code-polishy/internal/policy"
)

const sarifSchema = "https://json.schemastore.org/sarif-2.1.0.json"

type sarifLog struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool       sarifTool      `json:"tool"`
	Results    []sarifResult  `json:"results"`
	Properties map[string]any `json:"properties"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name            string      `json:"name"`
	InformationURI  string      `json:"informationUri"`
	SemanticVersion string      `json:"semanticVersion"`
	Rules           []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string       `json:"id"`
	ShortDescription sarifMessage `json:"shortDescription"`
}

type sarifResult struct {
	RuleID              string                 `json:"ruleId"`
	Level               string                 `json:"level"`
	Message             sarifMessage           `json:"message"`
	Locations           []sarifLocation        `json:"locations,omitempty"`
	RelatedLocations    []sarifRelatedLocation `json:"relatedLocations,omitempty"`
	PartialFingerprints map[string]string      `json:"partialFingerprints"`
	Suppressions        []sarifSuppression     `json:"suppressions,omitempty"`
	Properties          map[string]any         `json:"properties"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifRelatedLocation struct {
	ID               int                   `json:"id"`
	Message          *sarifMessage         `json:"message,omitempty"`
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
	EndLine     int `json:"endLine,omitempty"`
	EndColumn   int `json:"endColumn,omitempty"`
}

type sarifSuppression struct {
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	Justification string `json:"justification"`
}

func SARIF(report Report) ([]byte, error) {
	results := make([]sarifResult, 0, reportOccurrenceCount(report))
	rules := map[string]bool{}
	for _, finding := range report.Findings {
		results = append(results, findingSARIF(finding, ""))
		rules[finding.Check] = true
	}
	for _, suppressed := range report.Suppressed {
		results = append(results, findingSARIF(suppressed.Finding, suppressed.Exception.ID+": "+suppressed.Exception.Reason))
		rules[suppressed.Finding.Check] = true
	}
	for _, assessed := range report.Assessed {
		results = append(results, findingSARIF(assessed.Finding, assessed.Assessment.ID+": "+assessed.Assessment.Reason))
		rules[assessed.Finding.Check] = true
	}
	for _, assessed := range report.ReleaseAges {
		results = append(results, findingSARIF(assessed.Finding, assessed.Assessment.ID+": "+assessed.Assessment.Reason))
		rules[assessed.Finding.Check] = true
	}
	ruleIDs := make([]string, 0, len(rules))
	for rule := range rules {
		ruleIDs = append(ruleIDs, rule)
	}
	sort.Strings(ruleIDs)
	sarifRules := make([]sarifRule, 0, len(ruleIDs))
	for _, rule := range ruleIDs {
		sarifRules = append(sarifRules, sarifRule{ID: rule, ShortDescription: sarifMessage{Text: rule}})
	}
	log := sarifLog{Version: "2.1.0", Schema: sarifSchema, Runs: []sarifRun{{
		Tool: sarifTool{Driver: sarifDriver{
			Name: "Code Polishy", InformationURI: "https://github.com/riteofstring/code-polishy", SemanticVersion: "0.24.0", Rules: sarifRules,
		}},
		Results: results, Properties: map[string]any{"codePolishyReport": report},
	}}}
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if err := validateSARIFReport(data); err != nil {
		return nil, err
	}
	return data, nil
}

func findingSARIF(finding policy.Finding, suppression string) sarifResult {
	result := sarifResult{
		RuleID: finding.Check, Level: sarifLevel(finding.Severity), Message: sarifMessage{Text: finding.Message},
		PartialFingerprints: map[string]string{"codePolishy/v1": finding.Fingerprint},
		Properties: map[string]any{
			"evaluationStatus": finding.Status, "selectionRelation": finding.SelectionRelation,
			"scope": finding.Scope, "subject": finding.Subject, "module": finding.Module, "remediation": finding.Remediation,
		},
	}
	if finding.Path != "" {
		result.Locations = []sarifLocation{{PhysicalLocation: findingSARIFLocation(policy.FindingLocation{
			Path: finding.Path, Line: finding.Line, Column: finding.Column, EndLine: finding.EndLine, EndColumn: finding.EndColumn,
		})}}
	}
	for index, related := range finding.Related {
		location := sarifRelatedLocation{ID: index + 1, PhysicalLocation: findingSARIFLocation(related)}
		if related.Message != "" {
			location.Message = &sarifMessage{Text: related.Message}
		}
		result.RelatedLocations = append(result.RelatedLocations, location)
	}
	if suppression != "" {
		result.Suppressions = []sarifSuppression{{Kind: "external", Status: "accepted", Justification: suppression}}
	}
	return result
}

func findingSARIFLocation(location policy.FindingLocation) sarifPhysicalLocation {
	physical := sarifPhysicalLocation{ArtifactLocation: sarifArtifactLocation{URI: location.Path}}
	if location.Line > 0 {
		physical.Region = &sarifRegion{
			StartLine: location.Line, StartColumn: location.Column, EndLine: location.EndLine, EndColumn: location.EndColumn,
		}
	}
	return physical
}

func sarifLevel(severity policy.FindingSeverity) string {
	switch severity {
	case policy.FindingWarning:
		return "warning"
	case policy.FindingInformation:
		return "note"
	default:
		return "error"
	}
}
