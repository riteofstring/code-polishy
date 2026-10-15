package release

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func RustArchiveName(manifest Manifest) string {
	return "code-polishy-" + manifest.CodePolishyVersion + "-rust-" + manifest.Host + ".zip"
}

func InstallRustBundle(ctx context.Context, repoRoot, policyRoot, source string) error {
	manifest, err := VerifyLockedRelease(repoRoot, policyRoot)
	if err != nil {
		return err
	}
	tools, err := rustToolManifest(manifest)
	if err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(policyRoot)
	if err != nil {
		return err
	}
	staging, err := os.MkdirTemp(filepath.Dir(root), ".code-polishy-rust-install-")
	if err != nil {
		return err
	}
	keepStaging := false
	defer func() {
		if !keepStaging {
			os.RemoveAll(staging)
		}
	}()
	if err := stageRustBundle(ctx, repoRoot, manifest, tools, source, staging); err != nil {
		return err
	}
	if err := publishRustTools(root, staging, manifest); err != nil {
		keepStaging = true
		return fmt.Errorf("rust installation failed; recovery files retained at %s: %w", staging, err)
	}
	return nil
}

func stageRustBundle(ctx context.Context, repoRoot string, manifest, tools Manifest, source, staging string) error {
	archive, err := acquireRustBundle(ctx, repoRoot, manifest, source, staging)
	if err != nil {
		return err
	}
	extracted := filepath.Join(staging, "payload")
	if err := os.Mkdir(extracted, 0o755); err != nil {
		return err
	}
	if err := extractReleaseZip(archive, extracted); err != nil {
		return err
	}
	if err := tools.Verify(extracted); err != nil {
		return fmt.Errorf("rust bundle does not match the selected core release: %w", err)
	}
	if err := setRustToolPermissions(extracted, tools.Entries); err != nil {
		return err
	}
	return ctx.Err()
}

func setRustToolPermissions(root string, entries []Entry) error {
	for _, entry := range entries {
		if entry.Symlink != "" {
			continue
		}
		mode := os.FileMode(0o644)
		if strings.Contains(entry.Path, "/bin/") || strings.Contains(entry.Path, "/libexec/") {
			mode = 0o755
		}
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(entry.Path)), mode); err != nil {
			return err
		}
	}
	return nil
}

func acquireRustBundle(ctx context.Context, repoRoot string, manifest Manifest, source, staging string) (string, error) {
	var reader io.ReadCloser
	var err error
	if source != "" {
		reader, err = openLocalBundle(source)
	} else {
		reader, err = downloadRustBundle(ctx, repoRoot, manifest)
	}
	if err != nil {
		return "", err
	}
	defer reader.Close()
	archive := filepath.Join(staging, "rust.zip")
	output, err := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	written, copyErr := io.Copy(output, io.LimitReader(reader, maximumReleaseBundleBytes+1))
	if err := errors.Join(copyErr, output.Close()); err != nil {
		return "", err
	}
	if written > maximumReleaseBundleBytes {
		return "", errors.New("rust bundle exceeds its byte bound")
	}
	return archive, ctx.Err()
}

func downloadRustBundle(ctx context.Context, repoRoot string, manifest Manifest) (io.ReadCloser, error) {
	lock, present, err := ReadLock(repoRoot)
	if err != nil || !present || lock.Publication == nil {
		return nil, errors.New("rust setup requires the selected release publication")
	}
	index, err := parseReleaseHTTPSURL(lock.Publication.IndexURL)
	if err != nil {
		return nil, err
	}
	source, err := resolvePublicationURL(index, RustArchiveName(manifest))
	if err != nil {
		return nil, err
	}
	response, err := requestRelease(ctx, releaseHTTPClient(), source)
	if err != nil {
		return nil, err
	}
	if response.ContentLength > maximumReleaseBundleBytes {
		response.Body.Close()
		return nil, errors.New("rust bundle exceeds its byte bound")
	}
	return response.Body, nil
}

func publishRustTools(root, staging string, manifest Manifest) error {
	boundary, err := os.OpenRoot(filepath.Dir(root))
	if err != nil {
		return err
	}
	defer boundary.Close()
	target, stage := filepath.Base(root), filepath.Base(staging)
	paths := []string{".tools/rust/" + manifest.Host, rustNextestPath(manifest.Host)}
	installed, previous := []string{}, []string{}
	rollback := func(cause error) error {
		return restoreRustTools(boundary, target, stage, installed, previous, cause)
	}
	for _, path := range paths {
		destination := filepath.Join(target, path)
		if err := boundary.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return rollback(err)
		}
		if err := rustToolParents(root, path, map[string]bool{}); err != nil {
			return rollback(err)
		}
		if _, err := boundary.Lstat(destination); err == nil {
			backup := filepath.Join(stage, "previous", path)
			if err := boundary.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
				return rollback(err)
			}
			if err := boundary.Rename(destination, backup); err != nil {
				return rollback(err)
			}
			previous = append(previous, path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return rollback(err)
		}
		if err := boundary.Rename(filepath.Join(stage, "payload", path), destination); err != nil {
			return rollback(err)
		}
		installed = append(installed, path)
	}
	return nil
}

func restoreRustTools(boundary *os.Root, target, stage string, installed, previous []string, cause error) error {
	for index := len(installed) - 1; index >= 0; index-- {
		path := installed[index]
		cause = errors.Join(cause, boundary.Rename(filepath.Join(target, path), filepath.Join(stage, "payload", path)))
	}
	for index := len(previous) - 1; index >= 0; index-- {
		path := previous[index]
		cause = errors.Join(cause, boundary.Rename(filepath.Join(stage, "previous", path), filepath.Join(target, path)))
	}
	return cause
}
