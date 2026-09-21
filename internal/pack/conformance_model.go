package pack

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/schema"
)

const ConformanceLedgerProtocol = "language-conformance-ledger/v1"
const ConformanceFixtureProtocol = "language-conformance-fixture/v1"
const ConformanceReportProtocol = "language-conformance-report/v1"

const ConformanceLedgerSchema = schema.ConfigurationBase + "code-polishy-language-conformance-ledger-v1.schema.json"
const ConformanceFixtureSchema = schema.ConfigurationBase + "code-polishy-language-conformance-fixture-v1.schema.json"
const ConformanceReportSchema = schema.ConfigurationBase + "code-polishy-language-conformance-report-v1.schema.json"

const maximumConformanceDocumentBytes = 8 << 20
const maximumConformanceFiles = 10000
const maximumConformanceFixtureBytes = 64 << 20

type ConformanceLedger struct {
	Schema        string                `json:"$schema,omitempty"`
	Protocol      string                `json:"protocol"`
	TaskBase      string                `json:"taskBase"`
	LockedRelease string                `json:"lockedRelease"`
	References    ConformanceReferences `json:"references"`
	FixtureFiles  []string              `json:"fixtures"`
	Behaviors     []ConformanceBehavior `json:"behaviors"`
	Fixtures      []ConformanceFixture  `json:"-"`
	Path          string                `json:"-"`
	Root          string                `json:"-"`
	SHA256        string                `json:"-"`
}

type ConformanceReferences struct {
	CurrentMain      ConformanceReference `json:"currentMain"`
	ImmutableOrigin  ConformanceReference `json:"immutableOrigin"`
	OptionalProvider ConformanceReference `json:"optionalProvider"`
}

type ConformanceReference struct {
	Revision string `json:"revision"`
	Version  string `json:"version"`
	SHA256   string `json:"sha256,omitempty"`
}

type ConformanceBehavior struct {
	ID                    string   `json:"id"`
	Language              string   `json:"language"`
	Capability            string   `json:"capability"`
	Guarantee             string   `json:"guarantee"`
	CurrentImplementation []string `json:"currentImplementation"`
	Helpers               []string `json:"helpers"`
	CLIRoutes             []string `json:"cliRoutes"`
	PolicyKeys            []string `json:"policyKeys"`
	Activation            []string `json:"activation"`
	Documentation         []string `json:"documentation"`
	Tests                 []string `json:"tests"`
	FutureOwner           string   `json:"futureOwner"`
	BoundaryRationale     string   `json:"boundaryRationale"`
	Fixtures              []string `json:"fixtures"`
	Commands              []string `json:"commands"`
	Profiles              []string `json:"profiles"`
	Modes                 []string `json:"modes"`
	Platforms             []string `json:"platforms"`
	ReferenceChange       string   `json:"referenceChange,omitempty"`
	Status                string   `json:"status"`
}

type ConformanceFixture struct {
	Schema         string                     `json:"$schema,omitempty"`
	Protocol       string                     `json:"protocol"`
	ID             string                     `json:"id"`
	Maturity       string                     `json:"maturity"`
	Gap            string                     `json:"gap,omitempty"`
	BehaviorIDs    []string                   `json:"behaviorIds"`
	Files          []ConformanceFixtureFile   `json:"files"`
	Arguments      []string                   `json:"arguments"`
	TimeoutSeconds int                        `json:"timeoutSeconds"`
	Platforms      []string                   `json:"platforms"`
	Expected       ConformanceExpectedOutcome `json:"expected"`
}

type ConformanceFixtureFile struct {
	Path          string  `json:"path"`
	Mode          string  `json:"mode"`
	Content       *string `json:"content,omitempty"`
	ContentBase64 *string `json:"contentBase64,omitempty"`
}

