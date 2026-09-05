package gaterun

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestIdentityDigestBindsCommandAndEnvironmentInputs(t *testing.T) {
	input := testIdentityInput([]CommandSpec{testCommand(OrdinaryTest, "unit", "FOO")})
	input.Environment = []EnvironmentInput{{Name: "FOO", Value: "top-secret", Present: true}}
	identity, err := NewIdentity(input)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := identity.Digest()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "top-secret") || !strings.Contains(string(encoded), ContentSHA256([]byte("top-secret"))) {
		t.Fatalf("identity exposes or omits environment fingerprint: %s", encoded)
	}
	cases := []struct {
		name   string
		mutate func(*IdentityInput)
	}{
		{name: "candidate", mutate: func(value *IdentityInput) { value.Candidate = strings.Repeat("c", 40) }},
		{name: "configuration", mutate: func(value *IdentityInput) { value.ConfigurationSHA256 = strings.Repeat("d", 64) }},
		{name: "Git evidence", mutate: func(value *IdentityInput) { value.GitEvidenceSHA256 = strings.Repeat("e", 64) }},
		{name: "policy validity", mutate: func(value *IdentityInput) { value.PolicyValiditySHA256 = strings.Repeat("f", 64) }},
		{name: "environment", mutate: func(value *IdentityInput) { value.Environment[0].Value = "other" }},
		{name: "command", mutate: func(value *IdentityInput) { value.Commands[0].Argv = []string{"tool", "changed"} }},
		{name: "category", mutate: func(value *IdentityInput) { value.Commands[0].Category = Check }},
		{name: "behavior review task selection", mutate: func(value *IdentityInput) {
			value.BehaviorReview = requiredBehaviorReview("task-request")
		}},
		{name: "behavior review selection digest", mutate: func(value *IdentityInput) {
			value.BehaviorReview.SelectionDigest = strings.Repeat("0", 64)
		}},
		{name: "behavior review full candidate", mutate: func(value *IdentityInput) {
			value.BehaviorReview.FullCandidate = true
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			changed := cloneIdentityInput(input)
			test.mutate(&changed)
			updated, err := NewIdentity(changed)
			if err != nil {
				t.Fatal(err)
			}
			updatedDigest, err := updated.Digest()
			if err != nil {
				t.Fatal(err)
			}
			if updatedDigest == digest {
				t.Fatalf("%s did not change the identity digest", test.name)
			}
		})
	}
}

func TestIdentityRequiresStrictBehaviorReview(t *testing.T) {
	input := testIdentityInput([]CommandSpec{testCommand(OrdinaryTest, "unit")})
	input.BehaviorReview = requiredBehaviorReview("task-request")
	cases := []struct {
		name   string
		mutate func(*BehaviorReview)
	}{
		{name: "state", mutate: func(value *BehaviorReview) { value.State = "unknown" }},
		{name: "boundary", mutate: func(value *BehaviorReview) { value.RequiredBoundary = "later" }},
		{name: "selection digest", mutate: func(value *BehaviorReview) { value.SelectionDigest = "invalid" }},
		{name: "missing selections", mutate: func(value *BehaviorReview) { value.SelectedFeatures = nil }},
		{name: "missing feature reasons", mutate: func(value *BehaviorReview) { value.SelectedFeatures[0].Reasons = []string{} }},
		{name: "duplicate feature", mutate: func(value *BehaviorReview) {
			value.SelectedFeatures = append(value.SelectedFeatures, cloneBehaviorReviewFeature(value.SelectedFeatures[0]))
		}},
		{name: "unordered feature", mutate: func(value *BehaviorReview) {
			value.SelectedFeatures = []BehaviorReviewFeatureSelection{
				{Name: "search", Reasons: []string{"task-request"}},
				{Name: "checkout", Reasons: []string{"task-request"}},
			}
		}},
		{name: "duplicate reason", mutate: func(value *BehaviorReview) {
			value.SelectedFeatures[0].Reasons = []string{"task-request", "task-request"}
		}},
		{name: "unordered reason", mutate: func(value *BehaviorReview) {
			value.SelectedFeatures[0].Reasons = []string{"task-request", "base-policy"}
		}},
		{name: "required receipt", mutate: func(value *BehaviorReview) {
			value.ReviewID, value.ReceiptPath = "review-123", behaviorReceiptPath()
		}},
		{name: "passed missing receipt", mutate: func(value *BehaviorReview) { value.State = BehaviorReviewPassed }},
		{name: "failed partial receipt", mutate: func(value *BehaviorReview) {
			value.State, value.ReviewID = BehaviorReviewFailed, "review-123"
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			changed := cloneIdentityInput(input)
			test.mutate(&changed.BehaviorReview)
			if _, err := NewIdentity(changed); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("NewIdentity() error = %v, want invalid input", err)
			}
		})
	}
}

