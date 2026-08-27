package artifactsecurity

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

const openVEXContext = "https://openvex.dev/ns/v0.2.0"

var allowedOpenVEXJustifications = []string{
	"component_not_present", "vulnerable_code_not_present", "vulnerable_code_not_in_execute_path",
	"vulnerable_code_cannot_be_controlled_by_adversary", "inline_mitigations_already_exist",
}

type openVEXDocument struct {
	Context    string             `json:"@context"`
	ID         string             `json:"@id"`
	Author     string             `json:"author"`
	Timestamp  time.Time          `json:"timestamp"`
	Version    int                `json:"version"`
	Statements []openVEXStatement `json:"statements"`
}

type openVEXStatement struct {
	Vulnerability struct {
		Name string `json:"name"`
	} `json:"vulnerability"`
	Products []struct {
		ID string `json:"@id"`
	} `json:"products"`
	Status          string `json:"status"`
	Justification   string `json:"justification"`
	ImpactStatement string `json:"impact_statement"`
}

type openVEXAssessment struct {
	Advisory        string
	Product         string
	Justification   string
	ImpactStatement string
}

func parseOpenVEX(path string, now time.Time) (map[string]openVEXAssessment, error) {
	payload, err := readBoundedRegularFile(path, 64*1024)
	if err != nil {
		return nil, err
	}
	document, err := decodeOpenVEX(payload)
	if err != nil {
		return nil, err
	}
	if err := validateOpenVEXDocument(document, now); err != nil {
		return nil, err
	}
	assessments := map[string]openVEXAssessment{}
	for _, statement := range document.Statements {
		if err := appendOpenVEXStatement(assessments, statement); err != nil {
			return nil, err
		}
	}
	return assessments, nil
}

func decodeOpenVEX(payload []byte) (openVEXDocument, error) {
	var document openVEXDocument
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return openVEXDocument{}, fmt.Errorf("decode OpenVEX document: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return openVEXDocument{}, err
	}
	return document, nil
}

func validateOpenVEXDocument(document openVEXDocument, now time.Time) error {
	if document.Context != openVEXContext {
		return errors.New("OpenVEX document context is invalid")
	}
	if document.Version < 1 {
		return errors.New("OpenVEX document version is invalid")
	}
	if strings.TrimSpace(document.Author) == "" {
		return errors.New("OpenVEX document author is missing")
	}
	if document.Timestamp.IsZero() || document.Timestamp.After(now.Add(5*time.Minute)) {
		return errors.New("OpenVEX document timestamp is invalid")
	}
	identifier, err := url.Parse(document.ID)
	if err != nil || identifier.Scheme != "https" || identifier.Host == "" {
		return errors.New("OpenVEX @id must be an absolute HTTPS identity")
	}
	if len(document.Statements) == 0 || len(document.Statements) > 256 {
		return errors.New("OpenVEX document must contain 1-256 bounded statements")
	}
	return nil
}

func appendOpenVEXStatement(assessments map[string]openVEXAssessment, statement openVEXStatement) error {
	advisory := strings.TrimSpace(statement.Vulnerability.Name)
	if err := validateOpenVEXStatement(statement, advisory); err != nil {
		return err
	}
	for _, product := range statement.Products {
		if !validPURL(product.ID) {
			return fmt.Errorf("OpenVEX statement %s product %q is not an exact package PURL", advisory, product.ID)
		}
		key := openVEXKey(advisory, product.ID)
		if _, exists := assessments[key]; exists {
			return fmt.Errorf("OpenVEX document repeats %s", key)
		}
		assessments[key] = openVEXAssessment{
			Advisory: advisory, Product: product.ID, Justification: statement.Justification,
			ImpactStatement: statement.ImpactStatement,
		}
	}
	return nil
}

func validateOpenVEXStatement(statement openVEXStatement, advisory string) error {
	if !validAdvisoryID(advisory) {
		return fmt.Errorf("OpenVEX statement %q has an invalid advisory", advisory)
	}
	if statement.Status != "not_affected" {
		return fmt.Errorf("OpenVEX statement %q must be not_affected", advisory)
	}
	if !slices.Contains(allowedOpenVEXJustifications, statement.Justification) {
		return fmt.Errorf("OpenVEX statement %q has an unsupported justification", advisory)
	}
	if len(statement.ImpactStatement) < 20 || len(statement.ImpactStatement) > 2048 {
		return fmt.Errorf("OpenVEX statement %q has an invalid impact statement length", advisory)
	}
	if strings.TrimSpace(statement.ImpactStatement) != statement.ImpactStatement {
		return fmt.Errorf("OpenVEX statement %q impact statement has surrounding whitespace", advisory)
	}
	if len(statement.Products) == 0 {
		return fmt.Errorf("OpenVEX statement %q has no exact products", advisory)
	}
	return nil
}

func reconcileOpenVEX(
	observed []normalizedFinding,
	enforced []normalizedFinding,
	assessments map[string]openVEXAssessment,
) ([]normalizedFinding, []acceptedFinding, error) {
	remaining := normalizedFindingCounts(enforced)
	used := map[string]bool{}
	accepted := []acceptedFinding{}
	for _, finding := range observed {
		identity := normalizedFindingKey(finding)
		if remaining[identity] > 0 {
			remaining[identity]--
			continue
		}
		if finding.Kind != "vulnerability" || finding.PackagePURL == "" {
			return nil, nil, fmt.Errorf("OpenVEX suppressed non-vulnerability finding %s/%s", finding.Kind, finding.ID)
		}
		key := openVEXKey(finding.ID, finding.PackagePURL)
		assessment, exists := assessments[key]
		if !exists {
			return nil, nil, fmt.Errorf("OpenVEX suppressed unassessed finding %s", key)
		}
		used[key] = true
		accepted = append(accepted, acceptedFinding{
			Finding: finding, Justification: assessment.Justification, ImpactStatement: assessment.ImpactStatement,
		})
	}
	for _, key := range sortedFindingKeys(remaining) {
		if remaining[key] != 0 {
			return nil, nil, fmt.Errorf("OpenVEX enforcement introduced or changed finding %q", key)
		}
	}
	for key := range assessments {
		if !used[key] {
			return nil, nil, fmt.Errorf("OpenVEX assessment %q did not match an observed suppression", key)
		}
	}
	blocking := append([]normalizedFinding{}, enforced...)
	sortNormalizedFindings(blocking)
	slices.SortFunc(accepted, func(left, right acceptedFinding) int {
		return strings.Compare(normalizedFindingKey(left.Finding), normalizedFindingKey(right.Finding))
	})
	return blocking, accepted, nil
}

func validAdvisoryID(value string) bool {
	if len(value) < 8 || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	return strings.HasPrefix(value, "CVE-") || strings.HasPrefix(value, "GHSA-") || strings.HasPrefix(value, "OSV-")
}

func validPURL(value string) bool {
	if len(value) < len("pkg:a/b@1") || len(value) > 1024 || !strings.HasPrefix(value, "pkg:") || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	at := strings.LastIndex(value, "@")
	return at > len("pkg:") && at < len(value)-1
}

func openVEXKey(advisory, product string) string {
	return advisory + "\x00" + product
}
