package pack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/schema"
)

const CatalogProtocol = "code-polishy-pack-catalog/v1"
const CatalogSchema = schema.ConfigurationBase + "code-polishy-pack-catalog-v1.schema.json"

const maximumCatalogBytes = 8 << 20
const maximumCatalogEntries = 4096

type Catalog struct {
	Schema   string         `json:"$schema,omitempty"`
	Protocol string         `json:"protocol"`
	Entries  []CatalogEntry `json:"entries"`
}

type CatalogEntry struct {
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Digest          string            `json:"digest"`
	ArtifactURL     string            `json:"artifactUrl"`
	ArtifactBytes   int64             `json:"artifactBytes"`
	ManifestVersion int               `json:"manifestVersion"`
	ProtocolVersion int               `json:"protocolVersion"`
	EngineVersion   string            `json:"engineVersion"`
	Platforms       []string          `json:"platforms"`
	DiscoveryModes  []string          `json:"discoveryModes"`
	ExecutionTypes  []string          `json:"executionTypes"`
	Capabilities    []string          `json:"capabilities"`
	Tools           []string          `json:"tools"`
	Dependencies    []string          `json:"dependencies"`
	Licenses        []string          `json:"licenses"`
	Provenance      CatalogProvenance `json:"provenance"`
}

type CatalogProvenance struct {
	SourceRevision    string `json:"sourceRevision"`
	Builder           string `json:"builder"`
	AttestationURL    string `json:"attestationUrl"`
	AttestationSHA256 string `json:"attestationSha256"`
}

type LoadedCatalog struct {
	Catalog Catalog
	Path    string
	Root    string
	SHA256  string
}

func LoadCatalog(name, digest string) (LoadedCatalog, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return LoadedCatalog{}, err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 1 || info.Size() > maximumCatalogBytes {
		return LoadedCatalog{}, fmt.Errorf("catalog must be a regular file of 1 to %d bytes", maximumCatalogBytes)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return LoadedCatalog{}, err
	}
	data, err := os.ReadFile(canonical)
	if err != nil {
		return LoadedCatalog{}, err
	}
	if !validDigest(digest) || inputDigest(data) != digest {
		return LoadedCatalog{}, errors.New("catalog does not match the required SHA-256 digest")
	}
	if err := schema.NewValidator(CatalogSchema).Validate(data); err != nil {
		return LoadedCatalog{}, fmt.Errorf("validate catalog schema: %w", err)
	}
	catalog := Catalog{}
	if err := decodeCatalog(data, &catalog); err != nil {
		return LoadedCatalog{}, err
	}
	if err := validateCatalog(catalog); err != nil {
		return LoadedCatalog{}, err
	}
	return LoadedCatalog{Catalog: catalog, Path: canonical, Root: filepath.Dir(canonical), SHA256: digest}, nil
}

