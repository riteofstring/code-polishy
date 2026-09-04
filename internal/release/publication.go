package release

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const PublicationVersion = 1

type PublishedFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type PublishedManifest struct {
	PublishedFile
	ReleaseDigest string `json:"releaseDigest"`
	ContentDigest string `json:"contentDigest"`
}

type PublicationArtifact struct {
	DescriptorVersion  int               `json:"descriptorVersion"`
	CodePolishyVersion string            `json:"codePolishyVersion"`
	SourceRevision     string            `json:"sourceRevision"`
	Host               string            `json:"host"`
	Archive            PublishedFile     `json:"archive"`
	Manifest           PublishedManifest `json:"manifest"`
	SBOM               PublishedFile     `json:"sbom"`
	Provenance         PublishedFile     `json:"provenance"`
}

type PublicationIndex struct {
	IndexVersion       int                   `json:"indexVersion"`
	CodePolishyVersion string                `json:"codePolishyVersion"`
	SourceRevision     string                `json:"sourceRevision"`
	Artifacts          []PublicationArtifact `json:"artifacts"`
}

func WriteArchive(releaseRoot, output string) (string, error) {
	root, manifest, err := verifiedReleaseRoot(releaseRoot)
	if err != nil {
		return "", err
	}
	destination, err := newOutputPath(output, root)
	if err != nil {
		return "", err
	}
	return writeArchiveAtomically(root, destination, manifest)
}

func verifiedReleaseRoot(releaseRoot string) (string, Manifest, error) {
	root, err := filepath.Abs(releaseRoot)
	if err != nil {
		return "", Manifest{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", Manifest{}, err
	}
	manifest, present, err := ReadManifest(root)
	if err != nil || !present {
		if err == nil {
			err = errors.New("release archive source has no manifest")
		}
		return "", Manifest{}, err
	}
	if err := manifest.Verify(root); err != nil {
		return "", Manifest{}, err
	}
	return root, manifest, nil
}

func writeArchiveAtomically(root, destination string, manifest Manifest) (string, error) {
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".code-polishy-release-*.zip")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := writeReleaseZip(temporary, root, manifest); err != nil {
		return "", err
	}
	digest, err := fileSHA256(temporaryPath)
	if err != nil {
		return "", err
	}
	if err := verifyReleaseArchive(temporaryPath); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return "", err
	}
	keep = true
	return digest, nil
}

func PublishArchive(archive, destination string) (PublicationArtifact, error) {
	parent, final, err := newOutputDirectory(destination)
	if err != nil {
		return PublicationArtifact{}, err
	}
	source, err := inspectPublicationArchive(archive, parent)
	if err != nil {
		return PublicationArtifact{}, err
	}
	defer os.RemoveAll(source.inspection)
	payload, err := composePublication(source)
	if err != nil {
		return PublicationArtifact{}, err
	}
	if err := writePublicationDirectory(source.archive, parent, final, payload); err != nil {
		return PublicationArtifact{}, err
	}
	return payload.artifact, nil
}

type publicationSource struct {
	archive, inspection string
	archiveDigest       string
	archiveSize         int64
	manifest            Manifest
	manifestBytes       []byte
}

type publicationPayload struct {
	artifact                       PublicationArtifact
	descriptorName                 string
	descriptorBytes, manifestBytes []byte
	sbomBytes, provenanceBytes     []byte
}

func inspectPublicationArchive(archive, parent string) (publicationSource, error) {
	archivePath, err := regularAbsolutePath(archive)
	if err != nil {
		return publicationSource{}, err
	}
	inspection, err := os.MkdirTemp(parent, ".code-polishy-publication-inspect-")
	if err != nil {
		return publicationSource{}, err
	}
	valid := false
	defer func() {
		if !valid {
			_ = os.RemoveAll(inspection)
		}
	}()
	if err := extractReleaseZip(archivePath, inspection); err != nil {
		return publicationSource{}, err
	}
	manifest, manifestBytes, err := inspectExtractedManifest(inspection)
	if err != nil {
		return publicationSource{}, err
	}
	archiveDigest, err := fileSHA256(archivePath)
	if err != nil {
		return publicationSource{}, err
	}
	archiveInfo, err := os.Stat(archivePath)
	if err != nil {
		return publicationSource{}, err
	}
	valid = true
	return publicationSource{archivePath, inspection, archiveDigest, archiveInfo.Size(), manifest, manifestBytes}, nil
}

