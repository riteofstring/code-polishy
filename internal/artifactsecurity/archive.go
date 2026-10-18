package artifactsecurity

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

func saveDockerImage(ctx context.Context, client dockerClient, operation, image, archive string, maximum, metadataMaximum int64) (string, error) {
	identity, err := inspectImageID(ctx, client, operation+"-inspect", image)
	if err != nil {
		return "", err
	}
	if _, err := client.run(ctx, operation+"-save", "image", "save", "--output", archive, identity); err != nil {
		return "", err
	}
	return dockerArchiveImageID(archive, maximum, metadataMaximum)
}

func dockerArchiveImageID(archive string, maximum, metadataMaximum int64) (string, error) {
	payload, err := dockerArchiveEntry(archive, "manifest.json", maximum, metadataMaximum)
	if err != nil {
		return "", err
	}
	var manifest []struct {
		Config string `json:"Config"`
	}
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return "", fmt.Errorf("decode Docker archive manifest: %w", err)
	}
	if len(manifest) != 1 || manifest[0].Config == "" {
		return "", errors.New("Docker archive must identify exactly one image configuration")
	}
	config, err := dockerArchiveEntry(archive, manifest[0].Config, maximum, metadataMaximum)
	if err != nil {
		return "", err
	}
	return "sha256:" + bytesSHA256(config), nil
}

func dockerArchiveEntry(archive, name string, maximum, entryMaximum int64) ([]byte, error) {
	info, err := os.Lstat(archive)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maximum {
		return nil, fmt.Errorf("Docker archive must be a regular file no larger than %d bytes", maximum)
	}
	file, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readDockerArchiveEntry(tar.NewReader(file), name, entryMaximum)
}

func readDockerArchiveEntry(reader *tar.Reader, name string, maximum int64) ([]byte, error) {
	var payload []byte
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Name != name {
			continue
		}
		if payload != nil || header.Typeflag != tar.TypeReg || header.Size > maximum {
			return nil, fmt.Errorf("Docker archive entry %q must be one bounded regular file", name)
		}
		payload, err = io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
	}
	if payload == nil {
		return nil, fmt.Errorf("Docker archive omits %q", name)
	}
	return payload, nil
}
