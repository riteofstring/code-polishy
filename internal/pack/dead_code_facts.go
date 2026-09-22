package pack

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func deadCodeFindings(facts []DeadCodeFact) []policy.Finding {
	findings := make([]policy.Finding, 0, len(facts))
	for _, fact := range facts {
		findings = append(findings, policy.Finding{
			Check: "quality.deadCode", Path: fact.Path, Line: fact.Line,
			Subject: deadCodeSubject(fact), Message: deadCodeMessage(fact),
			Remediation: policy.FindingRemediation{
				Summary:     "Delete the unused definition. Do not generate reachability declarations or entry-point lists from dead-code findings.",
				NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "check", "--all"}, Cwd: "."},
			},
		})
	}
	return findings
}

func deadCodeSubject(fact DeadCodeFact) string {
	identity := strings.Join([]string{
		fact.Path, strconv.Itoa(fact.Line), strconv.Itoa(fact.EndLine), fact.Name, fact.Kind,
		strconv.Itoa(fact.Confidence), fact.Message,
	}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	return fact.Analyzer + ":" + hex.EncodeToString(digest[:])
}

func deadCodeMessage(fact DeadCodeFact) string {
	line := strconv.Itoa(fact.Line)
	if fact.EndLine != fact.Line {
		line += "-" + strconv.Itoa(fact.EndLine)
	}
	return "lines " + line + ": " + fact.Message + " (" + fact.Kind + ", " + strconv.Itoa(fact.Confidence) + "% confidence)"
}
