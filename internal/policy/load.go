package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
var environmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var siblingFallbackPattern = regexp.MustCompile(`^(?:\.\./)+[A-Za-z0-9][A-Za-z0-9._-]*(?:/[A-Za-z0-9][A-Za-z0-9._-]*)*$`)

func Load(repoRoot, configPath string) (Config, error) {
	if configPath == "" {
		configPath = filepath.Join(repoRoot, ConfigFilename)
	} else if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(repoRoot, configPath)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", configPath, err)
	}
	return Parse(data, configPath)
}

// Parse compiles a complete policy configuration from trusted bytes. Callers
// that read configuration from an immutable source, such as a Git commit, use
// this entry point so candidate working-tree configuration cannot affect the
// result.
func Parse(data []byte, source string) (Config, error) {
	if source == "" {
		source = ConfigFilename
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", source, err)
	}
	if err := requireEOF(decoder); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", source, err)
	}
	config.ConfigPath = source
	applyDefaults(&config)
	if err := validate(&config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("configuration contains more than one JSON value")
}

func applyDefaults(config *Config) {
	defaultInt(&config.Quality.MaxFileLines, MaxFileLines)
	defaultInt(&config.Quality.MaxTestFileLines, MaxTestFileLines)
	defaultInt(&config.Quality.Complexity.Go, MaxGoComplexity)
	defaultInt(&config.Quality.Complexity.GoTest, MaxGoTestComplexity)
	defaultInt(&config.Quality.Complexity.TypeScript, MaxTypeScriptComplexity)
	defaultInt(&config.Quality.Complexity.TypeScriptTest, MaxTypeScriptTestComplexity)
	defaultInt(&config.Quality.MaxDepth, MaxTypeScriptDepth)
	defaultInt(&config.Quality.MaxTestDepth, MaxTypeScriptTestDepth)
	defaultInt(&config.Quality.MaxParams, MaxTypeScriptParams)
	defaultInt(&config.Quality.MaxTestParams, MaxTypeScriptTestParams)
	defaultInt(&config.SupplyChain.MinimumReleaseAgeDays, MinimumReleaseAgeDays)
	defaultInt(&config.SupplyChain.PreferredNewDependencyAgeDays, PreferredNewDependencyAgeDays)
	defaultString(&config.SupplyChain.AuditLevel, "low")
	defaultStrings(&config.SupplyChain.AllowedDependencyProtocols, []string{"workspace:", "file:", "link:"})
	defaultString(&config.SupplyChain.NPMRegistryURL, "https://registry.npmjs.org")
	defaultString(&config.SupplyChain.ArtifactSecurity.OutputDirectory, ".code-polishy-reports/artifact-security")
	for index := range config.SupplyChain.ArtifactSecurity.Targets {
		target := &config.SupplyChain.ArtifactSecurity.Targets[index]
		if target.Producer != nil {
			defaultString(&target.Producer.Cwd, ".")
			defaultString(&target.Producer.Manifest, "artifacts.json")
			defaultInt(&target.Producer.TimeoutSeconds, 1800)
		}
	}
	for index := range config.Checks {
		commandDefaults(&config.Checks[index], []string{"check", "gate"})
	}
	for index := range config.Tests.Suites {
		suiteDefaults(&config.Tests.Suites[index])
	}
	for index := range config.PolicyModules.Overrides {
		defaultString(&config.PolicyModules.Overrides[index].Root, ".")
		config.PolicyModules.Overrides[index].Root = pathpkg.Clean(config.PolicyModules.Overrides[index].Root)
	}
}

func defaultInt(target *int, value int) {
	if *target == 0 {
		*target = value
	}
}

func defaultString(target *string, value string) {
	if *target == "" {
		*target = value
	}
}

func defaultStrings(target *[]string, value []string) {
	if *target == nil {
		*target = append([]string{}, value...)
	}
}

func suiteDefaults(suite *TestSuite) {
	// Performance measurements are meaningful only when no other governed
	// performance suite is competing for the host. This lease belongs to the
	// shared baseline, so target configuration may add resources but cannot
	// remove or replace it with an explicit empty or different list.
	if suite.Kind == "performance" && !slices.Contains(suite.ExclusiveResources, "performance") {
		suite.ExclusiveResources = append(suite.ExclusiveResources, "performance")
	}
	defaultString(&suite.Cwd, ".")
	defaultInt(&suite.TimeoutSeconds, 900)
	defaultString(&suite.Cost, defaultSuiteCost(*suite))
	normalizeExclusiveResources(&suite.ExclusiveResources)
	if suite.RunOn != nil {
		return
	}
	if supplementalOnlyKind(suite.Kind) {
		suite.RunOn = []string{"supplemental"}
		return
	}
	if suite.Scope == "module" {
		suite.RunOn = []string{"focused", "recommended", "full"}
		return
	}
	suite.RunOn = []string{"full"}
}

