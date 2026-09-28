package supplychain

import (
	"path/filepath"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func isOSVPackageSourceInput(repo repository.Repository, path string) bool {
	if repo.IsDependencyInput(path) {
		return true
	}
	normalized := filepath.ToSlash(path)
	name := strings.ToLower(filepath.Base(normalized))
	if name == "bom.json" || name == "bom.xml" || strings.HasSuffix(name, ".cdx.json") || strings.HasSuffix(name, ".cdx.xml") {
		return true
	}
	if spdxPackageSource(name) || systemPackageSource(normalized) {
		return true
	}
	return vendoredPackageSource(normalized)
}

func spdxPackageSource(name string) bool {
	if strings.HasSuffix(name, ".spdx") {
		return true
	}
	if !strings.Contains(name, ".spdx.") {
		return false
	}
	for _, suffix := range []string{".json", ".yml", ".rdf", ".xml"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func systemPackageSource(path string) bool {
	for _, suffix := range []string{"lib/apk/db/installed", "var/lib/apk/db/installed", "usr/lib/apk/db/installed", "var/lib/dpkg/status", "usr/lib/opkg/status"} {
		if path == suffix || strings.HasSuffix(path, "/"+suffix) {
			return true
		}
	}
	statusDirectory := strings.HasPrefix(path, "var/lib/dpkg/status.d/") || strings.Contains(path, "/var/lib/dpkg/status.d/")
	return statusDirectory && !strings.HasSuffix(path, ".md5sums")
}

func vendoredPackageSource(path string) bool {
	parts := strings.Split(strings.TrimPrefix(path, "./"), "/")
	for index, part := range parts {
		if index+2 >= len(parts) {
			continue
		}
		switch part {
		case "3rdparty", "dep", "deps", "thirdparty", "third-party", "third_party", "libs", "external", "externals", "vendor", "vendored":
			return true
		}
	}
	return false
}
