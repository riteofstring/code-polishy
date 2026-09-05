package behaviorreview

import (
	"fmt"

	policyschema "github.com/riteofstring/code-polishy/schema"
)

const architectureSchemaURL = policyschema.ConfigurationBase + "code-polishy-architecture-review.schema.json#/$defs/"

var architecturePacketValidator = policyschema.NewValidator(architectureSchemaURL + "packet")
var architectureBindingValidator = policyschema.NewValidator(architectureSchemaURL + "binding")
var architectureReceiptValidator = policyschema.NewValidator(architectureSchemaURL + "receipt")
var architectureResultValidator = policyschema.NewValidator(architectureSchemaURL + "result")

func validateArchitectureJSON(data []byte, destination any) error {
	var validator *policyschema.Validator
	switch destination.(type) {
	case *architecturePacket:
		validator = architecturePacketValidator
	case *architectureBinding:
		validator = architectureBindingValidator
	case *architectureReceipt:
		validator = architectureReceiptValidator
	case *ArchitectureReviewResult:
		validator = architectureResultValidator
	default:
		return fmt.Errorf("unsupported architecture evidence document")
	}
	return validator.Validate(data)
}
