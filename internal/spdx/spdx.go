package spdx

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
)

const (
	FactIdentity       = "spdx-license-list/v1"
	maxExpressionBytes = 4096
	maxTokens          = 512
	maxDepth           = 64
)

//go:embed data/licenses.json
var licensesJSON []byte

//go:embed data/exceptions.json
var exceptionsJSON []byte

//go:embed data/manifest.json
var manifestJSON []byte

type SnapshotIdentity struct {
	FactIdentity     string `json:"factIdentity"`
	Version          string `json:"version"`
	Tag              string `json:"tag"`
	Commit           string `json:"commit"`
	ReleaseDate      string `json:"releaseDate"`
	Source           string `json:"source"`
	LicensesSHA256   string `json:"licensesSha256"`
	ExceptionsSHA256 string `json:"exceptionsSha256"`
}

type licenseDocument struct {
	LicenseListVersion string          `json:"licenseListVersion"`
	Licenses           []licenseRecord `json:"licenses"`
	ReleaseDate        string          `json:"releaseDate"`
}

type exceptionDocument struct {
	LicenseListVersion string            `json:"licenseListVersion"`
	Exceptions         []exceptionRecord `json:"exceptions"`
	ReleaseDate        string            `json:"releaseDate"`
}

type licenseRecord struct {
	Reference       string   `json:"reference"`
	Deprecated      bool     `json:"isDeprecatedLicenseId"`
	FSFLibre        bool     `json:"isFsfLibre"`
	DetailsURL      string   `json:"detailsUrl"`
	ReferenceNumber int      `json:"referenceNumber"`
	Name            string   `json:"name"`
	ID              string   `json:"licenseId"`
	SeeAlso         []string `json:"seeAlso"`
	OSIApproved     bool     `json:"isOsiApproved"`
}

type exceptionRecord struct {
	Reference       string   `json:"reference"`
	Deprecated      bool     `json:"isDeprecatedLicenseId"`
	DetailsURL      string   `json:"detailsUrl"`
	ReferenceNumber int      `json:"referenceNumber"`
	Name            string   `json:"name"`
	ID              string   `json:"licenseExceptionId"`
	SeeAlso         []string `json:"seeAlso"`
}

type snapshot struct {
	identity   SnapshotIdentity
	licenses   map[string]string
	exceptions map[string]string
}

