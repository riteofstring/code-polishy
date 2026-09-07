package quality

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func pythonVultureFindings(repo repository.Repository, project repository.PythonProject, data []byte) []policy.Finding {
	return pythonVultureFindingsForSources(repo, project, project.Files, data)
}

func pythonVultureFindingsForSources(repo repository.Repository, project repository.PythonProject, sources []string, data []byte) []policy.Finding {
	response, err := parsePythonVultureResponse(data)
	if err != nil {
		return pythonVultureCoverage(sources, "the policy-owned Vulture output cannot be used: "+err.Error())
	}
	if response.Error != "" {
		if response.FactsError != "" {
			return []policy.Finding{{Check: "architecture.pythonFactsCoverage", Path: project.Manifest, Subject: "python-facts", Message: "the Python project type fact set is unavailable: " + response.FactsError}}
		}
		return pythonVultureCoverage(sources, "the policy-owned Vulture analysis failed: "+response.Error)
	}
	references, origins := pythonVultureReferences(repo, project)
	backends, backendOrigins := pythonVultureBackends(project)
	attributes, attributeOrigins := pythonVultureAttributes(repo, project)
	for id, origin := range backendOrigins {
		origins[id] = origin
	}
	for id, origin := range attributeOrigins {
		origins[id] = origin
	}
	contracts := pythonVultureContracts(repo, project)
	for _, contract := range contracts {
		origins[contract.ID] = pythonContractOrigin(repo, contract.PythonContract)
	}
	requireContracts := len(sources) == 0 || len(sources) == len(project.Files)
	if err := validatePythonVultureResponse(project.Files, sources, references, backends, attributes, contracts, requireContracts, response); err != nil {
		return pythonVultureCoverage(sources, "the policy-owned Vulture output cannot be used: "+err.Error())
	}
	findings := pythonVultureReferenceFindings(response.Problems, origins)
	for _, diagnostic := range response.Diagnostics {
		findings = append(findings, policy.Finding{
			Check: "quality.deadCode", Path: diagnostic.Path, Line: diagnostic.Line,
			Subject: pythonVultureSubject(diagnostic), Message: pythonVultureMessage(diagnostic),
			Remediation: policy.FindingRemediation{
				Summary:     "Delete the unused definition. Do not generate reachability declarations or entry-point lists from dead-code findings.",
				NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "check", "--all"}, Cwd: "."},
			},
		})
	}
	return findings
}

func parsePythonVultureResponse(data []byte) (pythonVultureResponse, error) {
	if len(data) > pythonStructuredOutputMaximumBytes {
		return pythonVultureResponse{}, fmt.Errorf("output exceeds %d bytes", pythonStructuredOutputMaximumBytes)
	}
	wire, err := decodePythonVultureResponse(data)
	if err != nil {
		return pythonVultureResponse{}, err
	}
	return pythonVultureResponseFromWire(wire)
}

func decodePythonVultureResponse(data []byte) (pythonVultureResponseWire, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	wire := pythonVultureResponseWire{}
	if err := decoder.Decode(&wire); err != nil {
		return pythonVultureResponseWire{}, fmt.Errorf("malformed JSON: %w", err)
	}
	if err := pythonVultureTrailingJSONError(decoder); err != nil {
		return pythonVultureResponseWire{}, err
	}
	return wire, nil
}

func pythonVultureTrailingJSONError(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("JSON has trailing data")
	}
	return fmt.Errorf("malformed trailing JSON: %w", err)
}

