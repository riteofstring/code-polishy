package release

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CopyTreeDereferenced materializes one closed input tree as regular files and
// directories. Release ZIPs deliberately contain no links, so pnpm's internal
// links are resolved only when they stay inside the selected bundle root.
func CopyTreeDereferenced(source, destination string) error {
	root, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return errors.New("release materialization source must be a directory")
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return errors.New("release materialization destination must not exist")
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	if err := copyDereferencedDirectory(root, root, destination, map[string]bool{}); err != nil {
		_ = os.RemoveAll(destination)
		return err
	}
	return nil
}

func copyDereferencedDirectory(root, source, destination string, stack map[string]bool) error {
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return err
	}
	if !releasePathWithin(root, resolved) {
		return fmt.Errorf("release materialization link leaves its source: %s", source)
	}
	if stack[resolved] {
		return fmt.Errorf("release materialization contains a link cycle at %s", source)
	}
	stack[resolved] = true
	defer delete(stack, resolved)
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyDereferencedEntry(root, resolved, destination, entry, stack); err != nil {
			return err
		}
	}
	return nil
}

func copyDereferencedEntry(root, source, destination string, entry os.DirEntry, stack map[string]bool) error {
	input, output := filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())
	info, err := os.Stat(input)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.Mkdir(output, info.Mode().Perm()|0o700); err != nil {
			return err
		}
		return copyDereferencedDirectory(root, input, output, stack)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("release materialization input is unsupported: %s", input)
	}
	resolvedFile, err := filepath.EvalSymlinks(input)
	if err != nil {
		return err
	}
	if !releasePathWithin(root, resolvedFile) {
		return fmt.Errorf("release materialization link leaves its source: %s", input)
	}
	return copyReleaseFile(resolvedFile, output, info.Mode().Perm())
}

func copyReleaseFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode|0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	return errors.Join(copyErr, output.Close())
}

func releasePathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
