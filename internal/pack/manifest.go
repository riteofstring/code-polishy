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

	"github.com/riteofstring/code-polishy/internal/policy"
)

const (
	ManifestFilename = "code-polishy-pack.json"
	ReceiptFilename  = "installation-receipt.json"
	ManifestVersion  = 3
	ProtocolVersion  = 4
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
var semanticVersionPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
var environmentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var shebangPattern = regexp.MustCompile(`^#!\S(?:[^\r\n]*\S)?$`)
var packCapabilities = []string{"format", "lint", "typecheck", "complexity", "dead-code", "architecture", "build", "dependency-policy", "lock-sync", "release-age", "security"}
var packDiscoveryModes = []string{"file-scoped", "static", "evaluated"}
var packExecutionTypes = []string{"self-contained", "host-toolchain"}

type Manifest struct {
	Schema          string     `json:"$schema,omitempty"`
	ManifestVersion int        `json:"manifestVersion"`
	Name            string     `json:"name"`
	Version         string     `json:"version"`
	EngineVersion   string     `json:"engineVersion"`
	ProtocolVersion int        `json:"protocolVersion"`
	Platforms       []string   `json:"platforms"`
	Executables     []string   `json:"executables,omitempty"`
	Languages       []Language `json:"languages"`
	Commands        []Command  `json:"commands"`
	Fixtures        []Fixture  `json:"fixtures"`
}

type Language struct {
	ID                  string                  `json:"id"`
	SourcePatterns      []string                `json:"sourcePatterns,omitempty"`
	TestPatterns        []string                `json:"testPatterns,omitempty"`
	Shebangs            []string                `json:"shebangs,omitempty"`
	DependencyManifests []string                `json:"dependencyManifests,omitempty"`
	DiscoveryMode       string                  `json:"discoveryMode"`
	MetadataPatterns    []string                `json:"metadataPatterns,omitempty"`
	Unsupported         []UnsupportedCapability `json:"unsupportedCapabilities,omitempty"`
}

type UnsupportedCapability struct {
	Capability string `json:"capability"`
	Reason     string `json:"reason"`
}

type Command struct {
	Name           string           `json:"name"`
	Argv           []string         `json:"argv"`
	Languages      []string         `json:"languages"`
	Capabilities   []string         `json:"capabilities"`
	Profiles       []string         `json:"profiles"`
	TimeoutSeconds int              `json:"timeoutSeconds"`
	Environment    []string         `json:"environment,omitempty"`
	Paths          []string         `json:"paths,omitempty"`
	Execution      CommandExecution `json:"execution"`
}

type CommandExecution struct {
	Type    string            `json:"type"`
	Network string            `json:"network"`
	Tools   []policy.PackTool `json:"tools,omitempty"`
}

type Fixture struct {
	Name           string   `json:"name"`
	Command        string   `json:"command"`
	Capability     string   `json:"capability"`
	Project        string   `json:"project"`
	Files          []string `json:"files"`
	ExpectedStatus string   `json:"expectedStatus"`
	ExpectedRules  []string `json:"expectedRules,omitempty"`
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
	if err := validateExecutablePaths(manifest.Executables); err != nil {
		return err
	}
	if len(manifest.Languages) == 0 || len(manifest.Commands) == 0 || len(manifest.Fixtures) == 0 {
		return errors.New("languages, commands, and fixtures must not be empty")
	}
	if err := validateLanguages(manifest.Languages); err != nil {
		return err
	}
	if err := validateCommands(manifest.Commands, manifest.Languages); err != nil {
		return err
	}
	if err := validateCapabilityClaims(manifest.Languages, manifest.Commands); err != nil {
		return err
	}
	return validateFixtures(manifest.Commands, manifest.Fixtures)
}

func validateExecutablePaths(executables []string) error {
	if len(executables) > 256 {
		return errors.New("executables must contain at most 256 paths")
	}
	if err := validateUnique(executables, "executables"); err != nil {
		return err
	}
	for index, executable := range executables {
		if err := exactRelativePath(executable); err != nil {
			return fmt.Errorf("executables[%d]: %w", index, err)
		}
	}
	return nil
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
	if !ValidEngineVersion(manifest.EngineVersion) {
		return errors.New("engineVersion must be an exact semantic version")
	}
	if manifest.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("protocolVersion must be %d", ProtocolVersion)
	}
	return nil
}

func ValidEngineVersion(value string) bool {
	return semanticVersionPattern.MatchString(value)
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
	shebangOwners := map[string]string{}
	manifestOwners := map[string]string{}
	for index, language := range languages {
		if err := validateLanguage(language, index, seen); err != nil {
			return err
		}
		if err := recordPatternOwners(sourceOwners, language.SourcePatterns, language.ID, "source"); err != nil {
			return err
		}
		if err := recordShebangOwners(shebangOwners, language.Shebangs, language.ID); err != nil {
			return err
		}
		if err := recordPatternOwners(manifestOwners, language.DependencyManifests, language.ID, "dependency manifest"); err != nil {
			return err
		}
	}
	return nil
}

