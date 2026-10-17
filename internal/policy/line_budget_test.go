package policy

import (
	"strings"
	"testing"
	"time"
)

func TestLoadAcceptsPartialLineBudget(t *testing.T) {
	t.Parallel()
	configured := strings.Replace(minimalConfig(), `"quality":{}`, `"quality":{"lineBudget":{"production":4000,"total":9000}}`, 1)
	config, err := Load(writeConfig(t, configured), "")
	if err != nil {
		t.Fatal(err)
	}
	if budget := config.Quality.LineBudget; budget == nil || *budget != (LineBudget{Production: 4000, Total: 9000}) {
		t.Fatalf("line budget = %+v", budget)
	}
}

func TestLoadRejectsInvalidLineBudget(t *testing.T) {
	t.Parallel()
	for name, declaration := range map[string]string{
		"empty":    `{}`,
		"zero":     `{"production":0}`,
		"negative": `{"test":-1}`,
		"unknown":  `{"go":100}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			configured := strings.Replace(minimalConfig(), `"quality":{}`, `"quality":{"lineBudget":`+declaration+`}`, 1)
			if _, err := Load(writeConfig(t, configured), ""); err == nil || !strings.Contains(err.Error(), schemaRejection) {
				t.Fatalf("error = %v, want %q", err, schemaRejection)
			}
		})
	}
}

func TestLineBudgetFindingsCannotBeSuppressed(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	for _, check := range []string{"quality.lineBudget", "quality.lineBudgetChange"} {
		finding := Finding{Check: check, Path: "repository", Subject: "production", Message: "over budget"}
		exception := Exception{ID: "budget", Check: check, Path: finding.Path, Subject: finding.Subject, Reason: "growth", Owner: "team", Expires: Date{Time: now.AddDate(0, 0, 1)}}
		kept, suppressed := ApplyExceptions([]Finding{finding}, []Exception{exception}, now)
		if len(kept) != 1 || len(suppressed) != 0 {
			t.Fatalf("%s: kept=%+v suppressed=%+v", check, kept, suppressed)
		}
	}
}
