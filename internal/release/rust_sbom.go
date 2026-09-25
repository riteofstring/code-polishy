package release

import (
	"fmt"
	"os"
	"strings"

	cdx "github.com/CycloneDX/cyclonedx-go"
)

var rustReleaseTargets = map[string]string{
	"darwin-arm64": "aarch64-apple-darwin",
	"darwin-x64":   "x86_64-apple-darwin",
	"linux-arm64":  "aarch64-unknown-linux-gnu",
	"linux-x64":    "x86_64-unknown-linux-gnu",
	"windows-x64":  "x86_64-pc-windows-msvc",
}

var rustReleaseComponents = []string{"cargo", "clippy", "rust-std", "rustc", "rustfmt"}

func rustReleaseSBOMEvidence(root string, manifest Manifest, tools map[string]cdx.Component) ([]cdx.Composition, error) {
	sealed, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer sealed.Close()
	for _, name := range []string{"rust", "cargo-nextest"} {
		component := tools[name]
		references, err := rustReleaseDistributions(sealed, manifest, name)
		if err != nil {
			return nil, err
		}
		properties, err := rustReleaseNotices(sealed, manifest, name)
		if err != nil {
			return nil, err
		}
		if name == "cargo-nextest" {
			source, err := nextestReleaseSource(sealed, manifest.Tools.Nextest)
			if err != nil {
				return nil, err
			}
			references = append(references, cdx.ExternalReference{Type: cdx.ERTypeLicense, URL: source})
		}
		component.ExternalReferences = &references
		component.Properties = &properties
		tools[name] = component
	}
	refs := []cdx.BOMReference{cdx.BOMReference(tools["cargo-nextest"].BOMRef), cdx.BOMReference(tools["rust"].BOMRef)}
	return []cdx.Composition{{Aggregate: cdx.CompositionAggregateIncomplete, Assemblies: &refs, Dependencies: &refs}}, nil
}

func rustReleaseArchives(manifest Manifest, name, host string) ([]string, error) {
	target := rustReleaseTargets[host]
	if target == "" {
		return nil, fmt.Errorf("rust release inventory has no target for %q", host)
	}
	if name == "rust" {
		archives := make([]string, 0, len(rustReleaseComponents))
		for _, component := range rustReleaseComponents {
			archives = append(archives, component+"-"+manifest.Tools.Rust+"-"+target+".tar.xz")
		}
		return archives, nil
	}
	extension := ".tar.gz"
	if strings.HasPrefix(host, "darwin-") {
		target = "universal-apple-darwin"
	} else if host == "windows-x64" {
		extension = ".zip"
	}
	return []string{"cargo-nextest-" + manifest.Tools.Nextest + "-" + target + extension}, nil
}

func rustReleaseDistributions(root *os.Root, manifest Manifest, name string) ([]cdx.ExternalReference, error) {
	expected := map[string]string{}
	for _, host := range supportedReleaseHosts {
		archives, err := rustReleaseArchives(manifest, name, host)
		if err != nil {
			return nil, err
		}
		for _, archive := range archives {
			expected[archive] = ""
		}
	}
	relative := "tools/" + name + "_checksums.txt"
	if !manifestRecordsCapabilityFile(manifest, relative, "") {
		return nil, fmt.Errorf("%s release inventory is absent from the manifest", name)
	}
	data, err := readCapabilityFile(root, relative, 32<<10)
	if err != nil {
		return nil, err
	}
	if err := readRustArchiveChecksums(data, expected); err != nil {
		return nil, fmt.Errorf("%s release inventory: %w", name, err)
	}
	archives, err := rustReleaseArchives(manifest, name, manifest.Host)
	if err != nil {
		return nil, err
	}
	prefix := "https://static.rust-lang.org/dist/"
	if name == "cargo-nextest" {
		prefix = "https://github.com/nextest-rs/nextest/releases/download/cargo-nextest-" + manifest.Tools.Nextest + "/"
	}
	references := make([]cdx.ExternalReference, 0, len(archives))
	for _, archive := range archives {
		hashes := []cdx.Hash{{Algorithm: cdx.HashAlgoSHA256, Value: expected[archive]}}
		references = append(references, cdx.ExternalReference{Type: cdx.ERTypeDistribution, URL: prefix + archive, Hashes: &hashes})
	}
	return references, nil
}

func readRustArchiveChecksums(data []byte, expected map[string]string) error {
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !digestPattern.MatchString(fields[1]) {
			return fmt.Errorf("archive checksum row is malformed")
		}
		digest, present := expected[fields[0]]
		if !present || digest != "" {
			return fmt.Errorf("unexpected or duplicate archive %q", fields[0])
		}
		expected[fields[0]] = fields[1]
	}
	for archive, digest := range expected {
		if digest == "" {
			return fmt.Errorf("missing archive %q", archive)
		}
	}
	return nil
}

func rustReleaseNoticePaths(host, name string) []string {
	if name == "cargo-nextest" {
		return []string{"tools/cargo-nextest-license.txt"}
	}
	prefix := ".tools/rust/" + host + "/share/"
	paths := []string{prefix + "doc/rust/COPYRIGHT.html", prefix + "doc/rust/COPYRIGHT-library.html", prefix + "doc/cargo/LICENSE-THIRD-PARTY"}
	for _, component := range rustReleaseComponents {
		for _, license := range []string{"LICENSE-APACHE", "LICENSE-MIT"} {
			paths = append(paths, prefix+"licenses/"+component+"/"+license)
		}
	}
	return paths
}

func rustReleaseNotices(root *os.Root, manifest Manifest, name string) ([]cdx.Property, error) {
	var properties []cdx.Property
	for _, relative := range rustReleaseNoticePaths(manifest.Host, name) {
		if !manifestRecordsCapabilityFile(manifest, relative, "") {
			return nil, fmt.Errorf("%s release notice is absent from the manifest: %s", name, relative)
		}
		info, err := capabilityFileInfo(root, relative)
		if err != nil || info.Size() == 0 {
			return nil, fmt.Errorf("%s release notice must be a nonempty contained regular file: %s", name, relative)
		}
		properties = append(properties, cdx.Property{Name: "code-polishy:notice-path", Value: relative})
	}
	return properties, nil
}

func nextestReleaseSource(root *os.Root, version string) (string, error) {
	data, err := readCapabilityFile(root, "tools/cargo-nextest-license.txt", 32<<10)
	if err != nil {
		return "", err
	}
	header, body, found := strings.Cut(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n\n")
	lines := strings.Split(header, "\n")
	if !found || len(lines) != 2 || lines[0] != "cargo-nextest "+version || strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("cargo-nextest notice does not identify its exact release and source")
	}
	const prefix = "Source: https://github.com/nextest-rs/nextest/blob/"
	revision, found := strings.CutPrefix(lines[1], prefix)
	if !found || !strings.HasSuffix(revision, "/LICENSE-MIT") || !revisionPattern.MatchString(strings.TrimSuffix(revision, "/LICENSE-MIT")) {
		return "", fmt.Errorf("cargo-nextest notice must identify an immutable upstream license")
	}
	return strings.TrimPrefix(lines[1], "Source: "), nil
}
