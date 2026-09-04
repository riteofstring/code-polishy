package release

import (
	"bufio"
	"bytes"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	runtimeinfo "runtime/debug"
	"slices"
	"strings"

	cdx "github.com/CycloneDX/cyclonedx-go"
	"github.com/riteofstring/code-polishy/internal/pythonfacts"
)

type releaseSBOMInventory struct {
	components   map[string]cdx.Component
	dependencies map[string]map[string]bool
}

type javascriptPackageMap struct {
	Packages map[string]javascriptPackageMapEntry `json:"packages"`
}

type javascriptPackageMapEntry struct {
	URL          string            `json:"url"`
	Dependencies map[string]string `json:"dependencies"`
}

func renderReleaseSBOM(manifest Manifest, releaseRoot, archiveName, archiveDigest string) ([]byte, error) {
	rootRef := "pkg:generic/code-polishy@" + manifest.CodePolishyVersion
	inventory := releaseSBOMInventory{components: map[string]cdx.Component{}, dependencies: map[string]map[string]bool{rootRef: {}}}
	toolRefs := releaseSBOMTools(manifest)
	for name, component := range toolRefs {
		inventory.add(component)
		inventory.edge(rootRef, component.BOMRef)
		toolRefs[name] = component
	}
	if err := inventory.addJavaScript(releaseRoot, rootRef); err != nil {
		return nil, err
	}
	if err := inventory.addPython(releaseRoot, rootRef, manifest); err != nil {
		return nil, err
	}
	if err := inventory.addGo(releaseRoot, manifest, rootRef, toolRefs); err != nil {
		return nil, err
	}
	components, dependencies, err := inventory.finalize(rootRef)
	if err != nil {
		return nil, err
	}
	rootProperties := []cdx.Property{
		{Name: "code-polishy:archive-name", Value: archiveName},
		{Name: "code-polishy:content-digest", Value: manifest.ContentDigest},
		{Name: "code-polishy:host", Value: manifest.Host},
		{Name: "code-polishy:release-digest", Value: manifest.ReleaseDigest},
		{Name: "code-polishy:source-revision", Value: manifest.SourceRevision},
	}
	rootHashes := []cdx.Hash{{Algorithm: cdx.HashAlgoSHA256, Value: archiveDigest}}
	document := cdx.NewBOM()
	document.SpecVersion = cdx.SpecVersion1_6
	document.SerialNumber = deterministicURN(archiveDigest)
	document.Metadata = &cdx.Metadata{Component: &cdx.Component{
		Type: cdx.ComponentTypeApplication, BOMRef: rootRef, PackageURL: rootRef, Name: "code-polishy", Version: manifest.CodePolishyVersion,
		Hashes: &rootHashes, Properties: &rootProperties,
	}}
	document.Components = &components
	document.Dependencies = &dependencies
	buffer := &bytes.Buffer{}
	if err := cdx.NewBOMEncoder(buffer, cdx.BOMFileFormatJSON).SetPretty(true).SetEscapeHTML(false).EncodeVersion(document, cdx.SpecVersion1_6); err != nil {
		return nil, fmt.Errorf("encode CycloneDX release SBOM: %w", err)
	}
	return buffer.Bytes(), nil
}

func releaseSBOMTools(manifest Manifest) map[string]cdx.Component {
	versions := map[string]string{
		"go": manifest.Tools.Go, "govulncheck": manifest.Tools.Govulncheck, "node": manifest.Tools.Node,
		"osv-scanner": manifest.Tools.OSVScanner, "pnpm": manifest.Tools.PNPM, "python": manifest.Tools.Python,
		"ruff": manifest.Tools.Ruff, "shellcheck": manifest.Tools.Shellcheck, "staticcheck": manifest.Tools.Staticcheck,
		"ty": manifest.Tools.Ty,
	}
	result := make(map[string]cdx.Component, len(versions))
	for name, version := range versions {
		ref := "pkg:generic/" + name + "@" + version
		result[name] = cdx.Component{Type: cdx.ComponentTypeApplication, BOMRef: ref, PackageURL: ref, Name: name, Version: version}
	}
	return result
}

