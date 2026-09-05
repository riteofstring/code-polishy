package policy

import (
	"encoding/json"
	"time"
)

const ConfigVersion = 4

const (
	BehaviorReviewOnRequest  = "on-request"
	BehaviorReviewMerge      = "merge"
	BehaviorReviewCheckpoint = "checkpoint"
	FinalGateOwnerLocal      = "local"
	FinalGateOwnerCI         = "ci"

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
	".git/**", ".tools/**", ".code-polishy-reports/**", ".code-polishy-artifacts/**", "node_modules/**", "**/node_modules/**",
	".venv/**", "**/.venv/**",
	"dist/**", "**/dist/**", "build/**", "**/build/**", "coverage/**",
	"**/coverage/**", "playwright-report/**", "**/playwright-report/**",
	"test-results/**", "**/test-results/**",
}

var DefaultGenerated = []string{
	"**/*.generated.*", "**/*_gen.go", "**/*.gen.go",
}

var DefaultTestPatterns = []string{
	"**/*_test.go", "**/test_*.py", "**/*_test.py", "**/*.test.*", "**/*.spec.*", "tests/**",
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
	Generation          Generation           `json:"generation,omitempty"`
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
	FinalGateOwner     string                `json:"finalGateOwner,omitempty"`
}

func (verification Verification) EffectiveFinalGateOwner() string {
	if verification.FinalGateOwner == "" {
		return FinalGateOwnerLocal
	}
	return verification.FinalGateOwner
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
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Aliases     []string `json:"aliases,omitempty"`
	Modules     []string `json:"modules,omitempty"`
	Paths       []string `json:"paths,omitempty"`
	Suites      []string `json:"suites"`
	RequiredAt  string   `json:"requiredAt,omitempty"`
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
	Data      []string `json:"data,omitempty"`

	EntryPoints                 []string                     `json:"entryPoints,omitempty"`
	GeneratedJavaScript         []GeneratedJavaScript        `json:"generatedJavaScript,omitempty"`
	PythonDynamicReferences     []PythonDynamicReference     `json:"pythonDynamicReferences,omitempty"`
	PythonComputedImports       []PythonComputedImport       `json:"pythonComputedImports,omitempty"`
	PythonExternalPluginImports []PythonExternalPluginImport `json:"pythonExternalPluginImports,omitempty"`
	PythonExternalAttributes    []PythonExternalAttribute    `json:"pythonExternalAttributes,omitempty"`

	Development []string       `json:"development,omitempty"`
	Languages   []LanguageRule `json:"languages,omitempty"`
}

type GeneratedJavaScript struct {
	Paths         []string `json:"paths"`
	SourcePackage string   `json:"sourcePackage"`
}

type PythonDynamicReference struct {
	Kind     string                 `json:"kind"`
	Project  string                 `json:"project"`
	Target   *PythonDynamicTarget   `json:"target,omitempty"`
	Registry *PythonDynamicRegistry `json:"registry,omitempty"`
	Consumer PythonDynamicConsumer  `json:"consumer"`
}

type PythonDynamicTarget struct {
	Module string `json:"module"`
	Symbol string `json:"symbol"`
}

type PythonDynamicRegistry struct {
	Path        string `json:"path"`
	JSONPointer string `json:"jsonPointer"`
}

type PythonDynamicConsumer struct {
	Kind         string               `json:"kind"`
	Importer     string               `json:"importer"`
	Module       string               `json:"module"`
	Callable     string               `json:"callable"`
	Site         PythonSourceLocation `json:"site"`
	Callee       string               `json:"callee"`
	Shape        string               `json:"shape"`
	Argument     string               `json:"argument"`
	SourceSHA256 string               `json:"sourceSha256"`
}

