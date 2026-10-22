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
	files[directory+"/notices-layout"] = trivyNoticeLayout + "\n"
	for index, shipped := range trivyFixtureModules(t) {
		for _, notice := range trivyFixtureNotices(t, index, shipped) {
			files[directory+"/"+notice] = "upstream notice\n"
		}
	}
}

func trivyFixtureNotices(t *testing.T, index int, shipped module.Version) []string {
	t.Helper()
	escaped, err := module.EscapePath(shipped.Path)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "licenses/" + escaped + "@" + shipped.Version + "/"
	switch index {
	case 0:
		return []string{prefix + "LICENSE", prefix + "internal/third_party/forked/LICENSE.txt", prefix + "zstd/internal/xxhash/NOTICE.txt"}
	case 1:
		return []string{prefix + "APACHE-LICENSE-2.0", prefix + "LICENSES/BSD-3-Clause.txt", prefix + "3rdpartylicenses.md"}
	case 2:
		return []string{prefix + "GO-LICENSE", prefix + "schema/cvss-v3.1.json.license"}
	default:
		return []string{prefix + "LICENSE"}
	}
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
	if len(modules) < 3 {
		t.Fatalf("the fixture binary records only %d shipped modules", len(modules))
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
		var wanted []string
		for _, notice := range trivyFixtureNotices(t, index, shipped) {
			wanted = append(wanted, directory+"/"+notice)
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
	first := directory + "/" + trivyFixtureNotices(t, 0, trivyFixtureModules(t)[0])[0]
	for _, test := range []struct {
		name    string
		change  func(map[string]string)
		problem string
	}{
		{"stub binary", func(files map[string]string) { files[directory+"/trivy"] = "not a Go binary" }, "build information"},
		{"nested license only", func(files map[string]string) { delete(files, first) }, "has no root license notice"},
		{"root Go source named for a license", func(files map[string]string) {
			delete(files, first)
			files[strings.TrimSuffix(first, "LICENSE")+"license.go"] = "package license\n"
		}, "has no root license notice"},
		{"missing notice layout", func(files map[string]string) { delete(files, directory+"/notices-layout") }, "notices are incomplete"},
		{"earlier notice layout", func(files map[string]string) { files[directory+"/notices-layout"] = "code-polishy-trivy-notices-v1\n" }, "use layout"},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := map[string]string{BinaryPath: "fixture"}
			addTrivyReleaseFixture(t, files, host)
			test.change(files)
			root, manifest := installedRelease(t, files, nil)
			if _, err := renderReleaseSBOM(manifest, root, "release.zip", exampleDigest); err == nil || !strings.Contains(err.Error(), test.problem) {
				t.Fatalf("SBOM error = %v, want %q", err, test.problem)
			}
		})
	}
}
