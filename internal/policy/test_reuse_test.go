package policy

import (
	"strings"
	"testing"
)

func TestReusableSuiteRequiresBoundedExecutionCapabilities(t *testing.T) {
	t.Parallel()
	configured := strings.Replace(minimalConfig(),
		`"argv":["go","test","./..."]`,
		`"reusable":true,"argv":["go","test","./..."],"exclusiveResources":["database"]`, 1)
	if _, err := Load(writeConfig(t, configured), ""); err == nil || !strings.Contains(err.Error(), "no external exclusive resources") {
		t.Fatalf("reusable external suite error = %v", err)
	}
}

func TestReusableAggregateCannotReplaceUnboundedSuite(t *testing.T) {
	t.Parallel()
	configured := strings.Replace(minimalConfig(), `"suites":[`, `"suites":[
    {"name":"aggregate","kind":"unit","scope":"repository","reusable":true,"argv":["go","test","./..."],"covers":["content-test"]},`, 1)
	if _, err := Load(writeConfig(t, configured), ""); err == nil || !strings.Contains(err.Error(), "cannot satisfy an unbounded suite") {
		t.Fatalf("reusable aggregate error = %v", err)
	}
}