func pythonVultureResponseFromWire(wire pythonVultureResponseWire) (pythonVultureResponse, error) {
	if !pythonVultureResponseWireComplete(wire) {
		return pythonVultureResponse{}, fmt.Errorf("output omits required fields")
	}
	if *wire.Protocol != pythonVultureProtocolVersion {
		return pythonVultureResponse{}, fmt.Errorf("protocol is not %d", pythonVultureProtocolVersion)
	}
	if *wire.ToolVersion != pythonVultureVersion {
		return pythonVultureResponse{}, fmt.Errorf("vulture version is not %s", pythonVultureVersion)
	}
	if len(*wire.Error) > pythonStructuredMessageMaximumBytes {
		return pythonVultureResponse{}, fmt.Errorf("error exceeds %d bytes", pythonStructuredMessageMaximumBytes)
	}
	if err := validatePythonVultureTimings(*wire.Timings); err != nil {
		return pythonVultureResponse{}, err
	}
	return pythonVultureResponse{
		Reachability: *wire.Reachability, Covered: *wire.Covered, Diagnostics: *wire.Diagnostics, Resolved: *wire.Resolved, Problems: *wire.Problems, Error: *wire.Error, FactsError: *wire.FactsError, Timings: *wire.Timings,
	}, nil
}

func pythonVultureResponseWireComplete(wire pythonVultureResponseWire) bool {
	return wire.Protocol != nil && wire.ToolVersion != nil && wire.Covered != nil && wire.Diagnostics != nil &&
		wire.Resolved != nil && wire.Problems != nil && wire.Error != nil && wire.FactsError != nil && wire.Reachability != nil && wire.Timings != nil
}

func validatePythonVultureResponse(files, targets []string, references []pythonVultureReference, backends []pythonVultureBackend, attributes []pythonVultureAttribute, contracts []pythonVultureContract, requireContracts bool, response pythonVultureResponse) error {
	if err := validatePythonVultureCovered(files, response.Covered); err != nil {
		return err
	}
	if err := validatePythonVultureReferences(references, backends, attributes, contracts, requireContracts, response.Resolved, response.Problems); err != nil {
		return err
	}
	if err := validatePythonReachabilityEvidence(files, references, response); err != nil {
		return err
	}
	return validatePythonVultureDiagnostics(targets, response.Diagnostics)
}

func validatePythonVultureCovered(files, covered []string) error {
	expected := map[string]bool{}
	for _, path := range files {
		expected[path] = true
	}
	actual := map[string]bool{}
	for _, path := range covered {
		if !expected[path] || actual[path] {
			return fmt.Errorf("covered paths are not an exact project inventory")
		}
		actual[path] = true
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("covered paths omit governed Python source")
	}
	return nil
}

func validatePythonVultureReferences(references []pythonVultureReference, backends []pythonVultureBackend, attributes []pythonVultureAttribute, contracts []pythonVultureContract, requireContracts bool, resolved []string, problems []pythonVultureProblem) error {
	if !requireContracts {
		contracts = nil
	}
	remaining, err := pythonVultureRequestedReferences(references, backends, attributes, contracts)
	if err != nil {
		return err
	}
	if err := pythonVultureConsumeResolvedReferences(remaining, resolved); err != nil {
		return err
	}
	if err := pythonVultureConsumeReferenceProblems(remaining, problems); err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf("reference result omits requested references")
	}
	return nil
}

func pythonVultureRequestedReferences(references []pythonVultureReference, backends []pythonVultureBackend, attributes []pythonVultureAttribute, contracts []pythonVultureContract) (map[string]bool, error) {
	requested := map[string]bool{}
	for _, reference := range references {
		if requested[reference.ID] {
			return nil, fmt.Errorf("policy dynamic references are not unique")
		}
		requested[reference.ID] = true
	}
	for _, backend := range backends {
		if requested[backend.ID] {
			return nil, fmt.Errorf("vulture references are not unique")
		}
		requested[backend.ID] = true
	}
	for _, attribute := range attributes {
		if requested[attribute.ID] {
			return nil, fmt.Errorf("vulture references are not unique")
		}
		requested[attribute.ID] = true
	}
	for _, contract := range contracts {
		if requested[contract.ID] {
			return nil, fmt.Errorf("vulture contracts are not unique")
		}
		requested[contract.ID] = true
	}
	return requested, nil
}

func pythonVultureConsumeResolvedReferences(remaining map[string]bool, resolved []string) error {
	for _, id := range resolved {
		if !remaining[id] {
			return fmt.Errorf("resolved references are not an exact request")
		}
		delete(remaining, id)
	}
	return nil
}

