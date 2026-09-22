package pack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/riteofstring/code-polishy/schema"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

const ContractValidationProtocol = "code-polishy-pack-validation/v1"
const ContractValidationSchema = schema.ConfigurationBase + "code-polishy-pack-validation-v1.schema.json"

const maximumContractManifestBytes = 16 << 20

type ContractValidationOptions struct {
	Kind        string
	InputPath   string
	RequestPath string
}

type ContractValidationReport struct {
	Schema    string                     `json:"$schema"`
	Protocol  string                     `json:"protocol"`
	Status    string                     `json:"status"`
	Documents []ContractDocumentIdentity `json:"documents"`
	Errors    []ContractValidationIssue  `json:"errors"`
}

type ContractDocumentIdentity struct {
	Role   string `json:"role"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ContractValidationIssue struct {
	Document string `json:"document"`
	Phase    string `json:"phase"`
	Path     string `json:"path"`
	Expected string `json:"expected,omitempty"`
	Message  string `json:"message"`
}

type contractDocument struct {
	identity ContractDocumentIdentity
	data     []byte
}

func ValidateContract(options ContractValidationOptions) (ContractValidationReport, error) {
	if err := validateContractOptions(options); err != nil {
		return ContractValidationReport{}, err
	}
	report := ContractValidationReport{
		Schema: ContractValidationSchema, Protocol: ContractValidationProtocol, Status: "passed",
		Documents: []ContractDocumentIdentity{}, Errors: []ContractValidationIssue{},
	}
	input, err := readContractDocument("input", options.Kind, options.InputPath, contractDocumentLimit(options.Kind))
	if err != nil {
		return ContractValidationReport{}, err
	}
	report.Documents = append(report.Documents, input.identity)
	documents, issues, err := validateContractInput(options, input)
	if err != nil {
		return ContractValidationReport{}, err
	}
	report.Documents = append(report.Documents, documents...)
	report.Errors = append(report.Errors, issues...)
	if len(report.Errors) != 0 {
		report.Status = "failed"
	}
	if err := validateContractReport(report); err != nil {
		return ContractValidationReport{}, err
	}
	return report, nil
}

func validateContractOptions(options ContractValidationOptions) error {
	if options.Kind != "manifest" && options.Kind != "request" && options.Kind != "response" {
		return errors.New("kind must be manifest, request, or response")
	}
	if strings.TrimSpace(options.InputPath) == "" {
		return errors.New("input path is required")
	}
	if options.Kind == "response" && strings.TrimSpace(options.RequestPath) == "" {
		return errors.New("response validation requires a request path")
	}
	if options.Kind != "response" && options.RequestPath != "" {
		return errors.New("request path is accepted only for response validation")
	}
	return nil
}

func validateContractInput(options ContractValidationOptions, input contractDocument) ([]ContractDocumentIdentity, []ContractValidationIssue, error) {
	switch options.Kind {
	case "manifest":
		return nil, validateManifestDocument(input.data), nil
	case "request":
		_, issues := validateRequestDocument(input.data, "input")
		return nil, issues, nil
	default:
		return validateResponseContract(options.RequestPath, input.data)
	}
}

func validateResponseContract(requestPath string, responseData []byte) ([]ContractDocumentIdentity, []ContractValidationIssue, error) {
	requestDocument, err := readContractDocument("request", "request", requestPath, 64<<20)
	if err != nil {
		return nil, nil, err
	}
	request, issues := validateRequestDocument(requestDocument.data, "request")
	if len(issues) == 0 {
		issues = validateResponseDocument(responseData, request)
	}
	return []ContractDocumentIdentity{requestDocument.identity}, issues, nil
}

func contractDocumentLimit(kind string) int64 {
	switch kind {
	case "request":
		return 64 << 20
	case "response":
		return 16 << 20
	default:
		return maximumContractManifestBytes
	}
}

func readContractDocument(role, kind, name string, limit int64) (contractDocument, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return contractDocument{}, err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return contractDocument{}, err
	}
	file, err := os.Open(canonical)
	if err != nil {
		return contractDocument{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit {
		return contractDocument{}, fmt.Errorf("%s must be a regular file of 1 to %d bytes", role, limit)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return contractDocument{}, err
	}
	if int64(len(data)) > limit {
		return contractDocument{}, fmt.Errorf("%s exceeds %d bytes", role, limit)
	}
	return contractDocument{
		identity: ContractDocumentIdentity{Role: role, Kind: kind, Path: filepath.ToSlash(canonical), SHA256: inputDigest(data)},
		data:     data,
	}, nil
}

func validateManifestDocument(data []byte) []ContractValidationIssue {
	if err := schema.NewValidator(schema.ConfigurationBase + "code-polishy-pack.schema.json").Validate(data); err != nil {
		return contractIssues("input", "schema", err)
	}
	if _, err := ParseManifest(data, ManifestFilename); err != nil {
		return contractIssues("input", "semantic", err)
	}
	return []ContractValidationIssue{}
}

func validateRequestDocument(data []byte, document string) (Request, []ContractValidationIssue) {
	if err := schema.NewValidator(schema.ConfigurationBase + "code-polishy-pack-request-v4.schema.json").Validate(data); err != nil {
		return Request{}, contractIssues(document, "schema", err)
	}
	request := Request{}
	if err := decodeContractDocument(data, &request); err != nil {
		return Request{}, contractIssues(document, "decode", err)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return Request{}, contractIssues(document, "semantic", err)
	}
	if err := schema.NewValidator(schema.ConfigurationBase + "code-polishy-pack-request-v4.schema.json").Validate(encoded); err != nil {
		return Request{}, contractIssues(document, "semantic", err)
	}
	return request, []ContractValidationIssue{}
}

func validateResponseDocument(data []byte, request Request) []ContractValidationIssue {
	if err := schema.NewValidator(schema.ConfigurationBase + "code-polishy-pack-response-v4.schema.json").Validate(data); err != nil {
		return contractIssues("input", "schema", err)
	}
	response, err := decodeResponse(data)
	if err != nil {
		return contractIssues("input", "decode", err)
	}
	if err := validateResponse(response, request); err != nil {
		return contractIssues("input", "semantic", err)
	}
	return []ContractValidationIssue{}
}

func decodeContractDocument(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("document contains more than one JSON value")
		}
		return err
	}
	return nil
}

func contractIssues(document, phase string, err error) []ContractValidationIssue {
	var constraint constraintError
	if errors.As(err, &constraint) {
		return []ContractValidationIssue{{
			Document: document, Phase: phase, Path: constraint.path, Expected: constraint.constraint,
			Message: boundedContractMessage(err.Error()),
		}}
	}
	var validation *jsonschema.ValidationError
	if errors.As(err, &validation) {
		leaves := contractSchemaLeaves(validation, nil)
		issues := make([]ContractValidationIssue, 0, len(leaves))
		for _, leaf := range leaves {
			issues = append(issues, ContractValidationIssue{
				Document: document, Phase: phase, Path: contractFieldPath(leaf.InstanceLocation),
				Message: boundedContractMessage(leaf.Error()),
			})
		}
		return issues
	}
	return []ContractValidationIssue{{Document: document, Phase: phase, Path: "document", Message: boundedContractMessage(err.Error())}}
}

func contractSchemaLeaves(problem *jsonschema.ValidationError, result []*jsonschema.ValidationError) []*jsonschema.ValidationError {
	if len(result) >= 64 {
		return result
	}
	if len(problem.Causes) == 0 {
		return append(result, problem)
	}
	for _, cause := range problem.Causes {
		result = contractSchemaLeaves(cause, result)
		if len(result) >= 64 {
			break
		}
	}
	return result
}

func contractFieldPath(parts []string) string {
	if len(parts) == 0 {
		return "document"
	}
	var result strings.Builder
	for index, part := range parts {
		if _, err := strconv.Atoi(part); err == nil {
			fmt.Fprintf(&result, "[%s]", part)
			continue
		}
		if index > 0 {
			result.WriteByte('.')
		}
		if contractIdentifier(part) {
			result.WriteString(part)
		} else {
			encoded, _ := json.Marshal(part)
			result.WriteByte('[')
			result.Write(encoded)
			result.WriteByte(']')
		}
	}
	return result.String()
}

func contractIdentifier(value string) bool {
	for index, character := range value {
		if character == '_' || unicode.IsLetter(character) || index > 0 && unicode.IsDigit(character) {
			continue
		}
		return false
	}
	return value != ""
}

func boundedContractMessage(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 4096 {
		value = value[:4096]
	}
	if value == "" {
		return "validation failed"
	}
	return value
}

func validateContractReport(report ContractValidationReport) error {
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	if err := schema.NewValidator(ContractValidationSchema).Validate(data); err != nil {
		return fmt.Errorf("validate contract report: %w", err)
	}
	return nil
}
