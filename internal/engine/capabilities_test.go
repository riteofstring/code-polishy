package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestCapabilitiesExposeInstalledPackAndUnavailableSelectedPack(t *testing.T) {
	t.Parallel()
	dataRoot := t.TempDir()
	t.Cleanup(func() { makePackTreeWritable(dataRoot) })
	identity, _, err := pack.Install(packIntegrationSource(t), dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	root := contentRepository(t, nil)
	writeEngineFile(t, root, "content/main.fixture", "good\n", 0o600)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	policyEngine.PackDataRoot = dataRoot
	policyEngine.Repository.Config.Packs = []policy.PackSelection{
		{Name: identity.Name, Version: identity.Version, Digest: identity.Digest},
		{Name: "missing-pack", Version: "1.0.0", Digest: strings.Repeat("a", 64)},
	}
	inventory, err := policyEngine.Capabilities("")
	if err != nil {
		t.Fatal(err)
	}
	var installed, unavailable, lint bool
	for _, entry := range inventory.Capabilities {
		switch entry.ID {
		case "language-pack:fixture-language":
			installed = entry.Availability == "available" && entry.Source.SHA256 == identity.Digest && entry.Source.Version == identity.Version
		case "language-pack:missing-pack":
			unavailable = entry.Availability == "unavailable" && entry.Reason != ""
		case "pack-capability:pack.fixture-language.adapter.lint":
			lint = entry.Availability == "available" && entry.Name == "lint" && entry.Source.Name == identity.Name && slices.Equal(entry.Enforcement, []string{"check", "gate"})
		}
	}
	if !installed || !unavailable || !lint {
		t.Fatalf("pack inventory omitted installed=%v unavailable=%v lint=%v: %+v", installed, unavailable, lint, inventory.Capabilities)
	}
	if _, err := os.Stat(filepath.Join(root, ".code-polishy-reports")); !os.IsNotExist(err) {
		t.Fatalf("capability discovery wrote report state: %v", err)
	}
}

func TestCapabilityCandidatesAreBoundedDeterministicAndExplicitOnly(t *testing.T) {
	t.Parallel()
	entries := []CapabilityEntry{}
	for index := range 25 {
		name := fmt.Sprintf("feature-%02d", index)
		entries = append(entries, configuredCapability("behavior-feature", name, "Review purchase behavior.", ""))
	}
	entries[24].Aliases = []string{"purchase"}
	selected, matched, err := selectCapabilityCandidates(entries, "purchase")
	if err != nil || len(selected) != 20 || matched != 25 || selected[0].Name != "feature-24" || selected[1].Name != "feature-00" {
		t.Fatalf("candidates=%+v matched=%d error=%v", selected, matched, err)
	}
	slices.Reverse(entries)
	repeated, count, err := selectCapabilityCandidates(entries, "purchase")
	if err != nil || count != matched || len(repeated) != len(selected) {
		t.Fatalf("repeated candidates=%+v count=%d error=%v", repeated, count, err)
	}
	for index := range selected {
		if selected[index].ID != repeated[index].ID {
			t.Fatalf("input order changed candidate %d", index)
		}
	}
	if selected, _, err := selectCapabilityCandidates(entries, "unrelated"); err != nil || len(selected) != 0 {
		t.Fatalf("unrelated query returned candidates=%+v error=%v", selected, err)
	}
	for _, query := range []string{strings.Repeat("x", 1025), strings.Repeat("term ", 17), "\x00", "\u2003"} {
		if _, err := normalizedCapabilityQuery(query); err == nil {
			t.Fatalf("accepted malformed query %q", query)
		}
	}
}
