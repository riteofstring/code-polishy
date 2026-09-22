package pack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type InputFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type PolicyInput struct {
	Quality      policy.Quality           `json:"quality"`
	Modules      []PolicyModuleInput      `json:"modules"`
	Files        []SourceInput            `json:"files"`
	Declarations []PolicyDeclarationInput `json:"declarations"`
}

type PolicyDeclarationInput struct {
	Kind    string          `json:"kind"`
	Version int             `json:"version"`
	Scopes  []string        `json:"scopes"`
	Inputs  []string        `json:"inputs"`
	Data    json.RawMessage `json:"data"`
}

type PolicyModuleInput struct {
	Name string `json:"name"`
	Root string `json:"root"`
}

type SourceInput struct {
	Scopes      []string `json:"scopes"`
	Owner       string   `json:"owner"`
	Context     string   `json:"context"`
	Path        string   `json:"path"`
	Language    string   `json:"language"`
	Test        bool     `json:"test"`
	Generated   bool     `json:"generated"`
	Data        bool     `json:"data"`
	Development bool     `json:"development"`
}

type ToolIdentity struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type Coverage struct {
	Analyzed    []string      `json:"analyzed"`
	Unsupported []Unsupported `json:"unsupported"`
}

type Unsupported struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type SourceFacts struct {
	Imports   *[]ImportFact   `json:"imports,omitempty"`
	Comments  *[]CommentFact  `json:"comments,omitempty"`
	Functions *[]FunctionFact `json:"functions,omitempty"`
	Literals  *[]LiteralFact  `json:"literals,omitempty"`
	DeadCode  *[]DeadCodeFact `json:"deadCode,omitempty"`
}

type ImportFact struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Specifier string `json:"specifier"`
	Resolved  string `json:"resolved"`
	Package   string `json:"package"`
	Kind      string `json:"kind"`
}

type CommentFact struct {
	Path             string `json:"path"`
	Line             int    `json:"line"`
	Column           int    `json:"column"`
	Kind             string `json:"kind"`
	Raw              string `json:"raw"`
	Complete         bool   `json:"complete"`
	BeforeCode       bool   `json:"beforeCode"`
	Preamble         bool   `json:"preamble"`
	ByteZero         bool   `json:"byteZero"`
	MachineDirective bool   `json:"machineDirective,omitempty"`
}

type LiteralFact struct {
	Path        string `json:"path"`
	Line        int    `json:"line"`
	Column      int    `json:"column"`
	Value       string `json:"value"`
	RootContext bool   `json:"rootContext"`
}

type FunctionFact struct {
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
	Name       string `json:"name"`
	Complexity int    `json:"complexity"`
	Depth      int    `json:"depth"`
	Parameters int    `json:"parameters"`
}

type DeadCodeFact struct {
	Analyzer   string `json:"analyzer"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
	EndLine    int    `json:"endLine"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Confidence int    `json:"confidence"`
	Message    string `json:"message"`
}

func validateAnalysisResponse(response Response, request Request) error {
	if response.Status == "operational-failure" {
		if response.Coverage != nil || response.Facts != nil {
			return errors.New("operational failure cannot assert analysis coverage or facts")
		}
		return nil
	}
	if err := validateCoverage(response, request); err != nil {
		return err
	}
	if err := validateSourceFacts(response, request); err != nil {
		return err
	}
	if err := validateDiagnosticCoverage(request, response); err != nil {
		return err
	}
	return validateInputs(response.Inputs)
}

func validateCoverage(response Response, request Request) error {
	if response.Coverage == nil || response.Coverage.Analyzed == nil || response.Coverage.Unsupported == nil {
		return expected("coverage", "explicit analyzed and unsupported arrays")
	}
	accounted, err := accountCoverage(*response.Coverage, request.DiagnosticFiles)
	if err != nil {
		return err
	}
	for _, path := range request.Files {
		if !accounted[path] {
			return expected("coverage", fmt.Sprintf("requested path %q to be accounted for", path))
		}
	}
	if response.Status == "pass" && len(response.Coverage.Unsupported) > 0 {
		return expected("status", "incomplete when coverage.unsupported is non-empty")
	}
	return nil
}

func accountCoverage(coverage Coverage, selected []string) (map[string]bool, error) {
	accounted := map[string]bool{}
	for index, path := range coverage.Analyzed {
		if err := accountCoveragePath(accounted, selected, path, indexed("coverage.analyzed", index)); err != nil {
			return nil, err
		}
	}
	for index, item := range coverage.Unsupported {
		label := indexed("coverage.unsupported", index)
		if strings.TrimSpace(item.Reason) == "" || len(item.Reason) > 4096 {
			return nil, expected(label+".reason", "1 to 4096 non-whitespace bytes")
		}
		if err := accountCoveragePath(accounted, selected, item.Path, label+".path"); err != nil {
			return nil, err
		}
	}
	return accounted, nil
}

