package engine

import (
	"bytes"
	"fmt"
	"sync"

	policyschema "github.com/riteofstring/code-polishy/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type reportSchemaValidator struct {
	once   sync.Once
	schema *jsonschema.Schema
	err    error
}

var canonicalReportValidator reportSchemaValidator
var sarifReportValidator reportSchemaValidator

func validateCanonicalReport(data []byte) error {
	return canonicalReportValidator.validate(ReportSchemaURL, policyschema.CodePolishyReport, data)
}

func validateSARIFReport(data []byte) error {
	return sarifReportValidator.validate(sarifSchema, policyschema.SARIF210, data)
}

func (validator *reportSchemaValidator) validate(url string, schemaData, documentData []byte) error {
	validator.once.Do(func() {
		compiler := jsonschema.NewCompiler()
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaData))
		if err != nil {
			validator.err = err
			return
		}
		if err := compiler.AddResource(url, document); err != nil {
			validator.err = err
			return
		}
		validator.schema, validator.err = compiler.Compile(url)
	})
	if validator.err != nil {
		return fmt.Errorf("compile report schema: %w", validator.err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(documentData))
	if err != nil {
		return fmt.Errorf("decode report document: %w", err)
	}
	if err := validator.schema.Validate(document); err != nil {
		return fmt.Errorf("report document does not match its schema: %w", err)
	}
	return nil
}
