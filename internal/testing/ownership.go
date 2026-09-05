package testing

import (
	"fmt"
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func OwnershipFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := ownershipPatternFindings(repo, files)
	for _, path := range files {
		if !repo.IsExecutableSource(path) || !repo.IsTest(path) {
			continue
		}
		owners := repo.TestOwnerships(path)
		if len(owners) == 0 {
			findings = append(findings, unmappedTestFinding(repo, files, path, nil))
			continue
		}
		if len(owners) != 1 {
			findings = append(findings, testOwnershipFinding(path, "overlap", "governed test matches multiple ownership entries; exactly one is required"))
			continue
		}
		if message := testOwnerCoverageMessage(repo, path, owners[0]); message != "" {
			findings = append(findings, testOwnershipFinding(path, owners[0].FocusedSuite, message))
		}
	}
	return findings
}

func testOwnerCoverageMessage(repo repository.Repository, path string, owner policy.TestOwnership) string {
	if _, err := repo.Resolve(path); err != nil {
		return "owned test must resolve to a contained repository file"
	}
	if _, exists := repo.Config.ModuleByName[owner.Module]; !exists {
		return fmt.Sprintf("test ownership names unknown production module %q", owner.Module)
	}
	for _, suite := range repo.Config.Tests.Suites {
		if suite.Name != owner.FocusedSuite {
			continue
		}
		if !policy.IsPrimaryTestSuite(suite, owner.Module) {
			return "primary focused suite must be quick, owned by this production module, and run in focused, recommended, and full"
		}
		if !policy.MatchesAny(path, suite.Paths) {
			return "primary focused suite execution paths do not include this owned test"
		}
		return ""
	}
	return fmt.Sprintf("test ownership names unknown focused suite %q", owner.FocusedSuite)
}

func ownershipPatternFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for index, ownership := range repo.Config.Tests.Ownership {
		for _, pattern := range ownership.Paths {
			matched := false
			for _, path := range files {
				if !policy.Match(path, pattern) || !repo.IsExecutableSource(path) {
					continue
				}
				if repo.IsTest(path) {
					matched = true
				} else {
					finding := testOwnershipFinding(path, "production-source", "test ownership pattern matches production source; ownership cannot classify it as a test")
					finding.Fields = map[string]string{"pattern": pattern}
					findings = append(findings, finding)
				}
			}
			if !matched {
				finding := testOwnershipFinding(policy.ConfigFilename, fmt.Sprintf("tests.ownership[%d]", index), "ownership pattern is stale: it matches no governed executable test")
				finding.Fields = map[string]string{"pattern": pattern}
				findings = append(findings, finding)
			}
		}
	}
	return findings
}

func testOwnershipFinding(path, subject, message string) policy.Finding {
	return policy.Finding{
		Check: "policy.testOwnership", Path: path, Subject: subject, Message: message,
		Remediation: policy.FindingRemediation{
			Summary:     "Declare exactly one production module and primary quick focused suite for each test. Choose ownership from the behavior the test verifies, not merely its directory.",
			NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "doctor"}, Cwd: "."},
		},
	}
}

func compatibleOwnerSuite(config policy.Config, module, path string) string {
	names := []string{}
	for _, suite := range config.Tests.Suites {
		if policy.IsPrimaryTestSuite(suite, module) && policy.MatchesAny(path, suite.Paths) {
			names = append(names, suite.Name)
		}
	}
	slices.Sort(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}
