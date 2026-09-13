package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rhysd/actionlint"
)

func parseConfiguration(data []byte) (*actionlint.Config, error) {
	if data == nil {
		return nil, nil
	}
	if len(data) > MaximumInputBytes {
		return nil, errors.New("actionlint configuration exceeds the byte limit")
	}
	config, err := actionlint.ParseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("invalid actionlint configuration: %w", err)
	}
	if len(config.Paths) != 0 {
		return nil, errors.New("actionlint path ignores cannot weaken shared workflow checks")
	}
	if len(config.SelfHostedRunner.Labels) > maximumJobs {
		return nil, errors.New("actionlint runner label count exceeds the adapter limit")
	}
	for _, label := range config.SelfHostedRunner.Labels {
		if strings.TrimSpace(label) == "" || strings.ContainsAny(label, "*?[\\") {
			return nil, errors.New("actionlint self-hosted runner labels must be exact non-empty names")
		}
		if err := validateStrings(label); err != nil {
			return nil, err
		}
	}
	return config, nil
}
