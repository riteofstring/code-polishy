package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestFormatRetainsAssetDirectoryLinksBesideConfigurationEdits(t *testing.T) {
	root := t.TempDir()
	writeEngineFile(t, root, "images/picture.png", "original", 0o600)
	writeEngineFile(t, root, "public/keep.txt", "text", 0o600)
	writeEngineFile(t, root, "index.ts", "export const value = 1;\n", 0o600)
	writeEngineFile(t, root, "package.json", "{\"type\":\"module\"}\n", 0o600)
	writeEngineFile(t, root, "settings.json", "{\"value\":1}\n", 0o600)
	if err := os.Symlink("../images", filepath.Join(root, "public/images")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	config := policy.Config{Quality: policy.EffectiveQuality(policy.Quality{}), Scope: policy.Scope{Generated: []string{"public/images"}}, Modules: []policy.Module{{Name: "application", Paths: []string{"images/**", "public/**", "settings.json"}}}, ModuleByName: map[string]int{"application": 0}}
	repo, err := repository.Open(root, enginePolicyRoot(t), config)
	if err != nil {
		t.Fatal(err)
	}
	engine := Engine{Repository: repo, Runner: runner.OSRunner{}}
	report := engine.Format(t.Context(), repository.Selection{Files: []string{"public/images", "settings.json"}})
	if HasFindings(report) || report.Formatting == nil || report.Formatting.Protected != 1 || report.Formatting.Rewritten != 1 {
		t.Fatalf("format failed: findings=%+v formatting=%+v", report.Findings, report.Formatting)
	}
	data, err := os.ReadFile(filepath.Join(root, "images/picture.png"))
	if err != nil || string(data) != "original" {
		t.Fatalf("protected asset changed: %q %v", data, err)
	}
	if text, err := os.Readlink(filepath.Join(root, "public/images")); err != nil || text != "../images" {
		t.Fatalf("asset link changed: %q %v", text, err)
	}
}