func (inventory *releaseSBOMInventory) add(component cdx.Component) {
	if _, found := inventory.components[component.BOMRef]; !found {
		inventory.components[component.BOMRef] = component
		inventory.dependencies[component.BOMRef] = map[string]bool{}
	}
}

func (inventory *releaseSBOMInventory) edge(source, target string) {
	if inventory.dependencies[source] == nil {
		inventory.dependencies[source] = map[string]bool{}
	}
	inventory.dependencies[source][target] = true
}

func (inventory *releaseSBOMInventory) addJavaScript(root, rootRef string) error {
	packageMap, err := readJavaScriptPackageMap(root)
	if err != nil {
		return err
	}
	licenses, err := javascriptInventoryLicenses(filepath.Join(root, "tools", "javascript_bundle_inventory.txt"))
	if err != nil {
		return err
	}
	refs, err := inventory.addJavaScriptComponents(packageMap.Packages, licenses, rootRef)
	if err != nil {
		return err
	}
	if len(licenses) != len(refs)-1 {
		return errors.New("sealed JavaScript package map and release inventory disagree")
	}
	return inventory.addJavaScriptEdges(packageMap.Packages, refs)
}

func readJavaScriptPackageMap(root string) (javascriptPackageMap, error) {
	mapPath := filepath.Join(root, ".tools", "javascript", "bundle", "node_modules", ".package-map.json")
	data, err := os.ReadFile(mapPath)
	if err != nil {
		return javascriptPackageMap{}, fmt.Errorf("read sealed JavaScript package map: %w", err)
	}
	if len(data) == 0 || len(data) > 8<<20 {
		return javascriptPackageMap{}, errors.New("sealed JavaScript package map has an invalid size")
	}
	if err := validateUniqueJSON(data); err != nil {
		return javascriptPackageMap{}, fmt.Errorf("validate sealed JavaScript package map: %w", err)
	}
	var packageMap javascriptPackageMap
	if err := decodeExactly(data, mapPath, &packageMap); err != nil {
		return javascriptPackageMap{}, err
	}
	if len(packageMap.Packages) < 2 || len(packageMap.Packages) > 10000 {
		return javascriptPackageMap{}, errors.New("sealed JavaScript package map has an invalid package count")
	}
	return packageMap, nil
}

func (inventory *releaseSBOMInventory) addJavaScriptComponents(packages map[string]javascriptPackageMapEntry, licenses map[string]string, rootRef string) (map[string]string, error) {
	refs := map[string]string{".": rootRef}
	for key, entry := range packages {
		if key == "." {
			continue
		}
		ref, err := inventory.addJavaScriptComponent(key, entry, licenses)
		if err != nil {
			return nil, err
		}
		refs[key] = ref
	}
	return refs, nil
}

func (inventory *releaseSBOMInventory) addJavaScriptComponent(key string, entry javascriptPackageMapEntry, licenses map[string]string) (string, error) {
	name, version, err := javascriptPackageIdentity(key)
	if err != nil || entry.URL == "" || len(entry.Dependencies) > 10000 {
		return "", fmt.Errorf("sealed JavaScript package %q has invalid identity or facts", key)
	}
	license, found := licenses[name+"@"+version]
	if !found {
		return "", fmt.Errorf("sealed JavaScript package %s@%s has no release inventory license", name, version)
	}
	ref := npmPURL(name, version)
	properties := []cdx.Property{{Name: "code-polishy:license-expression", Value: license}}
	inventory.add(cdx.Component{Type: cdx.ComponentTypeLibrary, BOMRef: ref, PackageURL: ref, Name: name, Version: version, Properties: &properties})
	return ref, nil
}