func accountCoveragePath(accounted map[string]bool, selected []string, path, label string) error {
	if !slices.Contains(selected, path) {
		return expected(label, "a path from diagnosticFiles")
	}
	if accounted[path] {
		return expected(label, "a path accounted for exactly once")
	}
	accounted[path] = true
	return nil
}

func validateSourceFacts(response Response, request Request) error {
	facts := SourceFacts{}
	if response.Facts != nil {
		facts = *response.Facts
	}
	if len(response.Coverage.Analyzed) > 0 && requiredFactsMissing(facts, request) {
		return fmt.Errorf("%s analysis requires its policy source facts", request.Capability)
	}
	return validateFactCollections(facts, request, response.Coverage.Analyzed)
}

func requiredFactsMissing(facts SourceFacts, request Request) bool {
	switch request.Capability {
	case "architecture":
		return facts.Imports == nil
	case "complexity":
		return facts.Functions == nil
	case "dead-code":
		return facts.DeadCode == nil
	case "lint":
		return !request.Policy.Quality.CommentsAllowed() && facts.Comments == nil
	}
	return false
}

func validateFactCollections(facts SourceFacts, request Request, analyzed []string) error {
	if err := validateImportFacts(facts.Imports, request.Capability, analyzed); err != nil {
		return err
	}
	if err := validateCommentFacts(facts.Comments, request.Capability, analyzed); err != nil {
		return err
	}
	if err := validateLiteralFacts(facts.Literals, request.Capability, analyzed); err != nil {
		return err
	}
	if err := validateFunctionFacts(facts.Functions, request.Capability, analyzed); err != nil {
		return err
	}
	return validateDeadCodeFacts(facts.DeadCode, request.Capability, analyzed)
}

func validateImportFacts(facts *[]ImportFact, capability string, analyzed []string) error {
	if facts == nil {
		return nil
	}
	if capability != "architecture" {
		return expected("facts.imports", "imports only for the architecture capability")
	}
	if len(*facts) > 20000 {
		return expected("facts.imports", "at most 20000 items")
	}
	for index, fact := range *facts {
		if err := validateImportFact(fact, analyzed, indexed("facts.imports", index)); err != nil {
			return err
		}
	}
	return nil
}

func validateCommentFacts(facts *[]CommentFact, capability string, analyzed []string) error {
	if facts == nil {
		return nil
	}
	if capability != "lint" {
		return expected("facts.comments", "comments only for the lint capability")
	}
	if len(*facts) > 20000 {
		return expected("facts.comments", "at most 20000 items")
	}
	for index, fact := range *facts {
		if err := validateCommentFact(fact, analyzed, indexed("facts.comments", index)); err != nil {
			return err
		}
	}
	return nil
}

func validateCommentFact(fact CommentFact, analyzed []string, label string) error {
	if err := validateFactLocation(fact.Path, fact.Line, fact.Column, analyzed, label); err != nil {
		return err
	}
	if !slices.Contains([]string{"Line", "Block", "Docstring", "HTML", "Shebang"}, fact.Kind) {
		return expected(label+".kind", "Line, Block, Docstring, HTML, or Shebang")
	}
	if len(fact.Raw) == 0 || len(fact.Raw) > 65536 {
		return expected(label+".raw", "1 to 65536 bytes")
	}
	if fact.MachineDirective && (!fact.Complete || !slices.Contains([]string{"Line", "Shebang"}, fact.Kind)) {
		return expected(label+".machineDirective", "true only for complete Line or Shebang facts")
	}
	return nil
}

func validateLiteralFacts(facts *[]LiteralFact, capability string, analyzed []string) error {
	if facts == nil {
		return nil
	}
	if capability != "lint" {
		return expected("facts.literals", "literals only for the lint capability")
	}
	if len(*facts) > 20000 {
		return expected("facts.literals", "at most 20000 items")
	}
	for index, fact := range *facts {
		label := indexed("facts.literals", index)
		if err := validateFactLocation(fact.Path, fact.Line, fact.Column, analyzed, label); err != nil {
			return err
		}
		if fact.Value == "" || len(fact.Value) > 4096 || strings.ContainsAny(fact.Value, "\x00\r\n") {
			return expected(label+".value", "1 to 4096 bytes without NUL or newlines")
		}
	}
	return nil
}

func validateFunctionFacts(facts *[]FunctionFact, capability string, analyzed []string) error {
	if facts == nil {
		return nil
	}
	if capability != "complexity" {
		return expected("facts.functions", "functions only for the complexity capability")
	}
	if len(*facts) > 20000 {
		return expected("facts.functions", "at most 20000 items")
	}
	for index, fact := range *facts {
		if err := validateFunctionFact(fact, analyzed, indexed("facts.functions", index)); err != nil {
			return err
		}
	}
	return nil
}

