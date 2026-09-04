package policy

import (
	"strings"
	"testing"
)

func TestTestArtifactContractsLoadExactDeclarations(t *testing.T) {
	t.Parallel()
	configured := minimalConfig()
	configured = strings.Replace(configured, `"modules":["content"],"argv":["go","test","./..."]`, `"modules":["content"],"argv":["go","test","./..."],"artifacts":[{"path":"junit.xml","type":"junit","required":true}]`, 1)
	config, err := Parse([]byte(configured), ConfigFilename)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Tests.Suites[0].Artifacts) != 1 || !config.Tests.Suites[0].Artifacts[0].Required {
		t.Fatalf("loaded artifact contracts = %+v", config.Tests.Suites[0].Artifacts)
	}
}

func TestTestArtifactContractsRejectUnsafeDeclarations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, from, to, want string
	}{
		{name: "artifact traversal", from: `"argv":["go","test","./..."]`, to: `"argv":["go","test","./..."],"artifacts":[{"path":"../junit.xml","type":"junit"}]`, want: "stay inside"},
		{name: "artifact type", from: `"argv":["go","test","./..."]`, to: `"argv":["go","test","./..."],"artifacts":[{"path":"result.xml","type":"coverage"}]`, want: "unsupported value"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			candidate := strings.Replace(minimalConfig(), test.from, test.to, 1)
			if _, err := Parse([]byte(candidate), ConfigFilename); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}