func TestIdentityRequiresExactDeclaredEnvironmentValues(t *testing.T) {
	input := testIdentityInput([]CommandSpec{testCommand(OrdinaryTest, "unit", "FOO")})
	for _, environment := range [][]EnvironmentInput{
		nil,
		{{Name: "EXTRA", Value: "value", Present: true}},
		{{Name: "FOO", Value: "value", Present: true}, {Name: "FOO", Value: "other", Present: true}},
		{{Name: "FOO", Value: "value", Present: false}},
	} {
		input.Environment = environment
		if _, err := NewIdentity(input); err == nil {
			t.Fatalf("NewIdentity accepted environment %+v", environment)
		}
	}
}

func TestIdentityBindsAmbientEnvironmentSeparately(t *testing.T) {
	input := testIdentityInput([]CommandSpec{testCommand(OrdinaryTest, "unit", "FOO")})
	input.Environment = []EnvironmentInput{{Name: "FOO", Value: "declared", Present: true}}
	input.AmbientEnvironment = []EnvironmentInput{{Name: "PATH", Value: "/managed/bin", Present: true}}
	identity, err := NewIdentity(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(identity.Environment) != 1 || identity.Environment[0].Name != "FOO" || len(identity.AmbientEnvironment) != 1 || identity.AmbientEnvironment[0].Name != "PATH" {
		t.Fatalf("identity environment separation = %+v", identity)
	}
	baseline, err := identity.Digest()
	if err != nil {
		t.Fatal(err)
	}
	changed := cloneIdentityInput(input)
	changed.AmbientEnvironment[0].Value = "/other/bin"
	updated, err := NewIdentity(changed)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := updated.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if digest == baseline {
		t.Fatal("ambient environment did not change the identity digest")
	}
	for _, invalid := range [][]EnvironmentInput{
		{{Name: "PATH", Value: "one", Present: true}, {Name: "PATH", Value: "two", Present: true}},
		{{Name: "PATH", Value: "value", Present: false}},
	} {
		changed := cloneIdentityInput(input)
		changed.AmbientEnvironment = invalid
		if _, err := NewIdentity(changed); err == nil {
			t.Fatalf("NewIdentity accepted ambient environment %+v", invalid)
		}
	}
}

func testIdentityInput(commands []CommandSpec) IdentityInput {
	return IdentityInput{
		Gate: MergeGate, RequestedBase: "origin/main", ExactBase: strings.Repeat("a", 40), Candidate: strings.Repeat("b", 40),
		PolicyLevel: "recommended", Release: ReleaseIdentity{Version: "0.19.0", Digest: strings.Repeat("e", 64)},
		ConfigurationSHA256: strings.Repeat("f", 64), Platform: Platform{OS: "linux", Arch: "amd64"}, Commands: cloneCommands(commands),
		Environment: []EnvironmentInput{}, BehaviorReview: notRunBehaviorReview(),
	}
}

func notRunBehaviorReview() BehaviorReview {
	return BehaviorReview{
		State: BehaviorReviewNotRun, RequiredBoundary: BehaviorReviewOnRequest,
		SelectedFeatures: []BehaviorReviewFeatureSelection{}, SelectionDigest: strings.Repeat("1", 64),
	}
}

func requiredBehaviorReview(reason string) BehaviorReview {
	return BehaviorReview{
		State: BehaviorReviewRequired, RequiredBoundary: BehaviorReviewMerge,
		SelectedFeatures: []BehaviorReviewFeatureSelection{{Name: "checkout", Reasons: []string{reason}}},
		SelectionDigest:  strings.Repeat("2", 64),
	}
}

func passedBehaviorReview() BehaviorReview {
	review := requiredBehaviorReview("task-request")
	review.State, review.ReviewID, review.ReceiptPath = BehaviorReviewPassed, "review-123", behaviorReceiptPath()
	return review
}

func behaviorReceiptPath() string {
	return ".code-polishy-reports/behavior-review/receipt.json"
}

func cloneBehaviorReviewFeature(feature BehaviorReviewFeatureSelection) BehaviorReviewFeatureSelection {
	return BehaviorReviewFeatureSelection{Name: feature.Name, Reasons: cloneStrings(feature.Reasons)}
}

func testCommand(category CommandCategory, name string, environment ...string) CommandSpec {
	return CommandSpec{
		Category: category, Scope: "module", Cost: "quick", Name: name, Argv: []string{"tool", name}, Cwd: ".",
		Provides: []string{}, Paths: []string{}, Modules: []string{"gaterun"}, RunOn: []string{"recommended"},
		Environment: append([]string{}, environment...), ExclusiveResources: []string{}, TimeoutSeconds: 60, PassFilePaths: []string{},
	}
}

func cloneIdentityInput(input IdentityInput) IdentityInput {
	input.Commands = cloneCommands(input.Commands)
	input.Environment = append([]EnvironmentInput{}, input.Environment...)
	input.AmbientEnvironment = append([]EnvironmentInput{}, input.AmbientEnvironment...)
	input.BehaviorReview = cloneBehaviorReview(input.BehaviorReview)
	return input
}
