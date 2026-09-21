package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const maximumPackMigrationGaps = 10000

var packMigrationCapabilities = []string{"architecture", "complexity", "dead-code", "format", "lint", "typecheck"}
var packMigrationLanguages = []string{"go", "python", "shell", "typescript"}

type packMigrationAssessment struct {
	inventory                  PackMigrationInventory
	coverage                   []PackMigrationCoverage
	coverageSHA256             string
	currentDiagnosticsSHA256   string
	candidateDiagnosticsSHA256 string
	addedDiagnostics           []PackMigrationDiagnostic
	gaps                       []PackMigrationGap
}

type packMigrationCoverageRecord struct {
	Path        string `json:"path"`
	Language    string `json:"language"`
	Capability  string `json:"capability"`
	Profile     string `json:"profile"`
	Generated   bool   `json:"generated"`
	BeforeOwner string `json:"beforeOwner"`
	AfterOwner  string `json:"afterOwner"`
	PackOwned   bool   `json:"packOwned"`
}

type packMigrationCoverageKey struct {
	language, capability, profile string
	generated                     bool
}

func assessPackMigration(ctx context.Context, current, candidate *Engine, configured policy.Config) (packMigrationAssessment, error) {
	currentFiles, err := current.Repository.AllFiles()
	if err != nil {
		return packMigrationAssessment{}, err
	}
	candidateFiles, err := candidate.Repository.AllFiles()
	if err != nil {
		return packMigrationAssessment{}, err
	}
	if !slices.Equal(currentFiles, candidateFiles) {
		return packMigrationAssessment{}, errors.New("candidate pack policy changes the governed file inventory")
	}
	records, coverage, gaps, nativeClaims, packClaims, err := comparePackMigrationCoverage(current.Repository, candidate.Repository, currentFiles)
	if err != nil {
		return packMigrationAssessment{}, err
	}
	currentReport, err := current.Doctor(ctx)
	if err != nil {
		return packMigrationAssessment{}, fmt.Errorf("evaluate current pack policy: %w", err)
	}
	candidateReport, err := candidate.Doctor(ctx)
	if err != nil {
		return packMigrationAssessment{}, fmt.Errorf("evaluate candidate pack policy: %w", err)
	}
	currentDiagnostics := packMigrationDiagnostics(currentReport)
	candidateDiagnostics := packMigrationDiagnostics(candidateReport)
	return packMigrationAssessment{
		inventory:                  buildPackMigrationInventory(current.Repository, configured, currentFiles, nativeClaims, packClaims),
		coverage:                   coverage,
		coverageSHA256:             packMigrationDigest(records),
		currentDiagnosticsSHA256:   packMigrationDigest(currentDiagnostics),
		candidateDiagnosticsSHA256: packMigrationDigest(candidateDiagnostics),
		addedDiagnostics:           addedPackMigrationDiagnostics(currentDiagnostics, candidateDiagnostics),
		gaps:                       gaps,
	}, nil
}

func comparePackMigrationCoverage(current, candidate repository.Repository, files []string) ([]packMigrationCoverageRecord, []PackMigrationCoverage, []PackMigrationGap, int, int, error) {
	records := []packMigrationCoverageRecord{}
	gaps := []PackMigrationGap{}
	summaries := map[packMigrationCoverageKey]*PackMigrationCoverage{}
	nativeClaims, packClaims := 0, 0
	for _, path := range files {
		language := current.Language(path)
		if !slices.Contains(packMigrationLanguages, language) {
			continue
		}
		generated := current.IsGenerated(path)
		for _, capability := range packMigrationCapabilities {
			profile := "check"
			if capability == "format" {
				profile = "format"
			}
			before := current.AnalysisOwner(path, capability, profile)
			required := current.NativeCapability(path, capability) || before.Pack != ""
			if !required {
				continue
			}
			if before.Native {
				nativeClaims++
			} else if before.Pack != "" {
				packClaims++
			}
			after := candidate.AnalysisOwner(path, capability, profile)
			packOwned := after.Pack != "" && after.Problem == ""
			records = append(records, packMigrationCoverageRecord{
				Path: path, Language: language, Capability: capability, Profile: profile, Generated: generated,
				BeforeOwner: packMigrationOwner(before), AfterOwner: packMigrationOwner(after), PackOwned: packOwned,
			})
			key := packMigrationCoverageKey{language: language, capability: capability, profile: profile, generated: generated}
			summary := summaries[key]
			if summary == nil {
				summary = &PackMigrationCoverage{Language: language, Capability: capability, Profile: profile, Generated: generated}
				summaries[key] = summary
			}
			summary.Required++
			if packOwned {
				summary.PackOwned++
				continue
			}
			summary.Missing++
			if len(gaps) >= maximumPackMigrationGaps {
				return nil, nil, nil, 0, 0, fmt.Errorf("pack migration exceeds %d coverage gaps", maximumPackMigrationGaps)
			}
			message := after.Problem
			if message == "" {
				message = "candidate selection does not assign this required capability to an exact pack"
			}
			gaps = append(gaps, PackMigrationGap{Path: path, Language: language, Capability: capability, Profile: profile, Message: message})
		}
	}
	sort.Slice(records, func(left, right int) bool {
		return packMigrationCoverageRecordKey(records[left]) < packMigrationCoverageRecordKey(records[right])
	})
	coverage := make([]PackMigrationCoverage, 0, len(summaries))
	for _, summary := range summaries {
		coverage = append(coverage, *summary)
	}
	sort.Slice(coverage, func(left, right int) bool {
		return packMigrationCoverageSummaryKey(coverage[left]) < packMigrationCoverageSummaryKey(coverage[right])
	})
	sort.Slice(gaps, func(left, right int) bool { return packMigrationGapKey(gaps[left]) < packMigrationGapKey(gaps[right]) })
	return records, coverage, gaps, nativeClaims, packClaims, nil
}