func validateLanguage(language Language, index int, seen map[string]bool) error {
	label := fmt.Sprintf("languages[%d]", index)
	if !identifierPattern.MatchString(language.ID) || seen[language.ID] {
		return fmt.Errorf("%s.id is invalid or duplicated", label)
	}
	seen[language.ID] = true
	if err := validateLanguageDiscovery(language, label); err != nil {
		return err
	}
	if err := validateLanguagePatterns(language, label); err != nil {
		return err
	}
	return validateUnsupportedCapabilities(language.Unsupported, label)
}

func validateLanguageDiscovery(language Language, label string) error {
	if len(language.SourcePatterns) == 0 && len(language.Shebangs) == 0 {
		return fmt.Errorf("%s requires sourcePatterns or shebangs", label)
	}
	if !slices.Contains(packDiscoveryModes, language.DiscoveryMode) {
		return expected(label+".discoveryMode", "file-scoped, static, or evaluated")
	}
	if language.DiscoveryMode == "file-scoped" && len(language.MetadataPatterns) != 0 {
		return expected(label+".metadataPatterns", "empty for file-scoped discovery")
	}
	if language.DiscoveryMode != "file-scoped" && len(language.MetadataPatterns) == 0 {
		return expected(label+".metadataPatterns", "at least one pattern for static or evaluated discovery")
	}
	return nil
}

func validateLanguagePatterns(language Language, label string) error {
	if err := validatePatterns(language.SourcePatterns, label+".sourcePatterns"); err != nil {
		return err
	}
	if err := validatePatterns(language.TestPatterns, label+".testPatterns"); err != nil {
		return err
	}
	if err := validateShebangs(language.Shebangs, label+".shebangs"); err != nil {
		return err
	}
	if err := validatePatterns(language.DependencyManifests, label+".dependencyManifests"); err != nil {
		return err
	}
	return validatePatterns(language.MetadataPatterns, label+".metadataPatterns")
}

func validateUnsupportedCapabilities(declarations []UnsupportedCapability, label string) error {
	if declarations != nil && len(declarations) == 0 {
		return expected(label+".unsupportedCapabilities", "one or more explicit capability absences or omission")
	}
	unsupported := map[string]bool{}
	for unsupportedIndex, declaration := range declarations {
		itemLabel := fmt.Sprintf("%s.unsupportedCapabilities[%d]", label, unsupportedIndex)
		if !slices.Contains(packCapabilities, declaration.Capability) || unsupported[declaration.Capability] {
			return expected(itemLabel+".capability", "a unique standard capability")
		}
		if strings.TrimSpace(declaration.Reason) == "" || len(declaration.Reason) > 4096 {
			return expected(itemLabel+".reason", "1 to 4096 non-whitespace bytes")
		}
		unsupported[declaration.Capability] = true
	}
	return nil
}

func validateShebangs(shebangs []string, label string) error {
	if err := validateUnique(shebangs, label); err != nil {
		return err
	}
	for _, shebang := range shebangs {
		if len(shebang) > 128 || !shebangPattern.MatchString(shebang) || strings.ContainsRune(shebang, 0) {
			return expected(label, "unique canonical shebang prefixes of at most 128 bytes")
		}
	}
	return nil
}

func validateCapabilityClaims(languages []Language, commands []Command) error {
	for languageIndex, language := range languages {
		for unsupportedIndex, declaration := range language.Unsupported {
			for _, command := range commands {
				if slices.Contains(command.Languages, language.ID) && slices.Contains(command.Capabilities, declaration.Capability) {
					return expected(fmt.Sprintf("languages[%d].unsupportedCapabilities[%d].capability", languageIndex, unsupportedIndex), "a capability not provided for this language")
				}
			}
		}
	}
	return nil
}

