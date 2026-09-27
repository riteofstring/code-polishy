package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallIndexedReleaseDownloadsAndLocksTheExactHostArchive(t *testing.T) {
	releaseRoot, manifest := installedRelease(t, map[string]string{BinaryPath: "engine", LauncherBinaryPath: "launcher"}, nil)
	archive := zipDirectory(t, releaseRoot)
	archiveData, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	archiveDigest := sha256.Sum256(archiveData)
	archiveSHA := hex.EncodeToString(archiveDigest[:])
	index := publicationIndexFixture(manifest, archiveSHA, int64(len(archiveData)))
	indexData, err := renderJSON(index)
	if err != nil {
		t.Fatal(err)
	}
	indexDigest := sha256.Sum256(indexData)
	indexSHA := hex.EncodeToString(indexDigest[:])
	host, err := Host()
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := publicationArtifactForHost(index, host)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/release-index.json":
			_, _ = response.Write(indexData)
		case "/" + artifact.Archive.Name:
			_, _ = response.Write(archiveData)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	prefix := filepath.Join(t.TempDir(), "prefix")
	installed, err := installIndexedRelease(context.Background(), server.Client(), server.URL+"/release-index.json", indexSHA, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Lock.LockVersion != LockVersion || installed.Lock.Publication == nil ||
		len(installed.Lock.Publication.Archives) != len(supportedReleaseHosts) || installed.Manifest.ReleaseDigest != manifest.ReleaseDigest {
		t.Fatalf("indexed release = %+v", installed)
	}
	if err := installed.Manifest.Verify(installed.Root); err != nil {
		t.Fatal(err)
	}
	assertWrittenHostArchive(t, installed.Lock, host, server.URL+"/"+artifact.Archive.Name, archiveSHA, int64(len(archiveData)))
}

func TestInstallIndexedReleaseReusesTheVerifiedInstalledHost(t *testing.T) {
	releaseRoot, manifest := installedRelease(t, map[string]string{BinaryPath: "engine", LauncherBinaryPath: "launcher"}, nil)
	index := publicationIndexFixture(manifest, exampleDigest, 1)
	indexData, err := renderJSON(index)
	if err != nil {
		t.Fatal(err)
	}
	indexDigest := sha256.Sum256(indexData)
	prefix := filepath.Join(t.TempDir(), "prefix")
	installPublicationFixture(t, releaseRoot, prefix, manifest)
	archiveRequests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/release-index.json" {
			_, _ = response.Write(indexData)
			return
		}
		archiveRequests++
		http.Error(response, "archive must not be requested", http.StatusInternalServerError)
	}))
	defer server.Close()
	installed, err := installIndexedRelease(context.Background(), server.Client(), server.URL+"/release-index.json", hex.EncodeToString(indexDigest[:]), prefix)
	if err != nil {
		t.Fatal(err)
	}
	canonicalPrefix, err := filepath.EvalSymlinks(prefix)
	if err != nil {
		t.Fatal(err)
	}
	if archiveRequests != 0 || installed.Root != DirectoryForManifest(canonicalPrefix, manifest) {
		t.Fatalf("archive requests = %d, installed root = %s", archiveRequests, installed.Root)
	}
	if err := installed.Manifest.Verify(installed.Root); err != nil {
		t.Fatal(err)
	}
}

func TestInstallIndexedReleaseRejectsCorruptInstalledHostWithoutDownloading(t *testing.T) {
	releaseRoot, manifest := installedRelease(t, map[string]string{BinaryPath: "engine", LauncherBinaryPath: "launcher"}, nil)
	index := publicationIndexFixture(manifest, exampleDigest, 1)
	indexData, err := renderJSON(index)
	if err != nil {
		t.Fatal(err)
	}
	indexDigest := sha256.Sum256(indexData)
	prefix := filepath.Join(t.TempDir(), "prefix")
	target := installPublicationFixture(t, releaseRoot, prefix, manifest)
	if err := os.WriteFile(filepath.Join(target, filepath.FromSlash(BinaryPath)), []byte("changed"), 0o755); err != nil {
		t.Fatal(err)
	}
	archiveRequests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/release-index.json" {
			_, _ = response.Write(indexData)
			return
		}
		archiveRequests++
		http.Error(response, "archive must not be requested", http.StatusInternalServerError)
	}))
	defer server.Close()
	_, err = installIndexedRelease(context.Background(), server.Client(), server.URL+"/release-index.json", hex.EncodeToString(indexDigest[:]), prefix)
	if err == nil || !strings.Contains(err.Error(), "is not the file") || archiveRequests != 0 {
		t.Fatalf("corrupt installed release: archive requests=%d err=%v", archiveRequests, err)
	}
}

