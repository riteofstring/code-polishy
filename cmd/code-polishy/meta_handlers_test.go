package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallBundleMetaValidatesItsCompleteBoundary(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		want      int
	}{
		{name: "unknown-option", arguments: []string{"--unknown"}, want: 2},
		{name: "positional", arguments: []string{"--source", "archive", "--sha256", "digest", "--prefix", "prefix", "extra"}, want: 2},
		{name: "missing-source", arguments: []string{"--sha256", "digest", "--prefix", "prefix"}, want: 2},
		{name: "missing-digest", arguments: []string{"--source", "archive", "--prefix", "prefix"}, want: 2},
		{name: "missing-prefix", arguments: []string{"--source", "archive", "--sha256", "digest"}, want: 2},
		{name: "missing-archive", arguments: []string{"--source", filepath.Join(t.TempDir(), "missing.zip"), "--sha256", strings.Repeat("0", 64), "--prefix", t.TempDir()}, want: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := handleInstallBundleMeta(invocation{arguments: test.arguments}); got != test.want {
				t.Fatalf("status = %d, want %d", got, test.want)
			}
		})
	}
}

func TestReleaseManifestWriteValidatesEveryModeSpecificField(t *testing.T) {
	revision := strings.Repeat("a", 40)
	tests := []releaseManifestOptions{
		{revision: revision},
		{root: t.TempDir()},
		{root: t.TempDir(), revision: revision, source: "source"},
		{root: t.TempDir(), revision: revision, destination: "destination"},
	}
	for _, options := range tests {
		if got := writeReleaseManifest(options); got != 2 {
			t.Fatalf("options = %+v, status = %d", options, got)
		}
	}
	if got := writeReleaseManifest(releaseManifestOptions{root: filepath.Join(t.TempDir(), "missing"), revision: revision}); got != 2 {
		t.Fatalf("missing release tree status = %d", got)
	}
}

func TestReleaseManifestMaterializeValidatesEveryModeSpecificField(t *testing.T) {
	tests := []releaseManifestOptions{
		{destination: filepath.Join(t.TempDir(), "destination")},
		{source: t.TempDir()},
		{source: t.TempDir(), destination: filepath.Join(t.TempDir(), "destination"), root: "root"},
		{source: t.TempDir(), destination: filepath.Join(t.TempDir(), "destination"), revision: "revision"},
	}
	for _, options := range tests {
		if got := materializeReleaseManifest(options); got != 2 {
			t.Fatalf("options = %+v, status = %d", options, got)
		}
	}
	if got := materializeReleaseManifest(releaseManifestOptions{
		source: filepath.Join(t.TempDir(), "missing"), destination: filepath.Join(t.TempDir(), "destination"),
	}); got != 2 {
		t.Fatalf("missing source status = %d", got)
	}
}

func TestReleaseManifestSatisfiesLockValidatesEveryModeSpecificField(t *testing.T) {
	if got := satisfiesLockReleaseManifest(t.TempDir(), releaseManifestOptions{}); got != 2 {
		t.Fatalf("missing root status = %d", got)
	}
	if got := satisfiesLockReleaseManifest(t.TempDir(), releaseManifestOptions{root: "root", source: "source"}); got != 2 {
		t.Fatalf("cross-mode field status = %d", got)
	}
}

func TestReleaseManifestMetaRejectsAmbiguousInvocations(t *testing.T) {
	tests := []invocation{
		{configPath: "custom.json", arguments: []string{"verify"}},
		{},
		{arguments: []string{"unknown"}},
		{arguments: []string{"verify", "--unknown"}},
		{arguments: []string{"verify", "extra"}},
		{arguments: []string{"archive", "--root", "root"}},
		{arguments: []string{"publish", "--archive", "archive"}},
		{arguments: []string{"index", "--output", "index.json"}},
		{arguments: []string{"oci-context", "--descriptor", "release.json", "--template", "Containerfile"}},
	}
	for _, invocation := range tests {
		if got := handleReleaseManifestMeta(invocation); got != 2 {
			t.Fatalf("invocation = %+v, status = %d", invocation, got)
		}
	}
}

func TestReleaseManifestPublicationModesRejectCrossModeFields(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(releaseManifestOptions) int
		value  releaseManifestOptions
	}{
		{name: "archive", invoke: archiveReleaseManifest, value: releaseManifestOptions{root: "root", output: "archive.zip", source: "source"}},
		{name: "publish", invoke: publishReleaseManifest, value: releaseManifestOptions{archive: "archive.zip", destination: "published", root: "root"}},
		{name: "index", invoke: indexReleaseManifest, value: releaseManifestOptions{output: "index.json", descriptors: []string{"release.json"}, archive: "archive.zip"}},
		{name: "oci-context", invoke: ociContextReleaseManifest, value: releaseManifestOptions{descriptor: "release.json", template: "Containerfile", destination: "context", output: "archive.zip"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if status := test.invoke(test.value); status != 2 {
				t.Fatalf("status = %d", status)
			}
		})
	}
}
