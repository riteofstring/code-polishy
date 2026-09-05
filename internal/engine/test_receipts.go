package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/testreceipt"
)

func (engine *Engine) suiteReceiptIdentity(suite policy.TestSuite) (testreceipt.Identity, string, error) {
	if !suite.Reusable {
		return testreceipt.Identity{}, "suite is not explicitly reusable", nil
	}
	if err := validateReusableArguments(suite.Argv); err != nil {
		return testreceipt.Identity{}, err.Error(), nil
	}
	configurationSHA256, releaseIdentity, err := suiteReceiptReleaseIdentity(engine.Repository)
	if err != nil {
		return testreceipt.Identity{}, "", err
	}
	inputs, selected, reason, err := suiteReceiptInputs(engine.Repository, suite)
	if err != nil || reason != "" {
		return testreceipt.Identity{}, reason, err
	}
	environment := suiteReceiptEnvironment(generationReceiptEnvironment(engine.Repository, suite, selected))
	tools := suiteReceiptTools(engine.Repository)
	if reason := unboundedSuiteTool(suite.Argv, tools, selected); reason != "" {
		return testreceipt.Identity{}, reason, nil
	}
	identity := testreceipt.Identity{
		Version: testreceipt.IdentityVersion, Release: releaseIdentity, PolicySchema: engine.Repository.Config.Version,
		ConfigurationSHA256: configurationSHA256, Platform: testreceipt.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
		Suite: cloneReceiptSuite(suite), Environment: environment, Tools: tools, Inputs: inputs,
	}
	if _, err := identity.Digest(); err != nil {
		return testreceipt.Identity{}, "", err
	}
	return identity, "", nil
}

func suiteReceiptReleaseIdentity(repo repository.Repository) (string, testreceipt.Release, error) {
	configuration, err := json.Marshal(repo.Config)
	if err != nil {
		return "", testreceipt.Release{}, err
	}
	digest := testreceiptContentDigest(configuration)
	identity := testreceipt.Release{Version: "development", Digest: digest}
	lock, found, err := release.ReadLock(repo.Root)
	if err != nil {
		return "", testreceipt.Release{}, err
	}
	if found {
		identity = testreceipt.Release{Version: lock.CodePolishyVersion, Digest: lock.ReleaseDigest}
	}
	return digest, identity, nil
}

func suiteReceiptInputs(repo repository.Repository, suite policy.TestSuite) ([]testreceipt.Input, []string, string, error) {
	files, err := repo.AllFiles()
	if err != nil {
		return nil, nil, "", err
	}
	selected, reason := suiteReceiptInputPaths(repo, suite, files)
	if reason != "" {
		return nil, selected, reason, nil
	}
	inputs, reason, err := hashSuiteReceiptInputs(repo, selected)
	return inputs, selected, reason, err
}

func suiteReceiptInputPaths(repo repository.Repository, suite policy.TestSuite, files []string) ([]string, string) {
	modules := suiteReceiptModules(repo.Config, suite)
	selected := map[string]bool{}
	for _, path := range files {
		owned := intersectsStrings(repo.OwnerModuleNames(path), modules)
		declared := policy.MatchesAny(path, suite.Paths) || policy.MatchesAny(path, suite.ExtraInputs)
		if owned || declared || repo.IsControlInput(path) || path == receiptConfigurationPath(repo) {
			selected[path] = true
		}
	}
	if reason := generationReceiptInputs(repo, suite, files, selected); reason != "" {
		return nil, reason
	}
	if local, localReason := localSuiteExecutable(repo, suite.Argv); localReason != "" {
		return nil, localReason
	} else if local != "" {
		selected[local] = true
	}
	result := make([]string, 0, len(selected))
	for path := range selected {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, ""
}

func suiteReceiptModules(config policy.Config, suite policy.TestSuite) []string {
	selected := map[string]bool{}
	queue := append([]string{}, suite.Modules...)
	if suite.Scope == "repository" {
		for _, module := range config.Modules {
			queue = append(queue, module.Name)
		}
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if selected[name] {
			continue
		}
		selected[name] = true
		index, found := config.ModuleByName[name]
		if found {
			queue = append(queue, config.Modules[index].DependsOn...)
		}
	}
	result := make([]string, 0, len(selected))
	for name := range selected {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func receiptConfigurationPath(repo repository.Repository) string {
	configured := repo.Config.ConfigPath
	if configured == "" {
		return policy.ConfigFilename
	}
	if filepath.IsAbs(configured) {
		relative, err := filepath.Rel(repo.Root, configured)
		if err == nil {
			configured = relative
		}
	}
	normalized, err := repo.NormalizePath(configured)
	if err != nil {
		return policy.ConfigFilename
	}
	return normalized
}

func localSuiteExecutable(repo repository.Repository, argv []string) (string, string) {
	if len(argv) == 0 || !strings.ContainsAny(argv[0], "/\\") {
		return "", ""
	}
	executable, reason := relativeSuiteExecutable(repo, argv[0])
	if reason != "" {
		return "", reason
	}
	normalized, err := repo.NormalizePath(executable)
	if err != nil || repo.IsExcluded(normalized) {
		return "", fmt.Sprintf("command executable %q is outside reusable inputs", argv[0])
	}
	info, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(normalized)))
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Sprintf("command executable %q is not a regular contained file", argv[0])
	}
	return normalized, ""
}

