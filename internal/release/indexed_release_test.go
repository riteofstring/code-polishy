package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	repoRoot := t.TempDir()
	if err := WriteLock(repoRoot, installed.Lock); err != nil {
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
	if lockedArchive.URL != server.URL+"/"+artifact.Archive.Name ||
		lockedArchive.SHA256 != archiveSHA ||
		lockedArchive.Size != int64(len(archiveData)) {
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