func inspectExtractedManifest(inspection string) (Manifest, []byte, error) {
	manifest, present, err := ReadManifest(inspection)
	if err != nil {
		return Manifest{}, nil, err
	}
	if !present {
		return Manifest{}, nil, errors.New("release archive has no manifest")
	}
	if err := manifest.Verify(inspection); err != nil {
		return Manifest{}, nil, err
	}
	manifestBytes, err := os.ReadFile(filepath.Join(inspection, ManifestFilename))
	return manifest, manifestBytes, err
}

func composePublication(source publicationSource) (publicationPayload, error) {
	manifest := source.manifest
	base := "code-polishy-" + manifest.CodePolishyVersion + "-" + manifest.Host
	archiveName := base + ".zip"
	manifestName := base + ".release-manifest.json"
	sbomName := base + ".sbom.cdx.json"
	provenanceName := base + ".provenance.intoto.json"
	descriptorName := base + ".release.json"
	sbom, err := renderReleaseSBOM(manifest, archiveName, source.archiveDigest)
	if err != nil {
		return publicationPayload{}, err
	}
	provenance, err := renderReleaseProvenance(manifest, archiveName, source.archiveDigest, manifestName, digestBytes(source.manifestBytes), sbomName, digestBytes(sbom))
	if err != nil {
		return publicationPayload{}, err
	}
	artifact := PublicationArtifact{
		DescriptorVersion: PublicationVersion, CodePolishyVersion: manifest.CodePolishyVersion,
		SourceRevision: manifest.SourceRevision, Host: manifest.Host,
		Archive:    PublishedFile{Name: archiveName, SHA256: source.archiveDigest, Size: source.archiveSize},
		Manifest:   PublishedManifest{PublishedFile: PublishedFile{Name: manifestName, SHA256: digestBytes(source.manifestBytes), Size: int64(len(source.manifestBytes))}, ReleaseDigest: manifest.ReleaseDigest, ContentDigest: manifest.ContentDigest},
		SBOM:       PublishedFile{Name: sbomName, SHA256: digestBytes(sbom), Size: int64(len(sbom))},
		Provenance: PublishedFile{Name: provenanceName, SHA256: digestBytes(provenance), Size: int64(len(provenance))},
	}
	if err := validatePublicationArtifact(artifact); err != nil {
		return publicationPayload{}, err
	}
	descriptor, err := renderJSON(artifact)
	if err != nil {
		return publicationPayload{}, err
	}
	return publicationPayload{artifact, descriptorName, descriptor, source.manifestBytes, sbom, provenance}, nil
}

func writePublicationDirectory(archive, parent, final string, payload publicationPayload) error {
	staging, err := os.MkdirTemp(parent, ".code-polishy-publication-")
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := copyRegularFile(archive, filepath.Join(staging, payload.artifact.Archive.Name), 0o644); err != nil {
		return err
	}
	files := map[string][]byte{
		payload.artifact.Archive.Name + ".sha256": []byte(payload.artifact.Archive.SHA256 + "  " + payload.artifact.Archive.Name + "\n"),
		payload.artifact.Manifest.Name:            payload.manifestBytes,
		payload.artifact.SBOM.Name:                payload.sbomBytes,
		payload.artifact.Provenance.Name:          payload.provenanceBytes,
	}
	files[payload.descriptorName] = payload.descriptorBytes
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(staging, name), data, 0o644); err != nil {
			return err
		}
	}
	if err := verifyPublishedFiles(staging, payload.artifact); err != nil {
		return err
	}
	if err := os.Rename(staging, final); err != nil {
		return err
	}
	keep = true
	return nil
}

