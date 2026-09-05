package policy

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/spdx"
)

var (
	nodeReleaseVersionPattern           = regexp.MustCompile(`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	goReleaseVersionPattern             = regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	artifactVersionPattern              = regexp.MustCompile(`^v?(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	pypiReleaseVersionPattern           = regexp.MustCompile(`^[0-9][A-Za-z0-9.!+_-]*$`)
	pythonBuildStandaloneVersionPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\+[0-9]{8}$`)
	releaseArtifactLocator              = regexp.MustCompile(`^[A-Za-z0-9@][A-Za-z0-9._@/+~-]*$`)
	pypiArtifactLocator                 = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)
	githubRepositoryPattern             = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

const (
	ReleaseArtifactSourcePyPI                  = "pypi"
	ReleaseArtifactSourcePythonBuildStandalone = "python-build-standalone"
)

func validateSupplyChain(supply SupplyChain) error {
	if supply.PreferredNewDependencyAgeDays < supply.MinimumReleaseAgeDays {
		return errors.New("supplyChain.preferredNewDependencyAgeDays must not be shorter than minimumReleaseAgeDays")
	}
	if err := validateSupplyChainInputs(supply); err != nil {
		return err
	}
	if err := validateVulnerabilityAssessments(supply.VulnerabilityAssessments); err != nil {
		return err
	}
	if err := validateReleaseAgeAssessments(supply.ReleaseAgeAssessments); err != nil {
		return err
	}
	if err := validateDependencyOverridePolicies(supply.DependencyOverridePolicies); err != nil {
		return err
	}
	return validateArtifactSecurity(supply.ArtifactSecurity)
}

func validateSupplyChainInputs(supply SupplyChain) error {
	if err := ValidateGitEvidence(supply.GitEvidence); err != nil {
		return err
	}
	if err := validateAllowedLicenses(supply.AllowedLicenses); err != nil {
		return err
	}
	if err := validateRegistryURL(supply.NPMRegistryURL); err != nil {
		return err
	}
	if err := validateReleaseArtifacts(supply.ReleaseArtifacts); err != nil {
		return err
	}
	return nil
}

func validateReleaseArtifacts(artifacts []ReleaseArtifact) error {
	seenNames := map[string]bool{}
	seenVersionFiles := map[string]bool{}
	for index, artifact := range artifacts {
		label := fmt.Sprintf("supplyChain.releaseArtifacts[%d]", index)
		if err := validateReleaseArtifact(artifact, label, seenNames, seenVersionFiles); err != nil {
			return err
		}
	}
	return nil
}

func validateReleaseArtifact(artifact ReleaseArtifact, label string, seenNames, seenVersionFiles map[string]bool) error {
	if seenNames[artifact.Name] {
		return fmt.Errorf("duplicate release artifact name %q", artifact.Name)
	}
	seenNames[artifact.Name] = true
	if seenVersionFiles[artifact.VersionFile] {
		return fmt.Errorf("duplicate release artifact version file %q", artifact.VersionFile)
	}
	seenVersionFiles[artifact.VersionFile] = true
	return validateReleaseArtifactSource(artifact, label)
}

func validateReleaseArtifactSource(artifact ReleaseArtifact, label string) error {
	switch artifact.Source {
	case "go-toolchain", "node-runtime", ReleaseArtifactSourcePythonBuildStandalone:
		return validateFixedReleaseArtifactSource(artifact, label)
	case "github-release":
		return validateGitHubReleaseArtifactSource(artifact, label)
	case "go-module", "npm":
		return validatePackageReleaseArtifactSource(artifact, label)
	case ReleaseArtifactSourcePyPI:
		return validatePyPIReleaseArtifactSource(artifact, label)
	}
	return nil
}

func validateFixedReleaseArtifactSource(artifact ReleaseArtifact, label string) error {
	if artifact.Locator != "" || artifact.TagPrefix != "" {
		return fmt.Errorf("%s source %s does not accept locator or tagPrefix", label, artifact.Source)
	}
	return nil
}

func validateGitHubReleaseArtifactSource(artifact ReleaseArtifact, label string) error {
	if !githubRepositoryPattern.MatchString(artifact.Locator) {
		return fmt.Errorf("%s.locator must be one exact GitHub owner/repository", label)
	}
	return nil
}

func validatePackageReleaseArtifactSource(artifact ReleaseArtifact, label string) error {
	if artifact.Locator == "" || !releaseArtifactLocator.MatchString(artifact.Locator) || strings.Contains(artifact.Locator, "..") {
		return fmt.Errorf("%s.locator must be one exact package identity", label)
	}
	if artifact.TagPrefix != "" {
		return fmt.Errorf("%s source %s does not accept tagPrefix", label, artifact.Source)
	}
	return nil
}

func validatePyPIReleaseArtifactSource(artifact ReleaseArtifact, label string) error {
	if !pypiArtifactLocator.MatchString(artifact.Locator) {
		return fmt.Errorf("%s.locator must be one exact PyPI project name", label)
	}
	if artifact.TagPrefix != "" {
		return fmt.Errorf("%s source %s does not accept tagPrefix", label, artifact.Source)
	}
	return nil
}

func validateArtifactSecurity(artifact ArtifactSecurity) error {
	seen := map[string]bool{}
	for index, target := range artifact.Targets {
		label := fmt.Sprintf("supplyChain.artifactSecurity.targets[%d]", index)
		if err := validateArtifactTarget(target, label, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateArtifactTarget(target ArtifactTarget, label string, seen map[string]bool) error {
	if seen[target.Name] {
		return fmt.Errorf("duplicate artifact security target %q", target.Name)
	}
	seen[target.Name] = true
	return validateArtifactTargetMode(target, label)
}

func validateArtifactTargetMode(target ArtifactTarget, label string) error {
	if target.Mode == "command" {
		return validateArtifactProducer(*target.Producer, label+".producer")
	}
	return nil
}

func validateArtifactProducer(producer ArtifactProducer, label string) error {
	if err := validateCommandArgv(producer.Argv, label); err != nil {
		return err
	}
	return nil
}

func validateVulnerabilityAssessments(assessments []VulnerabilityAssessment) error {
	seenIDs := map[string]bool{}
	seenCoordinates := map[string]bool{}
	now := time.Now().UTC().Truncate(24 * time.Hour)
	for index, assessment := range assessments {
		label := fmt.Sprintf("supplyChain.vulnerabilityAssessments[%d]", index)
		if err := validateVulnerabilityAssessment(assessment, label, seenIDs, seenCoordinates, now); err != nil {
			return err
		}
	}
	return nil
}

func validateVulnerabilityAssessment(assessment VulnerabilityAssessment, label string, seenIDs, seenCoordinates map[string]bool, now time.Time) error {
	if err := validateVulnerabilityIdentity(assessment, label, seenIDs, seenCoordinates); err != nil {
		return err
	}
	if err := validateVulnerabilityApproval(assessment, label); err != nil {
		return err
	}
	return validateVulnerabilityReviewWindow(assessment, label, now)
}

func validateVulnerabilityIdentity(assessment VulnerabilityAssessment, label string, seenIDs, seenCoordinates map[string]bool) error {
	if seenIDs[assessment.ID] {
		return fmt.Errorf("duplicate vulnerability assessment id %q", assessment.ID)
	}
	seenIDs[assessment.ID] = true
	if strings.ContainsAny(assessment.Advisory+assessment.Package, "*?[]") {
		return fmt.Errorf("%s must identify an exact observed vulnerability; wildcards are forbidden", label)
	}
	if !exactVulnerabilityAssessmentVersion(assessment.Ecosystem, assessment.AffectedVersion) {
		return fmt.Errorf("%s.affectedVersion must be one exact version for %s", label, assessment.Ecosystem)
	}
	coordinate := vulnerabilityAssessmentCoordinate(assessment)
	if seenCoordinates[coordinate] {
		return fmt.Errorf("%s duplicates an existing vulnerability assessment coordinate", label)
	}
	seenCoordinates[coordinate] = true
	return nil
}

func validateVulnerabilityApproval(assessment VulnerabilityAssessment, label string) error {
	for name, value := range map[string]string{
		"evidence": assessment.Evidence, "tracking": assessment.Tracking, "approval": assessment.Approval,
	} {
		if err := validateEvidenceURL(value, label+"."+name); err != nil {
			return err
		}
	}
	if assessment.Owner == assessment.ApprovedBy {
		return fmt.Errorf("%s.approvedBy must identify an approver distinct from owner", label)
	}
	return nil
}

func validateVulnerabilityReviewWindow(assessment VulnerabilityAssessment, label string, now time.Time) error {
	if err := validateReviewWindow(assessment.Reviewed, assessment.Expires, label, now); err != nil {
		return err
	}
	maximumDays := MaximumLowVulnerabilityDays
	switch assessment.Severity {
	case "moderate":
		maximumDays = MaximumModerateVulnerabilityDays
	case "high":
		maximumDays = MaximumHighNotAffectedVulnerabilityDays
	}
	if assessment.Expires.After(assessment.Reviewed.AddDate(0, 0, maximumDays)) {
		return fmt.Errorf("%s.expires must be within %d days of reviewed for %s severity", label, maximumDays, assessment.Severity)
	}
	return nil
}

func vulnerabilityAssessmentCoordinate(assessment VulnerabilityAssessment) string {
	return assessment.Ecosystem + "\x00" + assessment.Advisory + "\x00" + assessment.Package + "\x00" + assessment.AffectedVersion + "\x00" + assessment.Scope
}

func exactVulnerabilityAssessmentVersion(ecosystem, version string) bool {
	return exactReleaseAssessmentVersion(ecosystem, version)
}

func validateReleaseAgeAssessments(assessments []ReleaseAgeAssessment) error {
	seen := map[string]bool{}
	now := time.Now().UTC().Truncate(24 * time.Hour)
	for index, assessment := range assessments {
		label := fmt.Sprintf("supplyChain.releaseAgeAssessments[%d]", index)
		if err := validateReleaseAgeAssessment(assessment, label, seen, now); err != nil {
			return err
		}
	}
	return nil
}

func validateReleaseAgeAssessment(assessment ReleaseAgeAssessment, label string, seen map[string]bool, now time.Time) error {
	if seen[assessment.ID] {
		return fmt.Errorf("duplicate release-age assessment id %q", assessment.ID)
	}
	seen[assessment.ID] = true
	if strings.ContainsAny(assessment.Package+assessment.Version, "*?[]") {
		return fmt.Errorf("%s must identify one exact package and version; wildcards are forbidden", label)
	}
	if !exactReleaseAssessmentVersion(assessment.Ecosystem, assessment.Version) {
		return fmt.Errorf("%s.version must be one exact version for %s", label, assessment.Ecosystem)
	}
	if err := validateEvidenceURL(assessment.Evidence, label+".evidence"); err != nil {
		return err
	}
	return validateReviewWindow(assessment.Reviewed, assessment.Expires, label, now)
}

func exactReleaseAssessmentVersion(ecosystem, version string) bool {
	switch ecosystem {
	case "artifact":
		return ExactArtifactVersion(version)
	case "go":
		return goReleaseVersionPattern.MatchString(version)
	case "npm", "pnpm":
		return nodeReleaseVersionPattern.MatchString(version)
	case "pypi":
		return pypiReleaseVersionPattern.MatchString(version)
	default:
		return false
	}
}

func ExactArtifactVersion(version string) bool {
	return artifactVersionPattern.MatchString(version)
}

func ExactReleaseArtifactVersion(source, version string) bool {
	switch source {
	case ReleaseArtifactSourcePyPI:
		return pypiReleaseVersionPattern.MatchString(version)
	case ReleaseArtifactSourcePythonBuildStandalone:
		return pythonBuildStandaloneVersionPattern.MatchString(version)
	default:
		return ExactArtifactVersion(version)
	}
}

func validateEvidenceURL(value, label string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("%s must be an HTTPS URL without credentials or fragment", label)
	}
	return nil
}

func validateDependencyOverridePolicies(policies []DependencyOverridePolicy) error {
	seenIDs := map[string]bool{}
	seenOwners := map[string]bool{}
	now := time.Now().UTC().Truncate(24 * time.Hour)
	for index, override := range policies {
		label := fmt.Sprintf("supplyChain.dependencyOverridePolicies[%d]", index)
		if err := validateDependencyOverridePolicy(override, label, seenIDs, seenOwners, now); err != nil {
			return err
		}
	}
	return nil
}

func validateDependencyOverridePolicy(override DependencyOverridePolicy, label string, seenIDs, seenOwners map[string]bool, now time.Time) error {
	if seenIDs[override.ID] {
		return fmt.Errorf("duplicate dependency override policy id %q", override.ID)
	}
	seenIDs[override.ID] = true
	if err := recordDependencyOverrideOwner(override, label, seenOwners); err != nil {
		return err
	}
	return validateReviewWindow(override.Reviewed, override.Expires, label, now)
}

func recordDependencyOverrideOwner(override DependencyOverridePolicy, label string, seen map[string]bool) error {
	key := override.Path + "\x00" + override.Field
	if seen[key] {
		return fmt.Errorf("duplicate dependency override policy for %s %s", override.Path, override.Field)
	}
	seen[key] = true
	return nil
}

func validateReviewWindow(reviewed, expires Date, label string, now time.Time) error {
	if reviewed.After(now) {
		return fmt.Errorf("%s.reviewed must be a non-future YYYY-MM-DD date", label)
	}
	if expires.Before(reviewed.Time) {
		return fmt.Errorf("%s.expires must not precede reviewed", label)
	}
	if expires.After(now.AddDate(0, 0, MaximumExceptionDays)) {
		return fmt.Errorf("%s.expires must be within %d days", label, MaximumExceptionDays)
	}
	return nil
}

func validateAllowedLicenses(licenses []string) error {
	for index, license := range licenses {
		if err := spdx.ValidateAllowed(license); err != nil {
			return fmt.Errorf("supplyChain.allowedLicenses[%d] must be one identifier from the pinned SPDX snapshot, optionally qualified with WITH: %w", index, err)
		}
	}
	return nil
}

func validateRegistryURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("supplyChain.npmRegistryUrl must be an HTTPS URL without credentials, query, or fragment")
	}
	return nil
}
