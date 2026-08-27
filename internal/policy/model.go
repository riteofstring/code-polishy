package policy

import "time"

// ConfigVersion is the one .code-polishy.json version the sealed runtime reads.
// It names the shape a target declares facts in, so the schema, the parser, the
// templates, and every fixture carry the same number.
const ConfigVersion = 3

const (
	ConfigFilename = ".code-polishy.json"
	// LockFilename is the other file a target checks in: the exact Code Polishy
	// release it requires. internal/release owns what is in it; the name is
	// here because it names a target's policy control plane, as
	// ConfigFilename does.
	LockFilename                            = ".code-polishy.lock.json"
	MaxFileLines                            = 1000
	MaxTestFileLines                        = 1500
	MaxGoComplexity                         = 12
	MaxGoTestComplexity                     = 20
	MaxTypeScriptComplexity                 = 10
	MaxTypeScriptTestComplexity             = 10
	MaxTypeScriptDepth                      = 4
	MaxTypeScriptTestDepth                  = 8
	MaxTypeScriptParams                     = 5
	MaxTypeScriptTestParams                 = 8
	MinimumReleaseAgeDays                   = 30
	PreferredNewDependencyAgeDays           = 90
	MaximumLowVulnerabilityDays             = 90
	MaximumModerateVulnerabilityDays        = 30
	MaximumHighNotAffectedVulnerabilityDays = 30
	MaximumExceptionDays                    = 366
)

var DefaultExcludes = []string{
	".git/**", ".tools/**", ".code-polishy-reports/**", "node_modules/**", "**/node_modules/**",
	"dist/**", "**/dist/**", "build/**", "**/build/**", "coverage/**",
	"**/coverage/**", "playwright-report/**", "**/playwright-report/**",
	"test-results/**", "**/test-results/**",
}

var DefaultGenerated = []string{
	"**/*.generated.*", "**/*_gen.go", "**/*.gen.go",
}

var DefaultTestPatterns = []string{
	"**/*_test.go", "**/*.test.*", "**/*.spec.*", "tests/**",
	"**/tests/**", "__tests__/**", "**/__tests__/**",
}

type Config struct {
	Schema              string               `json:"$schema,omitempty"`
	Version             int                  `json:"version"`
	Project             Project              `json:"project"`
	Scope               Scope                `json:"scope,omitempty"`
	Quality             Quality              `json:"quality,omitempty"`
	Portability         Portability          `json:"portability,omitempty"`
	Modules             []Module             `json:"modules"`
	Verification        Verification         `json:"verification,omitempty"`
	Checks              []Command            `json:"checks,omitempty"`
	Tests               Testing              `json:"tests"`
	SupplyChain         SupplyChain          `json:"supplyChain,omitempty"`
	PolicyModules       PolicyModules        `json:"policyModules,omitempty"`
	Exceptions          []Exception          `json:"exceptions,omitempty"`
	ConfigPath          string               `json:"-"`
	ModuleByName        map[string]int       `json:"-"`
	ActivePolicyModules []ActivePolicyModule `json:"-"`
	// JavaScriptLintScopes is resolved, never configured: it is how the
	// conditional policy modules a repository activated reach the sealed
	// bundle's lint operation.
	JavaScriptLintScopes []JavaScriptLintScope `json:"-"`
}

type Verification struct {
	MergeGate          *MergeGate `json:"mergeGate,omitempty"`
	TrustedMergeTarget string     `json:"trustedMergeTarget,omitempty"`
}

type MergeGate struct {
	RecommendedModules []string `json:"recommendedModules"`
}