func (inventory *releaseSBOMInventory) addJavaScriptEdges(packages map[string]javascriptPackageMapEntry, refs map[string]string) error {
	for key, entry := range packages {
		source := refs[key]
		if source == "" {
			return fmt.Errorf("sealed JavaScript package map omitted %q", key)
		}
		for dependencyName, targetKey := range entry.Dependencies {
			target, err := javascriptDependencyTarget(key, dependencyName, targetKey, refs)
			if err != nil {
				return err
			}
			if target != source {
				inventory.edge(source, target)
			}
		}
	}
	return nil
}

func javascriptDependencyTarget(sourceKey, dependencyName, targetKey string, refs map[string]string) (string, error) {
	target := refs[targetKey]
	if target == "" {
		return "", fmt.Errorf("sealed JavaScript package %q names absent dependency %q", sourceKey, targetKey)
	}
	if targetKey != "." {
		name, _, _ := javascriptPackageIdentity(targetKey)
		if name != dependencyName {
			return "", fmt.Errorf("sealed JavaScript package %q misidentifies dependency %q", sourceKey, dependencyName)
		}
	}
	return target, nil
}

func javascriptPackageIdentity(value string) (string, string, error) {
	base := value
	if index := strings.IndexByte(base, '('); index >= 0 {
		base = base[:index]
	}
	separator := strings.LastIndexByte(base, '@')
	if separator <= 0 || separator == len(base)-1 {
		return "", "", errors.New("package key has no exact version")
	}
	name, version := base[:separator], base[separator+1:]
	if strings.ContainsAny(name+version, "\x00\r\n\t ") || strings.Contains(version, "/") {
		return "", "", errors.New("package key has invalid characters")
	}
	return name, version, nil
}

func npmPURL(name, version string) string {
	return "pkg:npm/" + strings.ReplaceAll(name, "@", "%40") + "@" + version
}

func javascriptInventoryLicenses(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read sealed JavaScript release inventory: %w", err)
	}
	if len(data) == 0 || len(data) > 2<<20 {
		return nil, errors.New("sealed JavaScript release inventory has an invalid size")
	}
	result := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		identity, license, found := strings.Cut(line, "\t")
		if !found || identity == "" || license == "" || result[identity] != "" {
			return nil, errors.New("sealed JavaScript release inventory has an invalid line")
		}
		result[identity] = license
	}
	return result, scanner.Err()
}

func (inventory *releaseSBOMInventory) addPython(root, rootRef string, manifest Manifest) error {
	inputs, err := pythonReleaseMetadataInputs(root, manifest)
	if err != nil {
		return err
	}
	expected := map[string]string{"packaging": manifest.Tools.Packaging, "vulture": manifest.Tools.Vulture}
	if len(inputs) != len(expected) {
		return errors.New("release Python distribution inventory is incomplete or contains an ungoverned distribution")
	}
	distributions, err := pythonReleaseDistributions(inputs, expected)
	if err != nil {
		return err
	}
	refs, err := inventory.addPythonComponents(rootRef, expected, distributions)
	if err != nil {
		return err
	}
	return inventory.addPythonEdges(distributions, refs)
}

func pythonReleaseMetadataInputs(root string, manifest Manifest) ([]pythonfacts.Input, error) {
	inputs := []pythonfacts.Input{}
	for _, entry := range manifest.Entries {
		if !strings.HasSuffix(strings.ToLower(entry.Path), ".dist-info/metadata") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entry.Path)))
		if err != nil {
			return nil, fmt.Errorf("read Python distribution metadata %s: %w", entry.Path, err)
		}
		if len(data) == 0 || len(data) > 2<<20 {
			return nil, fmt.Errorf("python distribution metadata %s has an invalid size", entry.Path)
		}
		inputs = append(inputs, pythonfacts.Input{Path: entry.Path, Source: string(data)})
	}
	return inputs, nil
}

