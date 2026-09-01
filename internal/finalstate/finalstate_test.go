package finalstate

import (
	"strings"
	"testing"
)

func TestBuildCreatesStableBoundedHunkEvidence(t *testing.T) {
	input := BuildInput{
		Path: "internal/soup/soup.go", Role: RoleProductionSource,
		Patch:  []byte("diff --git a/internal/soup/soup.go b/internal/soup/soup.go\n--- a/internal/soup/soup.go\n+++ b/internal/soup/soup.go\n@@ -1,3 +1,6 @@\n package soup\n+\n+func Tomato() string {\n+\treturn \"tomato\"\n+}\n"),
		Source: []byte("package soup\n\nfunc Tomato() string {\n\treturn \"tomato\"\n}\n"),
	}
	first, err := Build([]BuildInput{input})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build([]BuildInput{input})
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 != second.SHA256 || len(first.Paths) != 1 || len(first.Paths[0].Hunks) != 1 {
		t.Fatalf("evidence = %+v", first)
	}
	hunk := first.Paths[0].Hunks[0]
	if !hunk.Context.Available || hunk.Context.StartLine != 1 || hunk.Context.EndLine != 5 || !strings.HasPrefix(hunk.ID, "hunk-") {
		t.Fatalf("hunk = %+v", hunk)
	}
	if err := ValidateEvidence(first); err != nil {
		t.Fatal(err)
	}
}

func TestValidateFindingsBindsCorrectionToEvidenceAndIntent(t *testing.T) {
	evidence := soupEvidence(t)
	hunk := evidence.Paths[0].Hunks[0]
	finding := Finding{
		Kind: KindCorrectionResidue, Path: evidence.Paths[0].Path, Line: 4, PatchHunkSHA256: hunk.SHA256,
		IntentIDs: []string{"intent-remove-broccoli"}, Summary: "The recipe filters broccoli instead of removing it from construction.",
	}
	if err := ValidateFindings([]Finding{finding}, evidence, []string{"intent-remove-broccoli"}); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Finding){
		"kind":    func(value *Finding) { value.Kind = "guess" },
		"path":    func(value *Finding) { value.Path = "other.go" },
		"line":    func(value *Finding) { value.Line = 1000 },
		"hunk":    func(value *Finding) { value.PatchHunkSHA256 = strings.Repeat("0", 64) },
		"intent":  func(value *Finding) { value.IntentIDs = []string{"intent-invented"} },
		"control": func(value *Finding) { value.Summary = "bad\nsummary" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := finding
			candidate.IntentIDs = append([]string{}, finding.IntentIDs...)
			mutate(&candidate)
			if err := ValidateFindings([]Finding{candidate}, evidence, []string{"intent-remove-broccoli"}); err == nil {
				t.Fatalf("ValidateFindings() accepted %+v", candidate)
			}
		})
	}
	if err := ValidateFindings([]Finding{finding, finding}, evidence, []string{"intent-remove-broccoli"}); err == nil {
		t.Fatal("ValidateFindings() accepted duplicate findings")
	}
}

func TestValidateFindingsAllowsCleanDirectImplementation(t *testing.T) {
	evidence := soupEvidence(t)
	if err := ValidateFindings([]Finding{}, evidence, []string{"intent-remove-broccoli"}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildPreservesHistoricalAndFixturePathRoles(t *testing.T) {
	inputs := []BuildInput{
		{Path: "CHANGELOG.md", Role: RoleChangelog, Patch: []byte("diff --git a/CHANGELOG.md b/CHANGELOG.md\n@@ -1 +1 @@\n-old\n+new\n"), Source: []byte("new\n")},
		{Path: "docs/plans/change.md", Role: RolePlan, Patch: []byte("diff --git a/docs/plans/change.md b/docs/plans/change.md\n@@ -1 +1 @@\n-old\n+new\n"), Source: []byte("new\n")},
		{Path: "testdata/prompt.txt", Role: RoleFixture, Patch: []byte("diff --git a/testdata/prompt.txt b/testdata/prompt.txt\n@@ -1 +1 @@\n-old\n+new\n"), Source: []byte("new\n")},
	}
	evidence, err := Build(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Paths[0].Role != RoleChangelog || evidence.Paths[1].Role != RolePlan || evidence.Paths[2].Role != RoleFixture {
		t.Fatalf("roles = %+v", evidence.Paths)
	}
}

func soupEvidence(t *testing.T) Evidence {
	t.Helper()
	evidence, err := Build([]BuildInput{{
		Path: "internal/soup/soup.go", Role: RoleProductionSource,
		Patch:  []byte("diff --git a/internal/soup/soup.go b/internal/soup/soup.go\n@@ -1,4 +1,6 @@\n package soup\n+func Tomato() string {\n+\tif ingredient != Broccoli {\n+\t\treturn \"tomato\"\n+\t}\n+}\n"),
		Source: []byte("package soup\nfunc Tomato() string {\n\tif ingredient != Broccoli {\n\t\treturn \"tomato\"\n\t}\n}\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}
