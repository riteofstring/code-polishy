package pack

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const conformanceCommitMessage = "language conformance fixture"
const maximumConformanceGitOutputBytes = 72 << 20

func conformanceGitTool(ctx context.Context) (ConformanceToolIdentity, error) {
	name, err := exec.LookPath("git")
	if err != nil {
		return ConformanceToolIdentity{}, err
	}
	path, digest, err := conformanceExecutablePathAndDigest(name)
	if err != nil {
		return ConformanceToolIdentity{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	stdout, _, err := runConformanceGit(bounded, path, "", "", "", "--version")
	if err != nil {
		return ConformanceToolIdentity{}, err
	}
	version := strings.TrimSpace(string(stdout))
	if version == "" || len(version) > 1024 || strings.ContainsAny(version, "\r\n") {
		return ConformanceToolIdentity{}, errors.New("Git returned an invalid version identity")
	}
	return ConformanceToolIdentity{Path: path, SHA256: digest, Version: version}, nil
}

func materializeConformanceFixture(ctx context.Context, root string, fixture ConformanceFixture, lane, gitExecutable string) error {
	if err := os.Mkdir(root, 0o700); err != nil {
		return err
	}
	for _, file := range fixture.Files {
		data, err := conformanceFixtureBytes(file)
		if err != nil {
			return err
		}
		if err := writeConformanceMaterializedFile(root, file.Path, file.Mode, data); err != nil {
			return err
		}
	}
	for _, file := range conformanceLaneFiles(fixture.LaneOverrides, lane) {
		data, err := conformanceFixtureBytes(file)
		if err != nil {
			return err
		}
		if err := writeConformanceMaterializedFile(root, file.Path, file.Mode, data); err != nil {
			return err
		}
	}
	globalConfiguration := filepath.Join(filepath.Dir(root), "git-global.config")
	file, err := os.OpenFile(globalConfiguration, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		err = file.Close()
	} else if errors.Is(err, os.ErrExist) {
		info, statErr := os.Stat(globalConfiguration)
		if statErr != nil || !info.Mode().IsRegular() {
			return errors.New("conformance Git configuration is not a regular file")
		}
		err = nil
	}
	if err != nil {
		return err
	}
	git := func(arguments ...string) error {
		_, stderr, runErr := runConformanceGit(ctx, gitExecutable, root, globalConfiguration, fixture.Git.CommitTimestamp, arguments...)
		if runErr != nil {
			return fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), runErr, strings.TrimSpace(string(stderr)))
		}
		return nil
	}
	if err := git("init", "--quiet"); err != nil {
		return err
	}
	if err := git("symbolic-ref", "HEAD", "refs/heads/"+fixture.Git.Branch); err != nil {
		return err
	}
	for _, setting := range [][2]string{{"core.autocrlf", "false"}, {"core.filemode", "true"}, {"core.hooksPath", ".git/no-hooks"}, {"commit.gpgSign", "false"}} {
		if err := git("config", "--local", setting[0], setting[1]); err != nil {
			return err
		}
	}
	if err := git("add", "--all", "--force", "--", "."); err != nil {
		return err
	}
	if err := git("commit", "--quiet", "--no-verify", "--no-gpg-sign", "-m", conformanceCommitMessage); err != nil {
		return err
	}
	for _, change := range fixture.Git.Changes {
		target := filepath.Join(root, filepath.FromSlash(change.Path))
		if change.State == "deleted" {
			if err := os.Remove(target); err != nil {
				return err
			}
		} else {
			data, err := conformanceGitChangeBytes(change)
			if err != nil {
				return err
			}
			if err := writeConformanceMaterializedFile(root, change.Path, change.Mode, data); err != nil {
				return err
			}
		}
		if change.Staged {
			if err := git("add", "--all", "--force", "--", change.Path); err != nil {
				return err
			}
		}
	}
	return nil
}

func conformanceLaneFiles(overrides *ConformanceLaneOverrides, lane string) []ConformanceFixtureFile {
	if overrides == nil {
		return nil
	}
	if lane == "reference" {
		return overrides.Reference
	}
	return overrides.Candidate
}

func writeConformanceMaterializedFile(root, name, modeName string, data []byte) error {
	target := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if modeName == "0755" {
		mode = 0o755
	}
	if err := os.WriteFile(target, data, mode); err != nil {
		return err
	}
	return os.Chmod(target, mode)
}

