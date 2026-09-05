package policy

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestOperationalHandoffConfigurationAcceptsExactRepositorySituationsAndScopes(t *testing.T) {
	t.Parallel()
	handoffs := []OperationalHandoff{{
		Name: "release", Description: "Prepare and verify a production release.", Path: "docs/operations/release.md",
		Situations: []string{"release", "deploy-production"}, Modules: []string{"content"}, SourcePaths: []string{"content/release.go"},
	}}
	config, err := Parse(operationalHandoffConfiguration(t, handoffs), ConfigFilename)
	if err != nil || len(config.Documentation.Handoffs) != 1 || config.Documentation.Handoffs[0].Situations[1] != "deploy-production" {
		t.Fatalf("handoff config=%+v error=%v", config.Documentation.Handoffs, err)
	}
}

func TestOperationalHandoffConfigurationRejectsInvalidDeclarations(t *testing.T) {
	t.Parallel()
	valid := `{"name":"release","description":"Release procedure.","path":"docs/operations/release.md","situations":["release"]}`
	for name, declarations := range map[string]string{
		"missing trigger":       "[" + strings.Replace(valid, `,"situations":["release"]`, "", 1) + "]",
		"empty trigger":         "[" + strings.Replace(valid, `["release"]`, `[]`, 1) + "]",
		"duplicate trigger":     "[" + strings.Replace(valid, `["release"]`, `["release","release"]`, 1) + "]",
		"unknown property":      "[" + strings.Replace(valid, `"name"`, `"command":"deploy","name"`, 1) + "]",
		"duplicate name":        "[" + valid + "," + strings.Replace(valid, "release.md", "other.md", 1) + "]",
		"duplicate document":    "[" + valid + "," + strings.Replace(valid, `"name":"release"`, `"name":"deploy"`, 1) + "]",
		"unknown module":        "[" + strings.Replace(valid, `"situations":["release"]`, `"modules":["missing"]`, 1) + "]",
		"wildcard source":       "[" + strings.Replace(valid, `"situations":["release"]`, `"sourcePaths":["content/**"]`, 1) + "]",
		"escaping document":     "[" + strings.Replace(valid, "docs/operations/release.md", "../release.md", 1) + "]",
		"noncanonical path":     "[" + strings.Replace(valid, "docs/operations/release.md", "./release.md", 1) + "]",
		"wrong document":        "[" + strings.Replace(valid, "release.md", "release.json", 1) + "]",
		"document control":      "[" + strings.Replace(valid, "release.md", `release\n.md`, 1) + "]",
		"empty description":     "[" + strings.Replace(valid, "Release procedure.", " ", 1) + "]",
		"description newline":   "[" + strings.Replace(valid, "Release procedure.", `Release\nprocedure.`, 1) + "]",
		"long UTF8 description": "[" + strings.Replace(valid, "Release procedure.", strings.Repeat("é", 257), 1) + "]",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(operationalHandoffConfiguration(t, json.RawMessage(declarations)), ConfigFilename); err == nil {
				t.Fatalf("accepted invalid handoffs: %s", declarations)
			}
		})
	}
}

func TestOperationalHandoffConfigurationBoundsTheInventory(t *testing.T) {
	t.Parallel()
	handoffs := make([]OperationalHandoff, MaximumOperationalHandoffs+1)
	for index := range handoffs {
		handoffs[index] = OperationalHandoff{
			Name: fmt.Sprintf("handoff-%d", index), Description: "Repository procedure.",
			Path: fmt.Sprintf("docs/operations/%d.md", index), Situations: []string{"release"},
		}
	}
	if _, err := Parse(operationalHandoffConfiguration(t, handoffs), ConfigFilename); err == nil {
		t.Fatal("accepted an oversized handoff inventory")
	}
}

func TestOperationalHandoffSituationsRequireExactDistinctIdentifiers(t *testing.T) {
	t.Parallel()
	for _, situations := range [][]string{{"release", "release"}, {""}, {"please deploy"}, {"Release"}, {"release*"}, {strings.Repeat("x", 129)}, make([]string, 33)} {
		if err := ValidateHandoffSituations(situations); err == nil {
			t.Fatalf("accepted situations %v", situations)
		}
	}
	if err := ValidateHandoffSituations([]string{"authentication", "release", "deployment", "deploy-production"}); err != nil {
		t.Fatal(err)
	}
}

func TestOperationalHandoffSituationsRetainFullExplicitSelectionWithWorkflow(t *testing.T) {
	t.Parallel()
	requested := make([]string, 32)
	for index := range requested {
		requested[index] = fmt.Sprintf("situation-%d", index)
	}
	situations, err := HandoffContextSituations(requested, "design-context")
	if err != nil || len(situations) != 33 || situations[0] != "design-context" || requested[0] != "situation-0" {
		t.Fatalf("situations=%v requested=%v error=%v", situations, requested, err)
	}
	situations, err = HandoffContextSituations([]string{"design-context", "release"}, "design-context")
	if err != nil || len(situations) != 2 {
		t.Fatalf("overlapping workflow selection=%v error=%v", situations, err)
	}
}

func operationalHandoffConfiguration(t *testing.T, handoffs any) []byte {
	t.Helper()
	data, err := json.Marshal(handoffs)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(strings.Replace(minimalConfig(), `"checks":[]`, `"documentation":{"handoffs":`+string(data)+`},"checks":[]`, 1))
}
