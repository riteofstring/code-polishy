package policy

import "time"

const ConfigVersion = 3

const (
	BehaviorReviewOnRequest  = "on-request"
	BehaviorReviewMerge      = "merge"
	BehaviorReviewCheckpoint = "checkpoint"

	ConfigFilename = ".code-polishy.json"

	LockFilename                            = ".code-polishy.lock.json"
	MaxFileLines                            = 1000
	MaxTestFileLines                        = 1500
	MaxGoComplexity                         = 12
	MaxGoTestComplexity                     = 20
	MaxPythonComplexity                     = 10
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
	Documentation       Documentation        `json:"documentation,omitempty"`
	Packs               []PackSelection      `json:"packs,omitempty"`
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

	JavaScriptLintScopes []JavaScriptLintScope `json:"-"`
	PackManifests        []PackDependencyRule  `json:"-"`
}

type PackSelection struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

type PackDependencyRule struct {
	Pack     string
	Language string
	Paths    []string
}

type Verification struct {
	BehaviorReview     *BehaviorReviewPolicy `json:"behaviorReview,omitempty"`
	MergeGate          *MergeGate            `json:"mergeGate,omitempty"`
	TrustedMergeTarget string                `json:"trustedMergeTarget,omitempty"`
}

type BehaviorReviewPolicy struct {
	DefaultRequiredAt string                  `json:"defaultRequiredAt,omitempty"`
	Features          []BehaviorReviewFeature `json:"features,omitempty"`
}

func (policy BehaviorReviewPolicy) EffectiveRequiredAt(feature BehaviorReviewFeature) string {
	if feature.RequiredAt == "" {
		return policy.DefaultRequiredAt
	}
	return feature.RequiredAt
}

type BehaviorReviewFeature struct {
	Name       string   `json:"name"`
	Modules    []string `json:"modules,omitempty"`
	Paths      []string `json:"paths,omitempty"`
	Suites     []string `json:"suites"`
	RequiredAt string   `json:"requiredAt,omitempty"`
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

	EntryPoints []string `json:"entryPoints,omitempty"`

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
	AllowComments    *bool      `json:"allowComments,omitempty"`
	MaxDepth         int        `json:"maxDepth,omitempty"`
	MaxTestDepth     int        `json:"maxTestDepth,omitempty"`
	MaxParams        int        `json:"maxParams,omitempty"`
	MaxTestParams    int        `json:"maxTestParams,omitempty"`
}

type Complexity struct {
	Go             int `json:"go,omitempty"`
	GoTest         int `json:"goTest,omitempty"`
	Python         int `json:"python,omitempty"`
	TypeScript     int `json:"typescript,omitempty"`
	TypeScriptTest int `json:"typescriptTest,omitempty"`
}

func (quality Quality) CommentsAllowed() bool {
	return quality.AllowComments == nil || *quality.AllowComments
}

type Portability struct {
	ExternalInputs []ExternalInput `json:"externalInputs,omitempty"`
}

type Documentation struct {
	Design        []DesignDocument `json:"design,omitempty"`
	ProductInputs []string         `json:"productInputs,omitempty"`
}

type DesignDocument struct {
	Path        string   `json:"path"`
	Module      string   `json:"module,omitempty"`
	SourcePaths []string `json:"sourcePaths,omitempty"`
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
	Name               string       `json:"name"`
	Provides           []string     `json:"provides,omitempty"`
	Argv               []string     `json:"argv"`
	Cwd                string       `json:"cwd,omitempty"`
	Paths              []string     `json:"paths,omitempty"`
	Modules            []string     `json:"modules,omitempty"`
	RunOn              []string     `json:"runOn,omitempty"`
	Environment        []string     `json:"environment,omitempty"`
	ExclusiveResources []string     `json:"exclusiveResources"`
	TimeoutSeconds     int          `json:"timeoutSeconds,omitempty"`
	Managed            bool         `json:"-"`
	PassFiles          bool         `json:"-"`
	PassFilePaths      []string     `json:"-"`
	SealedEnvironment  bool         `json:"-"`
	Adapter            *PackAdapter `json:"-"`
	Stdin              []byte       `json:"-"`
}

type PackAdapter struct {
	PackName        string
	PackVersion     string
	PackDigest      string
	PackRoot        string
	ProtocolVersion int
	Capability      string
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

	AllowedLicenses            []string                   `json:"allowedLicenses,omitempty"`
	NPMRegistryURL             string                     `json:"npmRegistryUrl,omitempty"`
	Environment                []string                   `json:"environment,omitempty"`
	ReleaseArtifacts           []ReleaseArtifact          `json:"releaseArtifacts,omitempty"`
	VulnerabilityAssessments   []VulnerabilityAssessment  `json:"vulnerabilityAssessments,omitempty"`
	ReleaseAgeAssessments      []ReleaseAgeAssessment     `json:"releaseAgeAssessments,omitempty"`
	DependencyOverridePolicies []DependencyOverridePolicy `json:"dependencyOverridePolicies,omitempty"`
	ArtifactSecurity           ArtifactSecurity           `json:"artifactSecurity,omitempty"`
}

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
	Line          int
	Column        int
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
