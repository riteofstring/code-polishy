package supplychain

import (
	"encoding/hex"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func validateGitEvidenceStatement(statement gitEvidenceStatement, provider policy.GitEvidenceProvider, scope, lockDigest string, packages []resolvedPackage, now time.Time) error {
	if statement.Scope != scope || statement.LockSHA256 != lockDigest || statement.InventorySHA256 != gitEvidenceInventoryDigest(packages) {
		return gitEvidenceFailure("stale", "Git evidence does not bind the current lockfile and complete resolved dependency inventory")
	}
	if err := validateGitEvidenceTimes(statement, now); err != nil {
		return err
	}
	if err := validateGitEvidenceScan(statement.Scan, provider, statement.IssuedAt); err != nil {
		return err
	}
	if err := validateGitEvidenceSubjects(statement.Subjects, packages, provider.Issuer, statement.IssuedAt); err != nil {
		return err
	}
	return validateGitEvidenceScanPackages(statement.Scan, gitEvidencePackages(packages))
}

func validateGitEvidenceTimes(statement gitEvidenceStatement, now time.Time) error {
	if statement.IssuedAt.IsZero() || statement.IssuedAt.After(now) || now.Sub(statement.IssuedAt) > maximumGitEvidenceAge {
		return gitEvidenceFailure("stale", "Git evidence must have an authenticated issue time within the last 24 hours")
	}
	if !statement.ExpiresAt.After(now) || statement.ExpiresAt.Sub(statement.IssuedAt) > maximumGitEvidenceAge {
		return gitEvidenceFailure("expired", "Git evidence has expired or exceeds the 24-hour validity bound")
	}
	return nil
}

func validateGitEvidenceScan(scan gitEvidenceScan, provider policy.GitEvidenceProvider, issued time.Time) error {
	if err := gitEvidenceCoverageStatus(scan); err != nil {
		return err
	}
	if !slices.Contains(provider.Scanners, scan.Scanner) {
		return gitEvidenceFailure("unsupported-scanner", "Git evidence scanner identity is not authorized by configured provider trust")
	}
	if scan.CompletedAt.After(issued) || issued.Sub(scan.CompletedAt) > time.Hour {
		return gitEvidenceFailure("stale", "Git security assessment must complete within one hour before evidence issuance")
	}
	if !gitEvidenceBoundedText(scan.AdvisoryVersion, 256) || scan.AdvisoryUpdated.After(scan.CompletedAt) || issued.Sub(scan.AdvisoryUpdated) > maximumGitEvidenceAge {
		return gitEvidenceFailure("stale", "Git evidence must identify authenticated advisory data updated within the last 24 hours")
	}
	if !gitEvidenceScanInventoriesBounded(scan) {
		return gitEvidenceFailure("coverage", "Git evidence must explicitly report bounded vulnerability and license inventories")
	}
	return nil
}

func gitEvidenceScanInventoriesBounded(scan gitEvidenceScan) bool {
	return scan.Vulnerabilities != nil && len(scan.Vulnerabilities) <= 8192 && scan.Licenses != nil && len(scan.Licenses) <= 8192
}

func gitEvidenceCoverageStatus(scan gitEvidenceScan) error {
	if scan.Coverage == "authentication-unavailable" {
		return gitEvidenceFailure("authentication", "The trusted CI provider could not authenticate to the Git dependency; restore its existing repository credentials and rerun assessment")
	}
	if scan.Coverage == "unsupported" {
		return gitEvidenceFailure("unsupported-scanner", "The trusted CI provider reports unsupported Git scanning; select an authorized scanner covering the exact source and dependency inventory")
	}
	if scan.Coverage != "complete" || scan.Target != "git-contents-and-resolved-lock/v1" {
		return gitEvidenceFailure("coverage", "Git evidence must assess all exact Git contents and the complete resolved lock inventory")
	}
	return nil
}

func validateGitEvidenceSubjects(subjects []gitEvidenceSubject, packages []resolvedPackage, issuer string, issued time.Time) error {
	expected := map[string]resolvedPackage{}
	for _, item := range packages {
		if item.Source.Kind == "unsupported" {
			return gitEvidenceFailure("coverage", "The resolved lock contains an unsupported dependency source")
		}
		if item.Source.Kind == "git" {
			if err := item.Source.Git.ValidateExactPin(); err != nil {
				return gitEvidenceFailure("coverage", "Git evidence requires exact current candidate commit pins")
			}
			key := gitEvidencePackageKey(gitEvidencePackage{Ecosystem: item.Ecosystem, Name: item.Name, Version: item.Version, Source: gitEvidencePackageSource(item)})
			expected[key] = item
		}
	}
	if len(subjects) == 0 || len(subjects) > 1024 || len(subjects) != len(expected) {
		return gitEvidenceFailure("coverage", "Git evidence does not cover every resolved Git source exactly once")
	}
	for _, subject := range subjects {
		key := gitEvidenceSubjectKey(subject)
		if _, found := expected[key]; !found {
			return gitEvidenceFailure("stale", "Git evidence names a different repository, commit, subdirectory, or package")
		}
		delete(expected, key)
		if err := validateGitEvidenceSubject(subject, issuer, issued); err != nil {
			return err
		}
	}
	return nil
}

func gitEvidenceSubjectKey(subject gitEvidenceSubject) string {
	source := subject.Repository + "@" + subject.Commit
	if subject.Subdirectory != "" {
		source += "#subdirectory=" + subject.Subdirectory
	}
	return gitEvidencePackageKey(gitEvidencePackage{Ecosystem: subject.Ecosystem, Name: subject.Name, Version: subject.Version, Source: source})
}

func validateGitEvidenceSubject(subject gitEvidenceSubject, issuer string, issued time.Time) error {
	if !validGitEvidenceDigest(subject.TreeSHA256) {
		return gitEvidenceFailure("coverage", "Git evidence omitted the authenticated source tree content identity")
	}
	observation := subject.Observation
	if observation.Kind != "publication" && observation.Kind != "first-observed" {
		return gitEvidenceFailure("age-coverage", "Git age requires a trusted publication or first-observed record; Git author, committer, and tag timestamps are not accepted")
	}
	if observation.Timestamp.IsZero() || observation.Timestamp.After(issued) || !gitEvidenceObservationRecord(observation.Record, issuer) {
		return gitEvidenceFailure("age-coverage", "Git age evidence lacks an authenticated provider observation record for the exact commit")
	}
	return nil
}

func gitEvidenceObservationRecord(record, issuer string) bool {
	parsed, err := url.Parse(record)
	return err == nil && gitEvidenceBoundedText(record, 2048) && parsed.Scheme == "https" &&
		parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" &&
		gitEvidenceObservationPath(parsed) && strings.HasPrefix(record, strings.TrimSuffix(issuer, "/")+"/")
}

func gitEvidenceObservationPath(parsed *url.URL) bool {
	return !parsed.ForceQuery && parsed.RawPath == "" && parsed.Path == path.Clean(parsed.Path)
}

func validGitEvidenceDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && value == strings.ToLower(value)
}

