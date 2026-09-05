package policy

import (
	"fmt"

	policyschema "github.com/riteofstring/code-polishy/schema"
)

var runtimeValidator = policyschema.NewValidator(policyschema.ConfigurationBase + "code-polishy.schema.json")

func validateRuntimeSchema(data []byte) error {
	if err := runtimeValidator.Validate(data); err != nil {
		return fmt.Errorf("configuration does not match shipped schema: %w", err)
	}
	return nil
}
