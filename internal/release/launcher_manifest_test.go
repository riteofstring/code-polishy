package release

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLauncherManifestReadsHistoricalSchemasWithoutReinterpretingThem(t *testing.T) {
	t.Parallel()
	for _, version := range []int{2, 3, 4, 5, 6, 7} {
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

func TestLauncherManifestRejectsDeadcodeInHistoricalSchemas(t *testing.T) {
	t.Parallel()
	data, _ := historicalManifest(t, 7)
	changed := strings.Replace(string(data), `"tools":{`, `"tools":{"deadcode":"0.50.0",`, 1)
	if _, err := parseLauncherManifest([]byte(changed), ManifestFilename); err == nil {
		t.Fatal("version 7 accepted the later deadcode tool field")
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

func TestLauncherManifestRejectsRustToolsInHistoricalSchemas(t *testing.T) {
	t.Parallel()
	for _, version := range []int{5, 6} {
		data, _ := historicalManifest(t, version)
		for _, field := range []string{`"rust":"1.97.1",`, `"cargo-nextest":"0.9.138",`} {
			changed := strings.Replace(string(data), `"tools":{`, `"tools":{`+field, 1)
			if _, err := parseLauncherManifest([]byte(changed), ManifestFilename); err == nil {
				t.Fatalf("version %d accepted the later tool field %s", version, field)
			}
		}
	}
}

func historicalManifest(t *testing.T, version int) ([]byte, Manifest) {
	t.Helper()
	_, manifest := exampleRelease(t)
	manifest.ManifestVersion = version
	if version < 6 {
		manifest.CapabilityCatalogSHA256 = ""
	}
	manifest.Tools.Deadcode = ""
	if version < 7 {
		manifest.Tools.Rust = ""
		manifest.Tools.Nextest = ""
	}
	manifest.Tools.Packaging = ""
	manifest.Tools.Python = ""
	manifest.Tools.Vulture = ""
	if version == 2 {
		manifest.Tools.Ty = ""
	} else if version >= 4 {
		manifest.Tools.Python = "3.12.13+20260728"
		manifest.Tools.Vulture = "2.16"
	}
	if version >= 5 {
		manifest.Tools.Packaging = "26.3"
	}
	manifest.ReleaseDigest = manifest.Identity()
	return renderHistoricalManifest(t, manifest), manifest
}

func renderHistoricalManifest(t *testing.T, manifest Manifest) []byte {
	t.Helper()
	tools := historicalManifestTools(t, manifest)
	document := manifestDocument{
		ManifestVersion: manifest.ManifestVersion, CodePolishyVersion: manifest.CodePolishyVersion,
		SourceRevision: manifest.SourceRevision, Host: manifest.Host, Features: manifest.Features, Tools: tools,
		ReleaseDigest: manifest.ReleaseDigest, ContentDigest: manifest.ContentDigest,
		CapabilityCatalogSHA256: manifest.CapabilityCatalogSHA256,
		EntryCount:              manifest.EntryCount, Entries: manifest.Entries,
	}
	return render(t, document)
}

func historicalManifestTools(t *testing.T, manifest Manifest) json.RawMessage {
	t.Helper()
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
	versionFour := manifestToolsV4{
		Go: base.Go, Govulncheck: base.Govulncheck, Node: base.Node, OSVScanner: base.OSVScanner,
		PNPM: base.PNPM, Ruff: base.Ruff, Shellcheck: base.Shellcheck, Staticcheck: base.Staticcheck,
		Ty: manifest.Tools.Ty, Python: manifest.Tools.Python, Vulture: manifest.Tools.Vulture,
	}
	versionFive := manifestToolsV5{manifestToolsV4: versionFour, Packaging: manifest.Tools.Packaging}
	if manifest.ManifestVersion >= 7 {
		return render(t, manifestToolsV7{manifestToolsV5: versionFive, Rust: manifest.Tools.Rust, Nextest: manifest.Tools.Nextest})
	}
	if manifest.ManifestVersion >= 5 {
		return render(t, versionFive)
	}
	return render(t, versionFour)
}
