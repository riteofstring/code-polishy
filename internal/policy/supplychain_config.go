package policy

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	lowerSHA256Pattern        = regexp.MustCompile(`^[0-9a-f]{64}$`)
	nodeReleaseVersionPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	goReleaseVersionPattern   = regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	artifactVersionPattern    = regexp.MustCompile(`^v?(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	pypiReleaseVersionPattern = regexp.MustCompile(`^[0-9][A-Za-z0-9.!+_-]*$`)
	licenseIdentifierPattern  = regexp.MustCompile(`^[A-Za-z0-9.-]+\+?$`)
	releaseArtifactLocator    = regexp.MustCompile(`^[A-Za-z0-9@][A-Za-z0-9._@/+~-]*$`)
	githubRepositoryPattern   = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	tagPrefixPattern          = regexp.MustCompile(`^[A-Za-z0-9._-]*$`)
)

func validateSupplyChain(supply SupplyChain) error {
	if supply.MinimumReleaseAgeDays < MinimumReleaseAgeDays {
		return fmt.Errorf("supplyChain.minimumReleaseAgeDays must be at least %d", MinimumReleaseAgeDays)
	}
	if supply.PreferredNewDependencyAgeDays < supply.MinimumReleaseAgeDays {
		return errors.New("supplyChain.preferredNewDependencyAgeDays must not be shorter than minimumReleaseAgeDays")
	}
	if supply.AuditLevel != "low" {
		return errors.New("supplyChain.auditLevel must be low")
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
	if err := validateDependencyProtocols(supply.AllowedDependencyProtocols); err != nil {
		return err
	}
	if err := validateAllowedLicenses(supply.AllowedLicenses); err != nil {
		return err
	}
	if err := validateEnvironment(supply.Environment, "supplyChain.environment"); err != nil {
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
	if err := identifier(artifact.Name, label+".name"); err != nil {
		return err
	}
	if seenNames[artifact.Name] {
		return fmt.Errorf("duplicate release artifact name %q", artifact.Name)
	}
	seenNames[artifact.Name] = true
	if err := repositoryPath(artifact.VersionFile, label+".versionFile", false); err != nil {
		return err
	}
	if seenVersionFiles[artifact.VersionFile] {
		return fmt.Errorf("duplicate release artifact version file %q", artifact.VersionFile)
	}
	seenVersionFiles[artifact.VersionFile] = true
	if err := allowedValues([]string{artifact.Source}, []string{"github-release", "go-module", "go-toolchain", "node-runtime", "npm"}, label+".source"); err != nil {
		return err
	}
	if artifact.TagPrefix != "" && (!tagPrefixPattern.MatchString(artifact.TagPrefix) || len(artifact.TagPrefix) > 32) {
		return fmt.Errorf("%s.tagPrefix must be at most 32 URL-safe tag characters", label)
	}
	return validateReleaseArtifactSource(artifact, label)
}

func validateReleaseArtifactSource(artifact ReleaseArtifact, label string) error {
	switch artifact.Source {
	case "go-toolchain", "node-runtime":
		if artifact.Locator != "" || artifact.TagPrefix != "" {
			return fmt.Errorf("%s source %s does not accept locator or tagPrefix", label, artifact.Source)
		}
	case "github-release":
		if !githubRepositoryPattern.MatchString(artifact.Locator) {
			return fmt.Errorf("%s.locator must be one exact GitHub owner/repository", label)
		}
	case "go-module", "npm":
		if artifact.Locator == "" || !releaseArtifactLocator.MatchString(artifact.Locator) || strings.Contains(artifact.Locator, "..") {
			return fmt.Errorf("%s.locator must be one exact package identity", label)
		}
		if artifact.TagPrefix != "" {
			return fmt.Errorf("%s source %s does not accept tagPrefix", label, artifact.Source)
		}
	}
	return nil
}

func validateArtifactSecurity(artifact ArtifactSecurity) error {
	if err := repositoryPath(artifact.OutputDirectory, "supplyChain.artifactSecurity.outputDirectory", false); err != nil {
		return err
	}
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
	if err := identifier(target.Name, label+".name"); err != nil {
		return err
	}
	if seen[target.Name] {
		return fmt.Errorf("duplicate artifact security target %q", target.Name)
	}
	seen[target.Name] = true
	if target.Module != "" && !identifierPattern.MatchString(target.Module) {
		return fmt.Errorf("%s.module must be a lowercase dotted or dashed identifier", label)
	}
	if err := allowedValues([]string{target.Mode}, []string{"archive", "command", "dockerfile"}, label+".mode"); err != nil {
		return err
	}
	if target.Platform != "" && !slices.Contains([]string{"linux/amd64", "linux/arm64"}, target.Platform) {
		return fmt.Errorf("%s.platform must be linux/amd64 or linux/arm64", label)
	}
	if err := validateArtifactTargetPaths(target, label); err != nil {
		return err
	}
	return validateArtifactTargetMode(target, label)
}

func validateArtifactTargetPaths(target ArtifactTarget, label string) error {
	paths := map[string]string{
		"dockerfile": target.Dockerfile,
		"context":    target.Context,
		"archive":    target.Archive,
		"openVex":    target.OpenVEX,
	}
	for name, value := range paths {
		if value == "" {
			continue
		}
		if err := repositoryPath(value, label+"."+name, false); err != nil {
			return err
		}
	}
	return nil
}

func validateArtifactTargetMode(target ArtifactTarget, label string) error {
	switch target.Mode {
	case "dockerfile":
		return validateDockerfileArtifactTarget(target, label)
	case "archive":
		return validateArchiveArtifactTarget(target, label)
	case "command":
		return validateCommandArtifactTarget(target, label)
	default:
		return nil
	}
}

func validateDockerfileArtifactTarget(target ArtifactTarget, label string) error {
	if target.Dockerfile == "" || target.Context == "" {
		return fmt.Errorf("%s dockerfile mode requires dockerfile and context", label)
	}
	if target.Archive != "" || target.Producer != nil {
		return fmt.Errorf("%s dockerfile mode accepts dockerfile and context only", label)
	}
	return nil
}

func validateArchiveArtifactTarget(target ArtifactTarget, label string) error {
	if target.Archive == "" {
		return fmt.Errorf("%s archive mode requires archive", label)
	}
	if target.Dockerfile != "" || target.Context != "" || target.Producer != nil {
		return fmt.Errorf("%s archive mode accepts archive only", label)
	}
	return nil
}

func validateCommandArtifactTarget(target ArtifactTarget, label string) error {
	if target.Producer == nil {
		return fmt.Errorf("%s command mode requires producer", label)
	}
	if target.Dockerfile != "" || target.Context != "" || target.Archive != "" {
		return fmt.Errorf("%s command mode accepts producer only", label)
	}
	return validateArtifactProducer(*target.Producer, label+".producer")
}

func validateArtifactProducer(producer ArtifactProducer, label string) error {
	if err := validateCommandArgv(producer.Argv, label); err != nil {
		return err
	}
	if err := repositoryPath(producer.Cwd, label+".cwd", false); err != nil {
		return err
	}
	if err := repositoryPath(producer.Manifest, label+".manifest", false); err != nil {
		return err
	}
	if err := validateEnvironment(producer.Environment, label+".environment"); err != nil {
		return err
	}
	if producer.TimeoutSeconds < 1 || producer.TimeoutSeconds > 3600 {
		return fmt.Errorf("%s.timeoutSeconds must be between 1 and 3600", label)
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
	if err := validateVulnerabilityDisposition(assessment, label); err != nil {
		return err
	}
	if err := validateVulnerabilityApproval(assessment, label); err != nil {
		return err
	}
	return validateVulnerabilityReviewWindow(assessment, label, now)
}

func validateVulnerabilityIdentity(assessment VulnerabilityAssessment, label string, seenIDs, seenCoordinates map[string]bool) error {
	if err := identifier(assessment.ID, label+".id"); err != nil {
		return err
	}
	if seenIDs[assessment.ID] {
		return fmt.Errorf("duplicate vulnerability assessment id %q", assessment.ID)
	}
	seenIDs[assessment.ID] = true
	if err := allowedValues([]string{assessment.Ecosystem}, []string{"go", "npm", "pnpm", "pypi"}, label+".ecosystem"); err != nil {
		return err
	}
	values := map[string]string{
		"advisory": assessment.Advisory, "package": assessment.Package,
		"affectedVersion": assessment.AffectedVersion, "reason": assessment.Reason,
		"impact": assessment.Impact, "owner": assessment.Owner, "approvedBy": assessment.ApprovedBy,
	}
	for name, value := range values {
		if err := nonempty(value, label+"."+name); err != nil {
			return err
		}
	}
	if strings.ContainsAny(assessment.Advisory+assessment.Package, "*?[]") {
		return fmt.Errorf("%s must identify an exact observed vulnerability; wildcards are forbidden", label)
	}
	if !exactVulnerabilityAssessmentVersion(assessment.Ecosystem, assessment.AffectedVersion) {
		return fmt.Errorf("%s.affectedVersion must be one exact version for %s", label, assessment.Ecosystem)
	}
	if err := repositoryPath(assessment.Scope, label+".scope", false); err != nil {
		return err
	}
	coordinate := vulnerabilityAssessmentCoordinate(assessment)
	if seenCoordinates[coordinate] {
		return fmt.Errorf("%s duplicates an existing vulnerability assessment coordinate", label)
	}
	seenCoordinates[coordinate] = true
	return nil
}

func validateVulnerabilityDisposition(assessment VulnerabilityAssessment, label string) error {
	if err := allowedValues([]string{assessment.Severity}, []string{"low", "moderate", "high"}, label+".severity"); err != nil {
		return err
	}
	if err := allowedValues([]string{assessment.Status}, []string{"not-affected", "risk-accepted"}, label+".status"); err != nil {
		return err
	}
	if assessment.Severity == "high" && assessment.Status != "not-affected" {
		return fmt.Errorf("%s.status must be not-affected for high severity", label)
	}
	if err := validateVulnerabilityBasis(assessment.Status, assessment.Basis, label); err != nil {
		return err
	}
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

func validateVulnerabilityBasis(status, basis, label string) error {
	allowed := map[string][]string{
		"not-affected":  {"false-positive", "unreachable"},
		"risk-accepted": {"mitigated", "temporary-no-fix"},
	}
	if !slices.Contains(allowed[status], basis) {
		return fmt.Errorf("%s.basis is not valid for status %s", label, status)
	}
	return nil
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
	if err := identifier(assessment.ID, label+".id"); err != nil {
		return err
	}
	if seen[assessment.ID] {
		return fmt.Errorf("duplicate release-age assessment id %q", assessment.ID)
	}
	seen[assessment.ID] = true
	if err := allowedValues([]string{assessment.Ecosystem}, []string{"artifact", "go", "npm", "pnpm", "pypi"}, label+".ecosystem"); err != nil {
		return err
	}
	if err := allowedValues([]string{assessment.Category}, []string{"security-fix", "supported-release"}, label+".category"); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"package": assessment.Package, "version": assessment.Version,
		"reason": assessment.Reason, "owner": assessment.Owner,
	} {
		if err := nonempty(value, label+"."+name); err != nil {
			return err
		}
	}
	if strings.ContainsAny(assessment.Package+assessment.Version, "*?[]") {
		return fmt.Errorf("%s must identify one exact package and version; wildcards are forbidden", label)
	}
	if !exactReleaseAssessmentVersion(assessment.Ecosystem, assessment.Version) {
		return fmt.Errorf("%s.version must be one exact version for %s", label, assessment.Ecosystem)
	}
	if err := repositoryPath(assessment.Scope, label+".scope", false); err != nil {
		return err
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
	if err := identifier(override.ID, label+".id"); err != nil {
		return err
	}
	if seenIDs[override.ID] {
		return fmt.Errorf("duplicate dependency override policy id %q", override.ID)
	}
	seenIDs[override.ID] = true
	if err := allowedValues([]string{override.Ecosystem}, []string{"npm", "pnpm"}, label+".ecosystem"); err != nil {
		return err
	}
	if err := repositoryPath(override.Path, label+".path", false); err != nil {
		return err
	}
	if err := allowedValues([]string{override.Field}, []string{"overrides", "pnpm.overrides"}, label+".field"); err != nil {
		return err
	}
	if err := recordDependencyOverrideOwner(override, label, seenOwners); err != nil {
		return err
	}
	if !lowerSHA256Pattern.MatchString(override.ContentSHA256) {
		return fmt.Errorf("%s.contentSha256 must be an exact lowercase SHA-256 digest", label)
	}
	for name, value := range map[string]string{"reason": override.Reason, "owner": override.Owner} {
		if err := nonempty(value, label+"."+name); err != nil {
			return err
		}
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
	if reviewed.IsZero() || reviewed.After(now) {
		return fmt.Errorf("%s.reviewed must be a non-future YYYY-MM-DD date", label)
	}
	if expires.IsZero() {
		return fmt.Errorf("%s.expires must use YYYY-MM-DD", label)
	}
	if expires.Before(reviewed.Time) {
		return fmt.Errorf("%s.expires must not precede reviewed", label)
	}
	if expires.After(now.AddDate(0, 0, MaximumExceptionDays)) {
		return fmt.Errorf("%s.expires must be within %d days", label, MaximumExceptionDays)
	}
	return nil
}

func validateDependencyProtocols(protocols []string) error {
	if err := validateUniqueStrings(protocols, "supplyChain.allowedDependencyProtocols", false); err != nil {
		return err
	}
	for _, protocol := range protocols {
		if !slices.Contains([]string{"workspace:", "file:", "link:"}, protocol) {
			return fmt.Errorf("unsupported dependency protocol %q", protocol)
		}
	}
	return nil
}

func validateAllowedLicenses(licenses []string) error {
	if err := validateUniqueStrings(licenses, "supplyChain.allowedLicenses", false); err != nil {
		return err
	}
	for index, license := range licenses {
		identifier, exception, qualified := strings.Cut(license, " WITH ")
		if !IsLicenseIdentifier(identifier) || qualified && !IsLicenseIdentifier(exception) {
			return fmt.Errorf("supplyChain.allowedLicenses[%d] must be an SPDX identifier, optionally qualified as \"<license> WITH <exception>\"", index)
		}
	}
	return nil
}

func IsLicenseIdentifier(value string) bool {
	return licenseIdentifierPattern.MatchString(value)
}

func validateRegistryURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("supplyChain.npmRegistryUrl must be an HTTPS URL without credentials, query, or fragment")
	}
	return nil
}