func WritePublicationIndex(descriptors []string, output string) (PublicationIndex, error) {
	if len(descriptors) != len(supportedReleaseHosts) {
		return PublicationIndex{}, fmt.Errorf("release index requires exactly %d host descriptors", len(supportedReleaseHosts))
	}
	artifacts, err := readPublicationArtifacts(descriptors)
	if err != nil {
		return PublicationIndex{}, err
	}
	version, revision, err := validatePublicationSet(artifacts)
	if err != nil {
		return PublicationIndex{}, err
	}
	publication := PublicationIndex{IndexVersion: PublicationVersion, CodePolishyVersion: version, SourceRevision: revision, Artifacts: artifacts}
	data, err := renderJSON(publication)
	if err != nil {
		return PublicationIndex{}, err
	}
	if err := writeNewFileAtomically(output, data); err != nil {
		return PublicationIndex{}, err
	}
	return publication, nil
}

func readPublicationArtifacts(descriptors []string) ([]PublicationArtifact, error) {
	artifacts := make([]PublicationArtifact, 0, len(descriptors))
	for _, descriptor := range descriptors {
		artifact, err := readPublicationArtifact(descriptor)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	slices.SortFunc(artifacts, func(first, second PublicationArtifact) int { return strings.Compare(first.Host, second.Host) })
	return artifacts, nil
}

func readPublicationArtifact(descriptor string) (PublicationArtifact, error) {
	path, err := regularAbsolutePath(descriptor)
	if err != nil {
		return PublicationArtifact{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return PublicationArtifact{}, err
	}
	var artifact PublicationArtifact
	if err := decodeExactly(data, path, &artifact); err != nil {
		return PublicationArtifact{}, err
	}
	if err := validatePublicationArtifact(artifact); err != nil {
		return PublicationArtifact{}, err
	}
	if err := verifyPublishedFiles(filepath.Dir(path), artifact); err != nil {
		return PublicationArtifact{}, err
	}
	return artifact, nil
}

func validatePublicationSet(artifacts []PublicationArtifact) (string, string, error) {
	version := artifacts[0].CodePolishyVersion
	revision := artifacts[0].SourceRevision
	for index, artifact := range artifacts {
		if artifact.Host != supportedReleaseHosts[index] {
			return "", "", fmt.Errorf("release index host %d is %s, expected %s", index+1, artifact.Host, supportedReleaseHosts[index])
		}
		if artifact.CodePolishyVersion != version || artifact.SourceRevision != revision {
			return "", "", errors.New("release index descriptors do not name one version and source revision")
		}
	}
	return version, revision, nil
}

func PrepareOCIContext(descriptor, template, destination string) (Manifest, error) {
	artifact, publicationRoot, err := readLinuxPublicationArtifact(descriptor)
	if err != nil {
		return Manifest{}, err
	}
	templateBytes, err := readOCIContainerfile(template)
	if err != nil {
		return Manifest{}, err
	}
	parent, final, err := newOutputDirectory(destination)
	if err != nil {
		return Manifest{}, err
	}
	return writeOCIContext(publicationRoot, parent, final, templateBytes, artifact)
}

func readLinuxPublicationArtifact(descriptor string) (PublicationArtifact, string, error) {
	descriptorPath, err := regularAbsolutePath(descriptor)
	if err != nil {
		return PublicationArtifact{}, "", err
	}
	artifact, err := readPublicationArtifact(descriptorPath)
	if err != nil {
		return PublicationArtifact{}, "", err
	}
	if !strings.HasPrefix(artifact.Host, "linux-") {
		return PublicationArtifact{}, "", errors.New("OCI context requires a Linux release descriptor")
	}
	return artifact, filepath.Dir(descriptorPath), nil
}

func readOCIContainerfile(template string) ([]byte, error) {
	templatePath, err := regularAbsolutePath(template)
	if err != nil {
		return nil, err
	}
	templateBytes, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, err
	}
	if len(templateBytes) == 0 || len(templateBytes) > 64*1024 {
		return nil, errors.New("OCI Containerfile template has an invalid size")
	}
	return templateBytes, nil
}

func writeOCIContext(publicationRoot, parent, final string, templateBytes []byte, artifact PublicationArtifact) (Manifest, error) {
	staging, err := os.MkdirTemp(parent, ".code-polishy-oci-context-")
	if err != nil {
		return Manifest{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(staging)
		}
	}()
	prefix := filepath.Join(staging, "root", "opt", "code-polishy")
	archive := filepath.Join(publicationRoot, artifact.Archive.Name)
	manifest, err := InstallLocalBundle(archive, artifact.Archive.SHA256, prefix)
	if err != nil {
		return Manifest{}, err
	}
	if manifest.ReleaseDigest != artifact.Manifest.ReleaseDigest || manifest.SourceRevision != artifact.SourceRevision {
		return Manifest{}, errors.New("OCI context release does not match its publication descriptor")
	}
	if err := os.WriteFile(filepath.Join(staging, "Containerfile"), templateBytes, 0o644); err != nil {
		return Manifest{}, err
	}
	buildArguments := renderOCIBuildArguments(artifact)
	if err := os.WriteFile(filepath.Join(staging, "build-args.env"), []byte(buildArguments), 0o644); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(staging, final); err != nil {
		return Manifest{}, err
	}
	keep = true
	return manifest, nil
}

func renderOCIBuildArguments(artifact PublicationArtifact) string {
	platform := "linux/amd64"
	if artifact.Host == "linux-arm64" {
		platform = "linux/arm64"
	}
	return fmt.Sprintf(
		"CODE_POLISHY_VERSION=%s\nCODE_POLISHY_SOURCE_REVISION=%s\nCODE_POLISHY_RELEASE_DIGEST=%s\nCODE_POLISHY_BUNDLE_SHA256=%s\nCODE_POLISHY_PLATFORM=%s\n",
		artifact.CodePolishyVersion, artifact.SourceRevision, artifact.Manifest.ReleaseDigest, artifact.Archive.SHA256, platform,
	)
}

func writeReleaseZip(output *os.File, root string, manifest Manifest) error {
	writer := zip.NewWriter(output)
	entries := append(slices.Clone(manifest.Entries), Entry{Path: ManifestFilename, SHA256: "manifest"})
	slices.SortFunc(entries, func(first, second Entry) int { return strings.Compare(first.Path, second.Path) })
	fixed := time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, entry := range entries {
		source := filepath.Join(root, filepath.FromSlash(entry.Path))
		info, err := os.Lstat(source)
		if err != nil {
			writer.Close()
			output.Close()
			return err
		}
		header := &zip.FileHeader{Name: entry.Path, Method: zip.Deflate, Modified: fixed}
		header.SetMode(info.Mode())
		if entry.Symlink != "" {
			header.Method = zip.Store
		}
		zipped, err := writer.CreateHeader(header)
		if err != nil {
			writer.Close()
			output.Close()
			return err
		}
		if entry.Symlink != "" {
			_, err = io.WriteString(zipped, entry.Symlink)
		} else {
			var input *os.File
			input, err = os.Open(source)
			if err == nil {
				_, err = io.Copy(zipped, input)
				err = errors.Join(err, input.Close())
			}
		}
		if err != nil {
			writer.Close()
			output.Close()
			return err
		}
	}
	return errors.Join(writer.Close(), output.Sync(), output.Close())
}

func verifyReleaseArchive(archive string) error {
	directory, err := os.MkdirTemp(filepath.Dir(archive), ".code-polishy-release-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	if err := extractReleaseZip(archive, directory); err != nil {
		return err
	}
	manifest, present, err := ReadManifest(directory)
	if err != nil || !present {
		if err == nil {
			err = errors.New("release archive has no manifest")
		}
		return err
	}
	return manifest.Verify(directory)
}

func renderReleaseSBOM(manifest Manifest, archiveName, archiveDigest string) ([]byte, error) {
	type hash struct {
		Algorithm string `json:"alg"`
		Content   string `json:"content"`
	}
	type property struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	type component struct {
		Type       string     `json:"type"`
		BOMRef     string     `json:"bom-ref"`
		Name       string     `json:"name"`
		Version    string     `json:"version"`
		Hashes     []hash     `json:"hashes,omitempty"`
		Properties []property `json:"properties,omitempty"`
	}
	tools := []struct{ name, version string }{
		{"go", manifest.Tools.Go}, {"govulncheck", manifest.Tools.Govulncheck}, {"node", manifest.Tools.Node},
		{"osv-scanner", manifest.Tools.OSVScanner}, {"pnpm", manifest.Tools.PNPM}, {"python", manifest.Tools.Python},
		{"ruff", manifest.Tools.Ruff}, {"shellcheck", manifest.Tools.Shellcheck}, {"staticcheck", manifest.Tools.Staticcheck},
		{"ty", manifest.Tools.Ty}, {"vulture", manifest.Tools.Vulture},
	}
	components := make([]component, 0, len(tools))
	for _, tool := range tools {
		components = append(components, component{Type: "application", BOMRef: "tool:" + tool.name + "@" + tool.version, Name: tool.name, Version: tool.version})
	}
	document := struct {
		BOMFormat    string `json:"bomFormat"`
		SpecVersion  string `json:"specVersion"`
		SerialNumber string `json:"serialNumber"`
		Version      int    `json:"version"`
		Metadata     struct {
			Component component `json:"component"`
		} `json:"metadata"`
		Components []component `json:"components"`
	}{BOMFormat: "CycloneDX", SpecVersion: "1.6", SerialNumber: deterministicURN(archiveDigest), Version: 1, Components: components}
	document.Metadata.Component = component{
		Type: "application", BOMRef: "pkg:generic/code-polishy@" + manifest.CodePolishyVersion,
		Name: "code-polishy", Version: manifest.CodePolishyVersion, Hashes: []hash{{Algorithm: "SHA-256", Content: archiveDigest}},
		Properties: []property{
			{Name: "code-polishy:archive-name", Value: archiveName},
			{Name: "code-polishy:content-digest", Value: manifest.ContentDigest},
			{Name: "code-polishy:host", Value: manifest.Host},
			{Name: "code-polishy:release-digest", Value: manifest.ReleaseDigest},
			{Name: "code-polishy:source-revision", Value: manifest.SourceRevision},
		},
	}
	return renderJSON(document)
}

func renderReleaseProvenance(manifest Manifest, archiveName, archiveDigest, manifestName, manifestDigest, sbomName, sbomDigest string) ([]byte, error) {
	document := map[string]any{
		"_type":         "https://in-toto.io/Statement/v1",
		"subject":       []any{map[string]any{"name": archiveName, "digest": map[string]string{"sha256": archiveDigest}}},
		"predicateType": "https://slsa.dev/provenance/v1",
		"predicate": map[string]any{
			"buildDefinition": map[string]any{
				"buildType":          "https://github.com/riteofstring/code-polishy/release/portable/v1",
				"externalParameters": map[string]string{"host": manifest.Host, "version": manifest.CodePolishyVersion},
				"internalParameters": map[string]string{"contentDigest": manifest.ContentDigest, "releaseDigest": manifest.ReleaseDigest},
				"resolvedDependencies": []any{
					map[string]any{"uri": "git+https://github.com/riteofstring/code-polishy@" + manifest.SourceRevision, "digest": map[string]string{"sha1": manifest.SourceRevision}},
					map[string]any{"uri": "file:" + manifestName, "digest": map[string]string{"sha256": manifestDigest}},
					map[string]any{"uri": "file:" + sbomName, "digest": map[string]string{"sha256": sbomDigest}},
				},
			},
			"runDetails": map[string]any{"builder": map[string]string{"id": "https://github.com/riteofstring/code-polishy"}},
		},
	}
	return renderJSON(document)
}

func validatePublicationArtifact(artifact PublicationArtifact) error {
	if artifact.DescriptorVersion != PublicationVersion || !versionPattern.MatchString(artifact.CodePolishyVersion) ||
		!revisionPattern.MatchString(artifact.SourceRevision) || !slices.Contains(supportedReleaseHosts, artifact.Host) {
		return errors.New("release publication descriptor has an invalid identity")
	}
	base := "code-polishy-" + artifact.CodePolishyVersion + "-" + artifact.Host
	files := []struct {
		file PublishedFile
		name string
	}{
		{artifact.Archive, base + ".zip"},
		{artifact.Manifest.PublishedFile, base + ".release-manifest.json"},
		{artifact.SBOM, base + ".sbom.cdx.json"},
		{artifact.Provenance, base + ".provenance.intoto.json"},
	}
	for _, candidate := range files {
		if candidate.file.Name != candidate.name || !digestPattern.MatchString(candidate.file.SHA256) || candidate.file.Size <= 0 {
			return fmt.Errorf("release publication descriptor has an invalid file %q", candidate.file.Name)
		}
	}
	if !digestPattern.MatchString(artifact.Manifest.ReleaseDigest) || !digestPattern.MatchString(artifact.Manifest.ContentDigest) {
		return errors.New("release publication descriptor has an invalid manifest identity")
	}
	return nil
}

func verifyPublishedFiles(root string, artifact PublicationArtifact) error {
	if err := verifyPublishedDigests(root, artifact); err != nil {
		return err
	}
	if err := verifyPublishedManifest(root, artifact); err != nil {
		return err
	}
	if err := verifyPublishedSBOM(root, artifact); err != nil {
		return err
	}
	return verifyPublishedProvenance(root, artifact)
}

func verifyPublishedDigests(root string, artifact PublicationArtifact) error {
	for _, published := range []PublishedFile{artifact.Archive, artifact.Manifest.PublishedFile, artifact.SBOM, artifact.Provenance} {
		path := filepath.Join(root, published.Name)
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("published release file %s is missing or invalid", path)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != published.Size {
			return fmt.Errorf("published release file %s is missing or invalid", path)
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		if digest != published.SHA256 {
			return fmt.Errorf("published release file %s does not match its descriptor", path)
		}
	}
	return nil
}

func verifyPublishedManifest(root string, artifact PublicationArtifact) error {
	manifestPath := filepath.Join(root, artifact.Manifest.Name)
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	manifest, err := parseManifest(manifestBytes, manifestPath)
	if err != nil {
		return err
	}
	if manifest.CodePolishyVersion != artifact.CodePolishyVersion || manifest.SourceRevision != artifact.SourceRevision ||
		manifest.Host != artifact.Host || manifest.ReleaseDigest != artifact.Manifest.ReleaseDigest ||
		manifest.ContentDigest != artifact.Manifest.ContentDigest {
		return errors.New("published release manifest does not match its descriptor")
	}
	return nil
}

func verifyPublishedSBOM(root string, artifact PublicationArtifact) error {
	var sbom struct {
		BOMFormat    string          `json:"bomFormat"`
		SpecVersion  string          `json:"specVersion"`
		SerialNumber string          `json:"serialNumber"`
		Version      int             `json:"version"`
		Metadata     json.RawMessage `json:"metadata"`
		Components   json.RawMessage `json:"components"`
	}
	if data, err := os.ReadFile(filepath.Join(root, artifact.SBOM.Name)); err != nil {
		return err
	} else if err := decodeExactly(data, artifact.SBOM.Name, &sbom); err != nil {
		return err
	}
	if sbom.BOMFormat != "CycloneDX" || sbom.SpecVersion != "1.6" || sbom.Version != 1 ||
		sbom.SerialNumber == "" || len(sbom.Metadata) == 0 || len(sbom.Components) == 0 {
		return errors.New("published release SBOM has an invalid identity")
	}
	return nil
}

func verifyPublishedProvenance(root string, artifact PublicationArtifact) error {
	var provenance struct {
		Type          string `json:"_type"`
		PredicateType string `json:"predicateType"`
		Subject       []struct {
			Name   string            `json:"name"`
			Digest map[string]string `json:"digest"`
		} `json:"subject"`
		Predicate json.RawMessage `json:"predicate"`
	}
	if data, err := os.ReadFile(filepath.Join(root, artifact.Provenance.Name)); err != nil {
		return err
	} else if err := decodeExactly(data, artifact.Provenance.Name, &provenance); err != nil {
		return err
	}
	if provenance.Type != "https://in-toto.io/Statement/v1" || provenance.PredicateType != "https://slsa.dev/provenance/v1" ||
		len(provenance.Subject) != 1 || provenance.Subject[0].Name != artifact.Archive.Name ||
		provenance.Subject[0].Digest["sha256"] != artifact.Archive.SHA256 || len(provenance.Predicate) == 0 {
		return errors.New("published release provenance does not match its descriptor")
	}
	return nil
}

func regularAbsolutePath(value string) (string, error) {
	if value == "" {
		return "", errors.New("path is empty")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("path is not a regular file: %s", absolute)
	}
	return absolute, nil
}

func newOutputPath(value, disallowedRoot string) (string, error) {
	if value == "" {
		return "", errors.New("output path is empty")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	if disallowedRoot != "" && (absolute == disallowedRoot || strings.HasPrefix(absolute, disallowedRoot+string(os.PathSeparator))) {
		return "", errors.New("release archive output must be outside the release root")
	}
	if _, err := os.Lstat(absolute); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("output path already exists")
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return "", err
	}
	return absolute, nil
}

func newOutputDirectory(value string) (string, string, error) {
	if value == "" {
		return "", "", errors.New("output directory is empty")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Lstat(absolute); !errors.Is(err, os.ErrNotExist) {
		return "", "", errors.New("output directory already exists")
	}
	parent := filepath.Dir(absolute)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", "", err
	}
	return parent, absolute, nil
}

func copyRegularFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		input.Close()
		return err
	}
	_, copyErr := io.Copy(output, input)
	return errors.Join(copyErr, input.Close(), output.Close())
}