func recordShebangOwners(owners map[string]string, shebangs []string, language string) error {
	for _, shebang := range shebangs {
		for owned, owner := range owners {
			if shebang == owned || strings.HasPrefix(shebang, owned+" ") || strings.HasPrefix(owned, shebang+" ") {
				return fmt.Errorf("shebang prefix %q is owned by both %s and %s", shebang, owner, language)
			}
		}
		owners[shebang] = language
	}
	return nil
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

func validateCommands(commands []Command, languages []Language) error {
	seen := map[string]bool{}
	knownLanguages := make([]string, 0, len(languages))
	for _, language := range languages {
		knownLanguages = append(knownLanguages, language.ID)
	}
	profiles := []string{"check", "gate", "format", "build", "supply-chain", "supply-chain-online", "security"}
	for index, command := range commands {
		if err := validateCommand(command, index, seen, knownLanguages, packCapabilities, profiles); err != nil {
			return err
		}
	}
	return nil
}

func validateCommand(command Command, index int, seen map[string]bool, languages, capabilities, profiles []string) error {
	label := fmt.Sprintf("commands[%d]", index)
	if !identifierPattern.MatchString(command.Name) || seen[command.Name] {
		return fmt.Errorf("%s.name is invalid or duplicated", label)
	}
	seen[command.Name] = true
	if err := validateCommandArgv(command.Argv, label); err != nil {
		return err
	}
	if err := validateAllowed(command.Languages, languages, label+".languages"); err != nil {
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
	if err := validatePatterns(command.Paths, label+".paths"); err != nil {
		return err
	}
	if err := validateCommandExecution(command, label); err != nil {
		return err
	}
	return validateCommandEnvironment(command.Environment, label)
}

func validateCommandExecution(command Command, label string) error {
	if !slices.Contains(packExecutionTypes, command.Execution.Type) {
		return expected(label+".execution.type", "self-contained or host-toolchain")
	}
	if command.Execution.Network != "none" {
		return expected(label+".execution.network", "none")
	}
	if command.Execution.Type == "self-contained" && len(command.Execution.Tools) != 0 {
		return expected(label+".execution.tools", "empty for self-contained execution")
	}
	if command.Execution.Type == "host-toolchain" && len(command.Execution.Tools) == 0 {
		return expected(label+".execution.tools", "at least one exact host tool identity")
	}
	return validateToolDeclarations(command.Execution.Tools, label+".execution.tools")
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
		if coverage[fixture.Command+":"+fixture.Capability] == nil {
			coverage[fixture.Command+":"+fixture.Capability] = map[string]bool{}
		}
		coverage[fixture.Command+":"+fixture.Capability][fixture.ExpectedStatus] = true
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
	if len(fixture.Files) == 0 {
		return fmt.Errorf("%s.files must select real source", label)
	}
	if err := validateUnique(fixture.Files, label+".files"); err != nil {
		return err
	}
	for _, file := range fixture.Files {
		if err := exactRelativePath(file); err != nil {
			return fmt.Errorf("%s.files: %w", label, err)
		}
	}
	return validateFixtureExpectation(fixture, label)
}

func validateFixtureExpectation(fixture Fixture, label string) error {
	if !slices.Contains([]string{"pass", "findings", "incomplete", "operational-failure"}, fixture.ExpectedStatus) {
		return fmt.Errorf("%s.expectedStatus is invalid", label)
	}
	if fixture.ExpectedStatus == "findings" && len(fixture.ExpectedRules) == 0 {
		return fmt.Errorf("%s requires expectedRules for the seeded defect", label)
	}
	if slices.Contains([]string{"pass", "operational-failure"}, fixture.ExpectedStatus) && len(fixture.ExpectedRules) > 0 {
		return fmt.Errorf("%s expectedRules require findings or incomplete status", label)
	}
	for _, rule := range fixture.ExpectedRules {
		if !validRule(rule) {
			return fmt.Errorf("%s has an invalid expected rule", label)
		}
	}
	if err := validateUnique(fixture.ExpectedRules, label+".expectedRules"); err != nil {
		return err
	}
	return nil
}

func validateFixtureCoverage(commands []Command, coverage map[string]map[string]bool) error {
	for _, command := range commands {
		for _, capability := range command.Capabilities {
			covered := coverage[command.Name+":"+capability]
			if !covered["pass"] || !covered["findings"] {
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

func validateToolDeclarations(tools []policy.PackTool, label string) error {
	identities := map[string]bool{}
	names := map[string]bool{}
	environmentNames := map[string]bool{}
	launchers := 0
	for index, tool := range tools {
		item := fmt.Sprintf("%s[%d]", label, index)
		environmentName := toolEnvironmentName(tool.ID)
		if !identifierPattern.MatchString(tool.ID) || identities[tool.ID] || environmentNames[environmentName] {
			return expected(item+".id", "a unique lowercase identifier")
		}
		if !identifierPattern.MatchString(tool.Name) || names[tool.Name] {
			return expected(item+".name", "a unique lowercase tool name")
		}
		if !semanticVersionPattern.MatchString(tool.Version) {
			return expected(item+".version", "an exact semantic version")
		}
		identities[tool.ID] = true
		names[tool.Name] = true
		environmentNames[environmentName] = true
		if tool.Launcher {
			launchers++
		}
	}
	if launchers > 1 {
		return expected(label, "at most one launcher")
	}
	return nil
}
