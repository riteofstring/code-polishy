package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMatchUsesSegmentAwareWildcards(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path, pattern string
		want          bool
	}{
		{"src/domain/file.ts", "src/domain/*", true},
		{"src/domain/nested/file.ts", "src/domain/*", false},
		{"src/domain/nested/file.ts", "src/domain/**", true},
		{"file.ts", "**/*.ts", true},
		{"deep/file.ts", "*.ts", false},
		{"deep/file.ts", "**/*.ts", true},
	}
	for _, test := range cases {
		if got := Match(test.path, test.pattern); got != test.want {
			t.Errorf("Match(%q, %q) = %v, want %v", test.path, test.pattern, got, test.want)
		}
	}
}

func TestLoadAppliesNonBypassableDefaults(t *testing.T) {
	t.Parallel()
	root := writeConfig(t, minimalConfig())
	config, err := Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if config.Quality.MaxFileLines != 1000 || config.Quality.Complexity.Go != 12 ||
		config.Quality.Complexity.Python != 10 ||
		config.Quality.MaxTestDepth != 8 || config.Quality.MaxTestParams != 8 {
		t.Fatalf("baseline defaults were not applied: %+v", config.Quality)
	}
	if config.SupplyChain.MinimumReleaseAgeDays != 30 {
		t.Fatalf("release age = %d", config.SupplyChain.MinimumReleaseAgeDays)
	}
	if config.SupplyChain.PreferredNewDependencyAgeDays != 90 || config.SupplyChain.AuditLevel != "low" {
		t.Fatalf("dependency admission defaults = %+v", config.SupplyChain)
	}
	moduleSuite := config.Tests.Suites[0]
	if moduleSuite.Cost != "quick" || !slices.Equal(moduleSuite.RunOn, []string{"focused", "recommended", "full"}) || moduleSuite.ExclusiveResources == nil {
		t.Fatalf("module suite defaults = %+v", moduleSuite)
	}
	repositorySuite := config.Tests.Suites[1]
	if repositorySuite.Cost != "standard" || !slices.Equal(repositorySuite.RunOn, []string{"full"}) {
		t.Fatalf("repository suite defaults = %+v", repositorySuite)
	}
}

func TestLoadAppliesCommentPolicyDefault(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		quality string
		allowed bool
	}{
		"omitted":        {quality: `{}`, allowed: true},
		"explicit false": {quality: `{"allowComments":false}`, allowed: false},
		"explicit true":  {quality: `{"allowComments":true}`, allowed: true},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			configText := strings.Replace(minimalConfig(), `"quality":{}`, `"quality":`+testCase.quality, 1)
			config, err := Load(writeConfig(t, configText), "")
			if err != nil {
				t.Fatal(err)
			}
			if config.Quality.CommentsAllowed() != testCase.allowed {
				t.Fatalf("CommentsAllowed() = %t, want %t", config.Quality.CommentsAllowed(), testCase.allowed)
			}
		})
	}
}

func TestDocumentationDesignMappingsResolveOneOwnerPerTarget(t *testing.T) {
	t.Parallel()
	documentation := `{"design":[
  {"path":"docs/design/content.md","module":"content"},
  {"path":"docs/design/entry.md","sourcePaths":["content/entry.go","content/config.ts"]}
]}`
	config, err := Load(writeConfig(t, strings.Replace(minimalConfig(), `"checks":[]`, `"documentation":`+documentation+`,"checks":[]`, 1)), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Documentation.Design) != 2 || config.Documentation.Design[0].Module != "content" ||
		!slices.Equal(config.Documentation.Design[1].SourcePaths, []string{"content/entry.go", "content/config.ts"}) {
		t.Fatalf("design documents = %+v", config.Documentation.Design)
	}
}

func TestDocumentationProductInputsAcceptExactMarkdownPaths(t *testing.T) {
	t.Parallel()
	documentation := `{"productInputs":["docs/site.MD","generated/reference.MARKDOWN"]}`
	config, err := Load(writeConfig(t, strings.Replace(minimalConfig(), `"checks":[]`, `"documentation":`+documentation+`,"checks":[]`, 1)), "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(config.Documentation.ProductInputs, []string{"docs/site.MD", "generated/reference.MARKDOWN"}) {
		t.Fatalf("product inputs = %v", config.Documentation.ProductInputs)
	}
}

