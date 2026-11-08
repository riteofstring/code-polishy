package ownedoutput

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func mountGuard(root, relative string, descendants bool) error {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return fmt.Errorf("mount topology unavailable: %w", err)
	}
	defer file.Close()
	return inspectMounts(file, root, filepath.Join(root, filepath.FromSlash(relative)), descendants)
}

func inspectMounts(input io.Reader, root, path string, descendants bool) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	count := 0
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || !strings.Contains(scanner.Text(), " - ") {
			return fmt.Errorf("incomplete mount topology")
		}
		mount := strings.NewReplacer("\\040", " ", "\\011", "\t", "\\012", "\n", "\\134", "\\").Replace(fields[4])
		if !filepath.IsAbs(mount) {
			return fmt.Errorf("nonabsolute mount point")
		}
		if mountOverlaps(root, path, mount, descendants) {
			return fmt.Errorf("mounted output at %q", mount)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read mount topology: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("empty mount topology")
	}
	return nil
}

func mountOverlaps(root, path, mount string, descendants bool) bool {
	return mount == path || descendants && strings.HasPrefix(mount, path+"/") ||
		mount != root && strings.HasPrefix(mount, root+"/") && strings.HasPrefix(path, mount+"/")
}
