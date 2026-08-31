package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestGateRunIdentityChangesWithEffectiveAmbientEnvironment(t *testing.T) {
	root := contentRepository(t, nil)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	commands := []MergeGateExecutionCommand{{
		Category: gaterun.OrdinaryTest, Kind: "unit", Scope: "repository", Cost: "quick",
		Command: policy.Command{Name: "unit", Argv: []string{"true"}, Cwd: ".", TimeoutSeconds: 30},
	}}
	previous, present := os.LookupEnv("CGO_ENABLED")
	defer func() {
		if present {
			_ = os.Setenv("CGO_ENABLED", previous)
		} else {
			_ = os.Unsetenv("CGO_ENABLED")
		}
	}()
	if err := os.Setenv("CGO_ENABLED", "0"); err != nil {
		t.Fatal(err)
	}
	first, err := gateRunIdentity(policyEngine, gaterun.MergeGate, "main", strings.Repeat("a", 40), strings.Repeat("b", 40), "recommended", commands, testGateRunBehaviorReview())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("CGO_ENABLED", "1"); err != nil {
		t.Fatal(err)
	}
	second, err := gateRunIdentity(policyEngine, gaterun.MergeGate, "main", strings.Repeat("a", 40), strings.Repeat("b", 40), "recommended", commands, testGateRunBehaviorReview())
	if err != nil {
		t.Fatal(err)
	}
	firstDigest, err := first.Digest()
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := second.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if firstDigest == secondDigest {
		t.Fatal("effective ambient environment did not invalidate the gate-run identity")
	}
}

func testGateRunBehaviorReview() gaterun.BehaviorReview {
	return gaterun.BehaviorReview{
		State: gaterun.BehaviorReviewNotRun, RequiredBoundary: gaterun.BehaviorReviewOnRequest,
		SelectedFeatures: []gaterun.BehaviorReviewFeatureSelection{}, SelectionDigest: strings.Repeat("0", 64),
	}
}

func TestWorkingTreeCandidateDigestChangesWithContent(t *testing.T) {
	root := documentationRepository(t)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "README.md", "# First\n", 0o600)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	firstSelection, err := policyEngine.Repository.SelectBase("main")
	if err != nil {
		t.Fatal(err)
	}
	first, err := workingTreeCandidateDigest(root, firstSelection)
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, "README.md", "# Second\n", 0o600)
	secondSelection, err := policyEngine.Repository.SelectBase("main")
	if err != nil {
		t.Fatal(err)
	}
	second, err := workingTreeCandidateDigest(root, secondSelection)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("working-tree content change did not change the candidate identity")
	}
}
