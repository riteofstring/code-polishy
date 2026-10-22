package release

import (
	"debug/buildinfo"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	cdx "github.com/CycloneDX/cyclonedx-go"
	"golang.org/x/mod/module"
)

var trivyLicenseNotice = regexp.MustCompile(`(?i)^(licen[cs]e|copying)`)

func trivyReleaseDirectory(host string) (string, error) {
	switch {
	case strings.HasSuffix(host, "-arm64"):
		return ".tools/trivy/linux-arm64", nil
	case strings.HasSuffix(host, "-x64"):
		return ".tools/trivy/linux-amd64", nil
	default:
		return "", fmt.Errorf("carried Trivy has no Linux platform for release host %q", host)
	}
}

func (inventory *releaseSBOMInventory) addTrivyNotices(root string, manifest Manifest) error {
	if manifest.Tools.Trivy == "" {
		return nil
	}
	directory, err := trivyReleaseDirectory(manifest.Host)
	if err != nil {
		return err
	}
	binary := directory + "/trivy"
	if !manifestRecordsCapabilityFile(manifest, binary, "") {
		return fmt.Errorf("carried Trivy is absent from the release manifest: %s", binary)
	}
	info, err := buildinfo.ReadFile(filepath.Join(root, filepath.FromSlash(binary)))
	if err != nil {
		return fmt.Errorf("read carried Trivy build information: %w", err)
	}
	for _, shipped := range shippedGoModules(info) {
		resolved := shipped
		if shipped.Replace != nil {
			resolved = shipped.Replace
		}
		if err := inventory.addTrivyModuleNotices(manifest, directory, resolved.Path, resolved.Version); err != nil {
			return err
		}
	}
	return nil
}

func (inventory *releaseSBOMInventory) addTrivyModuleNotices(manifest Manifest, directory, modulePath, version string) error {
	escaped, err := module.EscapePath(modulePath)
	if err != nil {
		return fmt.Errorf("carried Trivy module %s has an invalid path: %w", modulePath, err)
	}
	prefix := directory + "/licenses/" + escaped + "@" + version + "/"
	var properties []cdx.Property
	licensed := false
	for _, entry := range manifest.Entries {
		relative, found := strings.CutPrefix(entry.Path, prefix)
		if !found || entry.Symlink != "" || entry.SHA256 == "" || strings.Contains(relative, "/") {
			continue
		}
		licensed = licensed || trivyLicenseNotice.MatchString(path.Base(relative))
		properties = append(properties, cdx.Property{Name: "code-polishy:notice-path", Value: entry.Path})
	}
	if !licensed {
		return fmt.Errorf("carried Trivy module %s@%s has no license notice under %s", modulePath, version, prefix)
	}
	ref := "pkg:golang/" + modulePath + "@" + version
	component, found := inventory.components[ref]
	if !found {
		return fmt.Errorf("carried Trivy module %s@%s is absent from the SBOM inventory", modulePath, version)
	}
	component.Properties = &properties
	inventory.components[ref] = component
	return nil
}
