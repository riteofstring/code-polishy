package release

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestReleaseArchiveIsDeterministic(t *testing.T) {
	root, _ := installedRelease(t, map[string]string{BinaryPath: "engine", LauncherBinaryPath: "launcher"}, nil)
	first := filepath.Join(t.TempDir(), "first.zip")
	second := filepath.Join(t.TempDir(), "second.zip")
	firstDigest, err := WriteArchive(root, first)
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := WriteArchive(root, second)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstDigest != secondDigest || !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("identical release trees produced different archives")
	}
}

func TestPublicationRecordsAndInstallsTheVerifiedTree(t *testing.T) {
	publication, artifact, manifest := publicationFixture(t, true)
	if artifact.CodePolishyVersion != manifest.CodePolishyVersion || artifact.SourceRevision != manifest.SourceRevision ||
		artifact.Host != manifest.Host || artifact.Manifest.ReleaseDigest != manifest.ReleaseDigest {
		t.Fatalf("publication = %+v", artifact)
	}
	descriptor := filepath.Join(publication, "code-polishy-"+manifest.CodePolishyVersion+"-"+manifest.Host+".release.json")
	data, err := os.ReadFile(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	var recorded PublicationArtifact
	if err := json.Unmarshal(data, &recorded); err != nil || recorded.Archive != artifact.Archive {
		t.Fatalf("descriptor = %+v, error = %v", recorded, err)
	}
	checksum, err := os.ReadFile(filepath.Join(publication, artifact.Archive.Name+".sha256"))
	if err != nil || string(checksum) != artifact.Archive.SHA256+"  "+artifact.Archive.Name+"\n" {
		t.Fatalf("checksum = %q, error = %v", checksum, err)
	}
	prefix := filepath.Join(t.TempDir(), "prefix")
	installed, err := InstallLocalBundle(filepath.Join(publication, artifact.Archive.Name), artifact.Archive.SHA256, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if installed.ReleaseDigest != manifest.ReleaseDigest {
		t.Fatalf("installed release = %s", installed.ReleaseDigest)
	}
	if runtime.GOOS != "windows" {
		target := Directory(prefix, LockFor(manifest))
		link, err := os.Readlink(filepath.Join(target, "share", "current"))
		if err != nil || link != "payload" {
			t.Fatalf("installed link = %q, error = %v", link, err)
		}
	}
}

func publicationFixture(t *testing.T, includeLink bool) (string, PublicationArtifact, Manifest) {
	t.Helper()
	files := map[string]string{BinaryPath: "engine", LauncherBinaryPath: "launcher", "share/payload": "payload"}
	links := map[string]string{}
	if includeLink && runtime.GOOS != "windows" {
		links["share/current"] = "payload"
	}
	root, manifest := installedRelease(t, files, links)
	archive := filepath.Join(t.TempDir(), "release.zip")
	if _, err := WriteArchive(root, archive); err != nil {
		t.Fatal(err)
	}
	publication := filepath.Join(t.TempDir(), "published")
	artifact, err := PublishArchive(archive, publication)
	if err != nil {
		t.Fatal(err)
	}
	return publication, artifact, manifest
}

func TestPublicationRejectsTamperedSidecarsAndNonLinuxOCIContext(t *testing.T) {
	root, manifest := installedRelease(t, map[string]string{BinaryPath: "engine", LauncherBinaryPath: "launcher"}, nil)
	archive := filepath.Join(t.TempDir(), "release.zip")
	if _, err := WriteArchive(root, archive); err != nil {
		t.Fatal(err)
	}
	publication := filepath.Join(t.TempDir(), "published")
	artifact, err := PublishArchive(archive, publication)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := filepath.Join(publication, "code-polishy-"+manifest.CodePolishyVersion+"-"+manifest.Host+".release.json")
	template := filepath.Join(t.TempDir(), "Containerfile")
	if err := os.WriteFile(template, []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" {
		context := filepath.Join(t.TempDir(), "context")
		prepared, err := PrepareOCIContext(descriptor, template, context)
		if err != nil {
			t.Fatal(err)
		}
		if prepared.ReleaseDigest != manifest.ReleaseDigest {
			t.Fatalf("prepared release = %s", prepared.ReleaseDigest)
		}
		arguments, err := os.ReadFile(filepath.Join(context, "build-args.env"))
		if err != nil || !strings.Contains(string(arguments), "CODE_POLISHY_BUNDLE_SHA256="+artifact.Archive.SHA256) {
			t.Fatalf("build arguments = %q, error = %v", arguments, err)
		}
	} else if _, err := PrepareOCIContext(descriptor, template, filepath.Join(t.TempDir(), "context")); err == nil {
		t.Fatal("non-Linux publication produced an OCI context")
	}
	if err := os.WriteFile(filepath.Join(publication, artifact.SBOM.Name), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareOCIContext(descriptor, template, filepath.Join(t.TempDir(), "tampered-context")); err == nil {
		t.Fatal("tampered publication produced an OCI context")
	}
}

func TestPublicationIndexRequiresEveryHostFromOneSource(t *testing.T) {
	descriptors := make([]string, 0, len(supportedReleaseHosts))
	for _, host := range supportedReleaseHosts {
		root, manifest := foreignRelease(t, host)
		archive := filepath.Join(t.TempDir(), host+".zip")
		if _, err := WriteArchive(root, archive); err != nil {
			t.Fatal(err)
		}
		publication := filepath.Join(t.TempDir(), host)
		if _, err := PublishArchive(archive, publication); err != nil {
			t.Fatal(err)
		}
		descriptors = append(descriptors, filepath.Join(publication, "code-polishy-"+manifest.CodePolishyVersion+"-"+host+".release.json"))
	}
	output := filepath.Join(t.TempDir(), "release-index.json")
	index, err := WritePublicationIndex(descriptors, output)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Artifacts) != len(supportedReleaseHosts) {
		t.Fatalf("artifacts = %d", len(index.Artifacts))
	}
	for position, artifact := range index.Artifacts {
		if artifact.Host != supportedReleaseHosts[position] {
			t.Fatalf("host %d = %s", position, artifact.Host)
		}
	}
	if _, err := WritePublicationIndex(descriptors[:len(descriptors)-1], filepath.Join(t.TempDir(), "incomplete.json")); err == nil {
		t.Fatal("incomplete host index was accepted")
	}
}

func foreignRelease(t *testing.T, host string) (string, Manifest) {
	t.Helper()
	root, manifest := installedRelease(t, map[string]string{BinaryPath: "engine", LauncherBinaryPath: "launcher"}, nil)
	if host == "windows-x64" && manifest.Host != "windows-x64" {
		for _, pair := range [][2]string{{BinaryPath, "bin/code-polishy.exe"}, {LauncherBinaryPath, "bin/code-polishy-launcher.exe"}} {
			source := filepath.Join(root, filepath.FromSlash(pair[0]))
			destination := filepath.Join(root, filepath.FromSlash(pair[1]))
			if err := os.Rename(source, destination); err != nil {
				t.Fatal(err)
			}
			for index := range manifest.Entries {
				if manifest.Entries[index].Path == pair[0] {
					manifest.Entries[index].Path = pair[1]
				}
			}
		}
	} else if host != "windows-x64" && manifest.Host == "windows-x64" {
		for _, pair := range [][2]string{{BinaryPath, "bin/code-polishy"}, {LauncherBinaryPath, "bin/code-polishy-launcher"}} {
			source := filepath.Join(root, filepath.FromSlash(pair[0]))
			destination := filepath.Join(root, filepath.FromSlash(pair[1]))
			if err := os.Rename(source, destination); err != nil {
				t.Fatal(err)
			}
			for index := range manifest.Entries {
				if manifest.Entries[index].Path == pair[0] {
					manifest.Entries[index].Path = pair[1]
				}
			}
		}
	}
	manifest.Host = host
	slices.SortFunc(manifest.Entries, func(first, second Entry) int { return strings.Compare(first.Path, second.Path) })
	manifest.ContentDigest = releaseEntriesDigest(manifest.Entries)
	manifest.ReleaseDigest = manifest.Identity()
	data := render(t, manifest)
	if err := os.WriteFile(filepath.Join(root, ManifestFilename), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, manifest
}