func TestDocumentationProductInputsRejectNonExactOrNonMarkdownPaths(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		paths string
		want  string
	}{
		"duplicate": {
			paths: `["docs/site.md","docs/site.md"]`,
			want:  "must not contain duplicate",
		},
		"glob": {
			paths: `["docs/*.md"]`,
			want:  "concrete repository path",
		},
		"escape": {
			paths: `["../docs/site.md"]`,
			want:  "stay inside the repository",
		},
		"noncanonical": {
			paths: `["./docs/site.md"]`,
			want:  "canonical repository-relative path",
		},
		"non markdown": {
			paths: `["docs/site.txt"]`,
			want:  "must name a Markdown file",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			documentation := `{"productInputs":` + test.paths + `}`
			config := strings.Replace(minimalConfig(), `"checks":[]`, `"documentation":`+documentation+`,"checks":[]`, 1)
			if _, err := Load(writeConfig(t, config), ""); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDocumentationDesignMappingsRejectAmbiguousOrUnboundedTargets(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		documentation string
		config        func(string) string
		want          string
	}{
		"requires one selector": {
			documentation: `{"design":[{"path":"docs/design/content.md"}]}`,
			want:          "exactly one of module or sourcePaths",
		},
		"rejects both selectors": {
			documentation: `{"design":[{"path":"docs/design/content.md","module":"content","sourcePaths":["content/file.go"]}]}`,
			want:          "exactly one of module or sourcePaths",
		},
		"rejects unknown module": {
			documentation: `{"design":[{"path":"docs/design/content.md","module":"missing"}]}`,
			want:          "unknown module",
		},
		"rejects duplicate document": {
			documentation: `{"design":[{"path":"docs/design/content.md","module":"content"},{"path":"docs/design/content.md","sourcePaths":["content/file.go"]}]}`,
			want:          "duplicate design document path",
		},
		"rejects duplicate module": {
			documentation: `{"design":[{"path":"docs/design/one.md","module":"content"},{"path":"docs/design/two.md","module":"content"}]}`,
			want:          "more than one design document",
		},
		"rejects duplicate source": {
			documentation: `{"design":[{"path":"docs/design/one.md","sourcePaths":["content/file.go"]},{"path":"docs/design/two.md","sourcePaths":["content/file.go"]}]}`,
			want:          "source path \"content/file.go\" has more than one design document",
		},
		"rejects glob document path": {
			documentation: `{"design":[{"path":"docs/design/*.md","module":"content"}]}`,
			want:          "concrete repository path",
		},
		"rejects historical document path": {
			documentation: `{"design":[{"path":"docs/history/content.md","module":"content"}]}`,
			want:          "under docs/design",
		},
		"rejects non markdown document path": {
			documentation: `{"design":[{"path":"docs/design/content.markdown","module":"content"}]}`,
			want:          "under docs/design",
		},
		"rejects noncanonical document path": {
			documentation: `{"design":[{"path":"./docs/design/content.md","module":"content"}]}`,
			want:          "canonical repository-relative path",
		},
		"rejects glob source path": {
			documentation: `{"design":[{"path":"docs/design/content.md","sourcePaths":["content/*.go"]}]}`,
			want:          "concrete repository path",
		},
		"rejects unmatched source": {
			documentation: `{"design":[{"path":"docs/design/content.md","sourcePaths":["other/file.go"]}]}`,
			want:          "must match exactly one module, matched 0",
		},
		"rejects multiply owned source": {
			documentation: `{"design":[{"path":"docs/design/content.md","sourcePaths":["content/file.go"]}]}`,
			config: func(text string) string {
				return strings.Replace(text, `[{"name":"content","paths":["content/**"]}]`, `[{"name":"content","paths":["content/**"]},{"name":"other","paths":["content/**"]}]`, 1)
			},
			want: "must match exactly one module, matched 2",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := strings.Replace(minimalConfig(), `"checks":[]`, `"documentation":`+test.documentation+`,"checks":[]`, 1)
			if test.config != nil {
				config = test.config(config)
			}
			if _, err := Load(writeConfig(t, config), ""); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadNormalizesAndValidatesExclusiveResources(t *testing.T) {
	t.Parallel()
	configured := strings.Replace(minimalConfig(), `"argv":["go","test","./..."]`, `"argv":["go","test","./..."],"exclusiveResources":["performance","database"]`, 1)
	config, err := Load(writeConfig(t, configured), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := config.Tests.Suites[0].ExclusiveResources; !slices.Equal(got, []string{"database", "performance"}) {
		t.Fatalf("exclusive resources = %v", got)
	}
	duplicated := strings.Replace(minimalConfig(), `"argv":["go","test","./..."]`, `"argv":["go","test","./..."],"exclusiveResources":["performance","performance"]`, 1)
	if _, err := Load(writeConfig(t, duplicated), ""); err == nil || !strings.Contains(err.Error(), "duplicate identifier") {
		t.Fatalf("expected duplicate exclusive-resource error, got %v", err)
	}
	invalid := strings.Replace(minimalConfig(), `"argv":["go","test","./..."]`, `"argv":["go","test","./..."],"exclusiveResources":["../host"]`, 1)
	if _, err := Load(writeConfig(t, invalid), ""); err == nil || !strings.Contains(err.Error(), "lowercase dotted or dashed identifier") {
		t.Fatalf("expected invalid exclusive-resource error, got %v", err)
	}
}

func TestFocusedSuitesAreQuickAndIncludedInBroaderProfiles(t *testing.T) {
	t.Parallel()
	withoutRecommended := strings.Replace(minimalConfig(), `"modules":["content"],"argv":["go","test","./..."]`, `"modules":["content"],"argv":["go","test","./..."],"runOn":["focused","full"]`, 1)
	if _, err := Load(writeConfig(t, withoutRecommended), ""); err == nil || !strings.Contains(err.Error(), "include recommended") {
		t.Fatalf("expected focused hierarchy error, got %v", err)
	}
	withoutFull := strings.Replace(minimalConfig(), `"modules":["content"],"argv":["go","test","./..."]`, `"modules":["content"],"argv":["go","test","./..."],"runOn":["focused","recommended"]`, 1)
	if _, err := Load(writeConfig(t, withoutFull), ""); err == nil || !strings.Contains(err.Error(), "include full") {
		t.Fatalf("expected recommended hierarchy error, got %v", err)
	}
	standardFocused := strings.Replace(minimalConfig(), `"modules":["content"],"argv":["go","test","./..."]`, `"modules":["content"],"argv":["go","test","./..."],"cost":"standard"`, 1)
	if _, err := Load(writeConfig(t, standardFocused), ""); err == nil || !strings.Contains(err.Error(), "must be quick") {
		t.Fatalf("expected focused cost error, got %v", err)
	}
}

func TestExpensiveSuiteCannotEnterRecommendedProfile(t *testing.T) {
	t.Parallel()
	config := strings.Replace(minimalConfig(), `"scope":"repository","argv":["go","test","./..."]`, `"scope":"repository","argv":["go","test","./..."],"cost":"expensive","runOn":["recommended","full"]`, 1)
	if _, err := Load(writeConfig(t, config), ""); err == nil || !strings.Contains(err.Error(), "cannot be expensive") {
		t.Fatalf("expected recommended cost error, got %v", err)
	}
}

func TestExplicitEmptyProtocolsRemainStrict(t *testing.T) {
	t.Parallel()
	configText := strings.Replace(minimalConfig(), `"supplyChain":{}`, `"supplyChain":{"allowedDependencyProtocols":[]}`, 1)
	config, err := Load(writeConfig(t, configText), "")
	if err != nil {
		t.Fatal(err)
	}
	if config.SupplyChain.AllowedDependencyProtocols == nil || len(config.SupplyChain.AllowedDependencyProtocols) != 0 {
		t.Fatalf("protocols = %v", config.SupplyChain.AllowedDependencyProtocols)
	}
}

func TestLicensePolicyNamesSPDXIdentifiers(t *testing.T) {
	t.Parallel()
	allowed := `"allowedLicenses":["MIT","Apache-2.0","GPL-2.0-or-later","LGPL-2.1+","GPL-2.0-only WITH Classpath-exception-2.0"]`
	configText := strings.Replace(minimalConfig(), `"supplyChain":{}`, `"supplyChain":{`+allowed+`}`, 1)
	config, err := Load(writeConfig(t, configText), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(config.SupplyChain.AllowedLicenses) != 5 {
		t.Fatalf("licenses = %v", config.SupplyChain.AllowedLicenses)
	}
	for name, licenses := range map[string]string{
		"expression": `["MIT OR Apache-2.0"]`,
		"group":      `["(MIT)"]`,
		"exception":  `["MIT WITH"]`,
		"empty":      `[""]`,
		"duplicate":  `["MIT","MIT"]`,
	} {
		t.Run(name, func(t *testing.T) {
			text := strings.Replace(minimalConfig(), `"supplyChain":{}`, `"supplyChain":{"allowedLicenses":`+licenses+`}`, 1)
			if _, err := Load(writeConfig(t, text), ""); err == nil {
				t.Fatalf("an unusable license policy was accepted: %s", licenses)
			}
		})
	}
}

func TestDependencyAdmissionBaselineCannotBeWeakened(t *testing.T) {
	t.Parallel()
	for name, supply := range map[string]string{
		"release-age": `{"minimumReleaseAgeDays":29}`,
		"preference":  `{"minimumReleaseAgeDays":60,"preferredNewDependencyAgeDays":30}`,
		"audit":       `{"auditLevel":"moderate"}`,
	} {
		t.Run(name, func(t *testing.T) {
			configText := strings.Replace(minimalConfig(), `"supplyChain":{}`, `"supplyChain":`+supply, 1)
			if _, err := Load(writeConfig(t, configText), ""); err == nil {
				t.Fatalf("weakened supply-chain policy was accepted: %s", supply)
			}
		})
	}
}

func TestReleaseArtifactsRequireExactSourceBoundDeclarations(t *testing.T) {
	t.Parallel()
	artifacts := `"releaseArtifacts":[
    {"name":"go","versionFile":"scripts/go_version.txt","source":"go-toolchain"},
    {"name":"node","versionFile":"tools/node-version.txt","source":"node-runtime"},
    {"name":"staticcheck","versionFile":"tools/staticcheck-version.txt","source":"go-module","locator":"honnef.co/go/tools"},
    {"name":"ruff","versionFile":"tools/ruff-version.txt","source":"github-release","locator":"astral-sh/ruff"},
    {"name":"tool","versionFile":"tools/tool-version.txt","source":"npm","locator":"@example/tool"}
  ]`
	configText := strings.Replace(minimalConfig(), `"supplyChain":{}`, `"supplyChain":{`+artifacts+`}`, 1)
	config, err := Load(writeConfig(t, configText), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(config.SupplyChain.ReleaseArtifacts) != 5 {
		t.Fatalf("release artifacts = %+v", config.SupplyChain.ReleaseArtifacts)
	}

	invalid := map[string]string{
		"duplicate-name":  strings.Replace(artifacts, `"name":"node"`, `"name":"go"`, 1),
		"duplicate-file":  strings.Replace(artifacts, `"tools/node-version.txt"`, `"scripts/go_version.txt"`, 1),
		"missing-locator": strings.Replace(artifacts, `,"locator":"honnef.co/go/tools"`, ``, 1),
		"fixed-locator":   strings.Replace(artifacts, `"source":"node-runtime"`, `"source":"node-runtime","locator":"nodejs/node"`, 1),
		"wrong-prefix":    strings.Replace(artifacts, `"source":"npm","locator":"@example/tool"`, `"source":"npm","locator":"@example/tool","tagPrefix":"v"`, 1),
		"github-locator":  strings.Replace(artifacts, `"locator":"astral-sh/ruff"`, `"locator":"https://github.com/astral-sh/ruff"`, 1),
		"unknown-source":  strings.Replace(artifacts, `"source":"go-toolchain"`, `"source":"website"`, 1),
	}
	for name, declaration := range invalid {
		t.Run(name, func(t *testing.T) {
			text := strings.Replace(minimalConfig(), `"supplyChain":{}`, `"supplyChain":{`+declaration+`}`, 1)
			if _, err := Load(writeConfig(t, text), ""); err == nil {
				t.Fatalf("invalid release artifacts were accepted: %s", declaration)
			}
		})
	}
}

func TestReleaseAgeAssessmentCanIdentifyOneStandaloneArtifact(t *testing.T) {
	t.Parallel()
	reviewed := time.Now().UTC().Format("2006-01-02")
	expires := time.Now().UTC().AddDate(0, 0, 15).Format("2006-01-02")
	supply := `"supplyChain":{"releaseAgeAssessments":[{
    "id":"go-security-fix","ecosystem":"artifact","package":"go","version":"1.26.6","scope":"scripts/go_version.txt",
    "category":"security-fix","evidence":"https://go.dev/doc/devel/release","reason":"security release","owner":"security",
    "reviewed":"` + reviewed + `","expires":"` + expires + `"
  }]}`
	configText := strings.Replace(minimalConfig(), `"supplyChain":{}`, supply, 1)
	if _, err := Load(writeConfig(t, configText), ""); err != nil {
		t.Fatal(err)
	}
	invalid := strings.Replace(configText, `"version":"1.26.6"`, `"version":"latest"`, 1)
	if _, err := Load(writeConfig(t, invalid), ""); err == nil || !strings.Contains(err.Error(), "exact version") {
		t.Fatalf("expected exact artifact version error, got %v", err)
	}
}

func TestRegistryURLCannotContainCredentials(t *testing.T) {
	t.Parallel()
	configText := strings.Replace(minimalConfig(), `"supplyChain":{}`, `"supplyChain":{"npmRegistryUrl":"https://token@example.test"}`, 1)
	_, err := Load(writeConfig(t, configText), "")
	if err == nil || !strings.Contains(err.Error(), "without credentials") {
		t.Fatalf("expected registry credential error, got %v", err)
	}
}

func TestLoadReadsOnlyTheCurrentConfigVersion(t *testing.T) {
	t.Parallel()
	for name, version := range map[string]string{"submodule-era": "2", "unreleased": "4"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := writeConfig(t, strings.Replace(minimalConfig(), `"version":3`, `"version":`+version, 1))
			_, err := Load(root, "")
			if err == nil || !strings.Contains(err.Error(), "unsupported policy version "+version+"; expected 3") {
				t.Fatalf("expected version %s to be refused, got %v", version, err)
			}
		})
	}
}

func TestLoadRejectsUnknownConfiguration(t *testing.T) {
	t.Parallel()
	root := writeConfig(t, strings.Replace(minimalConfig(), `"kind":"content"`, `"kind":"content","typo":true`, 1))
	_, err := Load(root, "")
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}

func TestLoadAcceptsConfiguredMergeGate(t *testing.T) {
	t.Parallel()
	configText := strings.Replace(minimalConfig(), `"checks":[]`, `"verification":{"mergeGate":{"recommendedModules":["content"]}},"checks":[]`, 1)
	config, err := Load(writeConfig(t, configText), "")
	if err != nil {
		t.Fatal(err)
	}
	if config.Verification.MergeGate == nil || !slices.Equal(config.Verification.MergeGate.RecommendedModules, []string{"content"}) {
		t.Fatalf("merge gate = %+v", config.Verification.MergeGate)
	}
}

func TestLoadAcceptsCheckedInTrustedMergeTarget(t *testing.T) {
	t.Parallel()
	configText := strings.Replace(minimalConfig(), `"checks":[]`, `"verification":{"trustedMergeTarget":"origin/release"},"checks":[]`, 1)
	config, err := Load(writeConfig(t, configText), "")
	if err != nil {
		t.Fatal(err)
	}
	if config.Verification.TrustedMergeTarget != "origin/release" {
		t.Fatalf("trusted merge target = %q", config.Verification.TrustedMergeTarget)
	}
	invalid := strings.Replace(minimalConfig(), `"checks":[]`, `"verification":{"trustedMergeTarget":"origin main"},"checks":[]`, 1)
	if _, err := Load(writeConfig(t, invalid), ""); err == nil || !strings.Contains(err.Error(), "trustedMergeTarget") {
		t.Fatalf("expected invalid trusted merge target error, got %v", err)
	}
}

func TestLoadRejectsInvalidMergeGateModules(t *testing.T) {
	t.Parallel()
	for name, modules := range map[string]string{
		"empty":     `[]`,
		"duplicate": `["content","content"]`,
		"unknown":   `["scripts"]`,
	} {
		t.Run(name, func(t *testing.T) {
			configText := strings.Replace(minimalConfig(), `"checks":[]`, `"verification":{"mergeGate":{"recommendedModules":`+modules+`}},"checks":[]`, 1)
			if _, err := Load(writeConfig(t, configText), ""); err == nil {
				t.Fatalf("expected merge-gate validation error for %s", modules)
			}
		})
	}
}

func TestExternalInputsRequireCheapContractAndOrdinaryBehaviorEvidence(t *testing.T) {
	t.Parallel()
	declaration := `"portability":{"externalInputs":[{
	  "name":"catalog","kind":"repository","module":"content","sourcePaths":["scripts/admin/**"],
	  "resolution":["environment","default"],"environment":["CATALOG_REPO"],"unavailableBehavior":"warn",
	  "contractSuite":"content-test","behaviorSuite":"full","siblingFallback":"../shared-content"
	}]},`
	configText := strings.Replace(minimalConfig(), `"quality":{}`, declaration+`"quality":{}`, 1)
	config, err := Load(writeConfig(t, configText), "")
	if err != nil {
		t.Fatal(err)
	}
	input := config.Portability.ExternalInputs[0]
	if input.Name != "catalog" || input.SiblingFallback != "../shared-content" {
		t.Fatalf("input = %+v", input)
	}

	missingEnvironment := strings.Replace(configText, `"environment":["CATALOG_REPO"]`, `"environment":[]`, 1)
	if _, err := Load(writeConfig(t, missingEnvironment), ""); err == nil || !strings.Contains(err.Error(), "exactly when") {
		t.Fatalf("expected environment coverage error, got %v", err)
	}

	wrongContract := strings.Replace(configText, `"contractSuite":"content-test","behaviorSuite":"full"`, `"contractSuite":"full","behaviorSuite":"content-test"`, 1)
	if _, err := Load(writeConfig(t, wrongContract), ""); err == nil || !strings.Contains(err.Error(), "quick focused") {
		t.Fatalf("expected contract-suite error, got %v", err)
	}

	unknownBehavior := strings.Replace(configText, `"behaviorSuite":"full"`, `"behaviorSuite":"missing"`, 1)
	if _, err := Load(writeConfig(t, unknownBehavior), ""); err == nil || !strings.Contains(err.Error(), "unknown test suite") {
		t.Fatalf("expected behavior-suite error, got %v", err)
	}

	defaultFirst := strings.Replace(configText, `"resolution":["environment","default"]`, `"resolution":["default","environment"]`, 1)
	if _, err := Load(writeConfig(t, defaultFirst), ""); err == nil || !strings.Contains(err.Error(), "default last") {
		t.Fatalf("expected resolution-order error, got %v", err)
	}

	sameEvidence := strings.Replace(configText, `"behaviorSuite":"full"`, `"behaviorSuite":"content-test"`, 1)
	if _, err := Load(writeConfig(t, sameEvidence), ""); err == nil || !strings.Contains(err.Error(), "distinct") {
		t.Fatalf("expected distinct-evidence error, got %v", err)
	}

	invalidSibling := strings.Replace(configText, `"../shared-content"`, `"/Users/alice/catalog"`, 1)
	if _, err := Load(writeConfig(t, invalidSibling), ""); err == nil || !strings.Contains(err.Error(), "parent-relative") {
		t.Fatalf("expected sibling-path error, got %v", err)
	}

	serviceSibling := strings.Replace(configText, `"kind":"repository"`, `"kind":"service"`, 1)
	if _, err := Load(writeConfig(t, serviceSibling), ""); err == nil || !strings.Contains(err.Error(), "service inputs") {
		t.Fatalf("expected service sibling error, got %v", err)
	}
}

func TestConditionalPolicyModuleOverridesAreExactAndGoverned(t *testing.T) {
	t.Parallel()
	expiry := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02")
	disabled := `"policyModules":{"overrides":[{"name":"ty","root":"./web/","mode":"disabled","reason":"fixture package","owner":"quality","expires":"` + expiry + `"}]},`
	configText := strings.Replace(minimalConfig(), `"quality":{}`, disabled+`"quality":{}`, 1)
	config, err := Load(writeConfig(t, configText), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := config.PolicyModules.Overrides[0].Root; got != "web" {
		t.Fatalf("root = %q", got)
	}

	missingGovernance := strings.Replace(minimalConfig(), `"quality":{}`, `"policyModules":{"overrides":[{"name":"react","mode":"disabled"}]},"quality":{}`, 1)
	if _, err := Load(writeConfig(t, missingGovernance), ""); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("expected governed-disable error, got %v", err)
	}

	unknown := strings.Replace(minimalConfig(), `"quality":{}`, `"policyModules":{"overrides":[{"name":"django","mode":"enabled"}]},"quality":{}`, 1)
	if _, err := Load(writeConfig(t, unknown), ""); err == nil || !strings.Contains(err.Error(), "supported conditional") {
		t.Fatalf("expected unknown-module error, got %v", err)
	}
}

func TestCustomLanguageRulesAreValidated(t *testing.T) {
	t.Parallel()
	valid := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"languages":[{"name":"elixir","paths":["lib/**/*.ex"]}]},"quality":{}`, 1)
	if _, err := Load(writeConfig(t, valid), ""); err != nil {
		t.Fatalf("valid custom language: %v", err)
	}
	reserved := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"languages":[{"name":"go","paths":["lib/**/*.ex"]}]},"quality":{}`, 1)
	if _, err := Load(writeConfig(t, reserved), ""); err == nil || !strings.Contains(err.Error(), "custom language") {
		t.Fatalf("expected reserved-language error, got %v", err)
	}
}

func TestLoadRejectsWeakenedBudgets(t *testing.T) {
	t.Parallel()
	config := strings.Replace(minimalConfig(), `"quality":{}`, `"quality":{"maxFileLines":1001}`, 1)
	root := writeConfig(t, config)
	_, err := Load(root, "")
	if err == nil || !strings.Contains(err.Error(), "between 1 and 1000") {
		t.Fatalf("expected budget error, got %v", err)
	}
}

func TestLoadRejectsUniversalExclude(t *testing.T) {
	t.Parallel()
	config := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"exclude":["**"]},"quality":{}`, 1)
	root := writeConfig(t, config)
	_, err := Load(root, "")
	if err == nil || !strings.Contains(err.Error(), "cannot hide") {
		t.Fatalf("expected scope error, got %v", err)
	}
}

func TestLoadReadsDevelopmentScope(t *testing.T) {
	t.Parallel()
	valid := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"development":["*.config.ts"]},"quality":{}`, 1)
	config, err := Load(writeConfig(t, valid), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Scope.Development) != 1 || config.Scope.Development[0] != "*.config.ts" {
		t.Fatalf("development = %v", config.Scope.Development)
	}
	universal := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"development":["**"]},"quality":{}`, 1)
	if _, err := Load(writeConfig(t, universal), ""); err == nil ||
		!strings.Contains(err.Error(), "scope.development") {
		t.Fatalf("expected a scope.development error, got %v", err)
	}
}

func TestLoadRejectsModuleCycle(t *testing.T) {
	t.Parallel()
	config := strings.Replace(minimalConfig(), `"modules":[{"name":"content","paths":["content/**"]}]`, `"modules":[{"name":"a","paths":["a/**"],"dependsOn":["b"]},{"name":"b","paths":["b/**"],"dependsOn":["a"]}]`, 1)
	root := writeConfig(t, config)
	_, err := Load(root, "")
	if err == nil || !strings.Contains(err.Error(), "acyclic") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestLoadRejectsShellEvaluation(t *testing.T) {
	t.Parallel()
	config := strings.Replace(minimalConfig(), `"checks":[]`, `"checks":[{"name":"bad","provides":["lint"],"argv":["sh","-c","exit 0"]}]`, 1)
	root := writeConfig(t, config)
	_, err := Load(root, "")
	if err == nil || !strings.Contains(err.Error(), "shell -c") {
		t.Fatalf("expected shell-evaluation error, got %v", err)
	}
}

func TestLoadRejectsExplicitEmptyRunProfiles(t *testing.T) {
	t.Parallel()
	config := strings.Replace(minimalConfig(), `"checks":[]`, `"checks":[{"name":"lint","provides":["lint"],"argv":["true"],"runOn":[]}]`, 1)
	_, err := Load(writeConfig(t, config), "")
	if err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("expected empty-profile error, got %v", err)
	}
}

func TestLoadRejectsWildcardException(t *testing.T) {
	t.Parallel()
	expiry := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02")
	exception := `"exceptions":[{"id":"temporary","check":"quality.*","path":"x.go","subject":"x","reason":"migration","owner":"team","expires":"` + expiry + `"}]`
	config := strings.Replace(minimalConfig(), `"exceptions":[]`, exception, 1)
	root := writeConfig(t, config)
	_, err := Load(root, "")
	if err == nil || !strings.Contains(err.Error(), "wildcard") {
		t.Fatalf("expected wildcard error, got %v", err)
	}
}

func TestGeneralExceptionsCannotReplaceTypedSupplyChainGovernance(t *testing.T) {
	t.Parallel()
	expiry := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02")
	for _, check := range []string{
		"supplyChain.auditIgnore",
		"supplyChain.dependencyOverride",
		"supplyChain.goVulnerability",
		"supplyChain.nodeVulnerability",
		"supplyChain.osvVulnerability",
		"supplyChain.pnpmSecurity",
		"supplyChain.releaseAge",
	} {
		exception := `"exceptions":[{"id":"temporary","check":"` + check + `","path":"package.json","subject":"exact","reason":"migration","owner":"team","expires":"` + expiry + `"}]`
		config := strings.Replace(minimalConfig(), `"exceptions":[]`, exception, 1)
		if _, err := Load(writeConfig(t, config), ""); err == nil || !strings.Contains(err.Error(), "must name") {
			t.Errorf("expected typed-governance error for %s, got %v", check, err)
		}
	}
}

func TestReleaseAgeAssessmentRequiresExactYoungReleaseAndBoundedExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	identity := ReleaseAgeIdentity{
		Ecosystem: "pnpm", Package: "electron", Version: "43.3.0", Scope: "pnpm-lock.yaml",
		Released: now.AddDate(0, 0, -10), Eligible: now.AddDate(0, 0, 20),
	}
	assessment := ReleaseAgeAssessment{
		ID: "electron-supported", Ecosystem: identity.Ecosystem, Package: identity.Package,
		Version: identity.Version, Scope: identity.Scope, Category: "supported-release",
		Evidence: "https://example.test/support", Reason: "only supported release", Owner: "desktop",
		Reviewed: Date{Time: now.Truncate(24 * time.Hour)}, Expires: Date{Time: identity.Eligible.Truncate(24 * time.Hour)},
	}
	finding := Finding{
		Check: "supplyChain.releaseAge", Path: identity.Scope, Subject: ReleaseAgeSubject(identity),
		Message: "young", ReleaseAge: &identity,
	}
	kept, accepted := ApplyReleaseAgeAssessments([]Finding{finding}, []ReleaseAgeAssessment{assessment}, now, true)
	if len(kept) != 0 || len(accepted) != 1 {
		t.Fatalf("kept=%+v accepted=%+v", kept, accepted)
	}

	tooLong := assessment
	tooLong.Expires = Date{Time: identity.Eligible.AddDate(0, 0, 1).Truncate(24 * time.Hour)}
	kept, accepted = ApplyReleaseAgeAssessments([]Finding{finding}, []ReleaseAgeAssessment{tooLong}, now, true)
	if len(accepted) != 0 || len(kept) != 2 || kept[0].Check != "policy.releaseAgeAssessmentWindow" {
		t.Fatalf("overlong kept=%+v accepted=%+v", kept, accepted)
	}

	changed := assessment
	changed.Version = "43.3.1"
	kept, accepted = ApplyReleaseAgeAssessments([]Finding{finding}, []ReleaseAgeAssessment{changed}, now, true)
	if len(accepted) != 0 || len(kept) != 2 || kept[1].Check != "policy.releaseAgeAssessmentUnused" {
		t.Fatalf("changed kept=%+v accepted=%+v", kept, accepted)
	}
}

func TestReleaseAgeMetadataFailureDefersUnusedAssessmentFinding(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	assessment := ReleaseAgeAssessment{
		ID: "go-security-fix", Ecosystem: "artifact", Package: "go", Version: "1.26.6", Scope: "scripts/go_version.txt",
		Expires: Date{Time: now.AddDate(0, 0, 15).Truncate(24 * time.Hour)},
	}
	failure := Finding{
		Check: "supplyChain.releaseAge", Path: assessment.Scope, Subject: "go@1.26.6",
		Message: "resolve standalone artifact release: release metadata request failed with HTTP 403",
	}
	kept, accepted := ApplyReleaseAgeAssessments([]Finding{failure}, []ReleaseAgeAssessment{assessment}, now, true)
	if len(accepted) != 0 || len(kept) != 1 || kept[0] != failure {
		t.Fatalf("kept=%+v accepted=%+v", kept, accepted)
	}
}

func TestDateJSONRoundTripPreservesDateOnlyContract(t *testing.T) {
	t.Parallel()
	original := Date{Time: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"2026-08-15"` {
		t.Fatalf("marshaled date = %s", data)
	}
	var roundTripped Date
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatal(err)
	}
	if !roundTripped.Equal(original.Time) {
		t.Fatalf("round-tripped date = %s", roundTripped.Time)
	}
}

