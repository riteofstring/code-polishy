package rustfacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/repository"
)

type SourceLocation struct {
	StartByte int `json:"startByte"`
	EndByte   int `json:"endByte"`
	Line      int `json:"line"`
	Column    int `json:"column"`
	EndLine   int `json:"endLine"`
	EndColumn int `json:"endColumn"`
}

type SourceAttribute struct {
	SourceLocation
	Name              string            `json:"name"`
	HasValue          bool              `json:"hasValue"`
	TestConfiguration bool              `json:"testConfiguration"`
	Children          []SourceAttribute `json:"children"`
}

type SourceFunction struct {
	SourceLocation
	Name       string          `json:"name"`
	Context    FunctionContext `json:"context"`
	TestEntry  TestEntryKind   `json:"testEntry"`
	Body       string          `json:"body"`
	Parameters int             `json:"parameters"`
	Complexity int             `json:"complexity"`
	Depth      int             `json:"depth"`
}

type FunctionContext string

const (
	ProductionContext FunctionContext = "production"
	TestContext       FunctionContext = "test"
)

type TestEntryKind string

const (
	NotATest               TestEntryKind = "none"
	BuiltinTest            TestEntryKind = "builtin"
	ConditionalBuiltinTest TestEntryKind = "conditional-builtin"
)

type SourceMacro struct {
	SourceLocation
	Name       string `json:"name"`
	Definition bool   `json:"definition"`
}

type Source struct {
	Path       string            `json:"path"`
	Complete   bool              `json:"complete"`
	Error      string            `json:"error"`
	Comments   []SourceLocation  `json:"comments"`
	Attributes []SourceAttribute `json:"attributes"`
	Functions  []SourceFunction  `json:"functions"`
	Macros     []SourceMacro     `json:"macros"`
	Graph      []SourceGraphFact `json:"graph"`
}

type sourceCoordinates struct {
	text       string
	lineStarts []int
}

func indexSource(text string) sourceCoordinates {
	index := sourceCoordinates{text: text, lineStarts: []int{0}}
	for offset := range len(text) {
		if text[offset] == '\n' {
			index.lineStarts = append(index.lineStarts, offset+1)
		}
	}
	return index
}

func ParseSources(repo repository.Repository, inputs []SourceInput, data []byte) ([]Source, error) {
	if len(data) > 8<<20 {
		return nil, errors.New("rust syntax response exceeds 8388608 bytes")
	}
	var response struct {
		Protocol       string   `json:"protocol"`
		PythonVersion  string   `json:"pythonVersion"`
		ParserVersion  string   `json:"parserVersion"`
		GrammarVersion string   `json:"grammarVersion"`
		Sources        []Source `json:"sources"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("rust syntax response contains trailing data")
	}
	python, _, _ := strings.Cut(repo.ToolPin("python"), "+")
	if response.Protocol != SourceProtocol || response.PythonVersion != python || response.ParserVersion != "0.26.0" || response.GrammarVersion != "0.24.2" {
		return nil, errors.New("rust syntax protocol or runtime versions differ from the governed release")
	}
	if err := validateSourceInventory(inputs, response.Sources); err != nil {
		return nil, err
	}
	return response.Sources, nil
}

func validateSourceInventory(inputs []SourceInput, sources []Source) error {
	requested := map[string]string{}
	for _, input := range inputs {
		requested[input.Path] = input.Source
	}
	if len(requested) != len(inputs) || len(sources) != len(inputs) {
		return errors.New("rust syntax inventory does not match the requested sources")
	}
	for _, source := range sources {
		text, ok := requested[source.Path]
		if !ok {
			return errors.New("rust syntax response repeats or invents a source path")
		}
		delete(requested, source.Path)
		if err := validateSourceFacts(source, text); err != nil {
			return fmt.Errorf("%s: %w", source.Path, err)
		}
	}
	return nil
}

func validateSourceFacts(source Source, text string) error {
	locations, err := sourceFactLocations(source)
	if err != nil {
		return err
	}
	coordinates := indexSource(text)
	if err := validateSourceGraphFacts(source.Graph, coordinates); err != nil {
		return err
	}
	if err := validateSourceAttributes(source.Attributes, coordinates, 0); err != nil {
		return err
	}
	for _, location := range locations {
		if err := validateSourceLocation(location, coordinates); err != nil {
			return err
		}
	}
	return nil
}

func sourceFactLocations(source Source) ([]SourceLocation, error) {
	if source.Comments == nil || source.Attributes == nil || source.Functions == nil || source.Macros == nil || source.Graph == nil || source.Complete == (source.Error != "") {
		return nil, errors.New("rust source omitted complete facts or its coverage failure")
	}
	locations := append([]SourceLocation{}, source.Comments...)
	for _, function := range source.Functions {
		if err := validateSourceFunction(function); err != nil {
			return nil, err
		}
		locations = append(locations, function.SourceLocation)
	}
	for _, macro := range source.Macros {
		if macro.Name == "" {
			return nil, errors.New("rust macro omitted its original source identity")
		}
		locations = append(locations, macro.SourceLocation)
	}
	return locations, nil
}

func validateSourceFunction(function SourceFunction) error {
	if function.Name == "" || function.Complexity < 1 || function.Parameters < 0 || function.Depth < 0 {
		return errors.New("rust function omitted its identity or structural measurements")
	}
	if function.Context != ProductionContext && function.Context != TestContext {
		return errors.New("rust function omitted its production or test context")
	}
	switch function.TestEntry {
	case NotATest, BuiltinTest, ConditionalBuiltinTest:
	default:
		return errors.New("rust function omitted its test entry classification")
	}
	if function.Body != "empty" && function.Body != "nonempty" {
		return errors.New("rust function omitted its body evidence")
	}
	return nil
}

func validateSourceAttributes(attributes []SourceAttribute, coordinates sourceCoordinates, depth int) error {
	if depth > 64 {
		return errors.New("rust attribute facts exceed 64 levels")
	}
	for _, attribute := range attributes {
		if attribute.Name == "" || attribute.Children == nil {
			return errors.New("rust attribute omitted its parsed metadata")
		}
		if err := validateSourceLocation(attribute.SourceLocation, coordinates); err != nil {
			return err
		}
		if err := validateSourceAttributes(attribute.Children, coordinates, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func validateSourceLocation(location SourceLocation, source sourceCoordinates) error {
	if location.StartByte < 0 || location.EndByte < location.StartByte || location.EndByte > len(source.text) {
		return errors.New("rust syntax location lies outside its source")
	}
	for _, point := range []struct{ offset, line, column int }{{location.StartByte, location.Line, location.Column}, {location.EndByte, location.EndLine, location.EndColumn}} {
		line := sort.Search(len(source.lineStarts), func(index int) bool { return source.lineStarts[index] > point.offset }) - 1
		prefix := source.text[source.lineStarts[line]:point.offset]
		if !utf8.ValidString(prefix) || point.line != line+1 || point.column != utf8.RuneCountInString(prefix)+1 {
			return errors.New("rust syntax coordinates do not match the original UTF-8 source")
		}
	}
	return nil
}
