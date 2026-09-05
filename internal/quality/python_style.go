package quality

import (
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func pythonStyleFilteredDiagnostics(repo repository.Repository, diagnostics []pythonRuffDiagnostic) []pythonRuffDiagnostic {
	filtered := make([]pythonRuffDiagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if !repo.IsGenerated(diagnostic.Path) || !pythonCosmeticRuffRule(diagnostic.Code) {
			filtered = append(filtered, diagnostic)
		}
	}
	return filtered
}

func pythonCosmeticRuffRule(code string) bool {
	family := strings.TrimRight(code, "0123456789")
	if family != code && slices.Contains([]string{"I", "Q", "D", "N"}, family) {
		return true
	}
	if strings.HasPrefix(code, "E2") || strings.HasPrefix(code, "E3") {
		return true
	}
	return slices.Contains([]string{
		"COM812", "COM819",
		"E111", "E114", "E115", "E116", "E401", "E501", "E502",
		"E701", "E702", "E703", "E704", "E741", "E742", "E743",
		"W191", "W291", "W292", "W293",
	}, code)
}
