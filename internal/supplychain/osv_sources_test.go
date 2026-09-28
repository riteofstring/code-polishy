package supplychain

import (
	"testing"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestOSVPackageSourcesCoverDefaultInventoryExtractors(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{}
	for _, path := range []string{
		"inventory.cdx.json",
		"bom.xml",
		"inventory.spdx",
		"inventory.spdx.json",
		"vendor/zlib/source.c",
		"src/third_party/library/header.h",
		"lib/apk/db/installed",
		"var/lib/dpkg/status",
		"var/lib/dpkg/status.d/package",
	} {
		if !isOSVPackageSourceInput(repo, path) {
			t.Errorf("isOSVPackageSourceInput(%q) = false", path)
		}
	}
}

func TestOSVPackageSourcesRemainExact(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{}
	for _, path := range []string{
		"inventory.cdx.yaml",
		"inventory.spdx.yaml",
		"vendor/source.c",
		"var/lib/dpkg/status.d/package.md5sums",
		"application.json",
	} {
		if isOSVPackageSourceInput(repo, path) {
			t.Errorf("isOSVPackageSourceInput(%q) = true", path)
		}
	}
}
