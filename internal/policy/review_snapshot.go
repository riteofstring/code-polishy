package policy

import (
	"encoding/json"
	"fmt"

	policyschema "github.com/riteofstring/code-polishy/schema"
)

var reviewSnapshotValidator = policyschema.NewValidator(policyschema.ConfigurationBase + "code-polishy-review-snapshot.schema.json")

func ParseReviewSnapshot(data []byte, source string) (Config, error) {
	if len(data) == 0 || len(data) > maximumConfigurationBytes {
		return Config{}, fmt.Errorf("review snapshot has an invalid byte size")
	}
	if err := policyschema.ValidateUniqueJSON(data, 64); err != nil {
		return Config{}, err
	}
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return Config{}, err
	}
	if header.Version == ConfigVersion {
		return Parse(data, source)
	}
	if err := reviewSnapshotValidator.Validate(data); err != nil {
		return Config{}, fmt.Errorf("historical review snapshot: %w", err)
	}
	var snapshot struct {
		Modules []Module `json:"modules"`
		Scope   struct {
			Tests []string `json:"tests"`
		} `json:"scope"`
		Tests struct {
			Suites []TestSuite `json:"suites"`
		} `json:"tests"`
		Verification struct {
			BehaviorReview *BehaviorReviewPolicy `json:"behaviorReview"`
		} `json:"verification"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Config{}, err
	}
	config := Config{Version: header.Version, ConfigPath: source, Modules: snapshot.Modules,
		Tests:        Testing{Paths: snapshot.Scope.Tests, Suites: snapshot.Tests.Suites},
		Verification: Verification{BehaviorReview: snapshot.Verification.BehaviorReview}}
	if err := validateReviewSnapshot(&config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func validateReviewSnapshot(config *Config) error {
	if err := validateModules(config); err != nil {
		return err
	}
	names := map[string]bool{}
	for index := range config.Tests.Suites {
		suite := &config.Tests.Suites[index]
		if names[suite.Name] {
			return fmt.Errorf("duplicate historical test suite %q", suite.Name)
		}
		names[suite.Name] = true
		suiteDefaults(suite)
		if err := validateCommandModules(config, suite.Modules, suite.Name); err != nil {
			return err
		}
	}
	for _, module := range config.Modules {
		config.Tests.Ownership = append(config.Tests.Ownership, TestOwnership{Module: module.Name, Paths: module.Paths})
	}
	if review := config.Verification.BehaviorReview; review != nil {
		defaultString(&review.DefaultRequiredAt, BehaviorReviewOnRequest)
		features := map[string]bool{}
		for _, feature := range review.Features {
			if err := validateBehaviorReviewFeature(config, *review, feature, feature.Name, features); err != nil {
				return err
			}
		}
	}
	return nil
}
