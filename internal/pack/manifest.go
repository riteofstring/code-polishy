package pack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"runtime"
	"slices"
	"strings"
)

const (
	ManifestFilename = "code-polishy-pack.json"
	ReceiptFilename  = "installation-receipt.json"
	ManifestVersion  = 1
	ProtocolVersion  = 1
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
var semanticVersionPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
var environmentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Manifest struct {
	Schema          string     `json:"$schema,omitempty"`
	ManifestVersion int        `json:"manifestVersion"`
	Name            string     `json:"name"`
	Version         string     `json:"version"`
	ProtocolVersion int        `json:"protocolVersion"`
	Platforms       []string   `json:"platforms"`
	Languages       []Language `json:"languages"`
	Commands        []Command  `json:"commands"`
	Fixtures        []Fixture  `json:"fixtures"`
}

type Language struct {
	ID                  string   `json:"id"`
	SourcePatterns      []string `json:"sourcePatterns,omitempty"`
	DependencyManifests []string `json:"dependencyManifests,omitempty"`
}

type Command struct {
	Name           string   `json:"name"`
	Argv           []string `json:"argv"`
	Capabilities   []string `json:"capabilities"`
	Profiles       []string `json:"profiles"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	Environment    []string `json:"environment,omitempty"`
}

type Fixture struct {
	Name           string   `json:"name"`
	Command        string   `json:"command"`
	Capability     string   `json:"capability"`
	Project        string   `json:"project"`
	Files          []string `json:"files"`
	ExpectedStatus string   `json:"expectedStatus"`
}

func ParseManifest(data []byte, source string) (Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	manifest := Manifest{}
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", source, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("contains more than one JSON value")
		}
		return Manifest{}, fmt.Errorf("parse %s: %w", source, err)
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, fmt.Errorf("validate %s: %w", source, err)
	}
	return manifest, nil
}

func validateManifest(manifest Manifest) error {
	if err := validateManifestIdentity(manifest); err != nil {
		return err
	}
	if err := validatePlatforms(manifest.Platforms); err != nil {
		return err
	}
	if len(manifest.Languages) == 0 || len(manifest.Commands) == 0 || len(manifest.Fixtures) == 0 {
		return errors.New("languages, commands, and fixtures must not be empty")
	}
	if err := validateLanguages(manifest.Languages); err != nil {
		return err
	}
	if err := validateCommands(manifest.Commands); err != nil {
		return err
	}
	return validateFixtures(manifest.Commands, manifest.Fixtures)
}

func validateManifestIdentity(manifest Manifest) error {
	if manifest.ManifestVersion != ManifestVersion {
		return fmt.Errorf("manifestVersion must be %d", ManifestVersion)
	}
	if !identifierPattern.MatchString(manifest.Name) {
		return errors.New("name must be a lowercase identifier")
	}
	if !semanticVersionPattern.MatchString(manifest.Version) {
		return errors.New("version must be an exact semantic version")
	}
	if manifest.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("protocolVersion must be %d", ProtocolVersion)
	}
	return nil
}

func validatePlatforms(platforms []string) error {
	if len(platforms) == 0 {
		return errors.New("platforms must not be empty")
	}
	if err := validateUnique(platforms, "platforms"); err != nil {
		return err
	}
	allowed := []string{"darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64", "windows-amd64"}
	return validateAllowedValues(platforms, allowed, "platform")
}

func validateLanguages(languages []Language) error {
	seen := map[string]bool{}
	sourceOwners := map[string]string{}
	manifestOwners := map[string]string{}
	builtIn := []string{"dart", "go", "jvm", "native", "php", "protobuf", "python", "ruby", "rust", "shell", "sql", "swift", "typescript"}
	for index, language := range languages {
		if err := validateLanguage(language, index, seen, builtIn); err != nil {
			return err
		}
		if err := recordPatternOwners(sourceOwners, language.SourcePatterns, language.ID, "source"); err != nil {
			return err
		}
		if err := recordPatternOwners(manifestOwners, language.DependencyManifests, language.ID, "dependency manifest"); err != nil {
			return err
		}
	}
	return nil
}

func validateLanguage(language Language, index int, seen map[string]bool, builtIn []string) error {
	if !identifierPattern.MatchString(language.ID) || seen[language.ID] {
		return fmt.Errorf("languages[%d].id is invalid or duplicated", index)
	}
	seen[language.ID] = true
	if len(language.SourcePatterns) == 0 && !slices.Contains(builtIn, language.ID) {
		return fmt.Errorf("custom language %q requires sourcePatterns", language.ID)
	}
	if err := validatePatterns(language.SourcePatterns, fmt.Sprintf("languages[%d].sourcePatterns", index)); err != nil {
		return err
	}
	return validatePatterns(language.DependencyManifests, fmt.Sprintf("languages[%d].dependencyManifests", index))
}

func recordPatternOwners(owners map[string]string, patterns []string, language, kind string) error {
	for _, pattern := range patterns {
		if owner := owners[pattern]; owner != "" {
			return fmt.Errorf("%s pattern %q is owned by both %s and %s", kind, pattern, owner, language)
		}
		owners[pattern] = language
	}
	return nil
}

func validateCommands(commands []Command) error {
	seen := map[string]bool{}
	capabilities := []string{"format", "lint", "typecheck", "complexity", "dead-code", "architecture", "build", "dependency-policy", "lock-sync", "release-age", "security"}
	profiles := []string{"check", "gate", "format", "build", "supply-chain", "supply-chain-online", "security"}
	for index, command := range commands {
		if err := validateCommand(command, index, seen, capabilities, profiles); err != nil {
			return err
		}
	}
	return nil
}

func validateCommand(command Command, index int, seen map[string]bool, capabilities, profiles []string) error {
	label := fmt.Sprintf("commands[%d]", index)
	if !identifierPattern.MatchString(command.Name) || seen[command.Name] {
		return fmt.Errorf("%s.name is invalid or duplicated", label)
	}
	seen[command.Name] = true
	if err := validateCommandArgv(command.Argv, label); err != nil {
		return err
	}
	if err := validateAllowed(command.Capabilities, capabilities, label+".capabilities"); err != nil {
		return err
	}
	if err := validateAllowed(command.Profiles, profiles, label+".profiles"); err != nil {
		return err
	}
	if command.TimeoutSeconds < 1 || command.TimeoutSeconds > 3600 {
		return fmt.Errorf("%s.timeoutSeconds must be between 1 and 3600", label)
	}
	return validateCommandEnvironment(command.Environment, label)
}

func validateCommandArgv(argv []string, label string) error {
	if len(argv) == 0 {
		return fmt.Errorf("%s.argv must not be empty", label)
	}
	for _, argument := range argv {
		if strings.TrimSpace(argument) == "" || strings.ContainsRune(argument, 0) {
			return fmt.Errorf("%s.argv contains an empty or invalid argument", label)
		}
	}
	if err := exactRelativePath(argv[0]); err != nil {
		return fmt.Errorf("%s.argv[0]: %w", label, err)
	}
	return nil
}

func validateCommandEnvironment(environment []string, label string) error {
	if err := validateUnique(environment, label+".environment"); err != nil {
		return err
	}
	forbidden := []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP", "LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH", "NODE_OPTIONS", "PYTHONPATH", "RUBYOPT", "JAVA_TOOL_OPTIONS", "GIT_CONFIG", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"}
	for _, name := range environment {
		if !environmentPattern.MatchString(name) || slices.Contains(forbidden, name) {
			return fmt.Errorf("%s.environment contains invalid or process-control name %q", label, name)
		}
	}
	return nil
}

func validateFixtures(commands []Command, fixtures []Fixture) error {
	provided := providedCapabilities(commands)
	seen := map[string]bool{}
	coverage := map[string]map[string]bool{}
	for index, fixture := range fixtures {
		if err := validateFixture(fixture, index, provided, seen); err != nil {
			return err
		}
		if coverage[fixture.Capability] == nil {
			coverage[fixture.Capability] = map[string]bool{}
		}
		coverage[fixture.Capability][fixture.ExpectedStatus] = true
	}
	return validateFixtureCoverage(commands, coverage)
}

func providedCapabilities(commands []Command) map[string]map[string]bool {
	provided := map[string]map[string]bool{}
	for _, command := range commands {
		provided[command.Name] = map[string]bool{}
		for _, capability := range command.Capabilities {
			provided[command.Name][capability] = true
		}
	}
	return provided
}

func validateFixture(fixture Fixture, index int, provided map[string]map[string]bool, seen map[string]bool) error {
	label := fmt.Sprintf("fixtures[%d]", index)
	if !identifierPattern.MatchString(fixture.Name) || seen[fixture.Name] {
		return fmt.Errorf("%s.name is invalid or duplicated", label)
	}
	seen[fixture.Name] = true
	if !provided[fixture.Command][fixture.Capability] {
		return fmt.Errorf("%s references a command that does not provide %q", label, fixture.Capability)
	}
	if err := exactRelativePath(fixture.Project); err != nil {
		return fmt.Errorf("%s.project: %w", label, err)
	}
	for _, file := range fixture.Files {
		if err := exactRelativePath(file); err != nil {
			return fmt.Errorf("%s.files: %w", label, err)
		}
	}
	if !slices.Contains([]string{"pass", "findings", "operational-failure"}, fixture.ExpectedStatus) {
		return fmt.Errorf("%s.expectedStatus is invalid", label)
	}
	return nil
}

func validateFixtureCoverage(commands []Command, coverage map[string]map[string]bool) error {
	for _, command := range commands {
		for _, capability := range command.Capabilities {
			covered := coverage[capability]
			if !covered["pass"] || !(covered["findings"] || covered["operational-failure"]) {
				return fmt.Errorf("capability %q requires passing and deliberately failing fixtures", capability)
			}
		}
	}
	return nil
}

func validateAllowedValues(values, allowed []string, label string) error {
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return fmt.Errorf("unsupported %s %q", label, value)
		}
	}
	return nil
}

func validateAllowed(values, allowed []string, label string) error {
	if len(values) == 0 {
		return fmt.Errorf("%s must not be empty", label)
	}
	if err := validateUnique(values, label); err != nil {
		return err
	}
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return fmt.Errorf("%s contains unsupported value %q", label, value)
		}
	}
	return nil
}

func validatePatterns(patterns []string, label string) error {
	if err := validateUnique(patterns, label); err != nil {
		return err
	}
	for _, pattern := range patterns {
		if strings.TrimSpace(pattern) != pattern || pattern == "" || strings.Contains(pattern, "\\") || strings.HasPrefix(pattern, "/") || strings.Contains("/"+pattern+"/", "/../") {
			return fmt.Errorf("%s contains unsafe pattern %q", label, pattern)
		}
	}
	return nil
}

func validateUnique(values []string, label string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return fmt.Errorf("%s contains duplicate value %q", label, value)
		}
		seen[value] = true
	}
	return nil
}

func exactRelativePath(value string) error {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || path.Clean(value) != value || value == "." || value == ".." || strings.HasPrefix(value, "../") {
		return fmt.Errorf("%q must be an exact contained relative path", value)
	}
	return nil
}

func CurrentPlatform() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}