func TestApplyExceptionsRequiresExactTriple(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	exception := Exception{ID: "known", Check: "quality.fileLength", Path: "a.go", Subject: "1001", Reason: "split in progress", Owner: "team", Expires: Date{Time: now.AddDate(0, 0, 1)}}
	findings := []Finding{
		{Check: "quality.fileLength", Path: "a.go", Subject: "1001", Message: "too long"},
		{Check: "quality.fileLength", Path: "b.go", Subject: "1001", Message: "too long"},
	}
	kept, suppressed := ApplyExceptions(findings, []Exception{exception}, now)
	if len(kept) != 1 || kept[0].Path != "b.go" || len(suppressed) != 1 {
		t.Fatalf("kept=%+v suppressed=%+v", kept, suppressed)
	}
}

func TestExceptionRemainsValidThroughExpiryDate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC)
	exception := Exception{ID: "today", Check: "quality.fileLength", Path: "a.go", Subject: "1001", Reason: "last day", Owner: "team", Expires: Date{Time: now.Truncate(24 * time.Hour)}}
	findings := []Finding{{Check: exception.Check, Path: exception.Path, Subject: exception.Subject, Message: "too long"}}
	kept, suppressed := ApplyExceptions(findings, []Exception{exception}, now)
	if len(kept) != 0 || len(suppressed) != 1 {
		t.Fatalf("kept=%+v suppressed=%+v", kept, suppressed)
	}
}

