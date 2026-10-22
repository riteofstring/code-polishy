package release

import (
	"bytes"
	"debug/buildinfo"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	cdx "github.com/CycloneDX/cyclonedx-go"
	"golang.org/x/mod/module"
)

var trivyFixtureBinary = sync.OnceValues(func() ([]byte, error) {
	directory, err := os.MkdirTemp("", "code-polishy-trivy-fixture-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	binary := filepath.Join(directory, "trivy")
	build := exec.Command("go", "build", "-trimpath", "-o", binary, "github.com/riteofstring/code-polishy/cmd/code-polishy-launcher")
	if output, err := build.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("build the fixture Go binary: %w: %s", err, output)
	}
	return os.ReadFile(binary)
})

func addTrivyReleaseFixture(t *testing.T, files map[string]string, host string) {
	t.Helper()
	binary, err := trivyFixtureBinary()
	if err != nil {
		t.Fatal(err)
	}
	directory, err := trivyReleaseDirectory(host)
	if err != nil {
		t.Fatal(err)
	}
	files[directory+"/trivy"] = string(binary)
	for index, shipped := range trivyFixtureModules(t) {
		escaped, err := module.EscapePath(shipped.Path)
		if err != nil {
			t.Fatal(err)
		}
		files[directory+"/licenses/"+escaped+"@"+shipped.Version+"/LICENSE"] = "upstream license\n"
		if index == 0 {
			files[directory+"/"+trivyFixtureNestedNotice(t, escaped, shipped.Version)] = "forked component license\n"
		}
	}
}

func trivyFixtureNestedNotice(t *testing.T, escaped, version string) string {
	t.Helper()
	return "licenses/" + escaped + "@" + version + "/internal/third_party/forked/LICENSE.txt"
}

func trivyFixtureModules(t *testing.T) []module.Version {
	t.Helper()
	binary, err := trivyFixtureBinary()
	if err != nil {
		t.Fatal(err)
	}
	info, err := buildinfo.Read(bytes.NewReader(binary))
	if err != nil {
		t.Fatal(err)
	}
	var modules []module.Version
	for _, shipped := range shippedGoModules(info) {
		resolved := shipped
		if shipped.Replace != nil {
			resolved = shipped.Replace
		}
		modules = append(modules, module.Version{Path: resolved.Path, Version: resolved.Version})
	}
	if len(modules) == 0 {
		t.Fatal("the fixture binary records no shipped modules")
	}
	return modules
}

func TestReleaseSBOMRecordsCarriedTrivyModulesAndTheirNotices(t *testing.T) {
	root, manifest := installedRelease(t, map[string]string{BinaryPath: "fixture"}, nil)
	data, err := renderReleaseSBOM(manifest, root, "release.zip", exampleDigest)
	if err != nil {
		t.Fatal(err)
	}
	document := &cdx.BOM{}
	if err := cdx.NewBOMDecoder(bytes.NewReader(data), cdx.BOMFileFormatJSON).Decode(document); err != nil {
		t.Fatal(err)
	}
	components := map[string]cdx.Component{}
	for _, component := range *document.Components {
		components[component.BOMRef] = component
	}
	if _, found := components["pkg:generic/trivy@0.74.0"]; !found {
		t.Fatal("the SBOM omits the carried Trivy scanner")
	}
	dependencies := map[string]bool{}
	for _, dependency := range *document.Dependencies {
		if dependency.Ref == "pkg:generic/trivy@0.74.0" && dependency.Dependencies != nil {
			for _, target := range *dependency.Dependencies {
				dependencies[target] = true
			}
		}
	}
	directory, err := trivyReleaseDirectory(manifest.Host)
	if err != nil {
		t.Fatal(err)
	}
	for index, shipped := range trivyFixtureModules(t) {
		ref := "pkg:golang/" + shipped.Path + "@" + shipped.Version
		component, found := components[ref]
		if !found || !dependencies[ref] {
			t.Fatalf("the SBOM does not attribute %s to the carried Trivy", ref)
		}
		escaped, err := module.EscapePath(shipped.Path)
		if err != nil {
			t.Fatal(err)
		}
		wanted := []string{directory + "/licenses/" + escaped + "@" + shipped.Version + "/LICENSE"}
		if index == 0 {
			wanted = append(wanted, directory+"/"+trivyFixtureNestedNotice(t, escaped, shipped.Version))
		}
		var recorded []string
		if component.Properties != nil {
			for _, property := range *component.Properties {
				recorded = append(recorded, property.Value)
			}
		}
		slices.Sort(wanted)
		slices.Sort(recorded)
		if !slices.Equal(recorded, wanted) {
			t.Fatalf("%s notices = %v, want %v", ref, recorded, wanted)
		}
	}
}

func TestReleaseSBOMRefusesCarriedTrivyWithoutBuildInformationOrNotices(t *testing.T) {
	host, err := Host()
	if err != nil {
		t.Fatal(err)
	}
	directory, err := trivyReleaseDirectory(host)
	if err != nil {
		t.Fatal(err)
	}
	modules := trivyFixtureModules(t)
	escaped, err := module.EscapePath(modules[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, path, content, problem string
	}{
		{"stub binary", directory + "/trivy", "not a Go binary", "build information"},
		{"nested license only", directory + "/licenses/" + escaped + "@" + modules[0].Version + "/LICENSE", "", "has no root license notice"},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := map[string]string{BinaryPath: "fixture"}
			addTrivyReleaseFixture(t, files, host)
			if test.content == "" {
				delete(files, test.path)
				files[strings.TrimSuffix(test.path, "LICENSE")+"README.md"] = "not a license\n"
			} else {
				files[test.path] = test.content
			}
			root, manifest := installedRelease(t, files, nil)
			if _, err := renderReleaseSBOM(manifest, root, "release.zip", exampleDigest); err == nil || !strings.Contains(err.Error(), test.problem) {
				t.Fatalf("SBOM error = %v, want %q", err, test.problem)
			}
		})
	}
}
