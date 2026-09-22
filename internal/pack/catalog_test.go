package pack

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestAuthenticatedCatalogInstallsOnlyItsExactCompatibleTree(t *testing.T) {
	catalogRoot, source := containedCatalogPack(t)
	path, digest, entry := writeTestCatalog(t, catalogRoot, source, "0.25.0", nil)
	loaded, err := LoadCatalog(path, digest)
	if err != nil {
		t.Fatal(err)
	}
	dataRoot := filepath.Join(t.TempDir(), "packs")
	t.Cleanup(func() { makeWritable(dataRoot) })
	identity, installed, err := InstallOfficial(loaded, entry.Name, entry.Version, "0.25.0", dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if identity != (Identity{Name: entry.Name, Version: entry.Version, Digest: entry.Digest}) || installed != InstalledRoot(dataRoot, entry.Name, entry.Version, entry.Digest) {
		t.Fatalf("installed identity = %+v at %s", identity, installed)
	}
	second, secondRoot, err := InstallOfficial(loaded, entry.Name, entry.Version, "0.25.0", dataRoot)
	if err != nil || second != identity || secondRoot != installed {
		t.Fatalf("idempotent official install = %+v at %s: %v", second, secondRoot, err)
	}
	if _, _, err := InstallOfficial(loaded, entry.Name, entry.Version, "0.26.0", dataRoot); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("incompatible engine installed: %v", err)
	}
}

