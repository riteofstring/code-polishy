package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
)

func TestRenderPackMigrationConfigChangesOnlyPackPolicy(t *testing.T) {
	selection := policy.PackSelection{Name: "shell", Version: "1.2.3", Digest: strings.Repeat("a", 64)}
	without := []byte("{\n  \"version\": 4,\n  \"project\": {\"kind\": \"content\"}\n}\n")
	inserted, err := renderPackMigrationConfig(without, []policy.PackSelection{selection})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(inserted), "\"project\": {\"kind\": \"content\"}") || !strings.Contains(string(inserted), "\"packs\": [") {
		t.Fatalf("inserted configuration = %s", inserted)
	}
	with := []byte("{\n\t\"version\": 4,\n\t\"packs\": [{\"name\":\"old\",\"version\":\"1.0.0\",\"digest\":\"" + strings.Repeat("b", 64) + "\"}],\n\t\"marker\": {\"exact\":true}\n}\n")
	replaced, err := renderPackMigrationConfig(with, []policy.PackSelection{selection})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(replaced), "\"name\":\"old\"") || !strings.Contains(string(replaced), "\t\"marker\": {\"exact\":true}") {
		t.Fatalf("replaced configuration = %s", replaced)
	}
	var document struct {
		Packs []policy.PackSelection `json:"packs"`
	}
	if err := json.Unmarshal(replaced, &document); err != nil || !slices.Equal(document.Packs, []policy.PackSelection{selection}) {
		t.Fatalf("packs = %+v: %v", document.Packs, err)
	}
}