func pythonReleaseDistributions(inputs []pythonfacts.Input, expected map[string]string) (map[string]pythonfacts.Distribution, error) {
	python, err := pythonfacts.DefaultInterpreter()
	if err != nil {
		return nil, err
	}
	response, err := pythonfacts.Analyze(python, pythonfacts.Request{Metadata: pythonfacts.SortedInputs(inputs)})
	if err != nil {
		return nil, fmt.Errorf("analyze Python distribution metadata: %w", err)
	}
	distributions := map[string]pythonfacts.Distribution{}
	for _, distribution := range response.Metadata {
		if distribution.Error != "" {
			return nil, fmt.Errorf("parse Python distribution metadata %s: %s", distribution.Path, distribution.Error)
		}
		if _, found := distributions[distribution.Name]; found {
			return nil, fmt.Errorf("python distribution %q is installed more than once", distribution.Name)
		}
		distributions[distribution.Name] = distribution
	}
	if len(distributions) != len(expected) {
		return nil, errors.New("release Python distribution inventory is incomplete or contains an ungoverned distribution")
	}
	return distributions, nil
}

func (inventory *releaseSBOMInventory) addPythonComponents(rootRef string, expected map[string]string, distributions map[string]pythonfacts.Distribution) (map[string]string, error) {
	refs := map[string]string{}
	for _, name := range []string{"packaging", "vulture"} {
		version := expected[name]
		distribution, found := distributions[name]
		if !found || distribution.Version != version {
			return nil, fmt.Errorf("release Python distribution %q does not match its manifest identity", name)
		}
		ref := "pkg:pypi/" + name + "@" + version
		inventory.add(cdx.Component{Type: cdx.ComponentTypeLibrary, BOMRef: ref, PackageURL: ref, Name: name, Version: version})
		inventory.edge(rootRef, ref)
		refs[name] = ref
	}
	return refs, nil
}

func (inventory *releaseSBOMInventory) addPythonEdges(distributions map[string]pythonfacts.Distribution, refs map[string]string) error {
	for name, distribution := range distributions {
		for _, requirement := range distribution.Requirements {
			if requirement.Error != "" {
				return fmt.Errorf("python distribution %q has an invalid requirement: %s", name, requirement.Error)
			}
			if target := refs[requirement.Name]; target != "" && target != refs[name] {
				inventory.edge(refs[name], target)
			}
		}
	}
	return nil
}

func (inventory *releaseSBOMInventory) addGo(root string, manifest Manifest, rootRef string, tools map[string]cdx.Component) error {
	for _, entry := range manifest.Entries {
		if err := inventory.addGoEntry(root, entry, rootRef, tools); err != nil {
			return err
		}
	}
	return nil
}

func (inventory *releaseSBOMInventory) addGoEntry(root string, entry Entry, rootRef string, tools map[string]cdx.Component) error {
	owner := goBinaryOwner(entry.Path, rootRef, tools)
	if owner == "" || entry.SHA256 == "" {
		return nil
	}
	info, err := buildinfo.ReadFile(filepath.Join(root, filepath.FromSlash(entry.Path)))
	if err != nil {
		return nil
	}
	for _, module := range shippedGoModules(info) {
		resolved := module
		if module.Replace != nil {
			resolved = module.Replace
		}
		if invalidGoModule(resolved) {
			return fmt.Errorf("go binary %s contains an unversioned external module", entry.Path)
		}
		ref := "pkg:golang/" + resolved.Path + "@" + resolved.Version
		inventory.add(cdx.Component{Type: cdx.ComponentTypeLibrary, BOMRef: ref, PackageURL: ref, Name: resolved.Path, Version: resolved.Version})
		inventory.edge(owner, ref)
	}
	return nil
}

func shippedGoModules(info *buildinfo.BuildInfo) []*runtimeinfo.Module {
	modules := append([]*runtimeinfo.Module{}, info.Deps...)
	if info.Main.Path != "" && info.Main.Path != "github.com/riteofstring/code-polishy" {
		modules = append(modules, &info.Main)
	}
	return modules
}