func TestVulnerabilityAssessmentRequiresExactIdentityAndReportsUnused(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	assessment := VulnerabilityAssessment{
		ID: "accepted-advisory", Ecosystem: "pnpm", Advisory: "CVE-2026-1000",
		Package: "example", AffectedVersion: "1.2.3", Scope: "desktop/pnpm-lock.yaml", Severity: "low",
		Status: "not-affected", Basis: "unreachable", Reason: "unreachable", Impact: "the vulnerable path is not shipped",
		Evidence: "https://example.test/analysis", Tracking: "https://example.test/issues/1",
		Owner: "desktop", ApprovedBy: "security", Approval: "https://example.test/reviews/1",
		Reviewed: Date{Time: now}, Expires: Date{Time: now.AddDate(0, 0, 30)},
	}
	identity := VulnerabilityIdentity{
		Ecosystem: "pnpm", Advisory: "GHSA-abcd-1234-5678", Aliases: []string{"CVE-2026-1000"}, Package: "example",
		AffectedVersion: "1.2.3", Scope: "desktop/pnpm-lock.yaml", Severity: "low",
	}
	finding := Finding{
		Check: "supplyChain.nodeVulnerability", Path: identity.Scope,
		Subject: VulnerabilitySubject(identity), Message: "low vulnerability", Vulnerability: &identity,
	}
	kept, accepted := ApplyVulnerabilityAssessments([]Finding{finding}, []VulnerabilityAssessment{assessment}, now, true)
	if len(kept) != 0 || len(accepted) != 1 || accepted[0].Assessment.ID != assessment.ID {
		t.Fatalf("kept=%+v accepted=%+v", kept, accepted)
	}
	moderateCeiling := assessment
	moderateCeiling.Severity = "moderate"
	kept, accepted = ApplyVulnerabilityAssessments([]Finding{finding}, []VulnerabilityAssessment{moderateCeiling}, now, true)
	if len(kept) != 0 || len(accepted) != 1 {
		t.Fatalf("moderate ceiling did not accept low finding: kept=%+v accepted=%+v", kept, accepted)
	}

	changed := finding
	changedIdentity := identity
	changedIdentity.AffectedVersion = "1.2.4"
	changed.Vulnerability = &changedIdentity
	changed.Subject = VulnerabilitySubject(changedIdentity)
	kept, accepted = ApplyVulnerabilityAssessments([]Finding{changed}, []VulnerabilityAssessment{assessment}, now, true)
	if len(accepted) != 0 || len(kept) != 2 || kept[1].Check != "policy.vulnerabilityAssessmentUnused" {
		t.Fatalf("changed identity kept=%+v accepted=%+v", kept, accepted)
	}
}

