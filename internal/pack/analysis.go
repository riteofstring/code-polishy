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
	Quality     policy.Quality `json:"quality"`
	Files       []SourceInput  `json:"files"`
	EntryPoints []string       `json:"entryPoints"`
}

type SourceInput struct {
	Path        string `json:"path"`
	Language    string `json:"language"`
	Test        bool   `json:"test"`
	Generated   bool   `json:"generated"`
	Development bool   `json:"development"`
}

type RuntimeIdentity struct {
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
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
	Kind       string `json:"kind"`
	Raw        string `json:"raw"`
	Complete   bool   `json:"complete"`
	BeforeCode bool   `json:"beforeCode"`
	Preamble   bool   `json:"preamble"`
	ByteZero   bool   `json:"byteZero"`
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
	return validateInputs(response.Inputs)
}

func validateCoverage(response Response, request Request) error {
	if response.Coverage == nil || response.Coverage.Analyzed == nil || response.Coverage.Unsupported == nil {
		return errors.New("analysis requires explicit analyzed and unsupported coverage")
	}
	accounted, err := accountCoverage(*response.Coverage, request.Files)
	if err != nil {
		return err
	}
	for _, path := range request.Files {
		if !accounted[path] {
			return fmt.Errorf("analysis omitted requested path %q", path)
		}
	}
	if response.Status == "pass" && len(response.Coverage.Unsupported) > 0 {
		return errors.New("pass requires complete coverage")
	}
	return nil
}

func accountCoverage(coverage Coverage, selected []string) (map[string]bool, error) {
	accounted := map[string]bool{}
	for _, path := range coverage.Analyzed {
		if err := accountCoveragePath(accounted, selected, path); err != nil {
			return nil, err
		}
	}
	for _, item := range coverage.Unsupported {
		if strings.TrimSpace(item.Reason) == "" || len(item.Reason) > 4096 {
			return nil, fmt.Errorf("invalid unsupported reason for %s", item.Path)
		}
		if err := accountCoveragePath(accounted, selected, item.Path); err != nil {
			return nil, err
		}
	}
	return accounted, nil
}

func accountCoveragePath(accounted map[string]bool, selected []string, path string) error {
	if !slices.Contains(selected, path) || accounted[path] {
		return fmt.Errorf("invalid or repeated coverage path %q", path)
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
	return validateFunctionFacts(facts.Functions, request.Capability, analyzed)
}

func validateImportFacts(facts *[]ImportFact, capability string, analyzed []string) error {
	if facts == nil {
		return nil
	}
	if capability != "architecture" || len(*facts) > 20000 {
		return errors.New("unexpected or excessive import facts")
	}
	for _, fact := range *facts {
		if err := validateImportFact(fact, analyzed); err != nil {
			return err
		}
	}
	return nil
}

func validateCommentFacts(facts *[]CommentFact, capability string, analyzed []string) error {
	if facts == nil {
		return nil
	}
	if capability != "lint" || len(*facts) > 20000 {
		return errors.New("unexpected or excessive comment facts")
	}
	for _, fact := range *facts {
		if !validCommentFact(fact, analyzed) {
			return errors.New("invalid or incomplete source-comment fact")
		}
	}
	return nil
}

func validCommentFact(fact CommentFact, analyzed []string) bool {
	return validFactLocation(fact.Path, fact.Line, fact.Column, analyzed) && fact.Complete && len(fact.Raw) > 0 && len(fact.Raw) <= 65536 && slices.Contains([]string{"Line", "Block", "Docstring", "HTML", "Shebang"}, fact.Kind)
}

func validateFunctionFacts(facts *[]FunctionFact, capability string, analyzed []string) error {
	if facts == nil {
		return nil
	}
	if capability != "complexity" || len(*facts) > 20000 {
		return errors.New("unexpected or excessive function facts")
	}
	for _, fact := range *facts {
		if !validFunctionFact(fact, analyzed) {
			return errors.New("invalid function fact")
		}
	}
	return nil
}

func validFunctionFact(fact FunctionFact, analyzed []string) bool {
	return validFactLocation(fact.Path, fact.Line, fact.Column, analyzed) && strings.TrimSpace(fact.Name) != "" && len(fact.Name) <= 1024 && fact.Complexity >= 1 && fact.Depth >= 0 && fact.Parameters >= 0
}

func validateImportFact(fact ImportFact, analyzed []string) error {
	if !validFactLocation(fact.Path, fact.Line, fact.Column, analyzed) || strings.TrimSpace(fact.Specifier) == "" || len(fact.Specifier) > 4096 || len(fact.Package) > 214 {
		return errors.New("invalid import fact")
	}
	if !slices.Contains([]string{"runtime", "type-only", "re-export", "proven-dynamic"}, fact.Kind) {
		return errors.New("invalid import edge kind")
	}
	if fact.Resolved != "" {
		return exactRelativePath(fact.Resolved)
	}
	return nil
}

func validFactLocation(path string, line, column int, analyzed []string) bool {
	return slices.Contains(analyzed, path) && line > 0 && column > 0
}

func validateInputs(inputs []InputFile) error {
	if len(inputs) > 10000 {
		return errors.New("analysis input count exceeds its limit")
	}
	seen := map[string]bool{}
	for _, input := range inputs {
		if seen[input.Path] || exactRelativePath(input.Path) != nil || !validDigest(input.SHA256) {
			return errors.New("analysis inputs must have distinct contained paths and SHA-256 identities")
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
