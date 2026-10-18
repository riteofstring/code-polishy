package artifactsecurity

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const archiveFixtureConfig = `{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}`

func TestDockerArchiveConfigurationIdentity(t *testing.T) {
	actual, err := dockerArchiveImageID(filepath.Join("testdata", "docker29-image.tar"), 1024*1024, 1024*1024)
	const expected = "sha256:0cf74d99b5a97c3748ef2e9f91839752b96b00d2427cb57f18919e8ed7d01ad6"
	if err != nil || actual != expected {
		t.Fatalf("Docker 29 archive configuration identity = %q, error = %v", actual, err)
	}
	archive := filepath.Join(t.TempDir(), "legacy.tar")
	payload := fixtureDockerArchive(t, []archiveFixtureEntry{
		{name: "manifest.json", body: `[{"Config":"image.json","Layers":[]}]`},
		{name: "image.json", body: archiveFixtureConfig},
	})
	if err := os.WriteFile(archive, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	actual, err = dockerArchiveImageID(archive, 1024*1024, 1024*1024)
	if err != nil || actual != "sha256:c5b1d63604f273462ef36fadac3182d43ae6a6138731cf594b314835cf1c034f" {
		t.Fatalf("Docker legacy archive configuration identity = %q, error = %v", actual, err)
	}
}

func TestDockerArchiveRejectsMissingAmbiguousAndUnboundedIdentity(t *testing.T) {
	cases := map[string][]archiveFixtureEntry{
		"missing manifest":              {{name: "image.json", body: archiveFixtureConfig}},
		"invalid manifest":              {{name: "manifest.json", body: "["}},
		"missing configuration":         {{name: "manifest.json", body: `[{"Config":"missing.json"}]`}},
		"empty configuration reference": {{name: "manifest.json", body: `[{}]`}},
		"multiple images":               {{name: "manifest.json", body: `[{"Config":"one.json"},{"Config":"two.json"}]`}},
		"duplicate manifest":            {{name: "manifest.json", body: "[]"}, {name: "manifest.json", body: "[]"}},
		"linked manifest":               {{name: "manifest.json", kind: tar.TypeSymlink}},
		"oversized manifest":            {{name: "manifest.json", body: strings.Repeat(" ", 64*1024+1)}},
		"duplicate configuration": {
			{name: "manifest.json", body: `[{"Config":"image.json"}]`},
			{name: "image.json", body: archiveFixtureConfig}, {name: "image.json", body: archiveFixtureConfig},
		},
		"linked configuration": {
			{name: "manifest.json", body: `[{"Config":"image.json"}]`}, {name: "image.json", kind: tar.TypeLink},
		},
		"oversized configuration": {
			{name: "manifest.json", body: `[{"Config":"image.json"}]`}, {name: "image.json", body: strings.Repeat(" ", 1024*1024+1)},
		},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "image.tar")
			if err := os.WriteFile(archive, fixtureDockerArchive(t, entries), 0o600); err != nil {
				t.Fatal(err)
			}
			if identity, err := dockerArchiveImageID(archive, 2*1024*1024, 64*1024); err == nil {
				t.Fatalf("malformed archive accepted with identity %q", identity)
			}
		})
	}
	if _, err := dockerArchiveImageID(filepath.Join("testdata", "docker29-image.tar"), 1, 1024*1024); err == nil {
		t.Fatal("oversized archive was accepted")
	}
}

func TestRunRejectsScannerConfigurationMismatch(t *testing.T) {
	root := t.TempDir()
	policyRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.Repository{
		Root: root, PolicyRoot: policyRoot,
		Config: policy.Config{SupplyChain: policy.SupplyChain{ArtifactSecurity: policy.ArtifactSecurity{
			Targets: []policy.ArtifactTarget{{Name: "agent", Mode: "archive", Archive: "agent.tar"}},
		}}},
	}
	boundary := newScannerBoundary(t, policyRoot)
	boundary.imageID = "sha256:" + strings.Repeat("c", 64)
	result := Run(t.Context(), repo, boundary)
	if len(result.Findings) != 1 || result.Findings[0].Subject != "code-polishy-scanner:self-scan" ||
		!strings.Contains(result.Findings[0].Message, "does not match") {
		t.Fatalf("wrong scanner configuration identity was not refused: %+v", result)
	}
}

type archiveFixtureEntry struct {
	name string
	body string
	kind byte
}

func fixtureDockerArchive(t *testing.T, entries []archiveFixtureEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o600, Size: int64(len(entry.body)), Typeflag: entry.kind}
		if entry.kind == tar.TypeSymlink || entry.kind == tar.TypeLink {
			header.Linkname = "image.json"
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