type Project struct {
	Kind         string   `json:"kind"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type Scope struct {
	Exclude   []string `json:"exclude,omitempty"`
	Generated []string `json:"generated,omitempty"`
	// EntryPoints are the governed files something outside the repository
	// loads: a published entry, a runtime hook, a launcher. Reachability
	// analysis cannot infer them without executing target configuration, so a
	// target declares the ones the built-in conventions do not already cover.
	EntryPoints []string `json:"entryPoints,omitempty"`
	// Development are the governed files that exist only to build, configure,
	// or exercise the product and never ship with it. Tests already are; a
	// target declares the configuration, scripts, and harnesses that also are,
	// because only it knows which of its files it ships.
	Development []string       `json:"development,omitempty"`
	Languages   []LanguageRule `json:"languages,omitempty"`
}

type LanguageRule struct {
	Name  string   `json:"name"`
	Paths []string `json:"paths"`
}

type Quality struct {
	MaxFileLines     int        `json:"maxFileLines,omitempty"`
	MaxTestFileLines int        `json:"maxTestFileLines,omitempty"`
	Complexity       Complexity `json:"complexity,omitempty"`
	MaxDepth         int        `json:"maxDepth,omitempty"`
	MaxTestDepth     int        `json:"maxTestDepth,omitempty"`
	MaxParams        int        `json:"maxParams,omitempty"`
	MaxTestParams    int        `json:"maxTestParams,omitempty"`
}

type Complexity struct {
	Go             int `json:"go,omitempty"`
	GoTest         int `json:"goTest,omitempty"`
	TypeScript     int `json:"typescript,omitempty"`
	TypeScriptTest int `json:"typescriptTest,omitempty"`
}

type Portability struct {
	ExternalInputs []ExternalInput `json:"externalInputs,omitempty"`
}

type ExternalInput struct {
	Name                string   `json:"name"`
	Kind                string   `json:"kind"`
	Module              string   `json:"module"`
	SourcePaths         []string `json:"sourcePaths"`
	Resolution          []string `json:"resolution"`
	Environment         []string `json:"environment,omitempty"`
	UnavailableBehavior string   `json:"unavailableBehavior"`
	ContractSuite       string   `json:"contractSuite"`
	BehaviorSuite       string   `json:"behaviorSuite"`
	SiblingFallback     string   `json:"siblingFallback,omitempty"`
}

type Module struct {
	Name         string   `json:"name"`
	Paths        []string `json:"paths"`
	DependsOn    []string `json:"dependsOn,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type Command struct {
	Name               string   `json:"name"`
	Provides           []string `json:"provides,omitempty"`
	Argv               []string `json:"argv"`
	Cwd                string   `json:"cwd,omitempty"`
	Paths              []string `json:"paths,omitempty"`
	Modules            []string `json:"modules,omitempty"`
	RunOn              []string `json:"runOn,omitempty"`
	Environment        []string `json:"environment,omitempty"`
	ExclusiveResources []string `json:"exclusiveResources"`
	TimeoutSeconds     int      `json:"timeoutSeconds,omitempty"`
	Managed            bool     `json:"-"`
	PassFiles          bool     `json:"-"`
	PassFilePaths      []string `json:"-"`
	SealedEnvironment  bool     `json:"-"`
}

type Testing struct {
	RequiredKinds             []string    `json:"requiredKinds,omitempty"`
	RequiredSupplementalKinds []string    `json:"requiredSupplementalKinds,omitempty"`
	Suites                    []TestSuite `json:"suites"`
}

type TestSuite struct {
	Name               string   `json:"name"`
	Kind               string   `json:"kind"`
	Scope              string   `json:"scope"`
	Cost               string   `json:"cost,omitempty"`
	Modules            []string `json:"modules,omitempty"`
	Argv               []string `json:"argv"`
	Cwd                string   `json:"cwd,omitempty"`
	Paths              []string `json:"paths,omitempty"`
	RunOn              []string `json:"runOn,omitempty"`
	Environment        []string `json:"environment,omitempty"`
	ExclusiveResources []string `json:"exclusiveResources"`
	TimeoutSeconds     int      `json:"timeoutSeconds,omitempty"`
}

type SupplyChain struct {
	MinimumReleaseAgeDays         int      `json:"minimumReleaseAgeDays,omitempty"`
	PreferredNewDependencyAgeDays int      `json:"preferredNewDependencyAgeDays,omitempty"`
	AuditLevel                    string   `json:"auditLevel,omitempty"`
	AllowedDependencyProtocols    []string `json:"allowedDependencyProtocols,omitempty"`
	// AllowedLicenses is the license policy of a pnpm project, as the SPDX
	// identifiers a resolved dependency may be licensed under. An empty list
	// declares no license policy and enforces none.
	AllowedLicenses            []string                   `json:"allowedLicenses,omitempty"`
	NPMRegistryURL             string                     `json:"npmRegistryUrl,omitempty"`
	Environment                []string                   `json:"environment,omitempty"`
	ReleaseArtifacts           []ReleaseArtifact          `json:"releaseArtifacts,omitempty"`
	VulnerabilityAssessments   []VulnerabilityAssessment  `json:"vulnerabilityAssessments,omitempty"`
	ReleaseAgeAssessments      []ReleaseAgeAssessment     `json:"releaseAgeAssessments,omitempty"`
	DependencyOverridePolicies []DependencyOverridePolicy `json:"dependencyOverridePolicies,omitempty"`
	ArtifactSecurity           ArtifactSecurity           `json:"artifactSecurity,omitempty"`
}

// ReleaseArtifact declares one standalone third-party executable whose version
// is owned by a checked-in pin rather than a supported dependency lock. Source
// selects a fixed upstream metadata protocol; Locator identifies a package or
// GitHub repository within that protocol, never an arbitrary URL.
type ReleaseArtifact struct {
	Name        string `json:"name"`
	VersionFile string `json:"versionFile"`
	Source      string `json:"source"`
	Locator     string `json:"locator,omitempty"`
	TagPrefix   string `json:"tagPrefix,omitempty"`
}

type ArtifactSecurity struct {
	OutputDirectory string           `json:"outputDirectory,omitempty"`
	Targets         []ArtifactTarget `json:"targets,omitempty"`
}

