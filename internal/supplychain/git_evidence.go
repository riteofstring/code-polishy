package supplychain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type gitEvidencePackage struct {
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Source    string `json:"source"`
}

func verifyGitLockEvidence(repo repository.Repository, scope string, packages []resolvedPackage, now time.Time) ([]policy.Finding, *GitEvidenceReceipt) {
	declaration, provider, err := configuredGitEvidence(repo.Config.SupplyChain.GitEvidence, scope)
	if err != nil {
		return []policy.Finding{gitEvidenceFinding(scope, err)}, nil
	}
	statement, digest, err := readSignedGitEvidence(repo, declaration, provider)
	if err != nil {
		return []policy.Finding{gitEvidenceFinding(scope, err)}, nil
	}
	lock, err := readGitEvidenceFile(repo, scope, 16<<20)
	if err == nil {
		err = validateGitEvidenceStatement(statement, provider, scope, gitEvidenceDigest(lock), packages, now)
	}
	if err != nil {
		return []policy.Finding{gitEvidenceFinding(scope, err)}, nil
	}
	receipt := &GitEvidenceReceipt{
		Scope: scope, Provider: provider.Name, Path: declaration.Path, SHA256: digest,
		PolicySHA256: provider.PolicySHA256, LockSHA256: statement.LockSHA256,
		InventorySHA256: statement.InventorySHA256, RetrievedAt: now.UTC(), ExpiresAt: statement.ExpiresAt.UTC(),
	}
	return gitEvidencePolicyFindings(repo, statement, now), receipt
}

func configuredGitEvidence(configuration policy.GitEvidence, scope string) (policy.GitEvidenceAttestation, policy.GitEvidenceProvider, error) {
	if err := policy.ValidateGitEvidence(configuration); err != nil {
		return policy.GitEvidenceAttestation{}, policy.GitEvidenceProvider{}, gitEvidenceFailure("invalid", "Git evidence trust configuration is invalid")
	}
	for _, declaration := range configuration.Attestations {
		if declaration.Scope != scope {
			continue
		}
		for _, provider := range configuration.Providers {
			if provider.Name == declaration.Provider {
				return declaration, provider, nil
			}
		}
	}
	return policy.GitEvidenceAttestation{}, policy.GitEvidenceProvider{}, gitEvidenceFailure("unavailable", "Configure a trusted CI provider and export signed Git security and age evidence for this exact lockfile")
}

func gitEvidenceFinding(scope string, err error) policy.Finding {
	category := "invalid"
	var failure *gitEvidenceError
	if errors.As(err, &failure) {
		category = failure.category
	}
	return policy.Finding{
		Check: "supplyChain.gitEvidence", Path: scope, Subject: category, Message: err.Error(),
		Remediation: policy.FindingRemediation{Summary: "Use the configured authorized CI provider to assess the exact Git contents and resolved lock inventory, then export a current signed evidence artifact. Do not install dependencies or upload private inputs to obtain local verification."},
	}
}

func gitEvidenceDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func gitEvidencePackages(packages []resolvedPackage) []gitEvidencePackage {
	result := []gitEvidencePackage{}
	for _, item := range packages {
		source := gitEvidencePackageSource(item)
		if source == "" {
			continue
		}
		result = append(result, gitEvidencePackage{Ecosystem: item.Ecosystem, Name: item.Name, Version: item.Version, Source: source})
	}
	slices.SortFunc(result, func(left, right gitEvidencePackage) int {
		return strings.Compare(gitEvidencePackageKey(left), gitEvidencePackageKey(right))
	})
	return slices.Compact(result)
}

func gitEvidencePackageSource(item resolvedPackage) string {
	if item.Source.Kind == "git" {
		source := item.Source.Git
		source.User = ""
		return source.Identity()
	}
	if item.Source.Kind == "" || item.Source.Kind == "registry" {
		return "registry:" + item.Source.Registry
	}
	return ""
}

func gitEvidencePackageKey(item gitEvidencePackage) string {
	return strings.Join([]string{item.Ecosystem, item.Name, item.Version, item.Source}, "\x00")
}

func gitEvidenceInventoryDigest(packages []resolvedPackage) string {
	data, _ := json.Marshal(gitEvidencePackages(packages))
	return gitEvidenceDigest(data)
}

func gitEvidencePolicyFindings(repo repository.Repository, statement gitEvidenceStatement, now time.Time) []policy.Finding {
	findings := []policy.Finding{}
	for _, subject := range statement.Subjects {
		if finding := gitEvidenceAgeFinding(subject, statement.Scope, now, repo.Config.SupplyChain.MinimumReleaseAgeDays); finding != nil {
			findings = append(findings, *finding)
		}
	}
	allowed := licenseAllowList(repo.Config.SupplyChain.AllowedLicenses)
	for _, item := range statement.Scan.Licenses {
		if err := licenseAdmitted(item.Expression, allowed); err != nil {
			findings = append(findings, policy.Finding{Check: "supplyChain.dependencyLicense", Path: statement.Scope, Subject: item.Name + "@" + item.Source, Message: err.Error()})
		}
	}
	for _, observed := range statement.Scan.Vulnerabilities {
		findings = append(findings, gitEvidenceVulnerabilityFinding(observed, statement.Scope))
	}
	return uniqueFindings(findings)
}

func gitEvidenceAgeFinding(subject gitEvidenceSubject, scope string, now time.Time, minimumDays int) *policy.Finding {
	observed := subject.Observation.Timestamp
	if !observed.AddDate(0, 0, minimumDays).After(now) {
		return nil
	}
	identity := policy.ReleaseAgeIdentity{
		Ecosystem: "git", Package: subject.Repository, Version: subject.Commit, Scope: scope,
		Released: observed, Eligible: observed.AddDate(0, 0, minimumDays),
	}
	return &policy.Finding{
		Check: "supplyChain.releaseAge", Path: scope, Subject: policy.ReleaseAgeSubject(identity), ReleaseAge: &identity,
		Message: fmt.Sprintf("trusted %s record has not reached the %d-day minimum; eligible %s", subject.Observation.Kind, minimumDays, identity.Eligible.Format(time.RFC3339)),
	}
}

func gitEvidenceVulnerabilityFinding(observed gitEvidenceVulnerability, scope string) policy.Finding {
	version := observed.Version
	if strings.HasPrefix(observed.Source, "git+") {
		version = observed.Source
	}
	identity := policy.VulnerabilityIdentity{
		Ecosystem: observed.Ecosystem, Package: observed.Name, AffectedVersion: version, Scope: scope,
		Advisory: observed.ID, Aliases: []string{observed.ID}, Severity: observed.Severity, KnownExploited: *observed.KnownExploited,
	}
	return policy.Finding{
		Check: "supplyChain.gitVulnerability", Path: scope, Subject: policy.VulnerabilitySubject(identity), Vulnerability: &identity,
		Message: fmt.Sprintf("trusted Git and lock assessment reports %s in %s@%s (severity %s)", observed.ID, observed.Name, observed.Version, observed.Severity),
		Fields:  map[string]string{"source": observed.Source},
	}
}