func relativeSuiteExecutable(repo repository.Repository, executable string) (string, string) {
	if filepath.IsAbs(executable) {
		relative, err := filepath.Rel(repo.Root, executable)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", fmt.Sprintf("command executable %q is outside the repository", executable)
		}
		executable = relative
	}
	return executable, ""
}

func hashSuiteReceiptInputs(repo repository.Repository, paths []string) ([]testreceipt.Input, string, error) {
	result := make([]testreceipt.Input, 0, len(paths))
	for _, path := range paths {
		absolute := filepath.Join(repo.Root, filepath.FromSlash(path))
		info, err := os.Lstat(absolute)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Sprintf("input %q is linked, special, or unavailable", path), nil
		}
		file, err := os.Open(absolute)
		if err != nil {
			return nil, "", err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return nil, "", err
		}
		result = append(result, testreceipt.Input{Path: path, Mode: uint32(info.Mode()), SHA256: hex.EncodeToString(hash.Sum(nil))})
	}
	return result, "", nil
}

func suiteReceiptEnvironment(names []string) []testreceipt.Environment {
	unique := map[string]bool{}
	for _, name := range names {
		unique[name] = true
	}
	sorted := make([]string, 0, len(unique))
	for name := range unique {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	result := make([]testreceipt.Environment, 0, len(sorted))
	for _, name := range sorted {
		value, present := os.LookupEnv(name)
		entry := testreceipt.Environment{Name: name, Present: present}
		if present {
			entry.SHA256 = testreceiptContentDigest([]byte(value))
		}
		result = append(result, entry)
	}
	return result
}

func suiteReceiptTools(repo repository.Repository) []testreceipt.Tool {
	tools := []testreceipt.Tool{}
	for _, tool := range repo.CommandEnvironment().Tools {
		if tool.Version != "" {
			tools = append(tools, testreceipt.Tool{Name: tool.Name, Version: tool.Version})
		}
	}
	sort.Slice(tools, func(left, right int) bool { return tools[left].Name < tools[right].Name })
	return tools
}

func unboundedSuiteTool(argv []string, tools []testreceipt.Tool, selected []string) string {
	if len(argv) == 0 || strings.ContainsAny(argv[0], "/\\") {
		return ""
	}
	for _, tool := range tools {
		if tool.Name == argv[0] {
			return ""
		}
	}
	for _, allowed := range []string{"npm", "npx", "pnpm"} {
		if argv[0] == allowed && slicesContainBase(selected, "package.json") {
			return ""
		}
	}
	return fmt.Sprintf("command tool %q has no policy-owned version or contained executable input", argv[0])
}

func slicesContainBase(paths []string, name string) bool {
	for _, path := range paths {
		if filepath.Base(path) == name {
			return true
		}
	}
	return false
}

func intersectsStrings(left, right []string) bool {
	values := map[string]bool{}
	for _, value := range left {
		values[value] = true
	}
	for _, value := range right {
		if values[value] {
			return true
		}
	}
	return false
}

func cloneReceiptSuite(suite policy.TestSuite) policy.TestSuite {
	suite.Modules = append([]string{}, suite.Modules...)
	suite.Argv = append([]string{}, suite.Argv...)
	suite.Paths = append([]string{}, suite.Paths...)
	suite.ExtraInputs = append([]string{}, suite.ExtraInputs...)
	suite.Covers = append([]string{}, suite.Covers...)
	suite.Artifacts = append([]policy.TestArtifact{}, suite.Artifacts...)
	suite.RunOn = append([]string{}, suite.RunOn...)
	suite.Environment = append([]string{}, suite.Environment...)
	suite.ExclusiveResources = append([]string{}, suite.ExclusiveResources...)
	return suite
}

func testreceiptContentDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