var (
	loaded     snapshot
	loadErr    error
	loadOnce   sync.Once
	identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*\+?$`)
	licenseRef = regexp.MustCompile(`^LicenseRef-[A-Za-z0-9][A-Za-z0-9.-]*$`)
)

func Identity() (SnapshotIdentity, error) {
	data, err := authoritativeSnapshot()
	return data.identity, err
}

func LicenseIDs() ([]string, error) {
	data, err := authoritativeSnapshot()
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(data.licenses))
	for _, value := range data.licenses {
		result = append(result, value)
	}
	sortStrings(result)
	return result, nil
}

func ExceptionIDs() ([]string, error) {
	data, err := authoritativeSnapshot()
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(data.exceptions))
	for _, value := range data.exceptions {
		result = append(result, value)
	}
	sortStrings(result)
	return result, nil
}

func authoritativeSnapshot() (snapshot, error) {
	loadOnce.Do(loadSnapshot)
	return loaded, loadErr
}

func loadSnapshot() {
	loaded, loadErr = readSnapshot()
}

func readSnapshot() (snapshot, error) {
	identity, err := readSnapshotIdentity()
	if err != nil {
		return snapshot{}, err
	}
	licenses, exceptions, err := readSnapshotDocuments(identity)
	if err != nil {
		return snapshot{}, err
	}
	licenseIndex, err := indexRecords(licenses.Licenses, func(record licenseRecord) string { return record.ID })
	if err != nil {
		return snapshot{}, err
	}
	exceptionIndex, err := indexRecords(exceptions.Exceptions, func(record exceptionRecord) string { return record.ID })
	if err != nil {
		return snapshot{}, err
	}
	return snapshot{identity: identity, licenses: licenseIndex, exceptions: exceptionIndex}, nil
}

func readSnapshotIdentity() (SnapshotIdentity, error) {
	var identity SnapshotIdentity
	if err := decodeStrict(manifestJSON, &identity); err != nil {
		return SnapshotIdentity{}, fmt.Errorf("SPDX snapshot manifest: %w", err)
	}
	if identity.FactIdentity != FactIdentity || identity.Version == "" || identity.Tag != "v"+identity.Version || len(identity.Commit) != 40 {
		return SnapshotIdentity{}, errors.New("SPDX snapshot manifest has an invalid identity")
	}
	if digest(licensesJSON) != identity.LicensesSHA256 || digest(exceptionsJSON) != identity.ExceptionsSHA256 {
		return SnapshotIdentity{}, errors.New("SPDX snapshot data does not match its manifest")
	}
	return identity, nil
}

func readSnapshotDocuments(identity SnapshotIdentity) (licenseDocument, exceptionDocument, error) {
	var licenses licenseDocument
	var exceptions exceptionDocument
	if err := decodeStrict(licensesJSON, &licenses); err != nil {
		return licenseDocument{}, exceptionDocument{}, fmt.Errorf("SPDX license data: %w", err)
	}
	if err := decodeStrict(exceptionsJSON, &exceptions); err != nil {
		return licenseDocument{}, exceptionDocument{}, fmt.Errorf("SPDX exception data: %w", err)
	}
	if licenses.LicenseListVersion != identity.Version || exceptions.LicenseListVersion != identity.Version || licenses.ReleaseDate != identity.ReleaseDate || exceptions.ReleaseDate != identity.ReleaseDate {
		return licenseDocument{}, exceptionDocument{}, errors.New("SPDX snapshot documents do not reconcile with their manifest")
	}
	return licenses, exceptions, nil
}

func indexRecords[T any](records []T, id func(T) string) (map[string]string, error) {
	if len(records) == 0 || len(records) > 10000 {
		return nil, errors.New("SPDX snapshot has an invalid record count")
	}
	result := make(map[string]string, len(records))
	for _, record := range records {
		value := id(record)
		key := strings.ToLower(value)
		if !identifier.MatchString(value) || result[key] != "" {
			return nil, fmt.Errorf("SPDX snapshot has an invalid or duplicate identifier %q", value)
		}
		result[key] = value
	}
	return result, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSON contains trailing data")
	}
	return nil
}

func digest(data []byte) string {
	value := sha256.Sum256(data)
	return hex.EncodeToString(value[:])
}

type Expression struct {
	root *expressionNode
}

type expressionNode struct {
	kind  string
	value string
	left  *expressionNode
	right *expressionNode
}

func Parse(value string) (Expression, error) {
	data, err := authoritativeSnapshot()
	if err != nil {
		return Expression{}, err
	}
	if len(value) == 0 || len(value) > maxExpressionBytes || strings.TrimSpace(value) != value {
		return Expression{}, errors.New("SPDX expression has an invalid size or surrounding whitespace")
	}
	tokens, err := tokenize(value)
	if err != nil {
		return Expression{}, err
	}
	parser := expressionParser{tokens: tokens, snapshot: data}
	root, err := parser.disjunction(0)
	if err != nil {
		return Expression{}, err
	}
	if parser.position != len(tokens) {
		return Expression{}, fmt.Errorf("unexpected SPDX token %q", tokens[parser.position])
	}
	return Expression{root: root}, nil
}

func ValidateAllowed(value string) error {
	expression, err := Parse(value)
	if err != nil {
		return err
	}
	if expression.root.kind != "license" {
		return errors.New("allowed license must be one SPDX license term, optionally qualified with WITH")
	}
	return nil
}

func Admitted(value string, allowed map[string]bool) (bool, error) {
	expression, err := Parse(value)
	if err != nil {
		return false, err
	}
	return expression.root.admitted(allowed), nil
}

func (node *expressionNode) admitted(allowed map[string]bool) bool {
	switch node.kind {
	case "and":
		return node.left.admitted(allowed) && node.right.admitted(allowed)
	case "or":
		return node.left.admitted(allowed) || node.right.admitted(allowed)
	default:
		return allowed[strings.ToLower(node.value)]
	}
}

func tokenize(value string) ([]string, error) {
	tokens := make([]string, 0, 16)
	for position := 0; position < len(value); {
		if spdxWhitespace(value[position]) {
			position++
			continue
		}
		token, next := spdxToken(value, position)
		tokens = append(tokens, token)
		position = next
		if len(tokens) > maxTokens {
			return nil, errors.New("SPDX expression exceeds the token limit")
		}
	}
	if len(tokens) == 0 {
		return nil, errors.New("SPDX expression is empty")
	}
	return tokens, nil
}

func spdxWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r'
}

func spdxToken(value string, position int) (string, int) {
	if value[position] == '(' || value[position] == ')' {
		return value[position : position+1], position + 1
	}
	start := position
	for position < len(value) && !strings.ContainsRune(" \t\r\n()", rune(value[position])) {
		position++
	}
	return value[start:position], position
}

type expressionParser struct {
	tokens   []string
	position int
	snapshot snapshot
}

func (parser *expressionParser) disjunction(depth int) (*expressionNode, error) {
	left, err := parser.conjunction(depth)
	if err != nil {
		return nil, err
	}
	for parser.accept("OR") {
		right, err := parser.conjunction(depth)
		if err != nil {
			return nil, err
		}
		left = &expressionNode{kind: "or", left: left, right: right}
	}
	return left, nil
}

func (parser *expressionParser) conjunction(depth int) (*expressionNode, error) {
	left, err := parser.term(depth)
	if err != nil {
		return nil, err
	}
	for parser.accept("AND") {
		right, err := parser.term(depth)
		if err != nil {
			return nil, err
		}
		left = &expressionNode{kind: "and", left: left, right: right}
	}
	return left, nil
}

func (parser *expressionParser) term(depth int) (*expressionNode, error) {
	if depth > maxDepth {
		return nil, errors.New("SPDX expression exceeds the nesting limit")
	}
	if parser.accept("(") {
		node, err := parser.disjunction(depth + 1)
		if err != nil {
			return nil, err
		}
		if !parser.accept(")") {
			return nil, errors.New("SPDX expression has an unclosed group")
		}
		return node, nil
	}
	if parser.position == len(parser.tokens) {
		return nil, errors.New("SPDX expression is missing a license")
	}
	raw := parser.tokens[parser.position]
	parser.position++
	canonical, err := parser.license(raw)
	if err != nil {
		return nil, err
	}
	if parser.accept("WITH") {
		if parser.position == len(parser.tokens) {
			return nil, errors.New("SPDX WITH is missing an exception")
		}
		exception, found := parser.snapshot.exceptions[strings.ToLower(parser.tokens[parser.position])]
		if !found {
			return nil, fmt.Errorf("%q is not in the pinned SPDX exception list", parser.tokens[parser.position])
		}
		parser.position++
		canonical += " WITH " + exception
	}
	return &expressionNode{kind: "license", value: canonical}, nil
}

func (parser *expressionParser) license(raw string) (string, error) {
	if licenseRef.MatchString(raw) {
		return raw, nil
	}
	if canonical, found := parser.snapshot.licenses[strings.ToLower(raw)]; found {
		return canonical, nil
	}
	plus := strings.HasSuffix(raw, "+")
	identifier := strings.TrimSuffix(raw, "+")
	canonical, found := parser.snapshot.licenses[strings.ToLower(identifier)]
	if !found {
		return "", fmt.Errorf("%q is not in the pinned SPDX license list", identifier)
	}
	if plus {
		canonical += "+"
	}
	return canonical, nil
}

func (parser *expressionParser) accept(value string) bool {
	if parser.position >= len(parser.tokens) || parser.tokens[parser.position] != value {
		return false
	}
	parser.position++
	return true
}

func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for position := index; position > 0 && values[position] < values[position-1]; position-- {
			values[position], values[position-1] = values[position-1], values[position]
		}
	}
}