func TestPackMigrationPlansAppliesAndRollsBackWithoutChangingEngineLock(t *testing.T) {
	if pack.CurrentPlatform() == "windows-amd64" {
		t.Skip("fixture adapter uses a POSIX executable")
	}
	policyRoot := enginePolicyRoot(t)
	root := contentRepository(t, nil)
	writeEngineFile(t, root, "content/query.fixture", "SELECT 1;\n", 0o600)
	lockData, err := os.ReadFile(filepath.Join(policyRoot, release.LockFilename))
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, release.LockFilename, string(lockData), 0o600)
	before, err := os.ReadFile(filepath.Join(root, policy.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}
	catalogPath, catalogDigest, identity := writePackMigrationCatalog(t, policyRoot)
	dataRoot := filepath.Join(t.TempDir(), "packs")
	t.Cleanup(func() { _, _ = pack.Remove(dataRoot, identity.Name, identity.Version, identity.Digest) })
	planned, err := PlanPackMigration(t.Context(), PackMigrationPlanOptions{
		RepositoryRoot: root, PolicyRoot: policyRoot, CatalogPath: catalogPath,
		CatalogSHA256: catalogDigest, References: []string{identity.Name + "@" + identity.Version}, DataRoot: dataRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !planned.Plan.Ready || planned.Installed != 1 || planned.AlreadyPresent != 0 || len(planned.Plan.Gaps) != 0 {
		t.Fatalf("plan = %+v", planned)
	}
	unchanged, err := os.ReadFile(filepath.Join(root, policy.ConfigFilename))
	if err != nil || !slices.Equal(unchanged, before) {
		t.Fatalf("planning changed configuration: %v", err)
	}
	action := PackMigrationActionOptions{RepositoryRoot: root, PolicyRoot: policyRoot, PlanPath: planned.PlanPath, DataRoot: dataRoot}
	applied, err := ApplyPackMigration(t.Context(), action)
	if err != nil || !applied.Changed {
		t.Fatalf("apply = %+v: %v", applied, err)
	}
	configured, err := policy.Load(root, "")
	if err != nil || !slices.Equal(configured.Packs, planned.Plan.AfterPacks) {
		t.Fatalf("selected packs = %+v: %v", configured.Packs, err)
	}
	after, err := os.ReadFile(filepath.Join(root, policy.ConfigFilename))
	if err != nil || packMigrationBytesDigest(after) != planned.Plan.AfterConfigSHA256 {
		t.Fatalf("applied configuration = %v", err)
	}
	if repeated, err := ApplyPackMigration(t.Context(), action); err != nil || repeated.Changed {
		t.Fatalf("repeated apply = %+v: %v", repeated, err)
	}
	rolledBack, err := RollbackPackMigration(action)
	if err != nil || !rolledBack.Changed {
		t.Fatalf("rollback = %+v: %v", rolledBack, err)
	}
	restored, err := os.ReadFile(filepath.Join(root, policy.ConfigFilename))
	if err != nil || !slices.Equal(restored, before) {
		t.Fatalf("rollback did not restore exact configuration: %v", err)
	}
	currentLock, err := os.ReadFile(filepath.Join(root, release.LockFilename))
	if err != nil || !slices.Equal(currentLock, lockData) {
		t.Fatalf("engine lock changed: %v", err)
	}
	if _, err := ApplyPackMigration(t.Context(), action); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, policy.ConfigFilename), append(after, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RollbackPackMigration(action); err == nil || !strings.Contains(err.Error(), "unrelated changes") {
		t.Fatalf("rollback overwrote changed configuration: %v", err)
	}
}

func writePackMigrationCatalog(t *testing.T, policyRoot string) (string, string, pack.Identity) {
	t.Helper()
	versionData, err := os.ReadFile(filepath.Join(policyRoot, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	engineVersion := strings.TrimSpace(string(versionData))
	root := t.TempDir()
	source := filepath.Join(root, "artifact")
	pass := []byte("SELECT 1;\n")
	fail := []byte("BROKEN\n")
	passDigest := sha256.Sum256(pass)
	failDigest := sha256.Sum256(fail)
	capabilities := slices.Clone(packMigrationCapabilities)
	fixtures := []pack.Fixture{}
	var adapter strings.Builder
	adapter.WriteString("#!/bin/sh\nIFS= read -r request\ncase \"$request\" in\n")
	for _, capability := range capabilities {
		facts := ""
		switch capability {
		case "lint":
			facts = `,"facts":{"comments":[]}`
		case "complexity":
			facts = `,"facts":{"functions":[]}`
		case "architecture":
			facts = `,"facts":{"imports":[]}`
		}
		passResponse := fmt.Sprintf(`{"protocolVersion":4,"status":"pass","evidence":["parsed selected SQL"],"coverage":{"analyzed":["src/main.fixture"],"unsupported":[]}%s,"inputs":[{"path":"src/main.fixture","sha256":"%s"}]}`, facts, hex.EncodeToString(passDigest[:]))
		failResponse := fmt.Sprintf(`{"protocolVersion":4,"status":"findings","evidence":["parsed selected SQL"],"coverage":{"analyzed":["src/main.fixture"],"unsupported":[]}%s,"inputs":[{"path":"src/main.fixture","sha256":"%s"}],"findings":[{"capability":"%s","path":"src/main.fixture","subject":"SELECT","message":"invalid query","rule":"invalid-source","line":1,"column":1}]}`, facts, hex.EncodeToString(failDigest[:]), capability)
		fmt.Fprintf(&adapter, "  *'\"capability\":\"%s\"'*) case \"$request\" in *fixtures/fail*) printf '%%s\\n' '%s' ;; *) printf '%%s\\n' '%s' ;; esac ;;\n", capability, failResponse, passResponse)
		fixtures = append(fixtures,
			pack.Fixture{Name: capability + "-pass", Command: "adapter", Capability: capability, Project: "fixtures/pass", Files: []string{"src/main.fixture"}, ExpectedStatus: "pass"},
			pack.Fixture{Name: capability + "-fail", Command: "adapter", Capability: capability, Project: "fixtures/fail", Files: []string{"src/main.fixture"}, ExpectedStatus: "findings", ExpectedRules: []string{"invalid-source"}},
		)
	}
	adapter.WriteString("esac\n")
	manifest := pack.Manifest{
		Schema: "https://raw.githubusercontent.com/riteofstring/code-polishy/main/schema/code-polishy-pack.schema.json", ManifestVersion: pack.ManifestVersion, Name: "migration-sql", Version: "1.0.0",
		EngineVersion: engineVersion, ProtocolVersion: pack.ProtocolVersion, Platforms: []string{pack.CurrentPlatform()},
		Languages: []pack.Language{{ID: "sql", SourcePatterns: []string{"**/*.fixture"}, DiscoveryMode: "file-scoped"}},
		Commands:  []pack.Command{{Name: "adapter", Argv: []string{"bin/adapter"}, Languages: []string{"sql"}, Capabilities: capabilities, Profiles: []string{"check", "gate", "format"}, TimeoutSeconds: 30, Execution: pack.CommandExecution{Type: "self-contained", Network: "none"}}},
		Fixtures:  fixtures,
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, source, pack.ManifestFilename, string(append(manifestData, '\n')), 0o600)
	writeEngineFile(t, source, "README.md", "# Migration SQL fixture\n", 0o600)
	writeEngineFile(t, source, "bin/adapter", adapter.String(), 0o700)
	writeEngineFile(t, source, "fixtures/pass/src/main.fixture", string(pass), 0o600)
	writeEngineFile(t, source, "fixtures/fail/src/main.fixture", string(fail), 0o600)
	bootstrap := filepath.Join(t.TempDir(), "bootstrap")
	identity, _, err := pack.Install(source, bootstrap, engineVersion)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pack.Remove(bootstrap, identity.Name, identity.Version, identity.Digest); err != nil {
		t.Fatal(err)
	}
	artifactBytes := int64(0)
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		info, err := entry.Info()
		if err == nil {
			artifactBytes += info.Size()
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	attestation := []byte("migration fixture provenance\n")
	attestationDigest := sha256.Sum256(attestation)
	writeEngineFile(t, root, "attestation.json", string(attestation), 0o600)
	entry := pack.CatalogEntry{
		Name: identity.Name, Version: identity.Version, Digest: identity.Digest, ArtifactURL: "file:artifact", ArtifactBytes: artifactBytes,
		ManifestVersion: pack.ManifestVersion, ProtocolVersion: pack.ProtocolVersion, EngineVersion: engineVersion,
		Platforms: []string{pack.CurrentPlatform()}, DiscoveryModes: []string{"file-scoped"}, ExecutionTypes: []string{"self-contained"},
		Capabilities: capabilities, Tools: []string{"migration-adapter@1.0.0"}, Dependencies: []string{}, Licenses: []string{"MIT"},
		Provenance: pack.CatalogProvenance{SourceRevision: strings.Repeat("a", 40), Builder: "code-polishy", AttestationURL: "file:attestation.json", AttestationSHA256: hex.EncodeToString(attestationDigest[:])},
	}
	catalogData, err := json.MarshalIndent(pack.Catalog{Schema: pack.CatalogSchema, Protocol: pack.CatalogProtocol, Entries: []pack.CatalogEntry{entry}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	catalogData = append(catalogData, '\n')
	catalogPath := filepath.Join(root, "catalog.json")
	if err := os.WriteFile(catalogPath, catalogData, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(catalogData)
	return catalogPath, hex.EncodeToString(digest[:]), identity
}
