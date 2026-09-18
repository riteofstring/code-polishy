package release

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const maximumSourceCommandOutputBytes = 1 << 20

var errSourceCommandOutputLimit = errors.New("source checkout command output exceeds its byte bound")

type SourceRelease struct {
	Lock     Lock
	Manifest Manifest
	Root     string
}

type sourceCheckoutIdentity struct {
	Root, Revision, Version string
}

type sourceReleaseInstaller func(context.Context, string, string) error

type boundedSourceCommandOutput struct {
	buffer bytes.Buffer
}

func (output *boundedSourceCommandOutput) Write(data []byte) (int, error) {
	remaining := maximumSourceCommandOutputBytes - output.buffer.Len()
	if len(data) <= remaining {
		return output.buffer.Write(data)
	}
	if remaining > 0 {
		_, _ = output.buffer.Write(data[:remaining])
	}
	return len(data), errSourceCommandOutputLimit
}

func InstallSourceRelease(ctx context.Context, source, prefix string) (SourceRelease, error) {
	return installSourceRelease(ctx, source, prefix, runSourceReleaseInstaller)
}

func installSourceRelease(ctx context.Context, source, prefix string, installer sourceReleaseInstaller) (SourceRelease, error) {
	identity, err := inspectSourceCheckout(ctx, source)
	if err != nil {
		return SourceRelease{}, err
	}
	canonicalPrefix, err := prepareInstallPrefix(prefix)
	if err != nil {
		return SourceRelease{}, err
	}
	if err := installer(ctx, identity.Root, canonicalPrefix); err != nil {
		return SourceRelease{}, err
	}
	installedIdentity, err := inspectSourceCheckout(ctx, identity.Root)
	if err != nil {
		return SourceRelease{}, err
	}
	if installedIdentity != identity {
		return SourceRelease{}, errors.New("source checkout identity changed during installation")
	}
	return findInstalledSourceRelease(canonicalPrefix, identity)
}

func inspectSourceCheckout(ctx context.Context, source string) (sourceCheckoutIdentity, error) {
	root, info, err := canonicalSourceDirectory(source)
	if err != nil {
		return sourceCheckoutIdentity{}, err
	}
	if err := requireSourceCheckoutRoot(ctx, root, info); err != nil {
		return sourceCheckoutIdentity{}, err
	}
	status, err := runSourceGit(ctx, root, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return sourceCheckoutIdentity{}, err
	}
	if len(status) != 0 {
		return sourceCheckoutIdentity{}, errors.New("source checkout must be clean")
	}
	revision, err := sourceGitLine(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return sourceCheckoutIdentity{}, err
	}
	if !revisionPattern.MatchString(revision) {
		return sourceCheckoutIdentity{}, errors.New("source checkout has no lowercase full commit identity")
	}
	version, err := readSourceVersion(root)
	if err != nil {
		return sourceCheckoutIdentity{}, err
	}
	return sourceCheckoutIdentity{Root: root, Revision: revision, Version: version}, nil
}

func canonicalSourceDirectory(source string) (string, os.FileInfo, error) {
	if source == "" {
		return "", nil, errors.New("source checkout path is empty")
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return "", nil, err
	}
	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", nil, fmt.Errorf("resolve source checkout: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", nil, errors.New("source checkout must be an existing directory")
	}
	if !info.IsDir() {
		return "", nil, errors.New("source checkout must be an existing directory")
	}
	return root, info, nil
}