func defaultSuiteCost(suite TestSuite) string {
	if slices.Contains([]string{"browser", "visual", "e2e", "performance", "live"}, suite.Kind) || supplementalOnlyKind(suite.Kind) {
		return "expensive"
	}
	if suite.Scope == "module" {
		return "quick"
	}
	return "standard"
}

func commandDefaults(command *Command, runOn []string) {
	if command.Cwd == "" {
		command.Cwd = "."
	}
	if command.TimeoutSeconds == 0 {
		command.TimeoutSeconds = 900
	}
	if command.RunOn == nil {
		command.RunOn = append([]string{}, runOn...)
	}
	normalizeExclusiveResources(&command.ExclusiveResources)
}

func normalizeExclusiveResources(resources *[]string) {
	if *resources == nil {
		*resources = []string{}
		return
	}
	slices.Sort(*resources)
}

func validate(config *Config) error {
	if err := validateBase(config); err != nil {
		return err
	}
	if err := validateModules(config); err != nil {
		return err
	}
	if err := validateVerification(config); err != nil {
		return err
	}
	if err := validateChecks(config); err != nil {
		return err
	}
	if err := validateTests(config); err != nil {
		return err
	}
	if err := validatePortability(config); err != nil {
		return err
	}
	if err := validateSupplyChain(config.SupplyChain); err != nil {
		return err
	}
	if err := validatePolicyModules(config.PolicyModules); err != nil {
		return err
	}
	return validateExceptions(config.Exceptions)
}

func validateVerification(config *Config) error {
	if target := config.Verification.TrustedMergeTarget; target != "" {
		if strings.TrimSpace(target) != target || strings.HasPrefix(target, "-") || strings.ContainsAny(target, " \t\r\n\x00") {
			return errors.New("verification.trustedMergeTarget must be a non-option Git reference without whitespace")
		}
	}
	mergeGate := config.Verification.MergeGate
	if mergeGate == nil {
		return nil
	}
	if len(mergeGate.RecommendedModules) == 0 {
		return errors.New("verification.mergeGate.recommendedModules must not be empty")
	}
	if err := validateUniqueStrings(mergeGate.RecommendedModules, "verification.mergeGate.recommendedModules", true); err != nil {
		return err
	}
	for _, module := range mergeGate.RecommendedModules {
		if _, exists := config.ModuleByName[module]; !exists {
			return fmt.Errorf("verification.mergeGate.recommendedModules references unknown module %q", module)
		}
	}
	return nil
}