func TestVulnerabilityAssessmentCannotAcceptSeverityChangesOrKnownExploitedFindings(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	assessment := VulnerabilityAssessment{
		ID: "accepted-advisory", Ecosystem: "npm", Advisory: "CVE-2026-1000", Package: "example",
		AffectedVersion: "1.2.3", Scope: "package-lock.json", Severity: "low", Status: "risk-accepted", Basis: "mitigated",
		Reason: "input is filtered", Impact: "availability only", Evidence: "https://example.test/analysis",
		Tracking: "https://example.test/issues/1", Owner: "runtime", ApprovedBy: "security",
		Approval: "https://example.test/reviews/1", Reviewed: Date{Time: now}, Expires: Date{Time: now.AddDate(0, 0, 30)},
	}
	identity := VulnerabilityIdentity{
		Ecosystem: "npm", Advisory: assessment.Advisory, Package: assessment.Package,
		AffectedVersion: assessment.AffectedVersion, Scope: assessment.Scope, Severity: "moderate",
	}
	finding := Finding{Check: "supplyChain.osvVulnerability", Path: identity.Scope, Subject: VulnerabilitySubject(identity), Vulnerability: &identity}
	kept, accepted := ApplyVulnerabilityAssessments([]Finding{finding}, []VulnerabilityAssessment{assessment}, now, true)
	if len(accepted) != 0 || len(kept) != 2 || kept[0].Check != "policy.vulnerabilityAssessmentSeverity" {
		t.Fatalf("severity change kept=%+v accepted=%+v", kept, accepted)
	}

	identity.Severity = "high"
	identity.KnownExploited = true
	finding.Vulnerability = &identity
	kept, accepted = ApplyVulnerabilityAssessments([]Finding{finding}, []VulnerabilityAssessment{assessment}, now, true)
	if len(accepted) != 0 || len(kept) != 2 || kept[0].Check != "policy.vulnerabilityAssessmentKnownExploited" {
		t.Fatalf("known exploited kept=%+v accepted=%+v", kept, accepted)
	}
}

