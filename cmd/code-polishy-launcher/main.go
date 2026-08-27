// Command code-polishy-launcher is the stable installed entry point a target
// repository runs as `code-polishy`.
//
// It selects nothing on its own. The target's .code-polishy.lock.json names one
// exact installed release, and the launcher either hands control to that
// release or reports which release the target requires and how to install it
// locally. There is no channel, range, newest-wins rule, fallback release, or
// download.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/riteofstring/code-polishy/internal/release"
)

const installInstruction = "Install it from the matching Code Polishy checkout with ./scripts/install.sh."

func main() {
	executable, err := os.Executable()
	if err != nil {
		os.Exit(fail(os.Stderr, fmt.Errorf("resolve the installed Code Polishy launcher: %w", err)))
	}
	os.Exit(run(executable, os.Args[1:], os.Stderr, launch))
}

func run(executable string, arguments []string, stderr io.Writer, launcher func(string, []string) (int, error)) int {
	prefix, err := installPrefix(executable)
	if err != nil {
		return fail(stderr, err)
	}
	repoRoot, forwarded, err := parseArguments(arguments)
	if err != nil {
		return fail(stderr, err)
	}
	lock, present, err := release.ReadLock(repoRoot)
	if err != nil {
		return fail(stderr, err)
	}
	if !present {
		return fail(stderr, fmt.Errorf(
			"%s has no %s.\nWrite one by running `lock` from the exact release under %s that this repository requires",
			repoRoot, release.LockFilename, filepath.Join(prefix, "releases"),
		))
	}
	directory := release.Directory(prefix, lock)
	manifest, installed, err := release.ReadManifest(directory)
	if err != nil {
		return fail(stderr, err)
	}
	if !installed {
		return fail(stderr, fmt.Errorf(
			"%s requires Code Polishy %s %s, which is not installed at %s.\n%s",
			release.LockFilename, lock.CodePolishyVersion, lock.ReleaseDigest, directory, installInstruction,
		))
	}
	if err := manifest.Satisfies(lock); err != nil {
		return fail(stderr, fmt.Errorf("%w.\n%s", err, installInstruction))
	}
	if err := manifest.Verify(directory); err != nil {
		return fail(stderr, fmt.Errorf("%w.\n%s", err, installInstruction))
	}
	binary := filepath.Join(directory, filepath.FromSlash(release.BinaryPath))
	argv := append([]string{binary, "--policy-root", directory, "--repo-root", repoRoot}, forwarded...)
	status, err := launcher(binary, argv)
	if err != nil {
		return fail(stderr, err)
	}
	return status
}

// installPrefix is the directory the installed launcher lives under, resolved
// from the launcher itself. A release store is found beside the launcher rather
// than named by an environment variable, a configuration file, or the lock, so
// nothing a target checks in can point Code Polishy at another store.
func installPrefix(executable string) (string, error) {
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("resolve the installed Code Polishy launcher: %w", err)
	}
	return filepath.Dir(filepath.Dir(resolved)), nil
}

// parseArguments reads the leading global options the launcher must act on and
// forwards everything else untouched. --repo-root is resolved here and reissued
// from the release, and --policy-root is refused: the lock decides which
// release runs, so a target cannot point the launcher somewhere else.
func parseArguments(arguments []string) (repoRoot string, forwarded []string, err error) {
	working, err := os.Getwd()
	if err != nil {
		return "", nil, fmt.Errorf("resolve the working directory: %w", err)
	}
	repoRoot = working
	forwarded = []string{}
	for index := 0; index < len(arguments); index++ {
		name, value, spelledWithValue := globalOption(arguments[index])
		if name == "" {
			return repoRoot, append(forwarded, arguments[index:]...), nil
		}
		if name == "--policy-root" {
			return "", nil, fmt.Errorf(
				"--policy-root names a Code Polishy checkout, and the installed launcher runs the release %s names",
				release.LockFilename,
			)
		}
		if !spelledWithValue {
			if index+1 >= len(arguments) {
				return "", nil, fmt.Errorf("%s requires a value", name)
			}
			index++
			value = arguments[index]
		}
		if name == "--repo-root" {
			if repoRoot, err = resolveDirectory(working, value); err != nil {
				return "", nil, err
			}
			continue
		}
		forwarded = append(forwarded, name, value)
	}
	return repoRoot, forwarded, nil
}

// globalOption splits one leading global option into its name and any value
// spelled with `=`. Anything else is where the caller's own command begins.
func globalOption(argument string) (name, value string, spelledWithValue bool) {
	for _, known := range []string{"--repo-root", "--policy-root", "--config"} {
		if argument == known {
			return known, "", false
		}
		if suffix, joined := strings.CutPrefix(argument, known+"="); joined {
			return known, suffix, true
		}
	}
	return "", "", false
}

func resolveDirectory(working, candidate string) (string, error) {
	if candidate == "" {
		return "", fmt.Errorf("--repo-root requires a non-empty directory")
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(working, candidate)
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("--repo-root directory is unavailable: %w", err)
	}
	return resolved, nil
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "code-polishy: %v\n", err)
	return 2
}
