package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maximumPublicationIndexBytes int64 = 4 << 20
const maximumReleaseURLBytes = 4096

type IndexedRelease struct {
	Lock     Lock
	Manifest Manifest
	Root     string
	Index    PublicationIndex
}

func InstallIndexedRelease(ctx context.Context, indexURL, indexSHA256, prefix string) (IndexedRelease, error) {
	return installIndexedRelease(ctx, releaseHTTPClient(), indexURL, indexSHA256, prefix)
}

func releaseHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			_, err := parseReleaseHTTPSURL(request.URL.String())
			return err
		},
	}
}

func installIndexedRelease(ctx context.Context, client *http.Client, indexURL, indexSHA256, prefix string) (IndexedRelease, error) {
	parsedIndexURL, index, err := acquirePublicationIndex(ctx, client, indexURL, indexSHA256)
	if err != nil {
		return IndexedRelease{}, err
	}
	manifest, canonicalPrefix, err := installPublicationHost(ctx, client, parsedIndexURL, index, prefix)
	if err != nil {
		return IndexedRelease{}, err
	}
	lock, err := publishedLockFor(manifest, indexURL, indexSHA256, index)
	if err != nil {
		return IndexedRelease{}, err
	}
	return IndexedRelease{Lock: lock, Manifest: manifest, Root: Directory(canonicalPrefix, lock), Index: index}, nil
}

func resolvePublishedLock(ctx context.Context, client *http.Client, indexURL, indexSHA256 string, manifest Manifest) (Lock, error) {
	_, index, err := acquirePublicationIndex(ctx, client, indexURL, indexSHA256)
	if err != nil {
		return Lock{}, err
	}
	return publishedLockFor(manifest, indexURL, indexSHA256, index)
}

func acquirePublicationIndex(ctx context.Context, client *http.Client, indexURL, indexSHA256 string) (*url.URL, PublicationIndex, error) {
	if !digestPattern.MatchString(indexSHA256) {
		return nil, PublicationIndex{}, errors.New("release index requires an exact SHA-256 digest")
	}
	parsedIndexURL, err := parseReleaseHTTPSURL(indexURL)
	if err != nil {
		return nil, PublicationIndex{}, fmt.Errorf("release index URL: %w", err)
	}
	indexData, err := downloadReleaseBytes(ctx, client, parsedIndexURL, maximumPublicationIndexBytes)
	if err != nil {
		return nil, PublicationIndex{}, err
	}
	if digestBytes(indexData) != indexSHA256 {
		return nil, PublicationIndex{}, errors.New("release index checksum mismatch")
	}
	index, err := ParsePublicationIndex(indexData, indexURL)
	if err != nil {
		return nil, PublicationIndex{}, err
	}
	return parsedIndexURL, index, nil
}

func installPublicationHost(ctx context.Context, client *http.Client, indexURL *url.URL, index PublicationIndex, prefix string) (Manifest, string, error) {
	host, err := Host()
	if err != nil {
		return Manifest{}, "", err
	}
	artifact, err := publicationArtifactForHost(index, host)
	if err != nil {
		return Manifest{}, "", err
	}
	archiveURL, err := resolvePublicationURL(indexURL, artifact.Archive.Name)
	if err != nil {
		return Manifest{}, "", err
	}
	canonicalPrefix, err := prepareInstallPrefix(prefix)
	if err != nil {
		return Manifest{}, "", err
	}
	archive, err := downloadReleaseArchive(ctx, client, archiveURL, artifact.Archive, canonicalPrefix)
	if err != nil {
		return Manifest{}, "", err
	}
	defer os.Remove(archive)
	manifest, err := InstallLocalBundle(archive, artifact.Archive.SHA256, canonicalPrefix)
	if err != nil {
		return Manifest{}, "", err
	}
	return manifest, canonicalPrefix, nil
}

func ParsePublicationIndex(data []byte, source string) (PublicationIndex, error) {
	if len(data) == 0 || int64(len(data)) > maximumPublicationIndexBytes {
		return PublicationIndex{}, errors.New("release index exceeds its byte bound")
	}
	var index PublicationIndex
	if err := decodeExactly(data, source, &index); err != nil {
		return PublicationIndex{}, err
	}
	canonical, err := renderJSON(index)
	if err != nil || !bytes.Equal(canonical, data) {
		return PublicationIndex{}, errors.New("release index is not canonical")
	}
	if index.IndexVersion != PublicationVersion || len(index.Artifacts) != len(supportedReleaseHosts) {
		return PublicationIndex{}, errors.New("release index has an unsupported version or host set")
	}
	if err := validatePublicationIndexIdentity(index); err != nil {
		return PublicationIndex{}, err
	}
	return index, nil
}

func validatePublicationIndexIdentity(index PublicationIndex) error {
	for _, artifact := range index.Artifacts {
		if err := validatePublicationArtifact(artifact); err != nil {
			return err
		}
	}
	version, revision, err := validatePublicationSet(index.Artifacts)
	if err != nil {
		return err
	}
	if index.CodePolishyVersion != version || index.SourceRevision != revision {
		return errors.New("release index identity does not match its artifacts")
	}
	releaseDigest := index.Artifacts[0].Manifest.ReleaseDigest
	for _, artifact := range index.Artifacts[1:] {
		if artifact.Manifest.ReleaseDigest != releaseDigest {
			return errors.New("release index artifacts do not name one release digest")
		}
	}
	return nil
}

