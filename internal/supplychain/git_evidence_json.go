package supplychain

import policyschema "github.com/riteofstring/code-polishy/schema"

var gitEnvelopeValidator = policyschema.NewValidator(policyschema.ConfigurationBase + "code-polishy-git-evidence.schema.json#/$defs/envelope")
var gitStatementValidator = policyschema.NewValidator(policyschema.ConfigurationBase + "code-polishy-git-evidence.schema.json#/$defs/statement")

func validateGitEvidenceJSON(data []byte, destination any) error {
	if err := policyschema.ValidateUniqueJSON(data, 16); err != nil {
		return gitEvidenceFailure("invalid", "Git evidence contains duplicate, excessively nested, or malformed JSON fields")
	}
	var validator *policyschema.Validator
	switch destination.(type) {
	case *gitEvidenceEnvelope:
		validator = gitEnvelopeValidator
	case *gitEvidenceStatement:
		validator = gitStatementValidator
	default:
		return gitEvidenceFailure("invalid", "Git evidence document contract is unsupported")
	}
	if err := validator.Validate(data); err != nil {
		return gitEvidenceFailure("invalid", "Git evidence does not match the shipped document schema")
	}
	return nil
}