type ConformanceExpectedOutcome struct {
	ExitStatus       int                       `json:"exitStatus"`
	ReportStatus     string                    `json:"reportStatus"`
	RequiredRules    []string                  `json:"requiredRules"`
	RequiredCoverage []string                  `json:"requiredCoverage"`
	Writes           []ConformanceExpectedFile `json:"writes"`
	Protected        []string                  `json:"protected"`
}

type ConformanceExpectedFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func LoadConformanceLedger(name string) (ConformanceLedger, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return ConformanceLedger{}, err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return ConformanceLedger{}, err
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maximumConformanceDocumentBytes {
		return ConformanceLedger{}, fmt.Errorf("ledger must be a regular file of at most %d bytes", maximumConformanceDocumentBytes)
	}
	data, err := os.ReadFile(canonical)
	if err != nil {
		return ConformanceLedger{}, err
	}
	ledger := ConformanceLedger{}
	if err := schema.NewValidator(ConformanceLedgerSchema).Validate(data); err != nil {
		return ConformanceLedger{}, fmt.Errorf("validate ledger schema: %w", err)
	}
	if err := decodeConformanceDocument(data, &ledger); err != nil {
		return ConformanceLedger{}, fmt.Errorf("decode ledger: %w", err)
	}
	ledger.Path = canonical
	ledger.Root = filepath.Dir(canonical)
	ledger.SHA256 = inputDigest(data)
	if err := validateConformanceLedger(&ledger); err != nil {
		return ConformanceLedger{}, err
	}
	fixtures, err := loadConformanceFixtures(ledger.Root, ledger.FixtureFiles)
	if err != nil {
		return ConformanceLedger{}, err
	}
	ledger.Fixtures = fixtures
	if err := validateConformanceLinks(ledger); err != nil {
		return ConformanceLedger{}, err
	}
	return ledger, nil
}

func decodeConformanceDocument(data []byte, target any) error {
	if len(data) == 0 || len(data) > maximumConformanceDocumentBytes {
		return fmt.Errorf("document must contain 1 to %d bytes", maximumConformanceDocumentBytes)
	}
	if err := schema.ValidateUniqueJSON(data, 64); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("document contains more than one JSON value")
		}
		return err
	}
	return nil
}