func writeNewFileAtomically(output string, data []byte) error {
	destination, err := newOutputPath(output, "")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".code-polishy-index-*.json")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer os.Remove(path)
	_, writeErr := temporary.Write(data)
	closeErr := temporary.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(path, destination)
}

func renderJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func deterministicURN(digest string) string {
	bytes, _ := hex.DecodeString(digest[:32])
	bytes[6] = bytes[6]&0x0f | 0x50
	bytes[8] = bytes[8]&0x3f | 0x80
	encoded := hex.EncodeToString(bytes)
	return "urn:uuid:" + strings.Join([]string{encoded[:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:]}, "-")
}

func validateArchiveLink(relative, target string) error {
	if target == "" || strings.Contains(target, "\\") || path.IsAbs(target) {
		return fmt.Errorf("release bundle link %q has an unsafe target", relative)
	}
	resolved := path.Clean(path.Join(path.Dir(relative), target))
	if !isContainedPath(resolved) {
		return fmt.Errorf("release bundle link %q escapes its root", relative)
	}
	return nil
}

func readArchiveLink(entry *zip.File) (string, error) {
	if entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > 4096 {
		return "", errors.New("release bundle link has an invalid size")
	}
	input, err := entry.Open()
	if err != nil {
		return "", err
	}
	data, readErr := io.ReadAll(io.LimitReader(input, 4097))
	if err := errors.Join(readErr, input.Close()); err != nil {
		return "", err
	}
	if len(data) > 4096 || bytes.IndexByte(data, 0) >= 0 {
		return "", errors.New("release bundle link has invalid content")
	}
	return string(data), nil
}