func publishedLockFor(manifest Manifest, indexURL, indexSHA256 string, index PublicationIndex) (Lock, error) {
	indexData, err := renderJSON(index)
	if err != nil {
		return Lock{}, err
	}
	index, err = ParsePublicationIndex(indexData, "publication index")
	if err != nil {
		return Lock{}, err
	}
	parsedIndexURL, err := parseReleaseHTTPSURL(indexURL)
	if err != nil {
		return Lock{}, err
	}
	if !publishedIndexMatchesManifest(index, indexData, indexSHA256, manifest) {
		return Lock{}, errors.New("publication index does not match the installed release")
	}
	archives, err := lockedPublicationArchives(parsedIndexURL, index, manifest)
	if err != nil {
		return Lock{}, err
	}
	lock := Lock{
		LockVersion: LockVersion, CodePolishyVersion: manifest.CodePolishyVersion,
		ReleaseDigest: manifest.ReleaseDigest, Features: append([]string{}, manifest.Features...),
		Publication: &LockPublication{IndexURL: indexURL, IndexSHA256: indexSHA256, Archives: archives},
	}
	if _, err := parseLock(RenderLock(lock), LockFilename); err != nil {
		return Lock{}, err
	}
	return lock, nil
}

func publishedIndexMatchesManifest(index PublicationIndex, indexData []byte, indexSHA256 string, manifest Manifest) bool {
	return digestPattern.MatchString(indexSHA256) && digestBytes(indexData) == indexSHA256 &&
		index.CodePolishyVersion == manifest.CodePolishyVersion && index.SourceRevision == manifest.SourceRevision
}

func lockedPublicationArchives(indexURL *url.URL, index PublicationIndex, manifest Manifest) ([]LockedArchive, error) {
	archives := make([]LockedArchive, 0, len(index.Artifacts))
	for _, artifact := range index.Artifacts {
		if artifact.Manifest.ReleaseDigest != manifest.ReleaseDigest {
			return nil, errors.New("publication index archive does not match the installed release digest")
		}
		archiveURL, err := resolvePublicationURL(indexURL, artifact.Archive.Name)
		if err != nil {
			return nil, err
		}
		archives = append(archives, LockedArchive{
			Host: artifact.Host, URL: archiveURL.String(), SHA256: artifact.Archive.SHA256, Size: artifact.Archive.Size,
		})
	}
	return archives, nil
}

func parseReleaseHTTPSURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) == 0 || len(raw) > maximumReleaseURLBytes || strings.ContainsAny(raw, "\\\"\x00\r\n\t ") || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" {
		return nil, errors.New("release acquisition requires an HTTPS URL without credentials or a fragment")
	}
	return parsed, nil
}

func resolvePublicationURL(indexURL *url.URL, name string) (*url.URL, error) {
	reference, err := url.Parse(name)
	if err != nil {
		return nil, err
	}
	resolved := indexURL.ResolveReference(reference)
	return parseReleaseHTTPSURL(resolved.String())
}

func publicationArtifactForHost(index PublicationIndex, host string) (PublicationArtifact, error) {
	for _, artifact := range index.Artifacts {
		if artifact.Host == host {
			return artifact, nil
		}
	}
	return PublicationArtifact{}, fmt.Errorf("release index has no archive for %s", host)
}

func downloadReleaseBytes(ctx context.Context, client *http.Client, source *url.URL, maximum int64) ([]byte, error) {
	response, err := requestRelease(ctx, client, source)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.ContentLength > maximum {
		return nil, errors.New("release download exceeds its byte bound")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, errors.New("release download exceeds its byte bound")
	}
	return data, nil
}

func downloadReleaseArchive(ctx context.Context, client *http.Client, source *url.URL, artifact PublishedFile, prefix string) (string, error) {
	response, err := requestRelease(ctx, client, source)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.ContentLength >= 0 && response.ContentLength != artifact.Size {
		return "", errors.New("release archive size does not match its publication index")
	}
	temporary, err := os.CreateTemp(prefix, ".code-polishy-download-*.zip")
	if err != nil {
		return "", err
	}
	path := temporary.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, digest), io.LimitReader(response.Body, artifact.Size+1))
	closeErr := temporary.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return "", err
	}
	if written != artifact.Size || hex.EncodeToString(digest.Sum(nil)) != artifact.SHA256 {
		return "", errors.New("release archive does not match its publication index")
	}
	keep = true
	return filepath.Clean(path), nil
}

func requestRelease(ctx context.Context, client *http.Client, source *url.URL) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if _, err := parseReleaseHTTPSURL(response.Request.URL.String()); err != nil {
		response.Body.Close()
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("release download returned HTTP %d", response.StatusCode)
	}
	return response, nil
}
