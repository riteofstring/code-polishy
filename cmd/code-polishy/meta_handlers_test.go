package main

import (
	"os"
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

func TestGovernedEnvironmentMetaValidatesArgumentsBeforeOpeningRepository(t *testing.T) {
	repoRoot := t.TempDir()
	tests := []struct {
		name      string
		arguments []string
		want      int
	}{
		{name: "unknown-option", arguments: []string{"--unknown"}, want: 2},
		{name: "missing-output", arguments: nil, want: 2},
		{name: "positional", arguments: []string{"--output", filepath.Join(t.TempDir(), "receipt.json"), "extra"}, want: 2},
		{name: "invalid-repository", arguments: []string{"--output", filepath.Join(t.TempDir(), "receipt.json")}, want: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := handleGovernedEnvironmentMeta(invocation{
				repoRoot: repoRoot, policyRoot: repoRoot, arguments: test.arguments,
			})
			if got != test.want {
				t.Fatalf("status = %d, want %d", got, test.want)
			}
		})
	}
}

func TestWritePrivateFileCommitsCompletePrivateContent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "receipt.json")
	if err := writePrivateFile(path, []byte("receipt\n")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "receipt\n" {
		t.Fatalf("content = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file remains: %v", err)
	}
}

func TestWritePrivateFileRejectsInvalidDestinations(t *testing.T) {
	if err := writePrivateFile("", []byte("receipt")); err == nil {
		t.Fatal("empty output path was accepted")
	}
	root := t.TempDir()
	if err := writePrivateFile(filepath.Join(root, "missing", "receipt.json"), []byte("receipt")); err == nil {
		t.Fatal("missing parent was accepted")
	}
	destination := filepath.Join(root, "directory")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(destination, []byte("receipt")); err == nil {
		t.Fatal("directory destination was accepted")
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