func decodeCatalog(data []byte, catalog *Catalog) error {
	if err := schema.ValidateUniqueJSON(data, 32); err != nil {
		return fmt.Errorf("decode catalog: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(catalog); err != nil {
		return fmt.Errorf("decode catalog: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("decode catalog: document contains more than one JSON value")
	}
	return nil
}

func validateCatalog(catalog Catalog) error {
	if catalog.Protocol != CatalogProtocol {
		return expected("protocol", CatalogProtocol)
	}
	if len(catalog.Entries) == 0 || len(catalog.Entries) > maximumCatalogEntries {
		return expected("entries", fmt.Sprintf("1 to %d items", maximumCatalogEntries))
	}
	seen := map[string]bool{}
	for index, entry := range catalog.Entries {
		if err := validateCatalogEntry(entry, index, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateCatalogEntry(entry CatalogEntry, index int, seen map[string]bool) error {
	label := indexed("entries", index)
	identity := entry.Name + "@" + entry.Version
	if !identifierPattern.MatchString(entry.Name) || !semanticVersionPattern.MatchString(entry.Version) || seen[identity] {
		return expected(label, "one unique lowercase pack name and exact semantic version")
	}
	seen[identity] = true
	if !validDigest(entry.Digest) {
		return expected(label+".digest", "a lowercase SHA-256 digest")
	}
	if _, err := catalogArtifactPath(entry.ArtifactURL); err != nil {
		return expected(label+".artifactUrl", "a contained file: URL")
	}
	if entry.ArtifactBytes < 1 || entry.ArtifactBytes > maximumPackBytes {
		return expected(label+".artifactBytes", fmt.Sprintf("1 to %d", maximumPackBytes))
	}
	if entry.ManifestVersion != ManifestVersion {
		return expected(label+".manifestVersion", fmt.Sprintf("%d", ManifestVersion))
	}
	if entry.ProtocolVersion != ProtocolVersion {
		return expected(label+".protocolVersion", fmt.Sprintf("%d", ProtocolVersion))
	}
	if !ValidEngineVersion(entry.EngineVersion) {
		return expected(label+".engineVersion", "one exact semantic version")
	}
	if err := validatePlatforms(entry.Platforms); err != nil {
		return fmt.Errorf("%s.platforms: %w", label, err)
	}
	if err := validateAllowed(entry.DiscoveryModes, packDiscoveryModes, label+".discoveryModes"); err != nil {
		return err
	}
	if err := validateAllowed(entry.ExecutionTypes, packExecutionTypes, label+".executionTypes"); err != nil {
		return err
	}
	if err := validateAllowed(entry.Capabilities, packCapabilities, label+".capabilities"); err != nil {
		return err
	}
	lists := []struct {
		name     string
		values   []string
		required bool
	}{
		{name: "tools", values: entry.Tools, required: true},
		{name: "dependencies", values: entry.Dependencies},
		{name: "licenses", values: entry.Licenses, required: true},
	}
	for _, list := range lists {
		if err := validateCatalogTextList(list.values, label+"."+list.name, list.required); err != nil {
			return err
		}
	}
	return validateCatalogProvenance(entry.Provenance, label+".provenance")
}

func validateCatalogTextList(values []string, label string, required bool) error {
	if required && len(values) == 0 || len(values) > 4096 {
		return expected(label, "a nonempty list of at most 4096 bounded values")
	}
	if err := validateUnique(values, label); err != nil {
		return err
	}
	for index, value := range values {
		if strings.TrimSpace(value) != value || value == "" || len(value) > 512 || strings.IndexFunc(value, catalogControlCharacter) >= 0 {
			return expected(indexed(label, index), "1 to 512 trimmed bytes without control characters")
		}
	}
	return nil
}

func catalogControlCharacter(value rune) bool {
	return value < 0x20 || value == 0x7f
}

func validateCatalogProvenance(value CatalogProvenance, label string) error {
	if !validCatalogRevision(value.SourceRevision) {
		return expected(label+".sourceRevision", "a 40 or 64 character Git object ID")
	}
	if !identifierPattern.MatchString(value.Builder) {
		return expected(label+".builder", "a lowercase identifier")
	}
	if _, err := catalogArtifactPath(value.AttestationURL); err != nil {
		return expected(label+".attestationUrl", "a contained file: URL")
	}
	if !validDigest(value.AttestationSHA256) {
		return expected(label+".attestationSha256", "a lowercase SHA-256 digest")
	}
	return nil
}

func validCatalogRevision(value string) bool {
	return len(value) == 40 && validRevision(value) || len(value) == 64 && validDigest(value)
}

func catalogArtifactPath(value string) (string, error) {
	if !strings.HasPrefix(value, "file:") || strings.ContainsAny(value, "?#") {
		return "", errors.New("unsupported artifact URL")
	}
	path := strings.TrimPrefix(value, "file:")
	if strings.ContainsRune(path, 0) {
		return "", errors.New("artifact URL contains a NUL byte")
	}
	if err := exactRelativePath(path); err != nil {
		return "", err
	}
	return path, nil
}

func (catalog LoadedCatalog) Entry(name, version string) (CatalogEntry, error) {
	for _, entry := range catalog.Catalog.Entries {
		if entry.Name == name && entry.Version == version {
			return entry, nil
		}
	}
	return CatalogEntry{}, fmt.Errorf("catalog has no exact pack %s@%s", name, version)
}

func InstallOfficial(catalog LoadedCatalog, name, version, engineVersion, dataRoot string) (Identity, string, error) {
	entry, err := catalog.Entry(name, version)
	if err != nil {
		return Identity{}, "", err
	}
	if entry.EngineVersion != engineVersion {
		return Identity{}, "", fmt.Errorf("pack %s@%s does not support Code Polishy %s", name, version, engineVersion)
	}
	if err := verifyCatalogAttestation(catalog.Root, entry.Provenance); err != nil {
		return Identity{}, "", err
	}
	relative, _ := catalogArtifactPath(entry.ArtifactURL)
	source, err := containedCatalogArtifact(catalog.Root, relative)
	if err != nil {
		return Identity{}, "", err
	}
	tree, err := readSourceTree(source, true)
	if err != nil {
		return Identity{}, "", err
	}
	if err := verifyCatalogTree(entry, tree); err != nil {
		return Identity{}, "", err
	}
	return installTree(tree, dataRoot)
}

func verifyCatalogAttestation(root string, provenance CatalogProvenance) error {
	relative, _ := catalogArtifactPath(provenance.AttestationURL)
	candidate, info, err := containedCatalogPath(root, relative, false)
	if err != nil || info.Size() < 1 || info.Size() > maximumCatalogBytes {
		return errors.New("catalog provenance attestation is not a bounded regular file")
	}
	data, err := os.ReadFile(candidate)
	if err != nil {
		return err
	}
	if inputDigest(data) != provenance.AttestationSHA256 {
		return errors.New("catalog provenance attestation does not match its required SHA-256 digest")
	}
	return nil
}

func containedCatalogArtifact(root, relative string) (string, error) {
	candidate, _, err := containedCatalogPath(root, relative, true)
	return candidate, err
}

func containedCatalogPath(root, relative string, directory bool) (string, os.FileInfo, error) {
	candidate := root
	parts := strings.Split(filepath.FromSlash(relative), string(filepath.Separator))
	for index, part := range parts {
		candidate = filepath.Join(candidate, part)
		info, err := os.Lstat(candidate)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", nil, errors.New("catalog path contains a missing or linked component")
		}
		last := index == len(parts)-1
		if !last && !info.IsDir() || last && directory != info.IsDir() || last && !directory && !info.Mode().IsRegular() {
			return "", nil, errors.New("catalog path has an invalid file type")
		}
		if last {
			return candidate, info, nil
		}
	}
	return "", nil, errors.New("catalog path is empty")
}

func verifyCatalogTree(entry CatalogEntry, tree sourceTree) error {
	if tree.Receipt.Name != entry.Name || tree.Receipt.Version != entry.Version || tree.Receipt.Digest != entry.Digest {
		return errors.New("catalog entry does not match the pack tree identity")
	}
	bytes := int64(0)
	for _, file := range tree.Files {
		bytes += int64(len(file.Data))
	}
	if bytes != entry.ArtifactBytes {
		return errors.New("catalog entry does not match the pack tree byte size")
	}
	if entry.EngineVersion != tree.Manifest.EngineVersion || !sameStrings(entry.Platforms, tree.Manifest.Platforms) || !sameStrings(entry.DiscoveryModes, manifestDiscoveryModes(tree.Manifest)) || !sameStrings(entry.ExecutionTypes, manifestExecutionTypes(tree.Manifest)) || !sameStrings(entry.Capabilities, manifestCapabilities(tree.Manifest)) {
		return errors.New("catalog contract metadata does not match the pack manifest")
	}
	return nil
}

func manifestDiscoveryModes(manifest Manifest) []string {
	values := []string{}
	for _, language := range manifest.Languages {
		values = append(values, language.DiscoveryMode)
	}
	return sortedUnique(values)
}

func manifestExecutionTypes(manifest Manifest) []string {
	values := []string{}
	for _, command := range manifest.Commands {
		values = append(values, command.Execution.Type)
	}
	return sortedUnique(values)
}

func manifestCapabilities(manifest Manifest) []string {
	values := []string{}
	for _, command := range manifest.Commands {
		values = append(values, command.Capabilities...)
	}
	return sortedUnique(values)
}

func sameStrings(left, right []string) bool {
	left, right = sortedUnique(left), sortedUnique(right)
	return slices.Equal(left, right)
}

func ParsePackReference(value string) (string, string, error) {
	name, version, found := strings.Cut(value, "@")
	if !found || !identifierPattern.MatchString(name) || !semanticVersionPattern.MatchString(version) {
		return "", "", errors.New("pack reference must be NAME@VERSION with an exact semantic version")
	}
	return name, version, nil
}
