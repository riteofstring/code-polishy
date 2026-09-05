package release

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLauncherManifestReadsHistoricalSchemasWithoutReinterpretingThem(t *testing.T) {
	t.Parallel()
	for _, version := range []int{2, 3, 4, 5} {
		data, expected := historicalManifest(t, version)
		manifest, err := parseLauncherManifest(data, ManifestFilename)
		if err != nil {
			t.Fatalf("version %d: %v", version, err)
		}
		if manifest.ManifestVersion != version || manifest.Identity() != expected.ReleaseDigest {
			t.Fatalf("version %d manifest = %+v", version, manifest)
		}
		if _, err := parseManifest(data, ManifestFilename); err == nil {
			t.Fatalf("the current release parser accepted historical version %d", version)
		}
	}
}

func TestLauncherManifestKeepsHistoricalToolSchemasExact(t *testing.T) {
	t.Parallel()
	data, _ := historicalManifest(t, 3)
	withLaterField := strings.Replace(
		string(data), `"ty":"0.0.65"`, `"python":"3.12.13+20260728","ty":"0.0.65"`, 1,
	)
	if _, err := parseLauncherManifest([]byte(withLaterField), ManifestFilename); err == nil {
		t.Fatal("the version 3 manifest accepted a version 4 tool field")
	}
}

func TestLauncherManifestRejectsUnsupportedSchemas(t *testing.T) {
	t.Parallel()
	_, manifest := exampleRelease(t)
	for _, version := range []int{oldestLauncherManifestVersion - 1, ManifestVersion + 1} {
		manifest.ManifestVersion = version
		if _, err := parseLauncherManifest(render(t, manifest), ManifestFilename); err == nil {
			t.Fatalf("manifest version %d was accepted", version)
		}
	}
}

func historicalManifest(t *testing.T, version int) ([]byte, Manifest) {
	t.Helper()
	_, manifest := exampleRelease(t)
	manifest.ManifestVersion = version
	manifest.CapabilityCatalogSHA256 = ""
	manifest.Tools.Packaging = ""
	manifest.Tools.Python = ""
	manifest.Tools.Vulture = ""
	if version == 2 {
		manifest.Tools.Ty = ""
	} else if version >= 4 {
		manifest.Tools.Python = "3.12.13+20260728"
		manifest.Tools.Vulture = "2.16"
	}
	if version == 5 {
		manifest.Tools.Packaging = "26.3"
	}
	manifest.ReleaseDigest = manifest.Identity()
	tools := historicalManifestTools(t, manifest)
	document := manifestDocument{
		ManifestVersion: manifest.ManifestVersion, CodePolishyVersion: manifest.CodePolishyVersion,
		SourceRevision: manifest.SourceRevision, Host: manifest.Host, Features: manifest.Features, Tools: tools,
		ReleaseDigest: manifest.ReleaseDigest, ContentDigest: manifest.ContentDigest,
		EntryCount: manifest.EntryCount, Entries: manifest.Entries,
	}
	return render(t, document), manifest
}

func historicalManifestTools(t *testing.T, manifest Manifest) json.RawMessage {
	t.Helper()
	if manifest.ManifestVersion == 5 {
		return render(t, manifest.Tools)
	}
	base := manifestToolsV2{
		Go: manifest.Tools.Go, Govulncheck: manifest.Tools.Govulncheck, Node: manifest.Tools.Node,
		OSVScanner: manifest.Tools.OSVScanner, PNPM: manifest.Tools.PNPM, Ruff: manifest.Tools.Ruff,
		Shellcheck: manifest.Tools.Shellcheck, Staticcheck: manifest.Tools.Staticcheck,
	}
	if manifest.ManifestVersion == 2 {
		return render(t, base)
	}
	if manifest.ManifestVersion == 3 {
		return render(t, manifestToolsV3{
			Go: base.Go, Govulncheck: base.Govulncheck, Node: base.Node, OSVScanner: base.OSVScanner,
			PNPM: base.PNPM, Ruff: base.Ruff, Shellcheck: base.Shellcheck, Staticcheck: base.Staticcheck,
			Ty: manifest.Tools.Ty,
		})
	}
	return render(t, manifestToolsV4{
		Go: base.Go, Govulncheck: base.Govulncheck, Node: base.Node, OSVScanner: base.OSVScanner,
		PNPM: base.PNPM, Ruff: base.Ruff, Shellcheck: base.Shellcheck, Staticcheck: base.Staticcheck,
		Ty: manifest.Tools.Ty, Python: manifest.Tools.Python, Vulture: manifest.Tools.Vulture,
	})
}
