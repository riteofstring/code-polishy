package engine

import "github.com/riteofstring/code-polishy/internal/policy"

const PackMigrationProtocol = "code-polishy-pack-migration/v1"
const PackMigrationSchema = "https://raw.githubusercontent.com/riteofstring/code-polishy/main/schema/code-polishy-pack-migration-v1.schema.json"
const PackMigrationDirectory = ".code-polishy-reports/pack-migrations"

type PackMigrationPlan struct {
	Schema                     string                    `json:"$schema"`
	Protocol                   string                    `json:"protocol"`
	MigrationID                string                    `json:"migrationId"`
	EngineVersion              string                    `json:"engineVersion"`
	ConfigPath                 string                    `json:"configPath"`
	LockSHA256                 string                    `json:"lockSha256"`
	CatalogPath                string                    `json:"catalogPath"`
	CatalogSHA256              string                    `json:"catalogSha256"`
	BeforeConfigSHA256         string                    `json:"beforeConfigSha256"`
	AfterConfigSHA256          string                    `json:"afterConfigSha256"`
	BackupPath                 string                    `json:"backupPath"`
	BeforePacks                []policy.PackSelection    `json:"beforePacks"`
	AfterPacks                 []policy.PackSelection    `json:"afterPacks"`
	Packs                      []PackMigrationPack       `json:"packs"`
	Inventory                  PackMigrationInventory    `json:"inventory"`
	Coverage                   []PackMigrationCoverage   `json:"coverage"`
	CoverageSHA256             string                    `json:"coverageSha256"`
	CurrentDiagnosticsSHA256   string                    `json:"currentDiagnosticsSha256"`
	CandidateDiagnosticsSHA256 string                    `json:"candidateDiagnosticsSha256"`
	AddedDiagnostics           []PackMigrationDiagnostic `json:"addedDiagnostics"`
	Gaps                       []PackMigrationGap        `json:"gaps"`
	Ready                      bool                      `json:"ready"`
}

type PackMigrationPack struct {
	Selection      policy.PackSelection `json:"selection"`
	Capabilities   []string             `json:"capabilities"`
	Tools          []string             `json:"tools"`
	Dependencies   []string             `json:"dependencies"`
	Licenses       []string             `json:"licenses"`
	Builder        string               `json:"builder"`
	SourceRevision string               `json:"sourceRevision"`
}

type PackMigrationInventory struct {
	Files        int                              `json:"files"`
	Languages    []PackMigrationLanguageInventory `json:"languages"`
	CustomClaims []PackMigrationCustomClaim       `json:"customClaims"`
	NativeClaims int                              `json:"nativeClaims"`
	PackClaims   int                              `json:"packClaims"`
}

type PackMigrationLanguageInventory struct {
	Language       string `json:"language"`
	Files          int    `json:"files"`
	GeneratedFiles int    `json:"generatedFiles"`
}

type PackMigrationCustomClaim struct {
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	Paths        []string `json:"paths"`
	Profiles     []string `json:"profiles"`
}

type PackMigrationCoverage struct {
	Language   string `json:"language"`
	Capability string `json:"capability"`
	Profile    string `json:"profile"`
	Generated  bool   `json:"generated"`
	Required   int    `json:"required"`
	PackOwned  int    `json:"packOwned"`
	Missing    int    `json:"missing"`
}

type PackMigrationDiagnostic struct {
	Fingerprint string `json:"fingerprint"`
	RuleID      string `json:"ruleId"`
	Severity    string `json:"severity"`
	Path        string `json:"path"`
	Message     string `json:"message"`
}

type PackMigrationGap struct {
	Path       string `json:"path"`
	Language   string `json:"language"`
	Capability string `json:"capability"`
	Profile    string `json:"profile"`
	Message    string `json:"message"`
}

type PackMigrationPlanOptions struct {
	RepositoryRoot string
	PolicyRoot     string
	ConfigPath     string
	CatalogPath    string
	CatalogSHA256  string
	References     []string
	DataRoot       string
}

type PackMigrationActionOptions struct {
	RepositoryRoot string
	PolicyRoot     string
	ConfigPath     string
	PlanPath       string
	DataRoot       string
}

type PackMigrationPlanResult struct {
	Plan           PackMigrationPlan
	PlanPath       string
	Installed      int
	AlreadyPresent int
}

type PackMigrationActionResult struct {
	Plan    PackMigrationPlan
	Changed bool
}
