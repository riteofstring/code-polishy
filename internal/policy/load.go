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

const maximumConfigurationBytes = 8 * 1024 * 1024

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

func Parse(data []byte, source string) (Config, error) {
	if source == "" {
		source = ConfigFilename
	}
	if len(data) == 0 || len(data) > maximumConfigurationBytes {
		return Config{}, fmt.Errorf("parse %s: configuration has an invalid byte size", source)
	}
	if err := validateRuntimeSchema(data); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", source, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	config := Config{}
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", source, err)
	}
	if err := requireEOF(decoder); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", source, err)
	}
	config.ConfigPath = source
	applyDefaults(&config)
	normalized, err := json.Marshal(config)
	if err != nil {
		return Config{}, fmt.Errorf("normalize %s: %w", source, err)
	}
	if err := validateRuntimeSchema(normalized); err != nil {
		return Config{}, fmt.Errorf("parse %s after defaults: %w", source, err)
	}
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
	generationDefaults(&config.Generation)
	if behaviorReview := config.Verification.BehaviorReview; behaviorReview != nil {
		defaultString(&behaviorReview.DefaultRequiredAt, BehaviorReviewOnRequest)
	}
	defaultInt(&config.Quality.MaxFileLines, MaxFileLines)
	defaultInt(&config.Quality.MaxTestFileLines, MaxTestFileLines)
	defaultInt(&config.Quality.Complexity.Go, MaxGoComplexity)
	defaultInt(&config.Quality.Complexity.GoTest, MaxGoTestComplexity)
	defaultInt(&config.Quality.Complexity.Python, MaxPythonComplexity)
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
	if err := validateScope(config); err != nil {
		return err
	}
	if err := validateModules(config); err != nil {
		return err
	}
	if err := validatePacks(config.Packs); err != nil {
		return err
	}
	if err := validateDocumentation(config); err != nil {
		return err
	}
	if err := validateChecks(config); err != nil {
		return err
	}
	if err := validateTests(config); err != nil {
		return err
	}
	if err := validateVerification(config); err != nil {
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
	if seen[input.Name] {
		return fmt.Errorf("duplicate external input name %q", input.Name)
	}
	seen[input.Name] = true
	if _, exists := config.ModuleByName[input.Module]; !exists {
		return fmt.Errorf("%s.module references unknown module %q", label, input.Module)
	}
	return nil
}