func TestVulnerabilityAssessmentValidationRequiresBoundedIndependentApproval(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	assessment := VulnerabilityAssessment{
		ID: "accepted-advisory", Ecosystem: "npm", Advisory: "CVE-2026-1000", Package: "example",
		AffectedVersion: "1.2.3", Scope: "package-lock.json", Severity: "moderate", Status: "risk-accepted", Basis: "temporary-no-fix",
		Reason: "no compatible fix", Impact: "development-only tool", Evidence: "https://example.test/analysis",
		Tracking: "https://example.test/issues/1", Owner: "tooling", ApprovedBy: "security",
		Approval: "https://example.test/reviews/1", Reviewed: Date{Time: now}, Expires: Date{Time: now.AddDate(0, 0, MaximumModerateVulnerabilityDays)},
	}
	validate := func(candidate VulnerabilityAssessment) error {
		return validateVulnerabilityAssessment(candidate, "assessment", map[string]bool{}, map[string]bool{}, now)
	}
	if err := validate(assessment); err != nil {
		t.Fatalf("valid assessment: %v", err)
	}
	high := assessment
	high.Severity = "high"
	if err := validate(high); err == nil || !strings.Contains(err.Error(), "severity") {
		t.Fatalf("expected severity error, got %v", err)
	}
	broad := assessment
	broad.AffectedVersion = "<2.0.0"
	if err := validate(broad); err == nil || !strings.Contains(err.Error(), "exact version") {
		t.Fatalf("expected exact-version error, got %v", err)
	}
	sameApprover := assessment
	sameApprover.ApprovedBy = sameApprover.Owner
	if err := validate(sameApprover); err == nil || !strings.Contains(err.Error(), "distinct") {
		t.Fatalf("expected independent approval error, got %v", err)
	}
	overlong := assessment
	overlong.Expires = Date{Time: now.AddDate(0, 0, MaximumModerateVulnerabilityDays+1)}
	if err := validate(overlong); err == nil || !strings.Contains(err.Error(), "within 30 days") {
		t.Fatalf("expected bounded expiry error, got %v", err)
	}
}

func TestHighNotAffectedVulnerabilityAssessmentDispositionMatrix(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	assessment := highNotAffectedAssessment(now)
	validate := func(candidate VulnerabilityAssessment) error {
		return validateVulnerabilityAssessment(candidate, "assessment", map[string]bool{}, map[string]bool{}, now)
	}

	if err := validate(assessment); err != nil {
		t.Fatalf("high not-affected assessment was rejected: %v", err)
	}
	falsePositive := assessment
	falsePositive.Basis = "false-positive"
	if err := validate(falsePositive); err != nil {
		t.Fatalf("high false-positive assessment was rejected: %v", err)
	}
	for _, basis := range []string{"mitigated", "temporary-no-fix"} {
		riskAccepted := assessment
		riskAccepted.Status = "risk-accepted"
		riskAccepted.Basis = basis
		if err := validate(riskAccepted); err == nil || !strings.Contains(err.Error(), "status") {
			t.Fatalf("high risk acceptance with %q was not rejected by disposition: %v", basis, err)
		}
	}
	for _, basis := range []string{"mitigated", "temporary-no-fix"} {
		wrongBasis := assessment
		wrongBasis.Basis = basis
		if err := validate(wrongBasis); err == nil || !strings.Contains(err.Error(), "basis") {
			t.Fatalf("high not-affected assessment accepted unrelated basis %q: %v", basis, err)
		}
	}
}

func TestHighNotAffectedAssessmentReviewWindowIncludesExpiryDate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 16, 14, 0, 0, 0, time.UTC)
	assessment := highNotAffectedAssessment(now)
	validate := func(candidate VulnerabilityAssessment) error {
		return validateVulnerabilityAssessment(candidate, "assessment", map[string]bool{}, map[string]bool{}, now)
	}
	if err := validate(assessment); err != nil {
		t.Fatalf("maximum high not-affected window was rejected: %v", err)
	}
	overlong := assessment
	overlong.Expires = Date{Time: now.AddDate(0, 0, MaximumHighNotAffectedVulnerabilityDays+1)}
	if err := validate(overlong); err == nil || !strings.Contains(err.Error(), "within 30 days") {
		t.Fatalf("overlong high not-affected window was accepted: %v", err)
	}
	finding := highNotAffectedFinding(assessment, "high")
	kept, accepted := ApplyVulnerabilityAssessments([]Finding{finding}, []VulnerabilityAssessment{assessment}, assessment.Expires.Time, true)
	if len(kept) != 0 || len(accepted) != 1 || accepted[0].Assessment.ID != assessment.ID {
		t.Fatalf("assessment was not effective through its expiry date: kept=%+v accepted=%+v", kept, accepted)
	}
}

func TestHighNotAffectedAssessmentHonorsSeverityAndExactCoordinates(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	assessment := highNotAffectedAssessment(now)
	for _, severity := range []string{"low", "moderate", "high"} {
		finding := highNotAffectedFinding(assessment, severity)
		kept, accepted := ApplyVulnerabilityAssessments([]Finding{finding}, []VulnerabilityAssessment{assessment}, now, true)
		if len(kept) != 0 || len(accepted) != 1 || accepted[0].Finding.Vulnerability.Severity != severity {
			t.Fatalf("high not-affected assessment did not cover matching %s finding: kept=%+v accepted=%+v", severity, kept, accepted)
		}
	}
	for _, severity := range []string{"critical", "unknown"} {
		finding := highNotAffectedFinding(assessment, severity)
		kept, accepted := ApplyVulnerabilityAssessments([]Finding{finding}, []VulnerabilityAssessment{assessment}, now, true)
		if len(accepted) != 0 || len(kept) != 2 || kept[0].Check != "policy.vulnerabilityAssessmentSeverity" {
			t.Fatalf("high not-affected assessment accepted %s finding: kept=%+v accepted=%+v", severity, kept, accepted)
		}
	}
	knownExploited := highNotAffectedFinding(assessment, "high")
	knownExploited.Vulnerability.KnownExploited = true
	kept, accepted := ApplyVulnerabilityAssessments([]Finding{knownExploited}, []VulnerabilityAssessment{assessment}, now, true)
	if len(accepted) != 0 || len(kept) != 2 || kept[0].Check != "policy.vulnerabilityAssessmentKnownExploited" {
		t.Fatalf("high not-affected assessment accepted a known-exploited finding: kept=%+v accepted=%+v", kept, accepted)
	}

	changes := []struct {
		name   string
		mutate func(*VulnerabilityIdentity)
	}{
		{
			name: "advisory aliases",
			mutate: func(identity *VulnerabilityIdentity) {
				identity.Advisory = "GHSA-efgh-1234-5678"
				identity.Aliases = []string{"CVE-2026-2000"}
			},
		},
		{name: "package", mutate: func(identity *VulnerabilityIdentity) { identity.Package = "other" }},
		{name: "version", mutate: func(identity *VulnerabilityIdentity) { identity.AffectedVersion = "1.2.4" }},
		{name: "scope", mutate: func(identity *VulnerabilityIdentity) { identity.Scope = "other-lock.yaml" }},
	}
	for _, change := range changes {
		finding := highNotAffectedFinding(assessment, "high")
		change.mutate(finding.Vulnerability)
		finding.Subject = VulnerabilitySubject(*finding.Vulnerability)
		kept, accepted := ApplyVulnerabilityAssessments([]Finding{finding}, []VulnerabilityAssessment{assessment}, now, true)
		if len(accepted) != 0 || len(kept) != 2 || kept[1].Check != "policy.vulnerabilityAssessmentUnused" {
			t.Fatalf("changed %s matched high not-affected assessment: kept=%+v accepted=%+v", change.name, kept, accepted)
		}
	}
}

func highNotAffectedAssessment(now time.Time) VulnerabilityAssessment {
	return VulnerabilityAssessment{
		ID: "high-not-affected", Ecosystem: "pnpm", Advisory: "CVE-2026-1000", Package: "example",
		AffectedVersion: "1.2.3", Scope: "pnpm-lock.yaml", Severity: "high", Status: "not-affected", Basis: "unreachable",
		Reason: "the affected code path is not reachable", Impact: "the vulnerable capability is not shipped",
		Evidence: "https://example.test/analysis", Tracking: "https://example.test/issues/1", Owner: "runtime",
		ApprovedBy: "security", Approval: "https://example.test/reviews/1", Reviewed: Date{Time: now},
		Expires: Date{Time: now.AddDate(0, 0, MaximumHighNotAffectedVulnerabilityDays)},
	}
}

func highNotAffectedFinding(assessment VulnerabilityAssessment, severity string) Finding {
	identity := VulnerabilityIdentity{
		Ecosystem: assessment.Ecosystem, Advisory: "GHSA-abcd-1234-5678", Aliases: []string{assessment.Advisory},
		Package: assessment.Package, AffectedVersion: assessment.AffectedVersion, Scope: assessment.Scope, Severity: severity,
	}
	return Finding{
		Check: "supplyChain.osvVulnerability", Path: identity.Scope, Subject: VulnerabilitySubject(identity),
		Message: severity + " vulnerability", Vulnerability: &identity,
	}
}