func gitEvidenceBoundedText(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) &&
		strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}

func validateGitEvidenceScanPackages(scan gitEvidenceScan, packages []gitEvidencePackage) error {
	if len(packages) > 8192 || len(scan.Licenses) != len(packages) {
		return gitEvidenceFailure("coverage", "Git evidence omitted licenses from the complete resolved dependency inventory")
	}
	expected, licensed := map[string]bool{}, map[string]bool{}
	for _, item := range packages {
		expected[gitEvidencePackageKey(item)] = true
	}
	for _, license := range scan.Licenses {
		key := gitEvidencePackageKey(gitEvidencePackage{Ecosystem: license.Ecosystem, Name: license.Name, Version: license.Version, Source: license.Source})
		if !expected[key] || licensed[key] || !gitEvidenceBoundedText(license.Expression, 4096) {
			return gitEvidenceFailure("coverage", "Git license evidence has missing, duplicate, or mismatched package identities")
		}
		licensed[key] = true
	}
	return validateGitEvidenceVulnerabilities(scan.Vulnerabilities, expected)
}

func validateGitEvidenceVulnerabilities(vulnerabilities []gitEvidenceVulnerability, packages map[string]bool) error {
	seen := map[string]bool{}
	for _, vulnerability := range vulnerabilities {
		key := gitEvidencePackageKey(gitEvidencePackage{Ecosystem: vulnerability.Ecosystem, Name: vulnerability.Name, Version: vulnerability.Version, Source: vulnerability.Source})
		if !packages[key] || !gitEvidenceBoundedText(vulnerability.ID, 128) || vulnerability.KnownExploited == nil {
			return gitEvidenceFailure("coverage", "Git vulnerability evidence has incomplete or mismatched package and exploitation identities")
		}
		if !slices.Contains([]string{"low", "medium", "high", "critical", "unknown"}, vulnerability.Severity) || seen[key+"\x00"+vulnerability.ID] {
			return gitEvidenceFailure("coverage", "Git vulnerability evidence contains an invalid severity or duplicate advisory")
		}
		seen[key+"\x00"+vulnerability.ID] = true
	}
	return nil
}
