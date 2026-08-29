package policy

import (
	"fmt"
	"time"
)

func ApplyReleaseAgeAssessments(
	findings []Finding,
	assessments []ReleaseAgeAssessment,
	now time.Time,
	enforceUnused bool,
) ([]Finding, []AssessedReleaseAge) {
	currentDate := now.UTC().Truncate(24 * time.Hour)
	kept := make([]Finding, 0, len(findings))
	accepted := []AssessedReleaseAge{}
	used := make(map[string]bool, len(assessments))

	for _, finding := range findings {
		application := applyReleaseAgeFinding(finding, assessments, currentDate)
		if application.usedID != "" {
			used[application.usedID] = true
		}
		if application.issue != nil {
			kept = append(kept, *application.issue)
		}
		if application.accepted != nil {
			accepted = append(accepted, *application.accepted)
		} else {
			kept = append(kept, finding)
		}
	}

	for _, assessment := range assessments {
		if issue := releaseAgeAssessmentStatus(assessment, currentDate, used[assessment.ID], enforceUnused); issue != nil {
			kept = append(kept, *issue)
		}
	}
	return kept, accepted
}

type releaseAgeApplication struct {
	usedID   string
	accepted *AssessedReleaseAge
	issue    *Finding
}

func applyReleaseAgeFinding(finding Finding, assessments []ReleaseAgeAssessment, currentDate time.Time) releaseAgeApplication {
	if finding.ReleaseAge == nil {
		return releaseAgeApplication{}
	}
	for _, assessment := range assessments {
		if assessment.Expires.Before(currentDate) || !releaseAgeAssessmentMatches(*finding.ReleaseAge, assessment) {
			continue
		}
		application := releaseAgeApplication{usedID: assessment.ID}
		if assessment.Expires.After(finding.ReleaseAge.Eligible.UTC().Truncate(24 * time.Hour)) {
			application.issue = &Finding{
				Check: "policy.releaseAgeAssessmentWindow", Path: ConfigFilename, Subject: assessment.ID,
				Message: fmt.Sprintf("release-age assessment expires after %s, when the exact version reaches the hard minimum", finding.ReleaseAge.Eligible.Format("2006-01-02")),
			}
			return application
		}
		application.accepted = &AssessedReleaseAge{Finding: finding, Assessment: assessment}
		return application
	}
	return releaseAgeApplication{}
}

func releaseAgeAssessmentStatus(assessment ReleaseAgeAssessment, currentDate time.Time, used, enforceUnused bool) *Finding {
	if assessment.Expires.Before(currentDate) {
		return &Finding{
			Check: "policy.releaseAgeAssessmentExpired", Path: ConfigFilename, Subject: assessment.ID,
			Message: fmt.Sprintf("release-age assessment expired on %s", assessment.Expires.Format("2006-01-02")),
		}
	}
	if enforceUnused && !used {
		return &Finding{
			Check: "policy.releaseAgeAssessmentUnused", Path: ConfigFilename, Subject: assessment.ID,
			Message: "release-age assessment did not match an observed ecosystem, package, version, and scope",
		}
	}
	return nil
}

func releaseAgeAssessmentMatches(identity ReleaseAgeIdentity, assessment ReleaseAgeAssessment) bool {
	return identity.Ecosystem == assessment.Ecosystem &&
		identity.Package == assessment.Package &&
		identity.Version == assessment.Version &&
		identity.Scope == assessment.Scope
}

func ReleaseAgeSubject(identity ReleaseAgeIdentity) string {
	return identity.Package + "@" + identity.Version
}