func TestVulnerabilitySeverityNormalization(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"LOW": "low", "medium": "moderate", "6.9": "moderate", "8.8": "high", "9.8": "critical", "": "unknown",
	}
	for input, want := range cases {
		if got := NormalizeVulnerabilitySeverity(input); got != want {
			t.Fatalf("NormalizeVulnerabilitySeverity(%q)=%q want %q", input, got, want)
		}
	}
}

func TestLoadValidatesVulnerabilityAndOverrideGovernance(t *testing.T) {
	t.Parallel()
	reviewed := time.Now().UTC().Format("2006-01-02")
	expires := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02")
	supply := `"supplyChain":{
	"releaseAgeAssessments":[{
	  "id":"supported-release","ecosystem":"pnpm","package":"example","version":"1.2.3","scope":"pnpm-lock.yaml",
	  "category":"security-fix","evidence":"https://example.test/advisory","reason":"urgent patched release","owner":"security",
	  "reviewed":"` + reviewed + `","expires":"` + expires + `"
	}],
  "vulnerabilityAssessments":[{
    "id":"known-cve","ecosystem":"pnpm","advisory":"CVE-2026-1000","package":"example","affectedVersion":"1.2.3",
    "scope":"pnpm-lock.yaml","severity":"low","status":"not-affected","basis":"unreachable","reason":"unreachable",
    "impact":"not included in the shipped path","evidence":"https://example.test/analysis","tracking":"https://example.test/issues/1",
    "owner":"desktop","approvedBy":"security","approval":"https://example.test/reviews/1",
    "reviewed":"` + reviewed + `","expires":"` + expires + `"
  }],
  "dependencyOverridePolicies":[{
    "id":"node-overrides","ecosystem":"pnpm","path":"package.json","field":"pnpm.overrides",
    "contentSha256":"` + strings.Repeat("a", 64) + `","reason":"single lock graph","owner":"platform",
    "reviewed":"` + reviewed + `","expires":"` + expires + `"
  }]
}`
	configText := strings.Replace(minimalConfig(), `"supplyChain":{}`, supply, 1)
	if _, err := Load(writeConfig(t, configText), ""); err != nil {
		t.Fatalf("valid governance config: %v", err)
	}
	highNotAffected := strings.Replace(configText, `"severity":"low","status":"not-affected"`, `"severity":"high","status":"not-affected"`, 1)
	if _, err := Load(writeConfig(t, highNotAffected), ""); err != nil {
		t.Fatalf("valid high not-affected governance config: %v", err)
	}
	for _, basis := range []string{"mitigated", "temporary-no-fix"} {
		highRiskAccepted := strings.Replace(highNotAffected, `"status":"not-affected","basis":"unreachable"`, `"status":"risk-accepted","basis":"`+basis+`"`, 1)
		if _, err := Load(writeConfig(t, highRiskAccepted), ""); err == nil || !strings.Contains(err.Error(), "status") {
			t.Fatalf("high risk-accepted governance config with %s was accepted: %v", basis, err)
		}
	}
	for _, basis := range []string{"mitigated", "temporary-no-fix"} {
		wrongBasis := strings.Replace(highNotAffected, `"basis":"unreachable"`, `"basis":"`+basis+`"`, 1)
		if _, err := Load(writeConfig(t, wrongBasis), ""); err == nil || !strings.Contains(err.Error(), "basis") {
			t.Fatalf("high not-affected governance config with %s was accepted: %v", basis, err)
		}
	}
	invalidRelease := strings.Replace(configText, `"version":"1.2.3","scope":"pnpm-lock.yaml"`, `"version":"^1.2.3","scope":"pnpm-lock.yaml"`, 1)
	if _, err := Load(writeConfig(t, invalidRelease), ""); err == nil || !strings.Contains(err.Error(), "exact version") {
		t.Fatalf("expected exact release version error, got %v", err)
	}
}

func TestLoadValidatesArtifactSecurityTargetModes(t *testing.T) {
	t.Parallel()
	supply := `"supplyChain":{"artifactSecurity":{"targets":[{
  "name":"agent-image","module":"content","mode":"dockerfile","platform":"linux/arm64",
  "dockerfile":"content/Dockerfile","context":"content"
}]}}`
	configText := strings.Replace(minimalConfig(), `"supplyChain":{}`, supply, 1)
	config, err := Load(writeConfig(t, configText), "")
	if err != nil {
		t.Fatal(err)
	}
	if config.SupplyChain.ArtifactSecurity.OutputDirectory != ".code-polishy-reports/artifact-security" {
		t.Fatalf("artifact output default = %q", config.SupplyChain.ArtifactSecurity.OutputDirectory)
	}

	invalid := strings.Replace(supply, `"context":"content"`, `"context":"content","archive":"content/image.tar"`, 1)
	configText = strings.Replace(minimalConfig(), `"supplyChain":{}`, invalid, 1)
	if _, err := Load(writeConfig(t, configText), ""); err == nil || !strings.Contains(err.Error(), "dockerfile mode") {
		t.Fatalf("expected target-mode error, got %v", err)
	}
}

func TestLoadRejectsInvalidEnvironmentVariableName(t *testing.T) {
	t.Parallel()
	config := strings.Replace(minimalConfig(), `"checks":[]`, `"checks":[{"name":"lint","provides":["lint"],"argv":["true"],"environment":["NOT-VALID"]}]`, 1)
	root := writeConfig(t, config)
	_, err := Load(root, "")
	if err == nil || !strings.Contains(err.Error(), "environment variable") {
		t.Fatalf("expected environment error, got %v", err)
	}
}

func TestModuleSuiteNeedsModules(t *testing.T) {
	t.Parallel()
	config := strings.Replace(minimalConfig(), `"modules":["content"]`, `"modules":[]`, 1)
	root := writeConfig(t, config)
	_, err := Load(root, "")
	if err == nil || !strings.Contains(err.Error(), "exactly one module") {
		t.Fatalf("expected module suite error, got %v", err)
	}
}

func TestModuleSuiteCannotHideCrossModuleExecution(t *testing.T) {
	t.Parallel()
	config := strings.Replace(minimalConfig(), `"modules":["content"]`, `"modules":["content","other"]`, 1)
	config = strings.Replace(config, `"modules":[{"name":"content","paths":["content/**"]}]`, `"modules":[{"name":"content","paths":["content/**"]},{"name":"other","paths":["other/**"]}]`, 1)
	root := writeConfig(t, config)
	_, err := Load(root, "")
	if err == nil || !strings.Contains(err.Error(), "exactly one module") {
		t.Fatalf("expected single-module suite error, got %v", err)
	}
}

func TestSuiteKindCannotClaimSkipAsEvidence(t *testing.T) {
	t.Parallel()
	config := strings.Replace(minimalConfig(), `"kind":"content","scope":"module"`, `"kind":"skip","scope":"module"`, 1)
	_, err := Load(writeConfig(t, config), "")
	if err == nil || !strings.Contains(err.Error(), "executed evidence") {
		t.Fatalf("expected skip-kind error, got %v", err)
	}
}

func TestMutationSuiteDefaultsToIsolatedSupplementalProfile(t *testing.T) {
	t.Parallel()
	full := `{"name":"full","kind":"content","scope":"repository","argv":["go","test","./..."]}`
	mutation := `,{"name":"mutation","kind":"mutation","scope":"module","modules":["content"],"argv":["go","test","./..."]}`
	configText := strings.Replace(minimalConfig(), full, full+mutation, 1)
	config, err := Load(writeConfig(t, configText), "")
	if err != nil {
		t.Fatal(err)
	}
	suite := config.Tests.Suites[len(config.Tests.Suites)-1]
	if suite.Cost != "expensive" || !slices.Equal(suite.RunOn, []string{"supplemental"}) ||
		!slices.Equal(suite.ExclusiveResources, []string{}) {
		t.Fatalf("mutation defaults = %+v", suite)
	}
}

func TestLiveSuiteRequiresAnExternalApprovalGate(t *testing.T) {
	t.Parallel()
	for name, replacement := range map[string]string{
		"default-profile":      `{"name":"full","kind":"live","scope":"repository","argv":["go","test","./..."]}`,
		"supplemental-profile": `{"name":"full","kind":"live","scope":"repository","cost":"expensive","runOn":["supplemental"],"argv":["go","test","./..."]}`,
	} {
		t.Run(name, func(t *testing.T) {
			config := strings.Replace(minimalConfig(), `{"name":"full","kind":"content","scope":"repository","argv":["go","test","./..."]}`, replacement, 1)
			if _, err := Load(writeConfig(t, config), ""); err == nil || !strings.Contains(err.Error(), "typed external approval gate") {
				t.Fatalf("live suite was accepted as automatic evidence: %v", err)
			}
		})
	}
}