func TestReleaseHTTPClientDoesNotImposeAWholeTransferDeadline(t *testing.T) {
	client := releaseHTTPClient()
	deadline := false
	client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		_, deadline = request.Context().Deadline()
		return &http.Response{
			StatusCode: http.StatusOK,
			Request:    request,
			Body:       io.NopCloser(strings.NewReader("release")),
		}, nil
	})
	source, err := parseReleaseHTTPSURL("https://example.invalid/release.zip")
	if err != nil {
		t.Fatal(err)
	}
	response, err := requestRelease(context.Background(), client, source)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if deadline {
		t.Fatal("release transfer received a whole-request deadline")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func installPublicationFixture(t *testing.T, releaseRoot, prefix string, manifest Manifest) string {
	t.Helper()
	target := DirectoryForManifest(prefix, manifest)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(releaseRoot, target); err != nil {
		t.Fatal(err)
	}
	return target
}

func assertWrittenHostArchive(t *testing.T, lock Lock, host, archiveURL, archiveSHA string, archiveSize int64) {
	t.Helper()
	repoRoot := t.TempDir()
	if err := WriteLock(repoRoot, lock); err != nil {
		t.Fatal(err)
	}
	written, present, err := ReadLock(repoRoot)
	if err != nil || !present || written.Publication == nil {
		t.Fatalf("written lock: present=%v err=%v lock=%+v", present, err, written)
	}
	var lockedArchive LockedArchive
	for _, candidate := range written.Publication.Archives {
		if candidate.Host == host {
			lockedArchive = candidate
			break
		}
	}
	if lockedArchive.URL != archiveURL ||
		lockedArchive.SHA256 != archiveSHA ||
		lockedArchive.Size != archiveSize {
		t.Fatalf("locked host archive = %+v", lockedArchive)
	}
}

func TestWritePublishedReleaseLockCreatesArchiveBackedAdoptionAuthority(t *testing.T) {
	releaseRoot, manifest := installedRelease(t, map[string]string{BinaryPath: "engine", LauncherBinaryPath: "launcher"}, nil)
	archiveData, err := os.ReadFile(zipDirectory(t, releaseRoot))
	if err != nil {
		t.Fatal(err)
	}
	archiveDigest := sha256.Sum256(archiveData)
	index := publicationIndexFixture(manifest, hex.EncodeToString(archiveDigest[:]), int64(len(archiveData)))
	indexData, err := renderJSON(index)
	if err != nil {
		t.Fatal(err)
	}
	indexDigest := sha256.Sum256(indexData)
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(indexData)
	}))
	defer server.Close()
	repoRoot := t.TempDir()
	result, err := writePublishedReleaseLock(context.Background(), server.Client(), repoRoot, releaseRoot, server.URL+"/release-index.json", hex.EncodeToString(indexDigest[:]))
	if err != nil {
		t.Fatal(err)
	}
	written, present, err := ReadLock(repoRoot)
	if err != nil || !present || !bytes.Equal(RenderLock(written), RenderLock(result.Lock)) || written.LockVersion != LockVersion {
		t.Fatalf("published adoption lock: present=%v err=%v lock=%+v", present, err, written)
	}
}

func TestInstallIndexedReleaseRejectsAnUnpinnedIndex(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("{}\n"))
	}))
	defer server.Close()
	_, err := installIndexedRelease(context.Background(), server.Client(), server.URL+"/release-index.json", strings.Repeat("0", 64), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("unpinned index error = %v", err)
	}
}

func publicationIndexFixture(manifest Manifest, archiveSHA string, archiveSize int64) PublicationIndex {
	artifacts := make([]PublicationArtifact, 0, len(supportedReleaseHosts))
	for _, host := range supportedReleaseHosts {
		base := "code-polishy-" + manifest.CodePolishyVersion + "-" + host
		artifacts = append(artifacts, PublicationArtifact{
			DescriptorVersion: PublicationVersion, CodePolishyVersion: manifest.CodePolishyVersion,
			SourceRevision: manifest.SourceRevision, Host: host,
			Archive: PublishedFile{Name: base + ".zip", SHA256: archiveSHA, Size: archiveSize},
			Manifest: PublishedManifest{
				PublishedFile: PublishedFile{Name: base + ".release-manifest.json", SHA256: exampleDigest, Size: 1},
				ReleaseDigest: manifest.ReleaseDigest, ContentDigest: manifest.ContentDigest,
			},
			SBOM: PublishedFile{Name: base + ".sbom.cdx.json", SHA256: exampleDigest, Size: 1},
			DeterministicMetadata: PublishedFile{
				Name: base + ".build-metadata.intoto.json", SHA256: exampleDigest, Size: 1,
			},
		})
	}
	return PublicationIndex{
		IndexVersion: PublicationVersion, CodePolishyVersion: manifest.CodePolishyVersion,
		SourceRevision: manifest.SourceRevision, Artifacts: artifacts,
	}
}
