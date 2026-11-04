package javascript

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	maximumPackedBytes   = 64 << 20
	maximumUnpackedBytes = 256 << 20
	maximumPackedEntries = 16384
	packedPrefix         = "package/"
	packedManifest       = "package.json"
)

func (bundle Bundle) PackedImports(ctx context.Context, archive, name string) (ImportResult, error) {
	root, err := os.MkdirTemp("", "code-polishy-packed-")
	if err != nil {
		return ImportResult{}, err
	}
	defer os.RemoveAll(root)
	if root, err = filepath.EvalSymlinks(root); err != nil {
		return ImportResult{}, err
	}
	if err := unpackPackage(archive, root); err != nil {
		return ImportResult{}, fmt.Errorf("packed artifact %s: %w", filepath.Base(archive), err)
	}
	if err := requirePackedName(root, name); err != nil {
		return ImportResult{}, err
	}
	return bundle.packageImports(ctx, root)
}

func unpackPackage(archive, root string) error {
	info, err := os.Lstat(archive)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maximumPackedBytes {
		return fmt.Errorf("must be a regular file no larger than %d bytes", maximumPackedBytes)
	}
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	budget := unpackBudget{entries: maximumPackedEntries, bytes: maximumUnpackedBytes}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := budget.unpack(root, header, reader); err != nil {
			return err
		}
	}
}

type unpackBudget struct {
	entries int
	bytes   int64
}

func (budget *unpackBudget) unpack(root string, header *tar.Header, reader io.Reader) error {
	budget.entries--
	if budget.entries < 0 {
		return fmt.Errorf("has more than %d entries", maximumPackedEntries)
	}
	name := strings.TrimSuffix(header.Name, "/")
	if header.Typeflag == tar.TypeDir && name+"/" == packedPrefix {
		return nil
	}
	member, found := strings.CutPrefix(name, packedPrefix)
	if !found || !containedPath(member) {
		return fmt.Errorf("entry %q is not a contained path under %s", header.Name, packedPrefix)
	}
	if header.Typeflag == tar.TypeDir {
		return nil
	}
	if header.Typeflag != tar.TypeReg {
		return fmt.Errorf("entry %q is not a regular file", header.Name)
	}
	budget.bytes -= header.Size
	if budget.bytes < 0 {
		return fmt.Errorf("unpacks to more than %d bytes", maximumUnpackedBytes)
	}
	return writeMember(filepath.Join(root, filepath.FromSlash(member)), reader, header.Size)
}

func writeMember(target string, reader io.Reader, size int64) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.CopyN(file, reader, size)
	return errors.Join(err, file.Close())
}

func requirePackedName(root, name string) error {
	data, err := os.ReadFile(filepath.Join(root, packedManifest))
	if err != nil {
		return fmt.Errorf("packed artifact has no %s: %w", packedManifest, err)
	}
	var manifest struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("packed %s is not readable: %w", packedManifest, err)
	}
	if manifest.Name != name {
		return fmt.Errorf("packed artifact is package %q, not %q", manifest.Name, name)
	}
	return nil
}
