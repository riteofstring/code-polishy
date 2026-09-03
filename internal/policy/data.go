package policy

import (
	"fmt"
	"slices"
	"strings"
)

func validateDataPaths(config *Config) error {
	if err := validatePatterns(config.Scope.Data, "scope.data", true); err != nil {
		return err
	}
	for index, pattern := range config.Scope.Data {
		if err := validateDataPattern(config, index, pattern); err != nil {
			return err
		}
	}
	if len(config.Scope.Data) == 0 {
		return nil
	}
	return validateDataFormatCommands(config.Checks)
}

func validateDataPattern(config *Config, index int, pattern string) error {
	label := fmt.Sprintf("scope.data[%d]", index)
	if dataPatternIsExecutableSource(pattern) {
		return fmt.Errorf("%s must not match executable source", label)
	}
	if language, overlaps := dataLanguageOverlap(config.Scope.Languages, pattern); overlaps {
		return fmt.Errorf("%s must not match executable source declared by scope.languages %q", label, language)
	}
	if control, overlaps := dataControlOverlap(config, pattern); overlaps {
		return fmt.Errorf("%s must not match policy-sensitive control input pattern %q", label, control)
	}
	if !dataPatternHasSupportedExtension(pattern) {
		return fmt.Errorf("%s must match only .json, .jsonc, .yaml, or .yml files", label)
	}
	if overlapsScope(pattern, config.Scope.Exclude) || overlapsScope(pattern, DefaultExcludes) {
		return fmt.Errorf("%s must not overlap scope.exclude", label)
	}
	if overlapsScope(pattern, config.Scope.Generated) {
		return fmt.Errorf("%s must not overlap scope.generated", label)
	}
	return nil
}

func dataLanguageOverlap(rules []LanguageRule, pattern string) (string, bool) {
	for _, rule := range rules {
		for _, source := range rule.Paths {
			if PatternsOverlap(pattern, source) {
				return rule.Name, true
			}
		}
	}
	return "", false
}

func validateDataFormatCommands(commands []Command) error {
	for index, command := range commands {
		if slices.Contains(command.RunOn, "format") {
			return fmt.Errorf("checks[%d] cannot run during format while scope.data is declared; data-safe formatting must use a managed file-scoped formatter", index)
		}
	}
	return nil
}

func dataControlOverlap(config *Config, pattern string) (string, bool) {
	for _, control := range sensitiveControlPatterns {
		if PatternsOverlap(pattern, control) {
			return control, true
		}
	}
	for _, artifact := range config.SupplyChain.ReleaseArtifacts {
		if artifact.VersionFile != "" && PatternsOverlap(pattern, artifact.VersionFile) {
			return artifact.VersionFile, true
		}
	}
	return "", false
}

func overlapsScope(pattern string, candidates []string) bool {
	for _, candidate := range candidates {
		if PatternsOverlap(pattern, candidate) {
			return true
		}
	}
	return false
}

func dataPatternIsExecutableSource(pattern string) bool {
	for _, extension := range []string{
		".bash", ".c", ".cc", ".cjs", ".cpp", ".cs", ".cts", ".cxx", ".dart", ".go", ".h", ".hpp", ".java", ".js", ".jsx", ".kt", ".kts", ".mjs", ".mts", ".php", ".proto", ".py", ".pyi", ".rb", ".rs", ".sh", ".sql", ".swift", ".ts", ".tsx", ".vue", ".svelte", ".astro",
	} {
		if strings.HasSuffix(strings.ToLower(pattern), extension) {
			return true
		}
	}
	return false
}

func dataPatternHasSupportedExtension(pattern string) bool {
	pattern = strings.ToLower(pattern)
	return strings.HasSuffix(pattern, ".json") || strings.HasSuffix(pattern, ".jsonc") ||
		strings.HasSuffix(pattern, ".yaml") || strings.HasSuffix(pattern, ".yml")
}
