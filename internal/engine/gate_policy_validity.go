package engine

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func gatePolicyValiditySHA256(config policy.Config, now time.Time) string {
	today := now.UTC().Truncate(24 * time.Hour)
	states := []string{}
	add := func(kind, name string, expires policy.Date) {
		states = append(states, fmt.Sprintf("%s:%q:%t", kind, name, expires.Before(today)))
	}
	for _, exception := range config.Exceptions {
		add("exception", exception.ID, exception.Expires)
	}
	for _, assessment := range config.SupplyChain.VulnerabilityAssessments {
		add("vulnerability", assessment.ID, assessment.Expires)
	}
	for _, assessment := range config.SupplyChain.ReleaseAgeAssessments {
		add("release-age", assessment.ID, assessment.Expires)
	}
	for _, override := range config.PolicyModules.Overrides {
		if override.Mode != "enabled" {
			add("policy-module", override.Name+"\x00"+override.Root, override.Expires)
		}
	}
	slices.Sort(states)
	return gaterun.ContentSHA256([]byte(strings.Join(states, "\n")))
}

func (engine *Engine) currentGatePolicyValidity(expected string) error {
	if gatePolicyValiditySHA256(engine.Repository.Config, time.Now()) != expected {
		return fmt.Errorf("policy exception or assessment validity changed during gate execution")
	}
	return nil
}
