package release

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const oldestLauncherManifestVersion = 2

type manifestDocument struct {
	ManifestVersion    int             `json:"manifestVersion"`
	CodePolishyVersion string          `json:"codePolishyVersion"`
	SourceRevision     string          `json:"sourceRevision"`
	Host               string          `json:"host"`
	Features           []string        `json:"features"`
	Tools              json.RawMessage `json:"tools"`
	ReleaseDigest      string          `json:"releaseDigest"`
	ContentDigest      string          `json:"contentDigest"`
	EntryCount         int             `json:"entryCount"`
	Entries            []Entry         `json:"entries"`
}

type manifestToolsV2 struct {
	Go          string `json:"go"`
	Govulncheck string `json:"govulncheck"`
	Node        string `json:"node"`
	OSVScanner  string `json:"osv-scanner"`
	PNPM        string `json:"pnpm"`
	Ruff        string `json:"ruff"`
	Shellcheck  string `json:"shellcheck"`
	Staticcheck string `json:"staticcheck"`
}

type manifestToolsV3 struct {
	Go          string `json:"go"`
	Govulncheck string `json:"govulncheck"`
	Node        string `json:"node"`
	OSVScanner  string `json:"osv-scanner"`
	PNPM        string `json:"pnpm"`
	Ruff        string `json:"ruff"`
	Shellcheck  string `json:"shellcheck"`
	Staticcheck string `json:"staticcheck"`
	Ty          string `json:"ty"`
}

type manifestToolsV4 struct {
	Go          string `json:"go"`
	Govulncheck string `json:"govulncheck"`
	Node        string `json:"node"`
	OSVScanner  string `json:"osv-scanner"`
	PNPM        string `json:"pnpm"`
	Python      string `json:"python"`
	Ruff        string `json:"ruff"`
	Shellcheck  string `json:"shellcheck"`
	Staticcheck string `json:"staticcheck"`
	Ty          string `json:"ty"`
	Vulture     string `json:"vulture"`
}

func ReadLauncherManifest(releaseDir string) (Manifest, bool, error) {
	return readManifest(releaseDir, parseLauncherManifest)
}

func readManifest(releaseDir string, parse func([]byte, string) (Manifest, error)) (Manifest, bool, error) {
	source := filepath.Join(releaseDir, ManifestFilename)
	data, err := os.ReadFile(source)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, false, nil
	}
	if err != nil {
		return Manifest{}, false, fmt.Errorf("read %s: %w", source, err)
	}
	manifest, err := parse(data, source)
	return manifest, true, err
}

func parseLauncherManifest(data []byte, source string) (Manifest, error) {
	document := manifestDocument{}
	if err := decodeExactly(data, source, &document); err != nil {
		return Manifest{}, err
	}
	if document.ManifestVersion == ManifestVersion {
		return parseManifest(data, source)
	}
	if document.ManifestVersion < oldestLauncherManifestVersion || document.ManifestVersion >= ManifestVersion {
		return Manifest{}, fmt.Errorf(
			"%s records manifest version %d; this Code Polishy launcher reads versions %d through %d",
			source, document.ManifestVersion, oldestLauncherManifestVersion, ManifestVersion,
		)
	}
	tools, err := parseHistoricalManifestTools(document.Tools, source, document.ManifestVersion)
	if err != nil {
		return Manifest{}, err
	}
	manifest := document.manifest(tools)
	if err := validateManifestIdentity(manifest, source); err != nil {
		return Manifest{}, err
	}
	if err := validateEntries(manifest, source); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func parseHistoricalManifestTools(data []byte, source string, version int) (Tools, error) {
	if version == 2 {
		wire := manifestToolsV2{}
		if err := decodeExactly(data, source+" tools", &wire); err != nil {
			return Tools{}, err
		}
		return wire.tools(), nil
	}
	if version == 3 {
		wire := manifestToolsV3{}
		if err := decodeExactly(data, source+" tools", &wire); err != nil {
			return Tools{}, err
		}
		return wire.tools(), nil
	}
	wire := manifestToolsV4{}
	if err := decodeExactly(data, source+" tools", &wire); err != nil {
		return Tools{}, err
	}
	return wire.tools(), nil
}

func (document manifestDocument) manifest(tools Tools) Manifest {
	return Manifest{
		ManifestVersion: document.ManifestVersion, CodePolishyVersion: document.CodePolishyVersion,
		SourceRevision: document.SourceRevision, Host: document.Host, Features: document.Features, Tools: tools,
		ReleaseDigest: document.ReleaseDigest, ContentDigest: document.ContentDigest,
		EntryCount: document.EntryCount, Entries: document.Entries,
	}
}

func (wire manifestToolsV2) tools() Tools {
	return Tools{
		Go: wire.Go, Govulncheck: wire.Govulncheck, Node: wire.Node, OSVScanner: wire.OSVScanner,
		PNPM: wire.PNPM, Ruff: wire.Ruff, Shellcheck: wire.Shellcheck, Staticcheck: wire.Staticcheck,
	}
}

func (wire manifestToolsV3) tools() Tools {
	return Tools{
		Go: wire.Go, Govulncheck: wire.Govulncheck, Node: wire.Node, OSVScanner: wire.OSVScanner,
		PNPM: wire.PNPM, Ruff: wire.Ruff, Shellcheck: wire.Shellcheck, Staticcheck: wire.Staticcheck, Ty: wire.Ty,
	}
}

func (wire manifestToolsV4) tools() Tools {
	return Tools{
		Go: wire.Go, Govulncheck: wire.Govulncheck, Node: wire.Node, OSVScanner: wire.OSVScanner,
		PNPM: wire.PNPM, Python: wire.Python, Ruff: wire.Ruff, Shellcheck: wire.Shellcheck,
		Staticcheck: wire.Staticcheck, Ty: wire.Ty, Vulture: wire.Vulture,
	}
}
