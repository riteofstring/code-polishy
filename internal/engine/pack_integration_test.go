package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestOpenResolvesExactCommunityPackAndDoctorUsesItsCoverage(t *testing.T) {
	dataHome := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", dataHome)
	} else {
		t.Setenv("XDG_DATA_HOME", dataHome)
	}
	source := packIntegrationSource(t)
	dataRoot, err := pack.UserDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { makePackTreeWritable(dataRoot) })
	identity, _, err := pack.Install(source, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeEngineFile(t, root, "src/main.fixture", "good\n", 0o600)
	config := fmt.Sprintf(`{"version":3,"project":{"kind":"content"},"packs":[{"name":%q,"version":%q,"digest":%q}],"modules":[{"name":"app","paths":["src/**"]}],"tests":{"suites":[{"name":"app-test","kind":"unit","scope":"module","modules":["app"],"argv":["go","test","./..."]},{"name":"full","kind":"contract","scope":"repository","argv":["go","test","./..."]}]}}`, identity.Name, identity.Version, identity.Digest)
	writeEngineFile(t, root, policy.ConfigFilename, config, 0o600)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	report, err := policyEngine.Doctor(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool { return finding.Check == "policy.packUnavailable" }) {
		t.Fatalf("installed pack was unavailable: %+v", report.Findings)
	}
	if !slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool {
		return finding.Check == "policy.checkCoverage" && strings.Contains(finding.Subject, "format")
	}) {
		t.Fatalf("doctor did not apply generic language coverage: %+v", report.Findings)
	}
}

func packIntegrationSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	manifest := pack.Manifest{
		ManifestVersion: pack.ManifestVersion, Name: "fixture-language", Version: "1.0.0", ProtocolVersion: pack.ProtocolVersion, Platforms: []string{pack.CurrentPlatform()},
		Languages: []pack.Language{{ID: "fixture", SourcePatterns: []string{"**/*.fixture"}}},
		Commands:  []pack.Command{{Name: "adapter", Argv: []string{"bin/adapter"}, Capabilities: []string{"lint"}, Profiles: []string{"check", "gate"}, TimeoutSeconds: 30}},
		Fixtures: []pack.Fixture{
			{Name: "lint-pass", Command: "adapter", Capability: "lint", Project: "fixtures/pass", Files: []string{"src/main.fixture"}, ExpectedStatus: "pass"},
			{Name: "lint-fail", Command: "adapter", Capability: "lint", Project: "fixtures/fail", Files: []string{"src/main.fixture"}, ExpectedStatus: "findings"},
		},
	}
	manifestData, _ := json.Marshal(manifest)
	writeEngineFile(t, root, pack.ManifestFilename, string(manifestData), 0o600)
	writeEngineFile(t, root, "README.md", "# Fixture\n", 0o600)
	writeEngineFile(t, root, "bin/adapter", "adapter\n", 0o700)
	writeEngineFile(t, root, "fixtures/pass/src/main.fixture", "good\n", 0o600)
	writeEngineFile(t, root, "fixtures/fail/src/main.fixture", "bad\n", 0o600)
	return root
}

func makePackTreeWritable(root string) {
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil {
			_ = os.Chmod(path, 0o755)
		}
		return nil
	})
}