func pythonVultureConsumeReferenceProblems(remaining map[string]bool, problems []pythonVultureProblem) error {
	for _, problem := range problems {
		if !remaining[problem.ID] || strings.TrimSpace(problem.Message) == "" || len(problem.Message) > pythonStructuredMessageMaximumBytes {
			return fmt.Errorf("problem references are not an exact request")
		}
		delete(remaining, problem.ID)
	}
	return nil
}

func validatePythonVultureDiagnostics(files []string, diagnostics []pythonVultureDiagnostic) error {
	if len(diagnostics) > pythonStructuredDiagnosticMaximum {
		return fmt.Errorf("output contains more than %d diagnostics", pythonStructuredDiagnosticMaximum)
	}
	known := pythonVultureKnownFiles(files)
	seen := map[string]bool{}
	for _, diagnostic := range diagnostics {
		if !pythonVultureDiagnosticIsValid(known, diagnostic) {
			return fmt.Errorf("diagnostic is invalid")
		}
		key := pythonVultureDiagnosticIdentity(diagnostic)
		if seen[key] {
			return fmt.Errorf("diagnostic appears more than once")
		}
		seen[key] = true
	}
	return nil
}

func pythonVultureKnownFiles(files []string) map[string]bool {
	known := map[string]bool{}
	for _, path := range files {
		known[path] = true
	}
	return known
}

func pythonVultureDiagnosticIsValid(known map[string]bool, diagnostic pythonVultureDiagnostic) bool {
	return known[diagnostic.Path] && diagnostic.Line >= 1 && diagnostic.End >= diagnostic.Line && diagnostic.Name != "" &&
		len(diagnostic.Name) <= pythonStructuredMessageMaximumBytes && pythonVultureDiagnosticKind(diagnostic.Kind) &&
		diagnostic.Confidence >= 60 && diagnostic.Confidence <= 100 && strings.TrimSpace(diagnostic.Message) != "" &&
		len(diagnostic.Message) <= pythonStructuredMessageMaximumBytes
}

func pythonVultureDiagnosticIdentity(diagnostic pythonVultureDiagnostic) string {
	return strings.Join([]string{
		diagnostic.Path, strconv.Itoa(diagnostic.Line), strconv.Itoa(diagnostic.End), diagnostic.Name, diagnostic.Kind,
		strconv.Itoa(diagnostic.Confidence), diagnostic.Message,
	}, "\x00")
}

func pythonVultureDiagnosticKind(kind string) bool {
	return map[string]bool{
		"attribute": true, "class": true, "function": true, "import": true, "method": true,
		"property": true, "unreachable_code": true, "variable": true,
	}[kind]
}

func pythonVultureReferenceFindings(problems []pythonVultureProblem, origins map[string]pythonVultureReferenceOrigin) []policy.Finding {
	findings := make([]policy.Finding, 0, len(problems))
	for _, problem := range problems {
		origin := origins[problem.ID]
		findings = append(findings, policy.Finding{
			Check: origin.Check, Path: origin.Path, Line: origin.Line, Subject: origin.Subject,
			Message: origin.Message + problem.Message,
		})
	}
	return findings
}

func pythonVultureCoverage(files []string, message string) []policy.Finding {
	return pythonQualityCoverage(files, "quality.deadCodeCoverage", "vulture", message)
}

func pythonVultureSubject(diagnostic pythonVultureDiagnostic) string {
	identity := strings.Join([]string{
		diagnostic.Path, strconv.Itoa(diagnostic.Line), strconv.Itoa(diagnostic.End), diagnostic.Name, diagnostic.Kind,
		strconv.Itoa(diagnostic.Confidence), diagnostic.Message,
	}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	return "vulture:" + hex.EncodeToString(digest[:])
}

func pythonVultureMessage(diagnostic pythonVultureDiagnostic) string {
	line := strconv.Itoa(diagnostic.Line)
	if diagnostic.End != diagnostic.Line {
		line += "-" + strconv.Itoa(diagnostic.End)
	}
	return "lines " + line + ": " + diagnostic.Message + " (" + diagnostic.Kind + ", " + strconv.Itoa(diagnostic.Confidence) + "% confidence)"
}
