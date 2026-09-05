package policy

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const schemaRejection = "does not match shipped schema"

func TestRuntimeSchemaRejectsStructureAcceptedByTypedDecoding(t *testing.T) {
	t.Parallel()
	configuration := strings.Replace(minimalConfig(), `{"version":4`, `{"$schema":"","version":4`, 1)
	_, err := Parse([]byte(configuration), ConfigFilename)
	if err == nil || !strings.Contains(err.Error(), schemaRejection) {
		t.Fatalf("error = %v", err)
	}
}

func TestRuntimeSchemaInputIsBoundedBeforeValidation(t *testing.T) {
	t.Parallel()
	if _, err := Parse(bytes.Repeat([]byte{' '}, maximumConfigurationBytes+1), ConfigFilename); err == nil || !strings.Contains(err.Error(), "byte size") {
		t.Fatalf("error = %v", err)
	}
}

func TestRuntimeSchemaOwnsConditionalBoundaries(t *testing.T) {
	t.Parallel()
	computed := `{"project":"pyproject.toml","importer":"loader.py","module":"loader","moduleScope":true,"callee":"importlib.import_module","line":1,"column":1,"shape":"call","argument":"name","sourceSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","namespace":"loader.plugins","targets":["loader.plugins.one"]}`
	computedConfig := func(declaration string) string {
		return strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"pythonComputedImports":[`+declaration+`]},"quality":{}`, 1)
	}
	artifactConfig := func(target string) string {
		return strings.Replace(minimalConfig(), `"supplyChain":{}`, `"supplyChain":{"artifactSecurity":{"targets":[`+target+`]}}`, 1)
	}
	vulnerability := `{"id":"accepted","ecosystem":"npm","advisory":"CVE-2026-1000","package":"example","affectedVersion":"1.2.3","scope":"package-lock.json","severity":"high","status":"not-affected","basis":"unreachable","reason":"not reachable","impact":"not shipped","evidence":"https://example.test/evidence","tracking":"https://example.test/tracking","owner":"runtime","approvedBy":"security","approval":"https://example.test/approval","reviewed":"2026-09-01","expires":"2026-09-30"}`
	vulnerabilityConfig := func(assessment string) string {
		return strings.Replace(minimalConfig(), `"supplyChain":{}`, `"supplyChain":{"vulnerabilityAssessments":[`+assessment+`]}`, 1)
	}
	cases := map[string]string{
		"computed callsite selector":   computedConfig(strings.Replace(computed, `"moduleScope":true`, `"moduleScope":true,"callable":"load"`, 1)),
		"computed target selector":     computedConfig(strings.Replace(computed, `"namespace":"loader.plugins"`, `"namespace":"loader.plugins","entryPointGroup":"plugins"`, 1)),
		"computed entry point fields":  computedConfig(strings.Replace(computed, `"namespace":"loader.plugins"`, `"entryPointGroup":"plugins"`, 1)),
		"design selector":              strings.Replace(minimalConfig(), `"checks":[]`, `"documentation":{"design":[{"path":"docs/design/content.md","module":"content","sourcePaths":["content/file.go"]}]},"checks":[]`, 1),
		"behavior review scope":        strings.Replace(minimalConfig(), `"checks":[]`, `"verification":{"behaviorReview":{"features":[{"name":"content","description":"Published content behavior.","suites":["content-test"]}]}},"checks":[]`, 1),
		"module suite ownership":       strings.Replace(minimalConfig(), `"modules":["content"]`, `"modules":[]`, 1),
		"repository suite ownership":   strings.Replace(minimalConfig(), `"scope":"repository","argv"`, `"scope":"repository","modules":["content"],"argv"`, 1),
		"focused suite hierarchy":      strings.Replace(minimalConfig(), `"modules":["content"],"argv"`, `"modules":["content"],"runOn":["focused","full"],"argv"`, 1),
		"recommended suite hierarchy":  strings.Replace(minimalConfig(), `"scope":"repository","argv"`, `"scope":"repository","runOn":["recommended"],"argv"`, 1),
		"supplemental suite isolation": strings.Replace(minimalConfig(), `"kind":"content","scope":"module"`, `"kind":"content","scope":"module","cost":"expensive","runOn":["full","supplemental"]`, 1),
		"supplemental evidence kind":   strings.Replace(minimalConfig(), `"kind":"content","scope":"module"`, `"kind":"mutation","scope":"module","cost":"expensive","runOn":["full"]`, 1),
		"dockerfile target":            artifactConfig(`{"name":"image","mode":"dockerfile","dockerfile":"Dockerfile"}`),
		"archive target":               artifactConfig(`{"name":"archive","mode":"archive","archive":"dist/app.tar","dockerfile":"Dockerfile"}`),
		"command target":               artifactConfig(`{"name":"binary","mode":"command"}`),
		"high vulnerability":           vulnerabilityConfig(strings.Replace(vulnerability, `"status":"not-affected","basis":"unreachable"`, `"status":"risk-accepted","basis":"mitigated"`, 1)),
		"not affected basis":           vulnerabilityConfig(strings.Replace(vulnerability, `"basis":"unreachable"`, `"basis":"mitigated"`, 1)),
		"risk accepted basis":          vulnerabilityConfig(strings.Replace(strings.Replace(vulnerability, `"severity":"high"`, `"severity":"moderate"`, 1), `"status":"not-affected"`, `"status":"risk-accepted"`, 1)),
		"enabled policy metadata":      strings.Replace(minimalConfig(), `"quality":{}`, `"policyModules":{"overrides":[{"name":"ruff","mode":"enabled","reason":"temporary"}]},"quality":{}`, 1),
		"disabled policy governance":   strings.Replace(minimalConfig(), `"quality":{}`, `"policyModules":{"overrides":[{"name":"ruff","mode":"disabled"}]},"quality":{}`, 1),
	}
	for name, configuration := range cases {
		name, configuration := name, configuration
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(configuration), ConfigFilename)
			if err == nil || !strings.Contains(err.Error(), schemaRejection) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRuntimeSchemaClosesEveryObjectBoundary(t *testing.T) {
	t.Parallel()
	configuration := `{
  "version":4,
  "project":{"kind":"content"},
  "scope":{
    "generatedJavaScript":[{"paths":["generated/**"],"sourcePackage":"package.json"}],
    "pythonDynamicReferences":[{"kind":"target","project":"pyproject.toml","target":{"module":"app.entry","symbol":"serve"},"consumer":{"kind":"callsite","importer":"loader.py","module":"loader","callable":"load","site":{"line":3,"column":1},"callee":"pkgutil.resolve_name","shape":"module-object-call/v1","argument":"name","sourceSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}],
    "pythonComputedImports":[{"project":"pyproject.toml","importer":"loader.py","module":"loader","moduleScope":true,"callee":"importlib.import_module","line":1,"column":1,"shape":"call","argument":"name","sourceSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","namespace":"loader.plugins","configuration":[{"path":"plugins.json","jsonPointer":"/plugins","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}],
    "pythonExternalAttributes":[{"project":"pyproject.toml","module":"app","callable":"load","receiver":{"kind":"parameter","name":"settings","binding":{"line":1,"column":10},"type":"external.Settings"},"attribute":"path","write":{"line":2,"column":5}}],
    "languages":[{"name":"elixir","paths":["lib/**/*.ex"]}]
  },
  "quality":{"complexity":{"go":10}},
  "portability":{"externalInputs":[{"name":"catalog","kind":"file","module":"content","sourcePaths":["catalog.json"],"resolution":["config"],"unavailableBehavior":"fail","contractSuite":"content-test","behaviorSuite":"full"}]},
  "documentation":{"design":[{"path":"docs/design/content.md","module":"content"}]},
  "verification":{"behaviorReview":{"features":[{"name":"content","description":"Published content behavior.","modules":["content"],"suites":["content-test"]}]},"mergeGate":{"recommendedModules":["content"]}},
  "packs":[{"name":"community","version":"1.2.3","digest":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}],
  "modules":[{"name":"content","paths":["content/**"]}],
  "checks":[{"name":"lint","provides":["lint"],"argv":["lint"]}],
  "tests":{"ownership":[],"suites":[{"name":"content-test","kind":"content","scope":"module","modules":["content"],"argv":["test"],"artifacts":[{"path":"junit.xml","type":"junit"}]},{"name":"full","kind":"content","scope":"repository","argv":["test"]}]},
  "supplyChain":{
    "releaseArtifacts":[{"name":"go","versionFile":"go.version","source":"go-toolchain"}],
    "vulnerabilityAssessments":[{"id":"accepted","ecosystem":"npm","advisory":"CVE-2026-1000","package":"example","affectedVersion":"1.2.3","scope":"package-lock.json","severity":"moderate","status":"risk-accepted","basis":"mitigated","reason":"bounded","impact":"development only","evidence":"https://example.test/evidence","tracking":"https://example.test/tracking","owner":"runtime","approvedBy":"security","approval":"https://example.test/approval","reviewed":"2026-09-01","expires":"2026-09-30"}],
    "releaseAgeAssessments":[{"id":"release","ecosystem":"npm","package":"example","version":"1.2.3","scope":"package-lock.json","category":"security-fix","evidence":"https://example.test/release","reason":"security fix","owner":"security","reviewed":"2026-09-01","expires":"2026-09-30"}],
    "dependencyOverridePolicies":[{"id":"override","ecosystem":"npm","path":"package.json","field":"overrides","contentSha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","reason":"single graph","owner":"runtime","reviewed":"2026-09-01","expires":"2026-09-30"}],
    "artifactSecurity":{"targets":[{"name":"binary","mode":"command","producer":{"argv":["build"]}}]}
  },
  "policyModules":{"overrides":[{"name":"ruff","mode":"disabled","reason":"fixture","owner":"quality","expires":"2026-09-30"}]},
  "exceptions":[{"id":"temporary","check":"quality.lines","path":"content/file.go","subject":"file","reason":"migration","owner":"content","expires":"2026-09-30"}]
}`
	if err := validateRuntimeSchema([]byte(configuration)); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	boundaries := map[string][]any{
		"root": {}, "project": {"project"}, "scope": {"scope"},
		"language": {"scope", "languages", 0}, "generated JavaScript": {"scope", "generatedJavaScript", 0},
		"dynamic reference": {"scope", "pythonDynamicReferences", 0}, "dynamic target": {"scope", "pythonDynamicReferences", 0, "target"}, "dynamic consumer": {"scope", "pythonDynamicReferences", 0, "consumer"}, "dynamic consumer site": {"scope", "pythonDynamicReferences", 0, "consumer", "site"}, "computed import": {"scope", "pythonComputedImports", 0},
		"computed input": {"scope", "pythonComputedImports", 0, "configuration", 0}, "external attribute": {"scope", "pythonExternalAttributes", 0},
		"external receiver":         {"scope", "pythonExternalAttributes", 0, "receiver"},
		"external receiver binding": {"scope", "pythonExternalAttributes", 0, "receiver", "binding"},
		"external write location":   {"scope", "pythonExternalAttributes", 0, "write"},
		"quality":                   {"quality"}, "complexity": {"quality", "complexity"}, "portability": {"portability"},
		"external input": {"portability", "externalInputs", 0}, "documentation": {"documentation"}, "design document": {"documentation", "design", 0},
		"verification": {"verification"}, "behavior review": {"verification", "behaviorReview"},
		"behavior feature": {"verification", "behaviorReview", "features", 0}, "merge gate": {"verification", "mergeGate"},
		"pack": {"packs", 0}, "module": {"modules", 0}, "check": {"checks", 0}, "tests": {"tests"},
		"test suite": {"tests", "suites", 0}, "test artifact": {"tests", "suites", 0, "artifacts", 0},
		"supply chain": {"supplyChain"}, "release artifact": {"supplyChain", "releaseArtifacts", 0},
		"vulnerability assessment": {"supplyChain", "vulnerabilityAssessments", 0}, "release age assessment": {"supplyChain", "releaseAgeAssessments", 0},
		"dependency override": {"supplyChain", "dependencyOverridePolicies", 0}, "artifact security": {"supplyChain", "artifactSecurity"},
		"artifact target": {"supplyChain", "artifactSecurity", "targets", 0}, "artifact producer": {"supplyChain", "artifactSecurity", "targets", 0, "producer"},
		"policy modules": {"policyModules"}, "policy module override": {"policyModules", "overrides", 0}, "exception": {"exceptions", 0},
	}
	for name, path := range boundaries {
		name, path := name, path
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var candidate map[string]any
			if err := json.Unmarshal([]byte(configuration), &candidate); err != nil {
				t.Fatal(err)
			}
			runtimeSchemaObjectAt(t, candidate, path...)["unexpected"] = true
			encoded, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateRuntimeSchema(encoded); err == nil || !strings.Contains(err.Error(), schemaRejection) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func runtimeSchemaObjectAt(t testing.TB, document any, path ...any) map[string]any {
	t.Helper()
	current := document
	for _, part := range path {
		switch part := part.(type) {
		case string:
			current = current.(map[string]any)[part]
		case int:
			current = current.([]any)[part]
		default:
			t.Fatalf("unsupported path part %T", part)
		}
	}
	object, ok := current.(map[string]any)
	if !ok {
		t.Fatalf("path resolves to %T", current)
	}
	return object
}

func FuzzRuntimeSchemaBoundary(f *testing.F) {
	f.Add([]byte(minimalConfig()))
	f.Add([]byte(`{"version":4}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1024*1024 {
			return
		}
		_ = validateRuntimeSchema(data)
	})
}