func validateConformanceLedger(ledger *ConformanceLedger) error {
	if ledger.Protocol != ConformanceLedgerProtocol {
		return expected("protocol", ConformanceLedgerProtocol)
	}
	if !validRevision(ledger.TaskBase) {
		return expected("taskBase", "a lowercase 40-character Git revision")
	}
	if !semanticVersionPattern.MatchString(ledger.LockedRelease) {
		return expected("lockedRelease", "an exact semantic version")
	}
	if err := validateConformanceReferences(ledger.References); err != nil {
		return err
	}
	if len(ledger.FixtureFiles) == 0 {
		return expected("fixtures", "at least one fixture document")
	}
	if err := validateUnique(ledger.FixtureFiles, "fixtures"); err != nil {
		return err
	}
	for index, name := range ledger.FixtureFiles {
		if err := exactRelativePath(name); err != nil || !strings.HasSuffix(name, ".json") {
			return expected(indexed("fixtures", index), "an exact contained JSON path")
		}
	}
	if len(ledger.Behaviors) == 0 || len(ledger.Behaviors) > 4096 {
		return expected("behaviors", "1 to 4096 items")
	}
	seen := map[string]bool{}
	for index := range ledger.Behaviors {
		if err := validateConformanceBehavior(ledger.Behaviors[index], index, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateConformanceReferences(references ConformanceReferences) error {
	values := []struct {
		path  string
		value ConformanceReference
	}{
		{"references.currentMain", references.CurrentMain},
		{"references.immutableOrigin", references.ImmutableOrigin},
		{"references.optionalProvider", references.OptionalProvider},
	}
	for _, item := range values {
		if !validRevision(item.value.Revision) {
			return expected(item.path+".revision", "a lowercase 40-character Git revision")
		}
		if item.value.Version != "" && !semanticVersionPattern.MatchString(item.value.Version) {
			return expected(item.path+".version", "empty or an exact semantic version")
		}
		if item.value.SHA256 != "" && !validDigest(item.value.SHA256) {
			return expected(item.path+".sha256", "empty or a lowercase SHA-256 digest")
		}
	}
	return nil
}

func validateConformanceBehavior(behavior ConformanceBehavior, index int, seen map[string]bool) error {
	label := indexed("behaviors", index)
	if !identifierPattern.MatchString(behavior.ID) || seen[behavior.ID] {
		return expected(label+".id", "a unique lowercase identifier")
	}
	seen[behavior.ID] = true
	if !identifierPattern.MatchString(behavior.Language) {
		return expected(label+".language", "a lowercase identifier")
	}
	if !identifierPattern.MatchString(behavior.Capability) {
		return expected(label+".capability", "a lowercase identifier")
	}
	if err := validateConformanceText(behavior.Guarantee, label+".guarantee", 4096); err != nil {
		return err
	}
	if err := validateConformanceText(behavior.BoundaryRationale, label+".boundaryRationale", 4096); err != nil {
		return err
	}
	if !slices.Contains([]string{"core", "pack", "shared-protocol"}, behavior.FutureOwner) {
		return expected(label+".futureOwner", "core, pack, or shared-protocol")
	}
	if !slices.Contains([]string{"untested", "passing", "failing", "blocked", "deliberately-changed"}, behavior.Status) {
		return expected(label+".status", "untested, passing, failing, blocked, or deliberately-changed")
	}
	collections := []struct {
		path     string
		values   []string
		required bool
	}{
		{label + ".currentImplementation", behavior.CurrentImplementation, true},
		{label + ".helpers", behavior.Helpers, false},
		{label + ".cliRoutes", behavior.CLIRoutes, true},
		{label + ".policyKeys", behavior.PolicyKeys, false},
		{label + ".activation", behavior.Activation, true},
		{label + ".documentation", behavior.Documentation, true},
		{label + ".tests", behavior.Tests, true},
		{label + ".fixtures", behavior.Fixtures, true},
		{label + ".commands", behavior.Commands, true},
		{label + ".profiles", behavior.Profiles, true},
		{label + ".modes", behavior.Modes, true},
		{label + ".platforms", behavior.Platforms, true},
	}
	for _, collection := range collections {
		if err := validateConformanceStrings(collection.values, collection.path, collection.required); err != nil {
			return err
		}
	}
	return nil
}

func validateConformanceText(value, path string, maximum int) error {
	if strings.TrimSpace(value) == "" || len(value) > maximum {
		return expected(path, fmt.Sprintf("1 to %d non-whitespace bytes", maximum))
	}
	return nil
}

func validateConformanceStrings(values []string, path string, required bool) error {
	if required && len(values) == 0 {
		return expected(path, "at least one item")
	}
	if len(values) > 256 {
		return expected(path, "at most 256 items")
	}
	if err := validateUnique(values, path); err != nil {
		return err
	}
	for index, value := range values {
		if err := validateConformanceText(value, indexed(path, index), 4096); err != nil {
			return err
		}
	}
	return nil
}

func validRevision(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

func loadConformanceFixtures(root string, names []string) ([]ConformanceFixture, error) {
	directory, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	fixtures := make([]ConformanceFixture, 0, len(names))
	for index, name := range names {
		file, err := directory.Open(name)
		if err != nil {
			return nil, fmt.Errorf("fixtures[%d]: %w", index, err)
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() > maximumConformanceDocumentBytes {
			file.Close()
			return nil, expected(indexed("fixtures", index), fmt.Sprintf("a regular file of at most %d bytes", maximumConformanceDocumentBytes))
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maximumConformanceDocumentBytes+1))
		file.Close()
		if readErr != nil {
			return nil, readErr
		}
		fixture := ConformanceFixture{}
		if err := schema.NewValidator(ConformanceFixtureSchema).Validate(data); err != nil {
			return nil, fmt.Errorf("fixture %s schema: %w", name, err)
		}
		if err := decodeConformanceDocument(data, &fixture); err != nil {
			return nil, fmt.Errorf("fixture %s: %w", name, err)
		}
		if err := validateConformanceFixture(fixture, index); err != nil {
			return nil, fmt.Errorf("fixture %s: %w", name, err)
		}
		fixtures = append(fixtures, fixture)
	}
	return fixtures, nil
}

func validateConformanceFixture(fixture ConformanceFixture, index int) error {
	label := indexed("fixtures", index)
	if fixture.Protocol != ConformanceFixtureProtocol {
		return expected(label+".protocol", ConformanceFixtureProtocol)
	}
	if !identifierPattern.MatchString(fixture.ID) {
		return expected(label+".id", "a lowercase identifier")
	}
	if !slices.Contains([]string{"active", "planned"}, fixture.Maturity) {
		return expected(label+".maturity", "active or planned")
	}
	if fixture.Maturity == "planned" {
		if err := validateConformanceText(fixture.Gap, label+".gap", 4096); err != nil {
			return err
		}
	} else if fixture.Gap != "" {
		return expected(label+".gap", "absent for an active fixture")
	}
	if err := validateConformanceStrings(fixture.BehaviorIDs, label+".behaviorIds", true); err != nil {
		return err
	}
	if len(fixture.Files) == 0 || len(fixture.Files) > maximumConformanceFiles {
		return expected(label+".files", fmt.Sprintf("1 to %d items", maximumConformanceFiles))
	}
	paths := map[string]bool{}
	total := 0
	for fileIndex, file := range fixture.Files {
		fileLabel := indexed(label+".files", fileIndex)
		if err := exactRelativePath(file.Path); err != nil || paths[file.Path] {
			return expected(fileLabel+".path", "a unique exact contained relative path")
		}
		paths[file.Path] = true
		if !slices.Contains([]string{"0644", "0755"}, file.Mode) {
			return expected(fileLabel+".mode", "0644 or 0755")
		}
		data, err := conformanceFixtureBytes(file)
		if err != nil {
			return fmt.Errorf("%s: %w", fileLabel, err)
		}
		total += len(data)
		if total > maximumConformanceFixtureBytes {
			return expected(label+".files", fmt.Sprintf("at most %d decoded bytes", maximumConformanceFixtureBytes))
		}
	}
	if len(fixture.Arguments) == 0 || len(fixture.Arguments) > 128 {
		return expected(label+".arguments", "1 to 128 items")
	}
	for argumentIndex, argument := range fixture.Arguments {
		if strings.TrimSpace(argument) == "" || strings.ContainsRune(argument, 0) {
			return expected(indexed(label+".arguments", argumentIndex), "a nonempty argument without NUL")
		}
	}
	if !conformanceRequestsJSON(fixture.Arguments) {
		return expected(label+".arguments", "a --format json option")
	}
	if fixture.TimeoutSeconds < 1 || fixture.TimeoutSeconds > 3600 {
		return expected(label+".timeoutSeconds", "an integer from 1 through 3600")
	}
	if err := validateConformanceStrings(fixture.Platforms, label+".platforms", true); err != nil {
		return err
	}
	return validateConformanceExpected(fixture.Expected, label+".expected", paths)
}

func conformanceFixtureBytes(file ConformanceFixtureFile) ([]byte, error) {
	if (file.Content == nil) == (file.ContentBase64 == nil) {
		return nil, errors.New("expected exactly one of content or contentBase64")
	}
	if file.Content != nil {
		return []byte(*file.Content), nil
	}
	data, err := base64.StdEncoding.Strict().DecodeString(*file.ContentBase64)
	if err != nil {
		return nil, errors.New("expected canonical base64 content")
	}
	if base64.StdEncoding.EncodeToString(data) != *file.ContentBase64 {
		return nil, errors.New("expected canonical base64 content")
	}
	return data, nil
}

func conformanceRequestsJSON(arguments []string) bool {
	for index, argument := range arguments {
		if argument == "--format=json" || argument == "--format" && index+1 < len(arguments) && arguments[index+1] == "json" {
			return true
		}
	}
	return false
}

func validateConformanceExpected(outcome ConformanceExpectedOutcome, label string, fixturePaths map[string]bool) error {
	if outcome.ExitStatus < 0 || outcome.ExitStatus > 2 {
		return expected(label+".exitStatus", "0, 1, or 2")
	}
	if !slices.Contains([]string{"passed", "failed"}, outcome.ReportStatus) {
		return expected(label+".reportStatus", "passed or failed")
	}
	if err := validateConformanceStrings(outcome.RequiredRules, label+".requiredRules", false); err != nil {
		return err
	}
	if err := validateConformanceStrings(outcome.RequiredCoverage, label+".requiredCoverage", false); err != nil {
		return err
	}
	if err := validateConformanceStrings(outcome.Protected, label+".protected", false); err != nil {
		return err
	}
	writes := map[string]bool{}
	for index, file := range outcome.Writes {
		fileLabel := indexed(label+".writes", index)
		if err := exactRelativePath(file.Path); err != nil || writes[file.Path] {
			return expected(fileLabel+".path", "a unique exact contained relative path")
		}
		if !validDigest(file.SHA256) {
			return expected(fileLabel+".sha256", "a lowercase SHA-256 digest")
		}
		writes[file.Path] = true
	}
	for index, path := range outcome.Protected {
		if !fixturePaths[path] || writes[path] {
			return expected(indexed(label+".protected", index), "an existing fixture path not listed in writes")
		}
	}
	return nil
}

func validateConformanceLinks(ledger ConformanceLedger) error {
	behaviors := map[string]ConformanceBehavior{}
	for _, behavior := range ledger.Behaviors {
		behaviors[behavior.ID] = behavior
	}
	fixtures := map[string]ConformanceFixture{}
	for index, fixture := range ledger.Fixtures {
		if _, exists := fixtures[fixture.ID]; exists {
			return expected(indexed("fixtures", index)+".id", "an ID used by exactly one fixture")
		}
		fixtures[fixture.ID] = fixture
		for behaviorIndex, behaviorID := range fixture.BehaviorIDs {
			behavior, exists := behaviors[behaviorID]
			if !exists || !slices.Contains(behavior.Fixtures, fixture.ID) {
				return expected(indexed(indexed("fixtures", index)+".behaviorIds", behaviorIndex), "a behavior that links back to this fixture")
			}
		}
	}
	for behaviorIndex, behavior := range ledger.Behaviors {
		coveredPlatforms := map[string]bool{}
		for fixtureIndex, fixtureID := range behavior.Fixtures {
			fixture, exists := fixtures[fixtureID]
			if !exists || !slices.Contains(fixture.BehaviorIDs, behavior.ID) {
				return expected(indexed(indexed("behaviors", behaviorIndex)+".fixtures", fixtureIndex), "a fixture that links back to this behavior")
			}
			if behavior.Status == "passing" && fixture.Maturity != "active" {
				return expected(indexed(indexed("behaviors", behaviorIndex)+".fixtures", fixtureIndex), "an active fixture for a passing behavior")
			}
			for _, platform := range fixture.Platforms {
				coveredPlatforms[platform] = true
			}
		}
		for platformIndex, platform := range behavior.Platforms {
			if !coveredPlatforms[platform] {
				return expected(indexed(indexed("behaviors", behaviorIndex)+".platforms", platformIndex), "a platform covered by a linked fixture")
			}
		}
	}
	return nil
}