type ArtifactTarget struct {
	Name       string            `json:"name"`
	Module     string            `json:"module,omitempty"`
	Mode       string            `json:"mode"`
	Platform   string            `json:"platform,omitempty"`
	Dockerfile string            `json:"dockerfile,omitempty"`
	Context    string            `json:"context,omitempty"`
	Archive    string            `json:"archive,omitempty"`
	Producer   *ArtifactProducer `json:"producer,omitempty"`
	OpenVEX    string            `json:"openVex,omitempty"`
}

type ArtifactProducer struct {
	Argv           []string `json:"argv"`
	Cwd            string   `json:"cwd,omitempty"`
	Environment    []string `json:"environment,omitempty"`
	Manifest       string   `json:"manifest,omitempty"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty"`
}

type VulnerabilityAssessment struct {
	ID              string `json:"id"`
	Ecosystem       string `json:"ecosystem"`
	Advisory        string `json:"advisory"`
	Package         string `json:"package"`
	AffectedVersion string `json:"affectedVersion"`
	Scope           string `json:"scope"`
	Severity        string `json:"severity"`
	Status          string `json:"status"`
	Basis           string `json:"basis"`
	Reason          string `json:"reason"`
	Impact          string `json:"impact"`
	Evidence        string `json:"evidence"`
	Tracking        string `json:"tracking"`
	Owner           string `json:"owner"`
	ApprovedBy      string `json:"approvedBy"`
	Approval        string `json:"approval"`
	Reviewed        Date   `json:"reviewed"`
	Expires         Date   `json:"expires"`
}

type ReleaseAgeAssessment struct {
	ID        string `json:"id"`
	Ecosystem string `json:"ecosystem"`
	Package   string `json:"package"`
	Version   string `json:"version"`
	Scope     string `json:"scope"`
	Category  string `json:"category"`
	Evidence  string `json:"evidence"`
	Reason    string `json:"reason"`
	Owner     string `json:"owner"`
	Reviewed  Date   `json:"reviewed"`
	Expires   Date   `json:"expires"`
}

type DependencyOverridePolicy struct {
	ID            string `json:"id"`
	Ecosystem     string `json:"ecosystem"`
	Path          string `json:"path"`
	Field         string `json:"field"`
	ContentSHA256 string `json:"contentSha256"`
	Reason        string `json:"reason"`
	Owner         string `json:"owner"`
	Reviewed      Date   `json:"reviewed"`
	Expires       Date   `json:"expires"`
}

type PolicyModules struct {
	Overrides []PolicyModuleOverride `json:"overrides,omitempty"`
}

type PolicyModuleOverride struct {
	Name    string `json:"name"`
	Root    string `json:"root,omitempty"`
	Mode    string `json:"mode"`
	Reason  string `json:"reason,omitempty"`
	Owner   string `json:"owner,omitempty"`
	Expires Date   `json:"expires,omitempty"`
}

type ActivePolicyModule struct {
	Name     string
	Root     string
	Evidence string
}

// JavaScriptLintScope is the framework rule activation one governed root
// resolved to. The nearest scope containing a file decides which rules the
// sealed lint configuration runs over it; a file under no scope is linted with
// the shared budgets alone.
type JavaScriptLintScope struct {
	Root             string
	ReactHooks       bool
	JSXAccessibility bool
}

type Exception struct {
	ID      string `json:"id"`
	Check   string `json:"check"`
	Path    string `json:"path"`
	Subject string `json:"subject"`
	Reason  string `json:"reason"`
	Owner   string `json:"owner"`
	Expires Date   `json:"expires"`
}

type Date struct {
	time.Time
}

func (date Date) MarshalJSON() ([]byte, error) {
	return []byte(date.Time.Format(`"2006-01-02"`)), nil
}

func (date *Date) UnmarshalJSON(data []byte) error {
	value, err := time.Parse(`"2006-01-02"`, string(data))
	if err != nil {
		return err
	}
	date.Time = value
	return nil
}

type Finding struct {
	Check         string
	Path          string
	Subject       string
	Message       string
	Vulnerability *VulnerabilityIdentity
	ReleaseAge    *ReleaseAgeIdentity
}

type Advisory struct {
	Check   string
	Path    string
	Subject string
	Message string
}

type VulnerabilityIdentity struct {
	Ecosystem       string
	Advisory        string
	Aliases         []string
	Package         string
	AffectedVersion string
	Scope           string
	Severity        string
	KnownExploited  bool
}

type ReleaseAgeIdentity struct {
	Ecosystem string
	Package   string
	Version   string
	Scope     string
	Released  time.Time
	Eligible  time.Time
}

type Suppressed struct {
	Finding   Finding
	Exception Exception
}

type AssessedVulnerability struct {
	Finding    Finding
	Assessment VulnerabilityAssessment
}

type AssessedReleaseAge struct {
	Finding    Finding
	Assessment ReleaseAgeAssessment
}

func (finding Finding) Error() string {
	return finding.Check + ": " + finding.Path + ": " + finding.Message
}