func validatePortability(config *Config) error {
	seen := map[string]bool{}
	for index, input := range config.Portability.ExternalInputs {
		label := fmt.Sprintf("portability.externalInputs[%d]", index)
		if err := validateExternalInput(config, input, label, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateExternalInput(config *Config, input ExternalInput, label string, seen map[string]bool) error {
	if err := validateExternalInputIdentity(config, input, label, seen); err != nil {
		return err
	}
	if err := validateExternalInputResolution(input, label); err != nil {
		return err
	}
	return validateExternalInputEvidence(config, input, label)
}

func validateExternalInputIdentity(config *Config, input ExternalInput, label string, seen map[string]bool) error {
	if err := identifier(input.Name, label+".name"); err != nil {
		return err
	}
	if seen[input.Name] {
		return fmt.Errorf("duplicate external input name %q", input.Name)
	}
	seen[input.Name] = true
	if err := allowedValues([]string{input.Kind}, []string{"directory", "file", "repository", "service"}, label+".kind"); err != nil {
		return err
	}
	if _, exists := config.ModuleByName[input.Module]; !exists {
		return fmt.Errorf("%s.module references unknown module %q", label, input.Module)
	}
	if len(input.SourcePaths) == 0 {
		return fmt.Errorf("%s.sourcePaths must not be empty", label)
	}
	if err := validatePatterns(input.SourcePaths, label+".sourcePaths", false); err != nil {
		return err
	}
	return nil
}

func validateExternalInputResolution(input ExternalInput, label string) error {
	if err := allowedValues(input.Resolution, []string{"cli", "config", "default", "environment"}, label+".resolution"); err != nil {
		return err
	}
	if err := validateEnvironment(input.Environment, label+".environment"); err != nil {
		return err
	}
	usesEnvironment := slices.Contains(input.Resolution, "environment")
	if usesEnvironment != (len(input.Environment) > 0) {
		return fmt.Errorf("%s.environment must be non-empty exactly when resolution contains environment", label)
	}
	if err := allowedValues([]string{input.UnavailableBehavior}, []string{"fail", "warn"}, label+".unavailableBehavior"); err != nil {
		return err
	}
	if input.SiblingFallback != "" {
		if input.Kind == "service" {
			return fmt.Errorf("%s.siblingFallback is not supported for service inputs", label)
		}
		if !slices.Contains(input.Resolution, "default") {
			return fmt.Errorf("%s.siblingFallback requires default resolution", label)
		}
		if !siblingFallbackPattern.MatchString(input.SiblingFallback) {
			return fmt.Errorf("%s.siblingFallback must be an exact portable parent-relative path", label)
		}
	}
	if slices.Contains(input.Resolution, "default") && input.Resolution[len(input.Resolution)-1] != "default" {
		return fmt.Errorf("%s.resolution must place default last", label)
	}
	return nil
}

func validateExternalInputEvidence(config *Config, input ExternalInput, label string) error {
	contract, err := referencedSuite(config.Tests.Suites, input.ContractSuite, label+".contractSuite")
	if err != nil {
		return err
	}
	if contract.Scope != "module" || len(contract.Modules) != 1 || contract.Modules[0] != input.Module || contract.Cost != "quick" || !slices.Contains(contract.RunOn, "focused") {
		return fmt.Errorf("%s.contractSuite must name a quick focused suite owned by module %q", label, input.Module)
	}
	behavior, err := referencedSuite(config.Tests.Suites, input.BehaviorSuite, label+".behaviorSuite")
	if err != nil {
		return err
	}
	if input.ContractSuite == input.BehaviorSuite {
		return fmt.Errorf("%s must use distinct contractSuite and behaviorSuite evidence", label)
	}
	if !slices.Contains(behavior.RunOn, "full") || slices.Contains(behavior.RunOn, "supplemental") {
		return fmt.Errorf("%s.behaviorSuite must name an ordinary suite included in full", label)
	}
	return nil
}

func referencedSuite(suites []TestSuite, name, label string) (TestSuite, error) {
	if err := identifier(name, label); err != nil {
		return TestSuite{}, err
	}
	for _, suite := range suites {
		if suite.Name == name {
			return suite, nil
		}
	}
	return TestSuite{}, fmt.Errorf("%s references unknown test suite %q", label, name)
}

func validateBase(config *Config) error {
	// The sealed runtime reads exactly one configuration version. A version-2
	// file is the submodule-era shape, and it is refused rather than migrated:
	// nothing here reads a field it no longer owns or guesses what an older
	// target meant.
	if config.Version != ConfigVersion {
		return fmt.Errorf("unsupported policy version %d; expected %d", config.Version, ConfigVersion)
	}
	if err := nonempty(config.Project.Kind, "project.kind"); err != nil {
		return err
	}
	if err := validateUniqueStrings(config.Project.Capabilities, "project.capabilities", true); err != nil {
		return err
	}
	if err := validateQuality(config.Quality); err != nil {
		return err
	}
	return validateScope(config)
}

func validateQuality(quality Quality) error {
	limits := []struct {
		label string
		value int
		max   int
	}{
		{"quality.maxFileLines", quality.MaxFileLines, MaxFileLines},
		{"quality.maxTestFileLines", quality.MaxTestFileLines, MaxTestFileLines},
		{"quality.complexity.go", quality.Complexity.Go, MaxGoComplexity},
		{"quality.complexity.goTest", quality.Complexity.GoTest, MaxGoTestComplexity},
		{"quality.complexity.typescript", quality.Complexity.TypeScript, MaxTypeScriptComplexity},
		{"quality.complexity.typescriptTest", quality.Complexity.TypeScriptTest, MaxTypeScriptTestComplexity},
		{"quality.maxDepth", quality.MaxDepth, MaxTypeScriptDepth},
		{"quality.maxTestDepth", quality.MaxTestDepth, MaxTypeScriptTestDepth},
		{"quality.maxParams", quality.MaxParams, MaxTypeScriptParams},
		{"quality.maxTestParams", quality.MaxTestParams, MaxTypeScriptTestParams},
	}
	for _, limit := range limits {
		if limit.value < 1 || limit.value > limit.max {
			return fmt.Errorf("%s must be between 1 and %d", limit.label, limit.max)
		}
	}
	return nil
}

func validateScope(config *Config) error {
	if err := validatePatterns(config.Scope.Exclude, "scope.exclude", true); err != nil {
		return err
	}
	if err := validatePatterns(config.Scope.Generated, "scope.generated", true); err != nil {
		return err
	}
	if err := validatePatterns(config.Scope.EntryPoints, "scope.entryPoints", true); err != nil {
		return err
	}
	if err := validatePatterns(config.Scope.Development, "scope.development", true); err != nil {
		return err
	}
	return validateLanguageRules(config.Scope.Languages)
}

func validateLanguageRules(rules []LanguageRule) error {
	reserved := []string{"go", "typescript", "python", "shell", "rust", "jvm", "ruby", "php", "swift", "native", "protobuf", "sql", "dart"}
	seen := map[string]bool{}
	for index, rule := range rules {
		label := fmt.Sprintf("scope.languages[%d]", index)
		if err := identifier(rule.Name, label+".name"); err != nil {
			return err
		}
		if seen[rule.Name] || slices.Contains(reserved, rule.Name) {
			return fmt.Errorf("%s.name must be a unique custom language identifier", label)
		}
		seen[rule.Name] = true
		if len(rule.Paths) == 0 {
			return fmt.Errorf("%s.paths must not be empty", label)
		}
		if err := validatePatterns(rule.Paths, label+".paths", true); err != nil {
			return err
		}
	}
	return nil
}

func validateModules(config *Config) error {
	if len(config.Modules) == 0 {
		return errors.New("modules must contain at least one named module")
	}
	config.ModuleByName = make(map[string]int, len(config.Modules))
	for index, module := range config.Modules {
		if err := validateModuleDefinition(module, index, config.ModuleByName); err != nil {
			return err
		}
		config.ModuleByName[module.Name] = index
	}
	if err := validateModuleDependencies(config); err != nil {
		return err
	}
	if err := validateArtifactTargetModules(config); err != nil {
		return err
	}
	if cycle := moduleCycle(*config); len(cycle) > 0 {
		return fmt.Errorf("module dependency graph must be acyclic: %s", strings.Join(cycle, " -> "))
	}
	return nil
}

func validateModuleDefinition(module Module, index int, existing map[string]int) error {
	label := fmt.Sprintf("modules[%d]", index)
	if err := identifier(module.Name, label+".name"); err != nil {
		return err
	}
	if _, exists := existing[module.Name]; exists {
		return fmt.Errorf("duplicate module name %q", module.Name)
	}
	if len(module.Paths) == 0 {
		return fmt.Errorf("%s.paths must not be empty", label)
	}
	if err := validatePatterns(module.Paths, label+".paths", false); err != nil {
		return err
	}
	if err := validateUniqueStrings(module.DependsOn, label+".dependsOn", true); err != nil {
		return err
	}
	return validateUniqueStrings(module.Capabilities, label+".capabilities", true)
}

func validateArtifactTargetModules(config *Config) error {
	for _, target := range config.SupplyChain.ArtifactSecurity.Targets {
		if target.Module == "" {
			continue
		}
		if _, exists := config.ModuleByName[target.Module]; !exists {
			return fmt.Errorf("artifact security target %q references unknown module %q", target.Name, target.Module)
		}
	}
	return nil
}

func validateModuleDependencies(config *Config) error {
	for _, module := range config.Modules {
		for _, dependency := range module.DependsOn {
			if dependency == module.Name {
				return fmt.Errorf("module %q cannot depend on itself", module.Name)
			}
			if _, exists := config.ModuleByName[dependency]; !exists {
				return fmt.Errorf("module %q depends on unknown module %q", module.Name, dependency)
			}
		}
	}
	return nil
}

func validateChecks(config *Config) error {
	commandNames := map[string]bool{}
	for index := range config.Checks {
		if err := validateCommand(config, &config.Checks[index], fmt.Sprintf("checks[%d]", index), commandNames); err != nil {
			return err
		}
		if len(config.Checks[index].Provides) == 0 {
			return fmt.Errorf("checks[%d].provides must not be empty", index)
		}
		if err := validateUniqueStrings(config.Checks[index].Provides, fmt.Sprintf("checks[%d].provides", index), true); err != nil {
			return err
		}
		if err := allowedValues(config.Checks[index].RunOn, []string{"check", "gate", "format", "build", "security", "supply-chain", "supply-chain-online"}, fmt.Sprintf("checks[%d].runOn", index)); err != nil {
			return err
		}
	}
	return nil
}

func validateTests(config *Config) error {
	if len(config.Tests.Suites) == 0 {
		return errors.New("tests.suites must contain at least one deterministic suite")
	}
	if err := validateUniqueStrings(config.Tests.RequiredKinds, "tests.requiredKinds", true); err != nil {
		return err
	}
	if err := validateUniqueStrings(config.Tests.RequiredSupplementalKinds, "tests.requiredSupplementalKinds", true); err != nil {
		return err
	}
	suiteNames := map[string]bool{}
	for index := range config.Tests.Suites {
		if err := validateTestSuite(config, &config.Tests.Suites[index], index, suiteNames); err != nil {
			return err
		}
	}
	return nil
}

func validateTestSuite(config *Config, suite *TestSuite, index int, names map[string]bool) error {
	label := fmt.Sprintf("tests.suites[%d]", index)
	command := Command{Name: suite.Name, Argv: suite.Argv, Cwd: suite.Cwd, Paths: suite.Paths, Modules: suite.Modules, Environment: suite.Environment, ExclusiveResources: suite.ExclusiveResources, TimeoutSeconds: suite.TimeoutSeconds}
	if err := validateCommand(config, &command, label, names); err != nil {
		return err
	}
	if err := validateTestCommandArgv(suite.Argv, label); err != nil {
		return err
	}
	if err := identifier(suite.Kind, label+".kind"); err != nil {
		return err
	}
	if err := validateTestEvidence(suite, label); err != nil {
		return err
	}
	return validateTestProfiles(suite, label)
}

func validateTestProfiles(suite *TestSuite, label string) error {
	if suite.Kind == "live" {
		return fmt.Errorf("%s.kind live requires a typed external approval gate and cannot be an automatic test suite", label)
	}
	if err := allowedValues([]string{suite.Cost}, []string{"quick", "standard", "expensive"}, label+".cost"); err != nil {
		return err
	}
	if err := allowedValues(suite.RunOn, []string{"focused", "recommended", "full", "supplemental"}, label+".runOn"); err != nil {
		return err
	}
	if err := validateSupplementalProfile(suite, label); err != nil {
		return err
	}
	return validateNestedTestProfiles(suite, label)
}

func validateSupplementalProfile(suite *TestSuite, label string) error {
	if slices.Contains(suite.RunOn, "supplemental") {
		if len(suite.RunOn) != 1 {
			return fmt.Errorf("%s.runOn must contain only supplemental when supplemental is selected", label)
		}
		if suite.Cost != "expensive" {
			return fmt.Errorf("%s.cost must be expensive when runOn contains supplemental", label)
		}
	}
	if supplementalOnlyKind(suite.Kind) && !slices.Contains(suite.RunOn, "supplemental") {
		return fmt.Errorf("%s.runOn must be supplemental for %s evidence", label, suite.Kind)
	}
	return nil
}

func validateNestedTestProfiles(suite *TestSuite, label string) error {
	if slices.Contains(suite.RunOn, "focused") && suite.Cost != "quick" {
		return fmt.Errorf("%s.cost must be quick when runOn contains focused", label)
	}
	if slices.Contains(suite.RunOn, "focused") && !slices.Contains(suite.RunOn, "recommended") {
		return fmt.Errorf("%s.runOn must include recommended when it includes focused", label)
	}
	if slices.Contains(suite.RunOn, "recommended") && !slices.Contains(suite.RunOn, "full") {
		return fmt.Errorf("%s.runOn must include full when it includes recommended", label)
	}
	if slices.Contains(suite.RunOn, "recommended") && suite.Cost == "expensive" {
		return fmt.Errorf("%s.cost cannot be expensive when runOn contains recommended", label)
	}
	return nil
}

func validateTestEvidence(suite *TestSuite, label string) error {
	if slices.Contains([]string{"none", "noop", "skip", "skipped"}, suite.Kind) {
		return fmt.Errorf("%s.kind must describe executed evidence, not a skip", label)
	}
	if suite.Scope != "module" && suite.Scope != "repository" {
		return fmt.Errorf("%s.scope must be module or repository", label)
	}
	if suite.Scope == "module" && len(suite.Modules) != 1 {
		return fmt.Errorf("%s.modules must name exactly one module; cross-module suites use repository scope", label)
	}
	if suite.Scope == "repository" && len(suite.Modules) > 0 {
		return fmt.Errorf("%s.modules must be empty for a repository suite; use paths for focused triggers", label)
	}
	return nil
}

func supplementalOnlyKind(kind string) bool {
	return slices.Contains([]string{
		"acceptance-mutation", "crap", "gherkin-mutation", "mutation", "risk-analysis",
	}, kind)
}

func validateTestCommandArgv(argv []string, label string) error {
	executable := strings.ToLower(filepath.Base(argv[0]))
	if slices.Contains([]string{"false", "noop", "printf", "pwd", "test", "true", "echo", "env"}, executable) {
		return fmt.Errorf("%s.argv must execute a test-strength command, not obvious no-op %q", label, argv[0])
	}
	for index, argument := range argv {
		normalized := strings.ToLower(strings.TrimSpace(argument))
		if slices.Contains([]string{
			"--allow-no-tests", "--collect-only", "--dry-run", "--if-present", "--list-tests",
			"--listtests", "--pass-with-no-tests", "--passwithnotests", "-list", "--list",
		}, normalized) {
			return fmt.Errorf("%s.argv[%d] %q permits a green run without executing tests", label, index, argument)
		}
		if normalized == "-run=^$" || normalized == "--run=^$" ||
			(index > 0 && (strings.ToLower(argv[index-1]) == "-run" || strings.ToLower(argv[index-1]) == "--run") && normalized == "^$") {
			return fmt.Errorf("%s.argv[%d] %q selects no tests", label, index, argument)
		}
	}
	return nil
}

func validateCommand(config *Config, command *Command, label string, names map[string]bool) error {
	if err := validateCommandIdentity(command, label, names); err != nil {
		return err
	}
	if err := validateCommandArgv(command.Argv, label); err != nil {
		return err
	}
	if err := repositoryPath(command.Cwd, label+".cwd", false); err != nil {
		return err
	}
	if command.TimeoutSeconds < 1 || command.TimeoutSeconds > 3600 {
		return fmt.Errorf("%s.timeoutSeconds must be between 1 and 3600", label)
	}
	if err := validatePatterns(command.Paths, label+".paths", false); err != nil {
		return err
	}
	if err := validateEnvironment(command.Environment, label+".environment"); err != nil {
		return err
	}
	if err := ValidateExclusiveResources(command.ExclusiveResources); err != nil {
		return fmt.Errorf("%s.exclusiveResources: %w", label, err)
	}
	return validateCommandModules(config, command.Modules, label)
}

// ValidateExclusiveResources proves the normalized resource identifiers a
// command claims are safe to carry across the policy, runner, and receipt
// boundary. Parsing sorts identifiers before this runs; duplicate identifiers
// are rejected rather than silently changing a command's declared identity.
func ValidateExclusiveResources(resources []string) error {
	for index, resource := range resources {
		if err := identifier(resource, fmt.Sprintf("resource[%d]", index)); err != nil {
			return err
		}
		if index > 0 && resources[index-1] >= resource {
			if resources[index-1] == resource {
				return fmt.Errorf("contains duplicate identifier %q", resource)
			}
			return errors.New("must be sorted")
		}
	}
	return nil
}

func validateEnvironment(values []string, label string) error {
	if err := validateUniqueStrings(values, label, false); err != nil {
		return err
	}
	for _, value := range values {
		if !environmentNamePattern.MatchString(value) {
			return fmt.Errorf("%s contains invalid environment variable name %q", label, value)
		}
	}
	return nil
}

func validateCommandIdentity(command *Command, label string, names map[string]bool) error {
	if err := identifier(command.Name, label+".name"); err != nil {
		return err
	}
	if names[command.Name] {
		return fmt.Errorf("duplicate command or suite name %q", command.Name)
	}
	names[command.Name] = true
	return nil
}

func validateCommandArgv(argv []string, label string) error {
	if len(argv) == 0 {
		return fmt.Errorf("%s.argv must not be empty", label)
	}
	for index, argument := range argv {
		if err := nonempty(argument, fmt.Sprintf("%s.argv[%d]", label, index)); err != nil {
			return err
		}
	}
	if shellEvaluation(argv) {
		return fmt.Errorf("%s.argv must call a checked-in script instead of shell -c", label)
	}
	if strings.ContainsAny(argv[0], "/\\") {
		return repositoryPath(argv[0], label+".argv[0]", false)
	}
	return nil
}

func validateCommandModules(config *Config, modules []string, label string) error {
	if err := validateUniqueStrings(modules, label+".modules", true); err != nil {
		return err
	}
	for _, module := range modules {
		if _, exists := config.ModuleByName[module]; !exists {
			return fmt.Errorf("%s references unknown module %q", label, module)
		}
	}
	return nil
}

func shellEvaluation(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	name := filepath.Base(argv[0])
	return slices.Contains([]string{"sh", "bash", "dash", "ksh", "zsh"}, name) && slices.Contains([]string{"-c", "-lc"}, argv[1])
}

func validatePolicyModules(modules PolicyModules) error {
	seen := map[string]bool{}
	now := time.Now().UTC().Truncate(24 * time.Hour)
	for index, override := range modules.Overrides {
		label := fmt.Sprintf("policyModules.overrides[%d]", index)
		if err := validatePolicyModuleIdentity(override, label, seen); err != nil {
			return err
		}
		if err := validatePolicyModuleMode(override, label, now); err != nil {
			return err
		}
	}
	return nil
}

func validatePolicyModuleIdentity(override PolicyModuleOverride, label string, seen map[string]bool) error {
	if !slices.Contains([]string{"electron", "osv", "react", "ruff"}, override.Name) {
		return fmt.Errorf("%s.name is not a supported conditional policy module", label)
	}
	if err := repositoryPath(override.Root, label+".root", false); err != nil {
		return err
	}
	key := override.Name + "\x00" + override.Root
	if seen[key] {
		return fmt.Errorf("duplicate policy module override %q at %q", override.Name, override.Root)
	}
	seen[key] = true
	return nil
}

func validatePolicyModuleMode(override PolicyModuleOverride, label string, now time.Time) error {
	if override.Mode != "enabled" && override.Mode != "disabled" {
		return fmt.Errorf("%s.mode must be enabled or disabled", label)
	}
	if override.Mode == "enabled" {
		if override.Reason != "" || override.Owner != "" || !override.Expires.IsZero() {
			return fmt.Errorf("%s enabled override must not contain exception metadata", label)
		}
		return nil
	}
	return validateDisabledPolicyModule(override, label, now)
}

func validateDisabledPolicyModule(override PolicyModuleOverride, label string, now time.Time) error {
	if err := nonempty(override.Reason, label+".reason"); err != nil {
		return err
	}
	if err := nonempty(override.Owner, label+".owner"); err != nil {
		return err
	}
	if override.Expires.IsZero() {
		return fmt.Errorf("%s.expires must use YYYY-MM-DD", label)
	}
	if override.Expires.Before(now) {
		return fmt.Errorf("%s.expires has expired", label)
	}
	if override.Expires.After(now.AddDate(0, 0, MaximumExceptionDays)) {
		return fmt.Errorf("%s.expires must be within %d days", label, MaximumExceptionDays)
	}
	return nil
}

func validateExceptions(exceptions []Exception) error {
	seen := map[string]bool{}
	now := time.Now().UTC().Truncate(24 * time.Hour)
	for index, exception := range exceptions {
		label := fmt.Sprintf("exceptions[%d]", index)
		if seen[exception.ID] {
			return fmt.Errorf("duplicate exception id %q", exception.ID)
		}
		seen[exception.ID] = true
		if err := validateException(exception, label, now); err != nil {
			return err
		}
	}
	return nil
}

func validateException(exception Exception, label string, now time.Time) error {
	if err := identifier(exception.ID, label+".id"); err != nil {
		return err
	}
	for name, value := range map[string]string{"check": exception.Check, "path": exception.Path, "subject": exception.Subject, "reason": exception.Reason, "owner": exception.Owner} {
		if err := nonempty(value, label+"."+name); err != nil {
			return err
		}
	}
	if strings.ContainsAny(exception.Check+exception.Path+exception.Subject, "*?[]") {
		return fmt.Errorf("%s must be exact; wildcard exceptions are forbidden", label)
	}
	if strings.HasPrefix(exception.Check, "policy.") {
		return fmt.Errorf("%s.check cannot suppress policy findings", label)
	}
	if !suppressibleCheck(exception.Check) {
		return fmt.Errorf("%s.check must name an architecture, command, quality, supplyChain, or test finding", label)
	}
	if exception.Path != "repository" {
		if err := repositoryPath(exception.Path, label+".path", false); err != nil {
			return err
		}
	}
	if exception.Expires.IsZero() {
		return fmt.Errorf("%s.expires must use YYYY-MM-DD", label)
	}
	if exception.Expires.After(now.AddDate(0, 0, MaximumExceptionDays)) {
		return fmt.Errorf("%s.expires must be within %d days", label, MaximumExceptionDays)
	}
	return nil
}

func suppressibleCheck(check string) bool {
	if check == "command" {
		return true
	}
	if slices.Contains([]string{
		"supplyChain.auditIgnore",
		"supplyChain.dependencyOverride",
		"supplyChain.goVulnerability",
		"supplyChain.nodeVulnerability",
		"supplyChain.osvVulnerability",
		"supplyChain.pnpmSecurity",
		"supplyChain.releaseAge",
	}, check) {
		return false
	}
	for _, prefix := range []string{"architecture.", "quality.", "supplyChain.", "test."} {
		if strings.HasPrefix(check, prefix) {
			return true
		}
	}
	return false
}

func moduleCycle(config Config) []string {
	state := map[string]int{}
	stack := []string{}
	var visit func(string) []string
	visit = func(name string) []string {
		if state[name] == 1 {
			for index, item := range stack {
				if item == name {
					return append(append([]string{}, stack[index:]...), name)
				}
			}
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		stack = append(stack, name)
		module := config.Modules[config.ModuleByName[name]]
		for _, dependency := range module.DependsOn {
			if cycle := visit(dependency); len(cycle) > 0 {
				return cycle
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = 2
		return nil
	}
	for _, module := range config.Modules {
		if cycle := visit(module.Name); len(cycle) > 0 {
			return cycle
		}
	}
	return nil
}

func validatePatterns(patterns []string, label string, rejectUniversal bool) error {
	if err := validateUniqueStrings(patterns, label, false); err != nil {
		return err
	}
	for _, pattern := range patterns {
		if err := repositoryPath(pattern, label, true); err != nil {
			return err
		}
		normalized := strings.Trim(strings.TrimPrefix(pattern, "./"), "/")
		if rejectUniversal && slices.Contains([]string{"*", "**", "**/*", "*/**", "**/**"}, normalized) {
			return fmt.Errorf("%s cannot hide the entire repository", label)
		}
	}
	return nil
}

func repositoryPath(value, label string, allowGlob bool) error {
	if err := nonempty(value, label); err != nil {
		return err
	}
	if strings.Contains(value, "\\") {
		return fmt.Errorf("%s must use portable '/' separators", label)
	}
	if filepath.IsAbs(value) || strings.HasPrefix(value, "/") {
		return fmt.Errorf("%s must stay inside the repository", label)
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return fmt.Errorf("%s must stay inside the repository", label)
		}
	}
	if !allowGlob && strings.ContainsAny(value, "*?[]") {
		return fmt.Errorf("%s must be a concrete repository path", label)
	}
	return nil
}

func allowedValues(values, allowed []string, label string) error {
	if len(values) == 0 {
		return fmt.Errorf("%s must not be empty", label)
	}
	if err := validateUniqueStrings(values, label, false); err != nil {
		return err
	}
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return fmt.Errorf("%s contains unsupported value %q", label, value)
		}
	}
	return nil
}

func validateUniqueStrings(values []string, label string, validateIDs bool) error {
	seen := map[string]bool{}
	for index, value := range values {
		if err := nonempty(value, fmt.Sprintf("%s[%d]", label, index)); err != nil {
			return err
		}
		if validateIDs {
			if err := identifier(value, fmt.Sprintf("%s[%d]", label, index)); err != nil {
				return err
			}
		}
		if seen[value] {
			return fmt.Errorf("%s must not contain duplicate %q", label, value)
		}
		seen[value] = true
	}
	return nil
}

func identifier(value, label string) error {
	if err := nonempty(value, label); err != nil {
		return err
	}
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("%s must be a lowercase dotted or dashed identifier", label)
	}
	return nil
}

func nonempty(value, label string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must be a non-empty string", label)
	}
	return nil
}