type PythonComputedImport struct {
	Project         string                      `json:"project"`
	Importer        string                      `json:"importer"`
	Module          string                      `json:"module"`
	Callable        string                      `json:"callable,omitempty"`
	ModuleScope     bool                        `json:"moduleScope,omitempty"`
	Callee          string                      `json:"callee"`
	Line            int                         `json:"line"`
	Column          int                         `json:"column"`
	Shape           string                      `json:"shape"`
	Argument        string                      `json:"argument"`
	SourceSHA256    string                      `json:"sourceSha256"`
	Namespace       string                      `json:"namespace,omitempty"`
	EntryPointGroup string                      `json:"entryPointGroup,omitempty"`
	Targets         []string                    `json:"targets,omitempty"`
	Configuration   []PythonComputedImportInput `json:"configuration,omitempty"`
}

type PythonComputedImportInput struct {
	Path        string `json:"path"`
	JSONPointer string `json:"jsonPointer"`
	SHA256      string `json:"sha256"`
}

type PythonExternalAttribute struct {
	Project   string                 `json:"project"`
	Module    string                 `json:"module"`
	Callable  string                 `json:"callable"`
	Receiver  PythonExternalReceiver `json:"receiver"`
	Attribute string                 `json:"attribute"`
	Write     PythonSourceLocation   `json:"write"`
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
	Design        []DesignDocument     `json:"design,omitempty"`
	Handoffs      []OperationalHandoff `json:"handoffs,omitempty"`
	ProductInputs []string             `json:"productInputs,omitempty"`
}

type OperationalHandoff struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Path        string   `json:"path"`
	Situations  []string `json:"situations,omitempty"`
	Modules     []string `json:"modules,omitempty"`
	SourcePaths []string `json:"sourcePaths,omitempty"`
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
	Name                  string         `json:"name"`
	Provides              []string       `json:"provides,omitempty"`
	Argv                  []string       `json:"argv"`
	Cwd                   string         `json:"cwd,omitempty"`
	Paths                 []string       `json:"paths,omitempty"`
	Modules               []string       `json:"modules,omitempty"`
	RunOn                 []string       `json:"runOn,omitempty"`
	Environment           []string       `json:"environment,omitempty"`
	ExclusiveResources    []string       `json:"exclusiveResources"`
	TimeoutSeconds        int            `json:"timeoutSeconds,omitempty"`
	Managed               bool           `json:"-"`
	PassFiles             bool           `json:"-"`
	PassFilePaths         []string       `json:"-"`
	SealedEnvironment     bool           `json:"-"`
	Adapter               *PackAdapter   `json:"-"`
	Stdin                 []byte         `json:"-"`
	EnvironmentOverrides  []string       `json:"-"`
	TestArtifacts         []TestArtifact `json:"-"`
	TestArtifactSuite     string         `json:"-"`
	TestArtifactDirectory string         `json:"-"`
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
	Paths                     []string        `json:"paths,omitempty"`
	Ownership                 []TestOwnership `json:"ownership"`
	RequiredKinds             []string        `json:"requiredKinds,omitempty"`
	RequiredSupplementalKinds []string        `json:"requiredSupplementalKinds,omitempty"`
	Suites                    []TestSuite     `json:"suites"`
}

type TestOwnership struct {
	Paths        []string `json:"paths"`
	Module       string   `json:"module"`
	FocusedSuite string   `json:"focusedSuite"`
}

type TestSuite struct {
	Name               string         `json:"name"`
	Kind               string         `json:"kind"`
	Scope              string         `json:"scope"`
	Reusable           bool           `json:"reusable,omitempty"`
	Cost               string         `json:"cost,omitempty"`
	Modules            []string       `json:"modules,omitempty"`
	Argv               []string       `json:"argv"`
	Cwd                string         `json:"cwd,omitempty"`
	Paths              []string       `json:"paths,omitempty"`
	ExtraInputs        []string       `json:"extraInputs,omitempty"`
	Covers             []string       `json:"covers,omitempty"`
	Artifacts          []TestArtifact `json:"artifacts,omitempty"`
	RunOn              []string       `json:"runOn,omitempty"`
	Environment        []string       `json:"environment,omitempty"`
	ExclusiveResources []string       `json:"exclusiveResources"`
	TimeoutSeconds     int            `json:"timeoutSeconds,omitempty"`
}