func packMigrationOwner(owner repository.AnalysisOwner) string {
	if owner.Problem != "" {
		return "unavailable:" + owner.Problem
	}
	if owner.Pack != "" {
		return "pack:" + owner.Pack + ":" + owner.Name
	}
	if owner.Native {
		return owner.Name
	}
	return "unassigned"
}

func packMigrationCoverageRecordKey(record packMigrationCoverageRecord) string {
	return strings.Join([]string{record.Language, record.Capability, record.Profile, fmt.Sprintf("%t", record.Generated), record.Path}, "\x00")
}

func packMigrationCoverageSummaryKey(summary PackMigrationCoverage) string {
	return strings.Join([]string{summary.Language, summary.Capability, summary.Profile, fmt.Sprintf("%t", summary.Generated)}, "\x00")
}

func packMigrationGapKey(gap PackMigrationGap) string {
	return strings.Join([]string{gap.Language, gap.Capability, gap.Profile, gap.Path, gap.Message}, "\x00")
}

func buildPackMigrationInventory(repo repository.Repository, configured policy.Config, files []string, nativeClaims, packClaims int) PackMigrationInventory {
	byLanguage := map[string]*PackMigrationLanguageInventory{}
	for _, path := range files {
		language := repo.Language(path)
		if language == "" {
			continue
		}
		inventory := byLanguage[language]
		if inventory == nil {
			inventory = &PackMigrationLanguageInventory{Language: language}
			byLanguage[language] = inventory
		}
		inventory.Files++
		if repo.IsGenerated(path) {
			inventory.GeneratedFiles++
		}
	}
	languages := make([]PackMigrationLanguageInventory, 0, len(byLanguage))
	for _, inventory := range byLanguage {
		languages = append(languages, *inventory)
	}
	sort.Slice(languages, func(left, right int) bool { return languages[left].Language < languages[right].Language })
	claims := []PackMigrationCustomClaim{}
	for _, command := range configured.Checks {
		claims = append(claims, PackMigrationCustomClaim{
			Name: command.Name, Capabilities: sortedPackMigrationStrings(command.Provides),
			Paths: sortedPackMigrationStrings(command.Paths), Profiles: sortedPackMigrationStrings(command.RunOn),
		})
	}
	sort.Slice(claims, func(left, right int) bool { return claims[left].Name < claims[right].Name })
	return PackMigrationInventory{Files: len(files), Languages: languages, CustomClaims: claims, NativeClaims: nativeClaims, PackClaims: packClaims}
}

func packMigrationDiagnostics(report Report) []PackMigrationDiagnostic {
	diagnostics := []PackMigrationDiagnostic{}
	for _, finding := range report.Findings {
		if finding.Status != policy.FindingOpen {
			continue
		}
		diagnostics = append(diagnostics, PackMigrationDiagnostic{
			Fingerprint: finding.Fingerprint, RuleID: finding.Check, Severity: string(finding.Severity),
			Path: finding.Path, Message: finding.Message,
		})
	}
	sort.Slice(diagnostics, func(left, right int) bool { return diagnostics[left].Fingerprint < diagnostics[right].Fingerprint })
	return diagnostics
}

func addedPackMigrationDiagnostics(current, candidate []PackMigrationDiagnostic) []PackMigrationDiagnostic {
	present := map[string]bool{}
	for _, diagnostic := range current {
		present[diagnostic.Fingerprint] = true
	}
	added := []PackMigrationDiagnostic{}
	for _, diagnostic := range candidate {
		if !present[diagnostic.Fingerprint] {
			added = append(added, diagnostic)
		}
	}
	return added
}

func packMigrationHasNewErrors(diagnostics []PackMigrationDiagnostic) bool {
	return slices.ContainsFunc(diagnostics, func(diagnostic PackMigrationDiagnostic) bool {
		return diagnostic.Severity == string(policy.FindingError)
	})
}

func packMigrationDigest(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func sortedPackMigrationStrings(values []string) []string {
	result := slices.Clone(values)
	if result == nil {
		result = []string{}
	}
	sort.Strings(result)
	return slices.Compact(result)
}
