package policy

import "fmt"

func validateReliabilityReminder(config *Config) error {
	configured := config.Quality.ReliabilityReminder
	if configured == nil {
		return nil
	}
	if err := validateCommandModules(config, configured.Modules, "quality.reliabilityReminder.modules"); err != nil {
		return err
	}
	for index, path := range configured.SourcePaths {
		if err := repositoryPath(path, fmt.Sprintf("quality.reliabilityReminder.sourcePaths[%d]", index)); err != nil {
			return err
		}
	}
	return validateDesignSourcePaths(config, configured.SourcePaths, "quality.reliabilityReminder.sourcePaths", map[string]bool{})
}
