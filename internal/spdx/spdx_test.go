package spdx

import (
	"strings"
	"testing"
)

func TestExpressionsUseOfficialIdentifiersAndPolicyPrecedence(t *testing.T) {
	allowed := map[string]bool{
		"mit": true,
		"gpl-2.0-only+ with classpath-exception-2.0": true,
		"licenseref-contained":                       true,
		"nunit":                                      true,
	}
	accepted := []string{
		"mit",
		"MIT OR Apache-2.0 AND BSD-3-Clause",
		"GPL-2.0-only+ WITH Classpath-exception-2.0",
		"LicenseRef-contained",
		"Nunit",
	}
	for _, value := range accepted {
		admitted, err := Admitted(value, allowed)
		if err != nil || !admitted {
			t.Errorf("Admitted(%q) = %v, %v", value, admitted, err)
		}
	}
	rejected := []string{
		"(MIT OR Apache-2.0) AND BSD-3-Clause",
		"Apache-2.0 WITH Classpath-exception-2.0",
	}
	for _, value := range rejected {
		admitted, err := Admitted(value, allowed)
		if err != nil || admitted {
			t.Errorf("Admitted(%q) = %v, %v", value, admitted, err)
		}
	}
}

func TestExpressionsFailClosedAtGrammarAndResourceBoundaries(t *testing.T) {
	values := []string{
		"Future-Live-List-License",
		"MIT with Classpath-exception-2.0",
		"MIT WITH Future-exception",
		"MIT OR",
		"(MIT",
		"DocumentRef-external:LicenseRef-custom",
		strings.Repeat("(", maxDepth+2) + "MIT" + strings.Repeat(")", maxDepth+2),
		strings.Repeat("MIT OR ", maxTokens) + "MIT",
		strings.Repeat("M", maxExpressionBytes+1),
	}
	for _, value := range values {
		if _, err := Parse(value); err == nil {
			t.Errorf("Parse(%q) succeeded", value)
		}
	}
}

func TestAllowedLicensesAreSingleTerms(t *testing.T) {
	for _, value := range []string{"MIT", "GPL-2.0-only WITH Classpath-exception-2.0", "LicenseRef-local"} {
		if err := ValidateAllowed(value); err != nil {
			t.Errorf("ValidateAllowed(%q): %v", value, err)
		}
	}
	for _, value := range []string{"MIT OR Apache-2.0", "Unknown-License", "MIT WITH Unknown-exception"} {
		if err := ValidateAllowed(value); err == nil {
			t.Errorf("ValidateAllowed(%q) succeeded", value)
		}
	}
}
