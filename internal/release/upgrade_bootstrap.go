package release

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type NewerManifestError struct {
	Version int
	Source  string
}

func (failure *NewerManifestError) Error() string {
	return fmt.Sprintf("%s records manifest version %d; this Code Polishy reads manifest version %d", failure.Source, failure.Version, ManifestVersion)
}

type UpgradeHandoff struct {
	Root string
}

func (handoff *UpgradeHandoff) Error() string {
	return "upgrade planning requires the incoming release at " + handoff.Root
}

func bootstrapIndexedArchive(ctx context.Context, archive, prefix string, artifact PublicationArtifact) (string, error) {
	scratch, err := os.MkdirTemp(prefix, ".code-polishy-bootstrap-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	binary, err := extractUpgradeBootstrap(archive, scratch)
	if err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, binary, "--policy-root", scratch, "install-bundle", "--source", archive,
		"--sha256", artifact.Archive.SHA256, "--prefix", prefix)
	output := &bootstrapOutput{}
	command.Stdout, command.Stderr = output, output
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("incoming release bootstrap: %w: %s", err, output.String())
	}
	root := Directory(prefix, Lock{CodePolishyVersion: artifact.CodePolishyVersion, ReleaseDigest: artifact.Manifest.ReleaseDigest})
	digest, err := fileSHA256(filepath.Join(root, ManifestFilename))
	if err != nil {
		return "", err
	}
	if digest != artifact.Manifest.SHA256 {
		return "", errors.New("installed incoming manifest does not match the publication index")
	}
	return root, nil
}

func extractUpgradeBootstrap(archive, scratch string) (string, error) {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	selected, err := upgradeBootstrapEntry(reader.File)
	if err != nil {
		return "", err
	}
	input, err := selected.Open()
	if err != nil {
		return "", err
	}
	destination := filepath.Join(scratch, filepath.Base(filepath.FromSlash(BinaryPath)))
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		input.Close()
		return "", err
	}
	written, copyErr := io.Copy(output, io.LimitReader(input, int64(selected.UncompressedSize64)+1))
	if err := errors.Join(copyErr, input.Close(), output.Close()); err != nil {
		return "", err
	}
	if written != int64(selected.UncompressedSize64) {
		return "", errors.New("release archive bootstrap size mismatch")
	}
	return destination, nil
}

func upgradeBootstrapEntry(entries []*zip.File) (*zip.File, error) {
	var selected *zip.File
	for _, entry := range entries {
		if entry.Name != BinaryPath {
			continue
		}
		if selected != nil || !entry.Mode().IsRegular() || entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > 256<<20 {
			return nil, errors.New("release archive has an invalid installer bootstrap")
		}
		selected = entry
	}
	if selected == nil {
		return nil, errors.New("release archive has no installer bootstrap")
	}
	return selected, nil
}

type bootstrapOutput struct {
	buffer bytes.Buffer
}

func (output *bootstrapOutput) String() string {
	return output.buffer.String()
}

func (output *bootstrapOutput) Write(data []byte) (int, error) {
	count := len(data)
	remaining := 64<<10 - output.buffer.Len()
	if remaining > 0 {
		_, _ = output.buffer.Write(data[:min(count, remaining)])
	}
	return count, nil
}
