package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyAcceptsTheReleaseAsInstalled(t *testing.T) {
	t.Parallel()
	directory, manifest := exampleRelease(t)
	if err := manifest.Verify(directory); err != nil {
		t.Fatalf("a release as it was installed was refused: %v", err)
	}
	// A release keeps its own record, and nothing records that record.
	if err := os.Remove(filepath.Join(directory, ManifestFilename)); err != nil {
		t.Fatalf("remove the manifest: %v", err)
	}
	if err := manifest.Verify(directory); err != nil {
		t.Fatalf("the release's own record was verified as an installed entry: %v", err)
	}
}

func TestVerifyRefusesEveryPartOfAReleaseThatChanged(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		change  func(*testing.T, string)
		refusal string
	}{
		"a changed engine binary": {
			change:  func(t *testing.T, directory string) { write(t, directory, BinaryPath, "tampered") },
			refusal: "is not the file the installed Code Polishy release recorded",
		},
		"a changed file the engine reads": {
			change:  func(t *testing.T, directory string) { write(t, directory, "VERSION", "9.9.10\n") },
			refusal: "is not the file the installed Code Polishy release recorded",
		},
		"a missing recorded file": {
			change:  func(t *testing.T, directory string) { remove(t, directory, "bundle/.pnpm/tool/index.mjs") },
			refusal: "is missing",
		},
		"a missing recorded link": {
			change:  func(t *testing.T, directory string) { remove(t, directory, "bundle/node_modules/tool") },
			refusal: "is missing",
		},
		"a retargeted bundle link": {
			change: func(t *testing.T, directory string) {
				remove(t, directory, "bundle/node_modules/tool")
				link(t, directory, "bundle/node_modules/tool", "../.pnpm/other")
			},
			refusal: "is not the link the installed Code Polishy release recorded",
		},
		"a link where a file was recorded": {
			change: func(t *testing.T, directory string) {
				remove(t, directory, BinaryPath)
				link(t, directory, BinaryPath, "/usr/bin/env")
			},
			refusal: "is a link, and the installed Code Polishy release records a file",
		},
		"a file where a link was recorded": {
			change: func(t *testing.T, directory string) {
				remove(t, directory, "bundle/node_modules/tool")
				write(t, directory, "bundle/node_modules/tool", "// not a link\n")
			},
			refusal: "is a file, and the installed Code Polishy release records a link",
		},
		"a file the release does not record": {
			change:  func(t *testing.T, directory string) { write(t, directory, "bundle/extra.mjs", "// added\n") },
			refusal: "does not record it",
		},
		"a link the release does not record": {
			change:  func(t *testing.T, directory string) { link(t, directory, "bin/node", "/usr/local/bin/node") },
			refusal: "does not record it",
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			directory, manifest := exampleRelease(t)
			testCase.change(t, directory)
			err := manifest.Verify(directory)
			if err == nil || !strings.Contains(err.Error(), testCase.refusal) {
				t.Fatalf("%s was accepted: %v", name, err)
			}
		})
	}
}

func write(t *testing.T, directory, relative, content string) {
	t.Helper()
	installed := filepath.Join(directory, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(installed), err)
	}
	if err := os.WriteFile(installed, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", installed, err)
	}
}

func link(t *testing.T, directory, relative, target string) {
	t.Helper()
	installed := filepath.Join(directory, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(installed), err)
	}
	if err := os.Symlink(target, installed); err != nil {
		t.Fatalf("link %s: %v", installed, err)
	}
}

func remove(t *testing.T, directory, relative string) {
	t.Helper()
	if err := os.Remove(filepath.Join(directory, filepath.FromSlash(relative))); err != nil {
		t.Fatalf("remove %s: %v", relative, err)
	}
}