type TestArtifact struct {
	Path     string `json:"path"`
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
}

type SupplyChain struct {
	MinimumReleaseAgeDays         int      `json:"minimumReleaseAgeDays,omitempty"`
	PreferredNewDependencyAgeDays int      `json:"preferredNewDependencyAgeDays,omitempty"`
	AuditLevel                    string   `json:"auditLevel,omitempty"`
	AllowedDependencyProtocols    []string `json:"allowedDependencyProtocols,omitempty"`
	RecurringSecurityMonitoring   bool     `json:"recurringSecurityMonitoring,omitempty"`

	AllowedLicenses            []string                   `json:"allowedLicenses,omitempty"`
	NPMRegistryURL             string                     `json:"npmRegistryUrl,omitempty"`
	Environment                []string                   `json:"environment,omitempty"`
	ReleaseArtifacts           []ReleaseArtifact          `json:"releaseArtifacts,omitempty"`
	VulnerabilityAssessments   []VulnerabilityAssessment  `json:"vulnerabilityAssessments,omitempty"`
	ReleaseAgeAssessments      []ReleaseAgeAssessment     `json:"releaseAgeAssessments,omitempty"`
	DependencyOverridePolicies []DependencyOverridePolicy `json:"dependencyOverridePolicies,omitempty"`
	ArtifactSecurity           ArtifactSecurity           `json:"artifactSecurity,omitempty"`
	GitEvidence                GitEvidence                `json:"gitEvidence,omitempty"`
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

func (override PolicyModuleOverride) MarshalJSON() ([]byte, error) {
	type wireOverride struct {
		Name    string `json:"name"`
		Root    string `json:"root,omitempty"`
		Mode    string `json:"mode"`
		Reason  string `json:"reason,omitempty"`
		Owner   string `json:"owner,omitempty"`
		Expires *Date  `json:"expires,omitempty"`
	}
	var expires *Date
	if !override.Expires.IsZero() {
		expires = &override.Expires
	}
	return json.Marshal(wireOverride{
		Name: override.Name, Root: override.Root, Mode: override.Mode,
		Reason: override.Reason, Owner: override.Owner, Expires: expires,
	})
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
	Check               string                     `json:"ruleId"`
	Fingerprint         string                     `json:"fingerprint"`
	Severity            FindingSeverity            `json:"severity"`
	Status              FindingStatus              `json:"status"`
	Scope               FindingScope               `json:"scope"`
	SelectionRelation   SelectionRelation          `json:"selectionRelation"`
	SelectionEvidence   []FindingSelectionEvidence `json:"selectionEvidence,omitempty"`
	Path                string                     `json:"path"`
	Line                int                        `json:"line,omitempty"`
	Column              int                        `json:"column,omitempty"`
	EndLine             int                        `json:"endLine,omitempty"`
	EndColumn           int                        `json:"endColumn,omitempty"`
	Module              string                     `json:"module,omitempty"`
	Subject             string                     `json:"subject"`
	Related             []FindingLocation          `json:"relatedLocations,omitempty"`
	Fields              map[string]string          `json:"fields,omitempty"`
	GeneratedProducer   string                     `json:"generatedProducer,omitempty"`
	DependencyComponent *DependencyComponent       `json:"dependencyComponent,omitempty"`
	Message             string                     `json:"message"`
	Remediation         FindingRemediation         `json:"remediation"`
	Vulnerability       *VulnerabilityIdentity     `json:"vulnerability,omitempty"`
	ReleaseAge          *ReleaseAgeIdentity        `json:"releaseAge,omitempty"`
	SemanticIdentity    []string                   `json:"semanticIdentity,omitempty"`
}

type DependencyComponent struct {
	Protocol       string           `json:"protocol"`
	Classification string           `json:"classification"`
	Identity       string           `json:"identity"`
	Members        []DependencyNode `json:"members"`
	Edges          []DependencyEdge `json:"edges"`
	Witness        []DependencyEdge `json:"witness"`
}

type DependencyNode struct {
	Path       string `json:"path"`
	Language   string `json:"language"`
	Generated  bool   `json:"generated"`
	Test       bool   `json:"test"`
	Root       string `json:"root"`
	Module     string `json:"module"`
	Resolution string `json:"resolutionUnit"`
}

type DependencyEdge struct {
	Source           string `json:"source"`
	Target           string `json:"target"`
	SourceResolution string `json:"sourceResolutionUnit"`
	TargetResolution string `json:"targetResolutionUnit"`
	Line             int    `json:"line"`
	Column           int    `json:"column"`
	Ecosystem        string `json:"ecosystem"`
	Kind             string `json:"kind"`
}

type FindingSeverity string

const (
	FindingError       FindingSeverity = "error"
	FindingWarning     FindingSeverity = "warning"
	FindingInformation FindingSeverity = "information"
)

type FindingStatus string

const (
	FindingOpen       FindingStatus = "open"
	FindingSuppressed FindingStatus = "suppressed"
	FindingReviewed   FindingStatus = "reviewed"
)

type SelectionRelation string

const (
	SelectionSelected SelectionRelation = "selected"
	SelectionRelated  SelectionRelation = "related"
	SelectionContext  SelectionRelation = "context"
	SelectionGlobal   SelectionRelation = "global"
)

type FindingScope struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type FindingLocation struct {
	Path      string `json:"path"`
	Line      int    `json:"line,omitempty"`
	Column    int    `json:"column,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	EndColumn int    `json:"endColumn,omitempty"`
	Message   string `json:"message,omitempty"`
}

type FindingSelectionEvidence struct {
	Selected FindingLocation `json:"selected"`
	Related  FindingLocation `json:"related"`
	Kind     string          `json:"kind"`
}

type FindingRemediation struct {
	Summary       string                 `json:"summary"`
	Replacement   string                 `json:"replacement,omitempty"`
	Configuration json.RawMessage        `json:"configuration,omitempty"`
	NextCommand   *FindingCommand        `json:"nextCommand,omitempty"`
	Generation    *GenerationRemediation `json:"generation,omitempty"`
}

type GenerationRemediation struct {
	Producer      string                   `json:"producer"`
	Inputs        []string                 `json:"inputs"`
	Generate      GenerationCommand        `json:"generate"`
	Verify        GenerationCommand        `json:"verify"`
	Prerequisites []GenerationPrerequisite `json:"prerequisites"`
}

type GenerationPrerequisite struct {
	Operation string `json:"operation"`
	Message   string `json:"message"`
}

type FindingCommand struct {
	Argv []string `json:"argv"`
	Cwd  string   `json:"cwd"`
}

type Advisory struct {
	Check   string
	Path    string
	Subject string
	Message string
}

type VulnerabilityIdentity struct {
	Ecosystem       string   `json:"ecosystem"`
	Advisory        string   `json:"advisory"`
	Aliases         []string `json:"aliases"`
	Package         string   `json:"package"`
	AffectedVersion string   `json:"affectedVersion"`
	Scope           string   `json:"scope"`
	Severity        string   `json:"severity"`
	KnownExploited  bool     `json:"knownExploited"`
}

type ReleaseAgeIdentity struct {
	Ecosystem string    `json:"ecosystem"`
	Package   string    `json:"package"`
	Version   string    `json:"version"`
	Scope     string    `json:"scope"`
	Released  time.Time `json:"released"`
	Eligible  time.Time `json:"eligible"`
}

type Suppressed struct {
	Finding   Finding   `json:"finding"`
	Exception Exception `json:"exception"`
}

type AssessedVulnerability struct {
	Finding    Finding                 `json:"finding"`
	Assessment VulnerabilityAssessment `json:"assessment"`
}

type AssessedReleaseAge struct {
	Finding    Finding              `json:"finding"`
	Assessment ReleaseAgeAssessment `json:"assessment"`
}

func (finding Finding) Error() string {
	return finding.Check + ": " + finding.Path + ": " + finding.Message
}
