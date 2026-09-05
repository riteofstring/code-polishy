package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestGatePolicyExpiryInvalidatesTheStoredPassedIdentity(t *testing.T) {
	t.Parallel()
	today := time.Now().UTC().Truncate(24 * time.Hour)
	expires := policy.Date{Time: today.AddDate(0, 0, 7)}
	for _, kind := range []string{"exception", "vulnerability", "release-age", "policy-module"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			policyEngine := gateWarningCandidate(t, "recommended")
			setGateExpiry(&policyEngine.Repository.Config, kind, expires)
			head, err := policyEngine.Repository.CleanHead()
			if err != nil {
				t.Fatal(err)
			}
			identity, err := gateRunIdentity(policyEngine, gaterun.MergeGate, "main", head, head, "documentation", nil, testGateRunBehaviorReview(), nil)
			if err != nil || identity.PolicyValiditySHA256 == "" {
				t.Fatalf("gate did not bind current policy validity: %+v, error=%v", identity, err)
			}
			run, err := gaterun.Start(gaterun.StartOptions{RepositoryRoot: policyEngine.Repository.Root, Identity: identity})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := run.Finalize(gaterun.FinalizeOptions{Status: gaterun.RunPassed, BehaviorReview: identity.BehaviorReview}); err != nil {
				t.Fatal(err)
			}
			sameDay := identity
			sameDay.PolicyValiditySHA256 = gatePolicyValiditySHA256(policyEngine.Repository.Config, expires.Add(23*time.Hour))
			if _, err := gaterun.LoadReport(policyEngine.Repository.Root, sameDay); err != nil {
				t.Fatalf("validity within the inclusive expiry day discarded the passed gate: %v", err)
			}
			expired := identity
			expired.PolicyValiditySHA256 = gatePolicyValiditySHA256(policyEngine.Repository.Config, expires.Add(24*time.Hour))
			if _, err := gaterun.LoadReport(policyEngine.Repository.Root, expired); err == nil {
				t.Fatal("expired policy acceptance reused its earlier passed identity")
			}
		})
	}
}

func TestPolicyExpiryDuringGatePreventsAcceptanceAndReuse(t *testing.T) {
	t.Parallel()
	policyEngine := gateWarningCandidate(t, "recommended")
	now := time.Now().UTC().Truncate(24 * time.Hour)
	setGateExpiry(&policyEngine.Repository.Config, "exception", policy.Date{Time: now.AddDate(0, 0, -1)})
	head, err := policyEngine.Repository.CleanHead()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := gateRunIdentity(policyEngine, gaterun.MergeGate, "main", head, head, "documentation", nil, testGateRunBehaviorReview(), nil)
	if err != nil {
		t.Fatal(err)
	}
	controller := &gateRunController{
		candidate: head, gitEvidenceSHA256: identity.GitEvidenceSHA256, architectureArtifactsSHA256: identity.ArchitectureReviewSHA256,
		policyValiditySHA256: gatePolicyValiditySHA256(policyEngine.Repository.Config, now.AddDate(0, 0, -1)),
	}
	if err := controller.candidateIntegrityError(policyEngine); err == nil || !strings.Contains(err.Error(), "validity changed") {
		t.Fatalf("expired acceptance survived gate publication: %v", err)
	}
	if _, err := policyEngine.alreadyPassedMergeGate(t.Context(), "main", MergeGateExecutionPlan{}, controller); err == nil || !strings.Contains(err.Error(), "validity changed") {
		t.Fatalf("expired acceptance survived the cached gate path: %v", err)
	}
}

func TestGatePolicyValidityDoesNotExpireRepositoriesWithoutTimedAcceptances(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	before := gatePolicyValiditySHA256(policy.Config{}, now)
	after := gatePolicyValiditySHA256(policy.Config{}, now.AddDate(10, 0, 0))
	if before != after {
		t.Fatal("calendar time invalidated a gate with no timed policy acceptances")
	}
}

func setGateExpiry(config *policy.Config, kind string, expires policy.Date) {
	switch kind {
	case "exception":
		config.Exceptions = []policy.Exception{{ID: "lint", Check: "quality.lint", Path: "content/data.json", Subject: "fixture", Reason: "Reviewed fixture content.", Owner: "content", Expires: expires}}
	case "vulnerability":
		config.SupplyChain.VulnerabilityAssessments = []policy.VulnerabilityAssessment{{ID: "reviewed-dependency", Expires: expires}}
	case "release-age":
		config.SupplyChain.ReleaseAgeAssessments = []policy.ReleaseAgeAssessment{{ID: "supported-release", Expires: expires}}
	case "policy-module":
		config.PolicyModules.Overrides = []policy.PolicyModuleOverride{{Name: "python-quality", Root: ".", Mode: "disabled", Reason: "Reviewed disabled adapter.", Owner: "content", Expires: expires}}
	}
}
