package supplychain

import (
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestPNPMRejectsConfiguredProtectionsBeforeTheirIntroduction(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, value, before, supported string }{
		{"minimumReleaseAge", "43200", "10.15.0", "10.16.0"},
		{"trustPolicy", "no-downgrade", "10.20.0", "10.21.0"},
		{"blockExoticSubdeps", "true", "10.25.0", "10.26.0"},
		{"minimumReleaseAgeStrict", "true", "10.32.0", "11.0.0"},
		{"minimumReleaseAgeIgnoreMissingTime", "false", "10.32.0", "11.0.0"},
		{"trustLockfile", "false", "11.2.0", "11.3.0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := pnpmWorkspace{Path: "pnpm-workspace.yaml", Settings: map[string][]string{test.name: {test.value}}}
			config := policy.Config{}
			config.SupplyChain.MinimumReleaseAgeDays = 30
			before := pnpmRootSecurityFindings(config, ".", test.before, workspace)
			if !findingSubject(before, test.name) {
				t.Fatalf("ineffective setting passed: %+v", before)
			}
			after := pnpmRootSecurityFindings(config, ".", test.supported, workspace)
			if findingSubject(after, test.name) {
				t.Fatalf("supported protection failed: %+v", after)
			}
			delete(workspace.Settings, test.name)
			absent := pnpmRootSecurityFindings(config, ".", test.before, workspace)
			if findingSubject(absent, test.name) {
				t.Fatalf("unsupported protection was required: %+v", absent)
			}
		})
	}
}

func TestPNPMLifecyclePoliciesFollowThePinnedVersion(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		version, setting string
		accepted         bool
	}{
		{"10.23.0", "onlyBuiltDependencies", true},
		{"10.23.0", "allowBuilds", false},
		{"10.26.0", "allowBuilds", true},
		{"11.0.0", "allowBuilds", true},
		{"11.13.0", "onlyBuiltDependencies", false},
		{"11.13.0", "ignoredBuiltDependencies", false},
	} {
		t.Run(test.version+"/"+test.setting, func(t *testing.T) {
			payload := packageJSON{PackageManager: "pnpm@" + test.version}
			workspace := pnpmWorkspace{Path: "pnpm-workspace.yaml", Settings: map[string][]string{test.setting: {}}}
			findings := lifecycleDeclarationFindings("package.json", payload, workspace)
			if (len(findings) == 0) != test.accepted {
				t.Fatalf("findings=%+v", findings)
			}
		})
	}
	payload := packageJSON{PackageManager: "pnpm@11.13.0"}
	payload.PNPM.OnlyBuiltDependencies = []string{}
	workspace := pnpmWorkspace{Path: "pnpm-workspace.yaml", Settings: map[string][]string{"allowBuilds": {}}}
	findings := lifecycleDeclarationFindings("package.json", payload, workspace)
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "no longer reads package.json") {
		t.Fatalf("obsolete owner passed: %+v", findings)
	}
}