func validateFunctionFact(fact FunctionFact, analyzed []string, label string) error {
	if err := validateFactLocation(fact.Path, fact.Line, fact.Column, analyzed, label); err != nil {
		return err
	}
	if strings.TrimSpace(fact.Name) == "" || len(fact.Name) > 1024 {
		return expected(label+".name", "1 to 1024 non-whitespace bytes")
	}
	if fact.Complexity < 1 {
		return expected(label+".complexity", "an integer of at least 1")
	}
	if fact.Depth < 0 {
		return expected(label+".depth", "a non-negative integer")
	}
	if fact.Parameters < 0 {
		return expected(label+".parameters", "a non-negative integer")
	}
	return nil
}

func validateDeadCodeFacts(facts *[]DeadCodeFact, capability string, analyzed []string) error {
	if facts == nil {
		return nil
	}
	if capability != "dead-code" {
		return expected("facts.deadCode", "dead-code facts only for the dead-code capability")
	}
	if len(*facts) > 10000 {
		return expected("facts.deadCode", "at most 10000 items")
	}
	seen := map[string]bool{}
	for index, fact := range *facts {
		label := indexed("facts.deadCode", index)
		identity, err := validateDeadCodeFact(fact, analyzed, label)
		if err != nil {
			return err
		}
		if seen[identity] {
			return expected(label, "a dead-code fact listed only once")
		}
		seen[identity] = true
	}
	return nil
}

func validateDeadCodeFact(fact DeadCodeFact, analyzed []string, label string) (string, error) {
	if !validRule(fact.Analyzer) {
		return "", expected(label+".analyzer", "1 to 256 non-whitespace bytes without whitespace, backslash, or NUL")
	}
	if err := validateFactLocation(fact.Path, fact.Line, 1, analyzed, label); err != nil {
		return "", err
	}
	if fact.EndLine < fact.Line {
		return "", expected(label+".endLine", "a line at or after line")
	}
	if strings.TrimSpace(fact.Name) == "" || len(fact.Name) > 4096 {
		return "", expected(label+".name", "1 to 4096 non-whitespace bytes")
	}
	if !validRule(fact.Kind) {
		return "", expected(label+".kind", "1 to 256 non-whitespace bytes without whitespace, backslash, or NUL")
	}
	if fact.Confidence < 60 || fact.Confidence > 100 {
		return "", expected(label+".confidence", "an integer from 60 to 100")
	}
	if strings.TrimSpace(fact.Message) == "" || len(fact.Message) > 4096 {
		return "", expected(label+".message", "1 to 4096 non-whitespace bytes")
	}
	return deadCodeFactIdentity(fact), nil
}

func deadCodeFactIdentity(fact DeadCodeFact) string {
	return strings.Join([]string{
		fact.Analyzer, fact.Path, fmt.Sprint(fact.Line), fmt.Sprint(fact.EndLine), fact.Name, fact.Kind,
		fmt.Sprint(fact.Confidence), fact.Message,
	}, "\x00")
}

func validateImportFact(fact ImportFact, analyzed []string, label string) error {
	if err := validateFactLocation(fact.Path, fact.Line, fact.Column, analyzed, label); err != nil {
		return err
	}
	if strings.TrimSpace(fact.Specifier) == "" || len(fact.Specifier) > 4096 {
		return expected(label+".specifier", "1 to 4096 non-whitespace bytes")
	}
	if len(fact.Package) > 214 {
		return expected(label+".package", "at most 214 bytes")
	}
	if !slices.Contains([]string{"runtime", "type-only", "re-export", "proven-dynamic"}, fact.Kind) {
		return expected(label+".kind", "runtime, type-only, re-export, or proven-dynamic")
	}
	if fact.Resolved != "" {
		if err := exactRelativePath(fact.Resolved); err != nil {
			return expected(label+".resolved", "an exact contained relative path")
		}
	}
	return nil
}

func validateFactLocation(path string, line, column int, analyzed []string, label string) error {
	if !slices.Contains(analyzed, path) {
		return expected(label+".path", "a path from coverage.analyzed")
	}
	if line < 1 {
		return expected(label+".line", "a one-based line")
	}
	if column < 1 {
		return expected(label+".column", "a one-based UTF-8 byte column")
	}
	return nil
}

func validateInputs(inputs []InputFile) error {
	if len(inputs) > 10000 {
		return expected("inputs", "at most 10000 items")
	}
	seen := map[string]bool{}
	for index, input := range inputs {
		label := indexed("inputs", index)
		if exactRelativePath(input.Path) != nil {
			return expected(label+".path", "an exact contained relative path")
		}
		if seen[input.Path] {
			return expected(label+".path", "a path listed only once")
		}
		if !validDigest(input.SHA256) {
			return expected(label+".sha256", "a lowercase SHA-256 digest")
		}
		seen[input.Path] = true
	}
	return nil
}

func validRule(rule string) bool {
	return len(rule) > 0 && len(rule) <= 256 && strings.TrimSpace(rule) == rule && !strings.ContainsAny(rule, " \t\r\n\\\x00")
}

func analysisDigest(request Request, response Response) (string, error) {
	encoded, err := json.Marshal(struct {
		Request  Request
		Response Response
	}{request, response})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
