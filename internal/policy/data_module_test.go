package policy

import (
	"strings"
	"testing"
)

func TestDataModulePatternsRemainNarrowAndCannotHideControls(t *testing.T) {
	for _, pattern := range []string{"catalog.js", "catalog.mjs"} {
		text := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"data":["`+pattern+`"]},"quality":{}`, 1)
		if _, err := Load(writeConfig(t, text), ""); err != nil {
			t.Fatalf("data module %s rejected: %v", pattern, err)
		}
	}
	for _, pattern := range []string{"**/*.js", "eslint.config.mjs", ".github/actionlint.yml"} {
		text := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"data":["`+pattern+`"]},"quality":{}`, 1)
		if _, err := Load(writeConfig(t, text), ""); err == nil {
			t.Fatalf("control input %s admitted as data", pattern)
		}
	}
}
