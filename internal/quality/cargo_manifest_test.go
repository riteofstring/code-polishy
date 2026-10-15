package quality

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestCargoManifestCoverageReportsOriginalLocation(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	writeQualityFile(t, repo.Root, "crates/app/Cargo.toml", "[package]\nname = 'app'\nname = 'duplicate'\n")
	findings := CoverageFindings(repo, []string{"crates/app/Cargo.toml"})
	if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Check == "policy.rustProject" && finding.Path == "crates/app/Cargo.toml" && finding.Line == 3 && finding.Subject == "manifest"
	}) {
		t.Fatalf("missing Cargo manifest diagnostic: %+v", findings)
	}
}

func TestCargoManifestCoverageRetainsFocusedScope(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	writeQualityFile(t, repo.Root, "good/Cargo.toml", "[package]\nname = 'good'\nedition = '2024'\n")
	writeQualityFile(t, repo.Root, "bad/Cargo.toml", "[package\n")
	for _, finding := range CoverageFindings(repo, []string{"good/Cargo.toml"}) {
		if finding.Check == "policy.rustProject" {
			t.Fatalf("unselected Cargo project affected focused coverage: %+v", finding)
		}
	}
}
