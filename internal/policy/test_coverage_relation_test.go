package policy

import (
	"strings"
	"testing"
)

func TestTestSuiteCoversRequiresOneAcyclicCompatibleOwner(t *testing.T) {
	t.Parallel()
	valid := strings.Replace(minimalConfig(), `"tests":{"suites":[`, `"tests":{"suites":[
    {"name":"aggregate","kind":"unit","scope":"repository","cost":"quick","argv":["go","test","./..."],"covers":["content-test"]},`, 1)
	config, err := Load(writeConfig(t, valid), "")
	if err != nil || len(config.Tests.Suites[0].Covers) != 1 {
		t.Fatalf("config = %+v, error = %v", config.Tests.Suites, err)
	}
	for name, mutation := range map[string]func(string) string{
		"unknown": func(value string) string { return strings.Replace(value, `"content-test"]`, `"missing"]`, 1) },
		"cycle": func(value string) string {
			return strings.Replace(value, `"argv":["go","test","./..."]}`, `"argv":["go","test","./..."],"covers":["aggregate"]}`, 1)
		},
		"weaker timeout": func(value string) string {
			return strings.Replace(value, `"covers":["content-test"]`, `"timeoutSeconds":901,"covers":["content-test"]`, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, loadErr := Load(writeConfig(t, mutation(valid)), ""); loadErr == nil {
				t.Fatalf("accepted invalid covers relation %s", name)
			}
		})
	}
}