func invalidGoModule(module *runtimeinfo.Module) bool {
	return module.Path == "" || module.Version == "" || module.Version == "(devel)" || strings.HasPrefix(module.Path, ".")
}

func goBinaryOwner(entry, rootRef string, tools map[string]cdx.Component) string {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(entry)), ".exe")
	if base == "code-polishy" || base == "code-polishy-launcher" {
		return rootRef
	}
	if component, found := tools[base]; found {
		return component.BOMRef
	}
	return ""
}

func (inventory *releaseSBOMInventory) finalize(rootRef string) ([]cdx.Component, []cdx.Dependency, error) {
	if err := inventory.validateDependencyComponents(rootRef); err != nil {
		return nil, nil, err
	}
	if len(inventory.reachableComponents(rootRef)) != len(inventory.components)+1 {
		return nil, nil, errors.New("SBOM inventory contains an unreachable shipped component")
	}
	return inventory.sortedComponents(), inventory.sortedDependencies(), nil
}

func (inventory *releaseSBOMInventory) validateDependencyComponents(rootRef string) error {
	for source, targets := range inventory.dependencies {
		if source != rootRef {
			if _, found := inventory.components[source]; !found {
				return fmt.Errorf("SBOM dependency source %q has no component", source)
			}
		}
		for target := range targets {
			if _, found := inventory.components[target]; !found {
				return fmt.Errorf("SBOM dependency target %q has no component", target)
			}
		}
	}
	return nil
}

func (inventory *releaseSBOMInventory) reachableComponents(rootRef string) map[string]bool {
	reachable := map[string]bool{rootRef: true}
	queue := []string{rootRef}
	for len(queue) > 0 {
		source := queue[0]
		queue = queue[1:]
		for target := range inventory.dependencies[source] {
			if !reachable[target] {
				reachable[target] = true
				queue = append(queue, target)
			}
		}
	}
	return reachable
}

func (inventory *releaseSBOMInventory) sortedComponents() []cdx.Component {
	components := make([]cdx.Component, 0, len(inventory.components))
	for _, component := range inventory.components {
		components = append(components, component)
	}
	slices.SortFunc(components, func(left, right cdx.Component) int { return strings.Compare(left.BOMRef, right.BOMRef) })
	return components
}

func (inventory *releaseSBOMInventory) sortedDependencies() []cdx.Dependency {
	dependencies := make([]cdx.Dependency, 0, len(inventory.dependencies))
	for source, targetSet := range inventory.dependencies {
		targets := make([]string, 0, len(targetSet))
		for target := range targetSet {
			targets = append(targets, target)
		}
		slices.Sort(targets)
		dependencies = append(dependencies, cdx.Dependency{Ref: source, Dependencies: &targets})
	}
	slices.SortFunc(dependencies, func(left, right cdx.Dependency) int { return strings.Compare(left.Ref, right.Ref) })
	return dependencies
}

func validateUniqueJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	count := 0
	if err := validateUniqueJSONValue(decoder, 0, &count); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("JSON contains trailing data")
	}
	return nil
}

func validateUniqueJSONValue(decoder *json.Decoder, depth int, count *int) error {
	*count++
	if depth > 64 || *count > 1000000 {
		return errors.New("JSON exceeds its structural limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		err = validateUniqueJSONObject(decoder, depth, count)
	case '[':
		err = validateUniqueJSONArray(decoder, depth, count)
	default:
		return errors.New("JSON contains an invalid delimiter")
	}
	if err != nil {
		return err
	}
	_, err = decoder.Token()
	return err
}

func validateUniqueJSONObject(decoder *json.Decoder, depth int, count *int) error {
	seen := map[string]bool{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok || seen[key] {
			return errors.New("JSON object contains an invalid or duplicate key")
		}
		seen[key] = true
		if err := validateUniqueJSONValue(decoder, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}

func validateUniqueJSONArray(decoder *json.Decoder, depth int, count *int) error {
	for decoder.More() {
		if err := validateUniqueJSONValue(decoder, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}