func validateExternalInputResolution(input ExternalInput, label string) error {
	usesEnvironment := slices.Contains(input.Resolution, "environment")
	if usesEnvironment != (len(input.Environment) > 0) {
		return fmt.Errorf("%s.environment must be non-empty exactly when resolution contains environment", label)
	}
	if input.SiblingFallback != "" {
		if input.Kind == "service" {
			return fmt.Errorf("%s.siblingFallback is not supported for service inputs", label)
		}
		if !slices.Contains(input.Resolution, "default") {
			return fmt.Errorf("%s.siblingFallback requires default resolution", label)
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
	if err := validateExternalContractSuite(contract, input.Module, label); err != nil {
		return err
	}
	behavior, err := referencedSuite(config.Tests.Suites, input.BehaviorSuite, label+".behaviorSuite")
	if err != nil {
		return err
	}
	if behavior.Reusable {
		return fmt.Errorf("%s.behaviorSuite cannot be reusable because it observes an external input", label)
	}
	if input.ContractSuite == input.BehaviorSuite {
		return fmt.Errorf("%s must use distinct contractSuite and behaviorSuite evidence", label)
	}
	if !slices.Contains(behavior.RunOn, "full") || slices.Contains(behavior.RunOn, "supplemental") {
		return fmt.Errorf("%s.behaviorSuite must name an ordinary suite included in full", label)
	}
	return nil
}

func validateExternalContractSuite(contract TestSuite, module, label string) error {
	if contract.Reusable {
		return fmt.Errorf("%s.contractSuite cannot be reusable because it observes an external input", label)
	}
	owned := contract.Scope == "module" && len(contract.Modules) == 1 && contract.Modules[0] == module
	if !owned || contract.Cost != "quick" || !slices.Contains(contract.RunOn, "focused") {
		return fmt.Errorf("%s.contractSuite must name a quick focused suite owned by module %q", label, module)
	}
	return nil
}

func referencedSuite(suites []TestSuite, name, label string) (TestSuite, error) {
	for _, suite := range suites {
		if suite.Name == name {
			return suite, nil
		}
	}
	return TestSuite{}, fmt.Errorf("%s references unknown test suite %q", label, name)
}

func validateScope(config *Config) error {
	if err := validatePythonContracts(config.Scope.PythonContracts); err != nil {
		return err
	}
	if err := rejectUniversalPatterns(config.Scope.Exclude, "scope.exclude"); err != nil {
		return err
	}
	if err := rejectUniversalPatterns(config.Scope.Generated, "scope.generated"); err != nil {
		return err
	}
	if err := validateDataPaths(config); err != nil {
		return err
	}
	if err := rejectUniversalPatterns(config.Scope.EntryPoints, "scope.entryPoints"); err != nil {
		return err
	}
	if err := validateGeneratedJavaScript(config.Scope.GeneratedJavaScript); err != nil {
		return err
	}
	if err := validatePythonDynamicReferences(config.Scope.PythonDynamicReferences); err != nil {
		return err
	}
	if err := validatePythonComputedImports(config.Scope.PythonComputedImports); err != nil {
		return err
	}
	if err := validatePythonExternalPluginImports(&config.Scope); err != nil {
		return err
	}
	if err := validatePythonExternalAttributes(config.Scope.PythonExternalAttributes); err != nil {
		return err
	}
	if err := rejectUniversalPatterns(config.Scope.Development, "scope.development"); err != nil {
		return err
	}
	return validateLanguageRules(config.Scope.Languages)
}

func validateLanguageRules(rules []LanguageRule) error {
	reserved := []string{"go", "typescript", "python", "shell", "rust", "jvm", "ruby", "php", "swift", "native", "protobuf", "sql", "dart"}
	seen := map[string]bool{}
	for index, rule := range rules {
		label := fmt.Sprintf("scope.languages[%d]", index)
		if seen[rule.Name] || slices.Contains(reserved, rule.Name) {
			return fmt.Errorf("%s.name must be a unique custom language identifier", label)
		}
		seen[rule.Name] = true
		if err := rejectUniversalPatterns(rule.Paths, label+".paths"); err != nil {
			return err
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
	}
	return ValidateGeneration(config.Generation)
}

func validateTests(config *Config) error {
	suiteNames := map[string]bool{}
	for index := range config.Tests.Suites {
		if err := validateTestSuite(config, &config.Tests.Suites[index], index, suiteNames); err != nil {
			return err
		}
	}
	if err := validateTestCoverageRelations(config.Tests.Suites); err != nil {
		return err
	}
	return validateTestOwnership(config)
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
	if err := validateTestEvidence(suite, label); err != nil {
		return err
	}
	if suite.Reusable && len(suite.ExclusiveResources) > 0 {
		return fmt.Errorf("%s.reusable requires no external exclusive resources", label)
	}
	if err := validateTestArtifacts(suite.Artifacts, label); err != nil {
		return err
	}
	if suite.Kind == "live" {
		return fmt.Errorf("%s.kind live requires a typed external approval gate and cannot be an automatic test suite", label)
	}
	return nil
}

func validateTestCoverageRelations(suites []TestSuite) error {
	byName := map[string]TestSuite{}
	for _, suite := range suites {
		byName[suite.Name] = suite
	}
	coveredBy := map[string]string{}
	for _, suite := range suites {
		for _, targetName := range suite.Covers {
			target, found := byName[targetName]
			if !found {
				return fmt.Errorf("test suite %q covers unknown suite %q", suite.Name, targetName)
			}
			if targetName == suite.Name {
				return fmt.Errorf("test suite %q cannot cover itself", suite.Name)
			}
			if owner := coveredBy[targetName]; owner != "" {
				return fmt.Errorf("test suite %q is covered by both %q and %q", targetName, owner, suite.Name)
			}
			if err := validateTestCoverageCompatibility(suite, target); err != nil {
				return err
			}
			coveredBy[targetName] = suite.Name
		}
	}
	for name := range byName {
		seen := map[string]bool{name: true}
		for owner := coveredBy[name]; owner != ""; owner = coveredBy[owner] {
			if seen[owner] {
				return fmt.Errorf("test suite coverage contains a cycle through %q", owner)
			}
			seen[owner] = true
		}
	}
	return nil
}

func validateTestCoverageCompatibility(covering, target TestSuite) error {
	label := fmt.Sprintf("test suite %q cannot cover %q", covering.Name, target.Name)
	if covering.Reusable && !target.Reusable {
		return fmt.Errorf("%s because a reusable result cannot satisfy an unbounded suite", label)
	}
	if slices.Contains(covering.RunOn, "supplemental") != slices.Contains(target.RunOn, "supplemental") {
		return fmt.Errorf("%s across ordinary and supplemental profiles", label)
	}
	if covering.Scope == "module" && !slices.Equal(covering.Modules, target.Modules) {
		return fmt.Errorf("%s across different module ownership", label)
	}
	if covering.TimeoutSeconds > target.TimeoutSeconds {
		return fmt.Errorf("%s with a weaker timeout limit", label)
	}
	for field, values := range map[string][][]string{
		"environment":         {target.Environment, covering.Environment},
		"exclusive resources": {target.ExclusiveResources, covering.ExclusiveResources},
		"extra inputs":        {target.ExtraInputs, covering.ExtraInputs},
	} {
		if !stringSetContains(values[1], values[0]) {
			return fmt.Errorf("%s with mismatched %s", label, field)
		}
	}
	if !artifactSetContains(covering.Artifacts, target.Artifacts) {
		return fmt.Errorf("%s with mismatched artifact evidence", label)
	}
	return nil
}

func stringSetContains(container, required []string) bool {
	for _, value := range required {
		if !slices.Contains(container, value) {
			return false
		}
	}
	return true
}

func artifactSetContains(container, required []TestArtifact) bool {
	for _, expected := range required {
		if !slices.Contains(container, expected) {
			return false
		}
	}
	return true
}

func validateTestEvidence(suite *TestSuite, label string) error {
	if slices.Contains([]string{"none", "noop", "skip", "skipped"}, suite.Kind) {
		return fmt.Errorf("%s.kind must describe executed evidence, not a skip", label)
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
	if err := ValidateExclusiveResources(command.ExclusiveResources); err != nil {
		return fmt.Errorf("%s.exclusiveResources: %w", label, err)
	}
	return validateCommandModules(config, command.Modules, label)
}

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

func validateCommandIdentity(command *Command, label string, names map[string]bool) error {
	if names[command.Name] {
		return fmt.Errorf("duplicate command or suite name %q", command.Name)
	}
	names[command.Name] = true
	return nil
}

func validateCommandArgv(argv []string, label string) error {
	if shellEvaluation(argv) {
		return fmt.Errorf("%s.argv must call a checked-in script instead of shell -c", label)
	}
	if strings.ContainsAny(argv[0], "/\\") {
		return repositoryPath(argv[0], label+".argv[0]")
	}
	return nil
}

func validateCommandModules(config *Config, modules []string, label string) error {
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
	key := override.Name + "\x00" + override.Root
	if seen[key] {
		return fmt.Errorf("duplicate policy module override %q at %q", override.Name, override.Root)
	}
	seen[key] = true
	return nil
}

func validatePolicyModuleMode(override PolicyModuleOverride, label string, now time.Time) error {
	if override.Mode == "enabled" {
		return nil
	}
	return validateDisabledPolicyModule(override, label, now)
}

func validateDisabledPolicyModule(override PolicyModuleOverride, label string, now time.Time) error {
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
	if strings.HasPrefix(exception.Check, "policy.") {
		return fmt.Errorf("%s.check cannot suppress policy findings", label)
	}
	if !suppressibleCheck(exception.Check) {
		return fmt.Errorf("%s.check must name an architecture, quality, supplyChain, or test finding", label)
	}
	if exception.Expires.After(now.AddDate(0, 0, MaximumExceptionDays)) {
		return fmt.Errorf("%s.expires must be within %d days", label, MaximumExceptionDays)
	}
	return nil
}

func suppressibleCheck(check string) bool {
	if slices.Contains([]string{
		"architecture.fileCycle", "testing.fileCycle", "architecture.sourceGraphCoverage",
		"architecture.importCoverage", "architecture.pythonFactsCoverage",
		"architecture.reviewSignal", "policy.architectureReview",
	}, check) {
		return false
	}
	if slices.Contains([]string{
		"supplyChain.auditIgnore",
		"supplyChain.dependencyOverride",
		"supplyChain.goVulnerability",
		"supplyChain.gitEvidence",
		"supplyChain.gitVulnerability",
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

func rejectUniversalPatterns(patterns []string, label string) error {
	for _, pattern := range patterns {
		normalized := strings.Trim(strings.TrimPrefix(pattern, "./"), "/")
		if slices.Contains([]string{"*", "**", "**/*", "*/**", "**/**"}, normalized) {
			return fmt.Errorf("%s cannot hide the entire repository", label)
		}
	}
	return nil
}

func repositoryPath(value, label string) error {
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
	if strings.ContainsAny(value, "*?[]") {
		return fmt.Errorf("%s must be a concrete repository path", label)
	}
	return nil
}