func requireSourceCheckoutRoot(ctx context.Context, root string, info os.FileInfo) error {
	top, err := sourceGitLine(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	top, err = filepath.EvalSymlinks(top)
	if err != nil {
		return fmt.Errorf("resolve source checkout root: %w", err)
	}
	topInfo, err := os.Stat(top)
	if err != nil {
		return errors.New("source path must name the root of its Git checkout")
	}
	if !os.SameFile(info, topInfo) {
		return errors.New("source path must name the root of its Git checkout")
	}
	return nil
}

func readSourceVersion(root string) (string, error) {
	versionFile, err := os.Open(filepath.Join(root, "VERSION"))
	if err != nil {
		return "", errors.New("source checkout has no bounded VERSION")
	}
	versionData, readErr := io.ReadAll(io.LimitReader(versionFile, 257))
	if err := errors.Join(readErr, versionFile.Close()); err != nil {
		return "", errors.New("source checkout has no bounded VERSION")
	}
	if len(versionData) == 0 || len(versionData) > 256 {
		return "", errors.New("source checkout has no bounded VERSION")
	}
	version := strings.TrimSpace(string(versionData))
	if !versionPattern.MatchString(version) {
		return "", errors.New("source checkout has an invalid VERSION")
	}
	return version, nil
}

func sourceGitLine(ctx context.Context, root string, arguments ...string) (string, error) {
	data, err := runSourceGit(ctx, root, arguments...)
	if err != nil {
		return "", err
	}
	line := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if line == "" || strings.ContainsAny(line, "\x00\r\n") {
		return "", errors.New("source checkout command returned an invalid single line")
	}
	return line, nil
}

func runSourceGit(ctx context.Context, root string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, arguments...)...)
	standardOutput := &boundedSourceCommandOutput{}
	standardError := &boundedSourceCommandOutput{}
	command.Stdout = standardOutput
	command.Stderr = standardError
	if err := command.Run(); err != nil {
		diagnostic := strings.TrimSpace(standardError.buffer.String())
		if diagnostic == "" {
			diagnostic = err.Error()
		}
		return nil, fmt.Errorf("inspect source checkout: %s", diagnostic)
	}
	return append([]byte{}, standardOutput.buffer.Bytes()...), nil
}

func runSourceReleaseInstaller(ctx context.Context, source, prefix string) error {
	commands, err := sourceReleaseInstallCommands(ctx, source, prefix)
	if err != nil {
		return err
	}
	for _, command := range commands {
		command.Dir = source
		command.Stdin = os.Stdin
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("install source-backed release: %w", err)
		}
	}
	return nil
}

func sourceReleaseInstallCommands(ctx context.Context, source, prefix string) ([]*exec.Cmd, error) {
	toolInstaller := filepath.Join(source, "tools", "install-policy-tools.sh")
	releaseInstaller := filepath.Join(source, "scripts", "install.sh")
	if runtime.GOOS == "windows" {
		toolInstaller = filepath.Join(source, "tools", "install-policy-tools.ps1")
		releaseInstaller = filepath.Join(source, "scripts", "install.ps1")
	}
	for _, path := range []string{toolInstaller, releaseInstaller} {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
			return nil, errors.New("source checkout has no usable policy-tool and release installers")
		}
	}
	if runtime.GOOS == "windows" {
		return []*exec.Cmd{
			exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", toolInstaller),
			exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", releaseInstaller, "-Prefix", prefix),
		}, nil
	}
	return []*exec.Cmd{
		exec.CommandContext(ctx, toolInstaller),
		exec.CommandContext(ctx, releaseInstaller, "--prefix", prefix),
	}, nil
}

func findInstalledSourceRelease(prefix string, identity sourceCheckoutIdentity) (SourceRelease, error) {
	releasesRoot := filepath.Join(prefix, releasesDirectory)
	entries, err := os.ReadDir(releasesRoot)
	if err != nil {
		return SourceRelease{}, fmt.Errorf("read installed source releases: %w", err)
	}
	var result *SourceRelease
	for _, entry := range entries {
		candidate, err := installedSourceReleaseCandidate(prefix, releasesRoot, entry, identity)
		if err != nil {
			return SourceRelease{}, err
		}
		if candidate == nil {
			continue
		}
		if result != nil {
			return SourceRelease{}, errors.New("source installation produced more than one matching release")
		}
		result = candidate
	}
	if result == nil {
		return SourceRelease{}, errors.New("source installer did not publish the checked-out release")
	}
	return *result, nil
}

func installedSourceReleaseCandidate(prefix, releasesRoot string, entry os.DirEntry, identity sourceCheckoutIdentity) (*SourceRelease, error) {
	if !entry.IsDir() {
		return nil, nil
	}
	if !strings.HasPrefix(entry.Name(), identity.Version+"-") {
		return nil, nil
	}
	root := filepath.Join(releasesRoot, entry.Name())
	manifest, present, err := ReadManifest(root)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, nil
	}
	if manifest.CodePolishyVersion != identity.Version {
		return nil, nil
	}
	if manifest.SourceRevision != identity.Revision {
		return nil, nil
	}
	lock := LockFor(manifest)
	if root != Directory(prefix, lock) {
		return nil, errors.New("installed source release directory does not match its identity")
	}
	if err := manifest.Verify(root); err != nil {
		return nil, err
	}
	if err := manifest.Satisfies(lock); err != nil {
		return nil, err
	}
	return &SourceRelease{Lock: lock, Manifest: manifest, Root: root}, nil
}
