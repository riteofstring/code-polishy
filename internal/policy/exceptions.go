package policy

import (
	"fmt"
	"time"
)

func ApplyExceptions(findings []Finding, exceptions []Exception, now time.Time) ([]Finding, []Suppressed) {
	currentDate := now.UTC().Truncate(24 * time.Hour)
	kept := make([]Finding, 0, len(findings))
	suppressed := []Suppressed{}
	for _, finding := range findings {
		matched := false
		for _, exception := range exceptions {
			if exception.Expires.Before(currentDate) {
				continue
			}
			if exception.Check == finding.Check && exception.Path == finding.Path && exception.Subject == finding.Subject {
				suppressed = append(suppressed, Suppressed{Finding: finding, Exception: exception})
				matched = true
				break
			}
		}
		if !matched {
			kept = append(kept, finding)
		}
	}
	for _, exception := range exceptions {
		if exception.Expires.Before(currentDate) {
			kept = append(kept, Finding{Check: "policy.exceptionExpired", Path: ConfigFilename, Subject: exception.ID, Message: fmt.Sprintf("exception expired on %s", exception.Expires.Format("2006-01-02"))})
		}
	}
	return kept, suppressed
}
