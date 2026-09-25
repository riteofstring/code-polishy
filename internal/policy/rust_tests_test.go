package policy

import (
	"strings"
	"testing"
)

func TestRustSuiteConfigurationPreservesCargoSelection(t *testing.T) {
	t.Parallel()
	input := strings.Replace(minimalConfig(), `"argv":["go","test","./..."]`, `"rust":{"packages":["core_crate"],"features":["core_crate/serde"],"filter":"test(behavior)","noDefaultFeatures":true},"environment":["RUST_LOG","RUST_BACKTRACE"]`, 1)
	config, err := Parse([]byte(input), ConfigFilename)
	if err != nil {
		t.Fatal(err)
	}
	suite := config.Tests.Suites[0]
	if suite.Rust.Manifest != "Cargo.toml" || suite.Cwd != "." || !suite.Rust.NoDefaultFeatures || suite.Rust.Filter != "test(behavior)" || len(suite.Argv) != 0 {
		t.Fatalf("Cargo selector changed: %+v", suite)
	}
	artifacts := suite.ArtifactDeclarations()
	if len(artifacts) != 1 || artifacts[0].Type != NextestJUnitArtifact || !artifacts[0].Required {
		t.Fatalf("native test evidence is not required: %+v", artifacts)
	}
	copy := suite.Rust.Clone()
	copy.Packages[0] = "changed"
	copy.Features[0] = "changed"
	if suite.Rust.Packages[0] != "core_crate" || suite.Rust.Features[0] != "core_crate/serde" {
		t.Fatal("mutating copied selection changed the original")
	}
}

func TestRustSuiteConfigurationRejectsCommandAndEvidenceOverrides(t *testing.T) {
	t.Parallel()
	for name, selector := range map[string]string{
		"two runners":            `"rust":{},"argv":["cargo","test"]`,
		"no runner":              `"environment":[]`,
		"retry replacement":      `"rust":{},"retryArgv":["true"]`,
		"cwd replacement":        `"rust":{},"cwd":"crate"`,
		"escaped manifest":       `"rust":{"manifest":"../Cargo.toml"}`,
		"wrong manifest":         `"rust":{"manifest":"Cargo.lock"}`,
		"flags as package":       `"rust":{"packages":["--workspace"]}`,
		"joined features":        `"rust":{"features":["one two"]}`,
		"nul filter":             `"rust":{"filter":"test(\u0000)"}`,
		"ambient flags":          `"rust":{},"environment":["RUSTFLAGS"]`,
		"ambient retry":          `"rust":{},"environment":["NEXTEST_RETRIES"]`,
		"report override":        `"rust":{},"artifacts":[{"path":"nextest.xml","type":"junit"}]`,
		"doctest Nextest filter": `"rust":{"doctests":true,"filter":"test(behavior)"}`,
	} {
		t.Run(name, func(t *testing.T) {
			input := strings.Replace(minimalConfig(), `"argv":["go","test","./..."]`, selector, 1)
			if _, err := Parse([]byte(input), ConfigFilename); err == nil {
				t.Fatalf("unsafe Rust suite accepted: %s", selector)
			}
		})
	}
}

func TestRustDoctestConfigurationSelectsDistinctEvidence(t *testing.T) {
	t.Parallel()
	input := strings.Replace(minimalConfig(), `"argv":["go","test","./..."]`, `"rust":{"doctests":true,"packages":["core"]}`, 1)
	config, err := Parse([]byte(input), ConfigFilename)
	if err != nil {
		t.Fatal(err)
	}
	suite := config.Tests.Suites[0]
	artifacts := suite.ArtifactDeclarations()
	if !suite.Rust.Doctests || len(artifacts) != 1 || artifacts[0].Type != RustdocArtifact || !artifacts[0].Required {
		t.Fatalf("doctest selection lost native evidence: %+v, %+v", suite, artifacts)
	}
	normal := suite.Rust.Clone()
	normal.Doctests = false
	if normal.Equal(suite.Rust) || !suite.Rust.Equal(suite.Rust.Clone()) {
		t.Fatal("doctests and unit tests have indistinguishable selection")
	}
}

func TestRustSuiteConfigurationRetainsCargoIdentifierConventions(t *testing.T) {
	t.Parallel()
	input := strings.Replace(minimalConfig(), `"argv":["go","test","./..."]`, `"rust":{"packages":["core_crate@1.2.3"],"features":["std+alloc","codec.v2","unicode_λ"]}`, 1)
	config, err := Parse([]byte(input), ConfigFilename)
	if err != nil {
		t.Fatal(err)
	}
	settings := config.Tests.Suites[0].Rust
	if settings.Packages[0] != "core_crate@1.2.3" || len(settings.Features) != 3 || settings.Features[2] != "unicode_λ" {
		t.Fatalf("Cargo selectors were rewritten: %+v", settings)
	}
}