func conformanceGitSnapshot(ctx context.Context, gitExecutable, root string) (ConformanceGitIdentity, error) {
	globalConfiguration := filepath.Join(filepath.Dir(root), "git-global.config")
	run := func(arguments ...string) ([]byte, error) {
		stdout, stderr, err := runConformanceGit(ctx, gitExecutable, root, globalConfiguration, "", arguments...)
		if err != nil {
			return nil, fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(string(stderr)))
		}
		return stdout, nil
	}
	headOutput, err := run("rev-parse", "--verify", "HEAD")
	if err != nil {
		return ConformanceGitIdentity{}, err
	}
	branchOutput, err := run("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return ConformanceGitIdentity{}, err
	}
	indexOutput, err := run("diff", "--cached", "--no-ext-diff", "--binary", "--full-index", "--no-renames", "--")
	if err != nil {
		return ConformanceGitIdentity{}, err
	}
	statusOutput, err := run(
		"status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored=no", "--", ".",
		":(exclude).code-polishy-reports", ":(exclude).code-polishy-reports/**",
		":(exclude).code-polishy-artifacts", ":(exclude).code-polishy-artifacts/**",
	)
	if err != nil {
		return ConformanceGitIdentity{}, err
	}
	head := strings.TrimSpace(string(headOutput))
	branch := strings.TrimSpace(string(branchOutput))
	if !validConformanceObjectID(head) || !validConformanceBranch(branch) {
		return ConformanceGitIdentity{}, errors.New("Git returned an invalid repository identity")
	}
	status, err := conformanceGitStatus(statusOutput)
	if err != nil {
		return ConformanceGitIdentity{}, err
	}
	indexDigest := sha256.Sum256(indexOutput)
	return ConformanceGitIdentity{Head: head, Branch: branch, IndexDiffSHA256: hex.EncodeToString(indexDigest[:]), Status: status}, nil
}

func conformanceGitStatus(data []byte) ([]string, error) {
	if len(data) > maximumConformanceGitOutputBytes || !utf8.Valid(data) {
		return nil, errors.New("Git status output is invalid or exceeds its limit")
	}
	if len(data) == 0 {
		return []string{}, nil
	}
	if data[len(data)-1] != 0 {
		return nil, errors.New("Git status output is not NUL terminated")
	}
	items := strings.Split(string(data[:len(data)-1]), "\x00")
	if len(items) > maximumConformanceFiles*2 {
		return nil, errors.New("Git status output exceeds its item limit")
	}
	if slices.Contains(items, "") {
		return nil, errors.New("Git status output contains an empty item")
	}
	return items, nil
}

func validConformanceObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

func runConformanceGit(ctx context.Context, executable, root, globalConfiguration, commitTimestamp string, arguments ...string) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, executable, arguments...)
	if root != "" {
		command.Dir = root
	}
	command.Env = conformanceGitEnvironment(globalConfiguration, commitTimestamp)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if stdout.Len() > maximumConformanceGitOutputBytes || stderr.Len() > 64<<10 {
		return nil, nil, errors.New("Git output exceeds its limit")
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

func conformanceGitEnvironment(globalConfiguration, commitTimestamp string) []string {
	environment := []string{}
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(name, "GIT_") || slices.Contains([]string{"LANG", "LC_ALL", "TZ"}, name) {
			continue
		}
		environment = append(environment, value)
	}
	if commitTimestamp == "" {
		commitTimestamp = "2000-01-01T00:00:00Z"
	}
	environment = append(environment,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Code Polishy Conformance",
		"GIT_AUTHOR_EMAIL=conformance@code-polishy.invalid",
		"GIT_AUTHOR_DATE="+commitTimestamp,
		"GIT_COMMITTER_NAME=Code Polishy Conformance",
		"GIT_COMMITTER_EMAIL=conformance@code-polishy.invalid",
		"GIT_COMMITTER_DATE="+commitTimestamp,
		"LANG=C",
		"LC_ALL=C",
		"TZ=UTC",
	)
	if globalConfiguration != "" {
		environment = append(environment, "GIT_CONFIG_GLOBAL="+globalConfiguration)
	}
	return environment
}