func TestPerformanceSuiteAlwaysIncludesTheHostPerformanceResource(t *testing.T) {
	t.Parallel()
	performanceSuite := `"kind":"performance","scope":"module","cost":"expensive","runOn":["supplemental"],"modules":["content"]`
	for name, resourceDeclaration := range map[string]string{
		"omitted":           ``,
		"empty":             `,"exclusiveResources":[]`,
		"declares-baseline": `,"exclusiveResources":["performance"]`,
		"additional":        `,"exclusiveResources":["database"]`,
	} {
		t.Run(name, func(t *testing.T) {
			configText := strings.Replace(minimalConfig(), `"kind":"content","scope":"module","modules":["content"]`, performanceSuite, 1)
			configText = strings.Replace(configText, `"argv":["go","test","./..."]`, `"argv":["go","test","./..."]`+resourceDeclaration, 1)
			config, err := Load(writeConfig(t, configText), "")
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"performance"}
			if name == "additional" {
				want = []string{"database", "performance"}
			}
			if got := config.Tests.Suites[0].ExclusiveResources; !slices.Equal(got, want) {
				t.Fatalf("exclusive resources = %v, want %v", got, want)
			}
		})
	}
}

func TestSupplementalSuiteCannotLeakIntoFullProfile(t *testing.T) {
	t.Parallel()
	config := strings.Replace(minimalConfig(), `"kind":"content","scope":"module"`, `"kind":"mutation","scope":"module","cost":"expensive","runOn":["full","supplemental"]`, 1)
	_, err := Load(writeConfig(t, config), "")
	if err == nil || !strings.Contains(err.Error(), "only supplemental") {
		t.Fatalf("expected isolated supplemental error, got %v", err)
	}
}

func TestTestSuiteRejectsObviousNoOpAndPassWithoutTests(t *testing.T) {
	t.Parallel()
	noOp := strings.Replace(minimalConfig(), `"argv":["go","test","./..."]`, `"argv":["true"]`, 1)
	if _, err := Load(writeConfig(t, noOp), ""); err == nil || !strings.Contains(err.Error(), "obvious no-op") {
		t.Fatalf("expected no-op error, got %v", err)
	}
	passEmpty := strings.Replace(minimalConfig(), `"argv":["go","test","./..."]`, `"argv":["pnpm","test","--passWithNoTests"]`, 1)
	if _, err := Load(writeConfig(t, passEmpty), ""); err == nil || !strings.Contains(err.Error(), "without executing tests") {
		t.Fatalf("expected empty-test escape error, got %v", err)
	}
}

func TestCheckedInSchemaAndTemplatesLoad(t *testing.T) {
	type schemaProperty struct {
		Const string   `json:"const"`
		Enum  []string `json:"enum"`
	}
	type schemaRule struct {
		If struct {
			Properties map[string]schemaProperty `json:"properties"`
		} `json:"if"`
		Then struct {
			Properties map[string]schemaProperty `json:"properties"`
		} `json:"then"`
	}
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	schemaData, err := os.ReadFile(filepath.Join(repositoryRoot, "schema", "code-polishy.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties struct {
			Version struct {
				Const int `json:"const"`
			} `json:"version"`
		} `json:"properties"`
		Defs struct {
			VulnerabilityAssessment struct {
				Properties map[string]schemaProperty `json:"properties"`
				AllOf      []schemaRule              `json:"allOf"`
			} `json:"vulnerabilityAssessment"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	if schema.Properties.Version.Const != ConfigVersion {
		t.Errorf("schema version const = %d, parser expects %d", schema.Properties.Version.Const, ConfigVersion)
	}
	assessmentSchema := schema.Defs.VulnerabilityAssessment
	if !slices.Equal(assessmentSchema.Properties["severity"].Enum, []string{"low", "moderate", "high"}) {
		t.Errorf("schema severity values = %v", assessmentSchema.Properties["severity"].Enum)
	}
	highNotAffectedRule := false
	for _, rule := range assessmentSchema.AllOf {
		if rule.If.Properties["severity"].Const != "high" {
			continue
		}
		highNotAffectedRule = rule.Then.Properties["status"].Const == "not-affected" &&
			slices.Equal(rule.Then.Properties["basis"].Enum, []string{"false-positive", "unreachable"})
	}
	if !highNotAffectedRule {
		t.Error("schema does not restrict high assessments to not-affected false-positive or unreachable decisions")
	}
	configurations := []string{
		filepath.Join(repositoryRoot, ".code-polishy.json"),
		filepath.Join(repositoryRoot, "templates", "minimal", ".code-polishy.json"),
		filepath.Join(repositoryRoot, "templates", "typescript-go", ".code-polishy.json"),
	}
	for _, path := range configurations {
		if _, err := Load(repositoryRoot, path); err != nil {
			t.Errorf("load %s: %v", path, err)
		}
	}
}

func TestCheckedInToolVersionPinsHaveReleaseAgeCoverage(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	config, err := Load(repositoryRoot, filepath.Join(repositoryRoot, ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}
	pinMatches, err := filepath.Glob(filepath.Join(repositoryRoot, "tools", "*-version.txt"))
	if err != nil {
		t.Fatal(err)
	}
	pinMatches = append(pinMatches, filepath.Join(repositoryRoot, "scripts", "go_version.txt"))
	pins := map[string]bool{}
	for _, path := range pinMatches {
		relative, err := filepath.Rel(repositoryRoot, path)
		if err != nil {
			t.Fatal(err)
		}
		pins[filepath.ToSlash(relative)] = true
	}
	covered := map[string]bool{}
	for _, artifact := range config.SupplyChain.ReleaseArtifacts {
		if !pins[artifact.VersionFile] {
			t.Errorf("release artifact %s points to non-tool pin %s", artifact.Name, artifact.VersionFile)
		}
		covered[artifact.VersionFile] = true
	}

	packageData, err := os.ReadFile(filepath.Join(repositoryRoot, "tools", "javascript", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var packageManifest struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal(packageData, &packageManifest); err != nil {
		t.Fatal(err)
	}
	pnpmData, err := os.ReadFile(filepath.Join(repositoryRoot, "tools", "pnpm-version.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "pnpm@" + strings.TrimSpace(string(pnpmData)); packageManifest.PackageManager != want {
		t.Fatalf("JavaScript packageManager = %q, want %q", packageManifest.PackageManager, want)
	}
	covered["tools/pnpm-version.txt"] = true

	for path := range pins {
		if !covered[path] {
			t.Errorf("tool version pin %s has no release-age source", path)
		}
	}
}

func TestPackSelectionRequiresOneExactIdentityPerName(t *testing.T) {
	valid := strings.Replace(minimalConfig(), `"modules":`, `"packs":[{"name":"community-rust","version":"1.2.3","digest":"`+strings.Repeat("a", 64)+`"}],"modules":`, 1)
	config, err := Parse([]byte(valid), ConfigFilename)
	if err != nil || len(config.Packs) != 1 {
		t.Fatalf("valid pack selection failed: %+v %v", config.Packs, err)
	}
	for _, mutation := range []string{
		strings.Replace(valid, `"version":"1.2.3"`, `"version":"latest"`, 1),
		strings.Replace(valid, strings.Repeat("a", 64), "sha256:missing", 1),
		strings.Replace(valid, `],"modules":`, `,{"name":"community-rust","version":"1.2.4","digest":"`+strings.Repeat("b", 64)+`"}],"modules":`, 1),
	} {
		if _, err := Parse([]byte(mutation), ConfigFilename); err == nil {
			t.Fatal("invalid pack selection passed")
		}
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ConfigFilename), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func minimalConfig() string {
	return `{"version":3,"project":{"kind":"content"},"quality":{},"modules":[{"name":"content","paths":["content/**"]}],"checks":[],"tests":{"suites":[{"name":"content-test","kind":"content","scope":"module","modules":["content"],"argv":["go","test","./..."]},{"name":"full","kind":"content","scope":"repository","argv":["go","test","./..."]}]},"supplyChain":{},"exceptions":[]}`
}
