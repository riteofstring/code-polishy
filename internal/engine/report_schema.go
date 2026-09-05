package engine

import policyschema "github.com/riteofstring/code-polishy/schema"

var canonicalReportValidator = policyschema.NewValidator(ReportSchemaURL)
var sarifReportValidator = policyschema.NewValidator(sarifSchema)

func validateCanonicalReport(data []byte) error {
	return canonicalReportValidator.Validate(data)
}

func validateSARIFReport(data []byte) error {
	return sarifReportValidator.Validate(data)
}