func TestPublishedLocalCatalogAuthenticatesTheShippedProofPack(t *testing.T) {
	root := filepath.Join("..", "..")
	path := filepath.Join(root, "tools", "fixtures", "code-polishy-pack-catalog-v1.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(path, inputDigest(data))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := catalog.Entry("sqlite-syntax-proof", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := readSourceTree(filepath.Join(root, "tools", "fixtures", "language-pack"), true)
	if err != nil {
		t.Fatal(err)
	}
	artifactBytes := int64(0)
	for _, file := range tree.Files {
		artifactBytes += int64(len(file.Data))
	}
	if entry.Digest != tree.Receipt.Digest || entry.ArtifactBytes != artifactBytes {
		t.Fatalf("published pack identity digest=%s bytes=%d; catalog digest=%s bytes=%d", tree.Receipt.Digest, artifactBytes, entry.Digest, entry.ArtifactBytes)
	}
	if !slices.Contains(entry.Platforms, CurrentPlatform()) {
		return
	}
	versionData, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	dataRoot := filepath.Join(t.TempDir(), "packs")
	t.Cleanup(func() { makeWritable(dataRoot) })
	identity, _, err := InstallOfficial(catalog, entry.Name, entry.Version, strings.TrimSpace(string(versionData)), dataRoot)
	if err != nil || identity.Digest != entry.Digest {
		t.Fatalf("shipped catalog install = %+v: %v", identity, err)
	}
}

func TestCatalogAuthenticationAndMetadataFailBeforeInstallation(t *testing.T) {
	catalogRoot, source := containedCatalogPack(t)
	path, _, _ := writeTestCatalog(t, catalogRoot, source, "0.25.0", nil)
	if _, err := LoadCatalog(path, strings.Repeat("c", 64)); err == nil || !strings.Contains(err.Error(), "required SHA-256") {
		t.Fatalf("unauthenticated catalog loaded: %v", err)
	}
	path, digest, entry := writeTestCatalog(t, catalogRoot, source, "0.25.0", func(candidate *CatalogEntry) {
		candidate.Capabilities = []string{"format"}
	})
	loaded, err := LoadCatalog(path, digest)
	if err != nil {
		t.Fatal(err)
	}
	dataRoot := filepath.Join(t.TempDir(), "packs")
	if _, _, err := InstallOfficial(loaded, entry.Name, entry.Version, "0.25.0", dataRoot); err == nil || !strings.Contains(err.Error(), "contract metadata") {
		t.Fatalf("mismatched catalog metadata installed: %v", err)
	}
	if _, err := os.Stat(dataRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed catalog install wrote store state: %v", err)
	}
	path, digest, entry = writeTestCatalog(t, catalogRoot, source, "0.25.0", func(candidate *CatalogEntry) {
		candidate.Provenance.AttestationSHA256 = strings.Repeat("d", 64)
	})
	loaded, err = LoadCatalog(path, digest)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallOfficial(loaded, entry.Name, entry.Version, "0.25.0", dataRoot); err == nil || !strings.Contains(err.Error(), "attestation") {
		t.Fatalf("unverified provenance installed: %v", err)
	}
}

func TestCatalogArtifactsMustRemainContainedRegularDirectories(t *testing.T) {
	catalogRoot, source := containedCatalogPack(t)
	path, digest, _ := writeTestCatalog(t, catalogRoot, source, "0.25.0", func(candidate *CatalogEntry) {
		candidate.ArtifactURL = "file:../outside"
	})
	if _, err := LoadCatalog(path, digest); err == nil || !strings.Contains(err.Error(), "contained file: URL") {
		t.Fatalf("escaping artifact passed: %v", err)
	}
	if err := os.Symlink("artifacts", filepath.Join(catalogRoot, "linked")); err != nil {
		t.Fatal(err)
	}
	path, digest, entry := writeTestCatalog(t, catalogRoot, source, "0.25.0", func(candidate *CatalogEntry) {
		candidate.ArtifactURL = "file:linked/fixture-language"
	})
	loaded, err := LoadCatalog(path, digest)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallOfficial(loaded, entry.Name, entry.Version, "0.25.0", t.TempDir()); err == nil || !strings.Contains(err.Error(), "linked component") {
		t.Fatalf("linked artifact installed: %v", err)
	}
}

func TestLifecycleReportsExactStatesAndRequiresUnambiguousRemoval(t *testing.T) {
	dataRoot := filepath.Join(t.TempDir(), "packs")
	t.Cleanup(func() { makeWritable(dataRoot) })
	source := writePackSource(t)
	healthy, healthyRoot, err := Install(source, dataRoot, testEngineVersion)
	if err != nil {
		t.Fatal(err)
	}
	secondSource := writePackSource(t)
	writeTestFile(t, secondSource, "README.md", "# Different fixture pack\n", 0o644)
	corrupt, corruptRoot, err := Install(secondSource, dataRoot, testEngineVersion)
	if err != nil {
		t.Fatal(err)
	}
	makeWritable(corruptRoot)
	if err := os.WriteFile(filepath.Join(corruptRoot, "README.md"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := legacyPackTree(t, source)
	legacyRoot := InstalledRoot(dataRoot, legacy.Receipt.Name, legacy.Receipt.Version, legacy.Receipt.Digest)
	if err := publishTree(legacy, legacyRoot); err != nil {
		t.Fatal(err)
	}
	missingDigest := strings.Repeat("f", 64)
	selected := []policy.PackSelection{
		{Name: healthy.Name, Version: healthy.Version, Digest: healthy.Digest},
		{Name: "missing-pack", Version: "2.0.0", Digest: missingDigest},
	}
	statuses, err := List(dataRoot, selected, testEngineVersion)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		healthy.Digest:        "selected",
		corrupt.Digest:        "corrupt",
		legacy.Receipt.Digest: "incompatible",
		missingDigest:         "missing",
	}
	if len(statuses) != len(want) {
		t.Fatalf("statuses = %+v", statuses)
	}
	for _, status := range statuses {
		if want[status.Digest] != status.State {
			t.Fatalf("status = %+v, want %q", status, want[status.Digest])
		}
	}
	if _, err := Remove(dataRoot, healthy.Name, healthy.Version, ""); err == nil || !strings.Contains(err.Error(), "multiple installed digests") {
		t.Fatalf("ambiguous removal succeeded: %v", err)
	}
	removed, err := Remove(dataRoot, healthy.Name, healthy.Version, healthy.Digest)
	if err != nil || removed != healthy {
		t.Fatalf("exact removal = %+v: %v", removed, err)
	}
	if _, err := os.Stat(healthyRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed pack remains: %v", err)
	}
}

func containedCatalogPack(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := writePackSource(t)
	contained := filepath.Join(root, "artifacts", "fixture-language")
	if err := os.Rename(source, contained); err != nil {
		t.Fatal(err)
	}
	return root, contained
}

func writeTestCatalog(t *testing.T, root, source, engineVersion string, mutate func(*CatalogEntry)) (string, string, CatalogEntry) {
	t.Helper()
	tree, err := readSourceTree(source, true)
	if err != nil {
		t.Fatal(err)
	}
	bytes := int64(0)
	for _, file := range tree.Files {
		bytes += int64(len(file.Data))
	}
	attestation := []byte("fixture provenance\n")
	attestationPath := filepath.Join(root, "attestation.json")
	if err := os.WriteFile(attestationPath, attestation, 0o644); err != nil {
		t.Fatal(err)
	}
	entry := CatalogEntry{
		Name: tree.Receipt.Name, Version: tree.Receipt.Version, Digest: tree.Receipt.Digest,
		ArtifactURL: "file:artifacts/fixture-language", ArtifactBytes: bytes,
		ManifestVersion: ManifestVersion, ProtocolVersion: ProtocolVersion,
		EngineVersion: engineVersion, Platforms: slices.Clone(tree.Manifest.Platforms),
		DiscoveryModes: manifestDiscoveryModes(tree.Manifest), ExecutionTypes: manifestExecutionTypes(tree.Manifest),
		Capabilities: manifestCapabilities(tree.Manifest), Tools: []string{"fixture-adapter@1.0.0"},
		Dependencies: []string{}, Licenses: []string{"MIT"},
		Provenance: CatalogProvenance{SourceRevision: strings.Repeat("a", 40), Builder: "code-polishy", AttestationURL: "file:attestation.json", AttestationSHA256: inputDigest(attestation)},
	}
	if mutate != nil {
		mutate(&entry)
	}
	data, err := json.MarshalIndent(Catalog{Schema: CatalogSchema, Protocol: CatalogProtocol, Entries: []CatalogEntry{entry}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join(root, "catalog.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path, inputDigest(data), entry
}

func legacyPackTree(t *testing.T, source string) sourceTree {
	t.Helper()
	tree, err := readSourceTree(source, true)
	if err != nil {
		t.Fatal(err)
	}
	for index := range tree.Files {
		if tree.Files[index].Path == ManifestFilename {
			value := map[string]any{}
			if err := json.Unmarshal(tree.Files[index].Data, &value); err != nil {
				t.Fatal(err)
			}
			value["manifestVersion"] = float64(2)
			value["protocolVersion"] = float64(3)
			tree.Files[index].Data, err = json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	tree.Receipt = buildReceipt(tree.Manifest, tree.Files, map[string]bool{"bin/adapter": true})
	return tree
}
