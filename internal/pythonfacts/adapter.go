package pythonfacts

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	Protocol            = "python-facts/v1"
	PackagingVersion    = "26.3"
	maximumRequestSize  = 8 * 1024 * 1024
	maximumResponseSize = 16 * 1024 * 1024
	maximumItems        = 4096
)

//go:embed source_adapter.py
var sourceAdapterSource string

//go:embed adapter.py
var adapterSource string

type Input struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

type Request struct {
	Protocol     string   `json:"protocol"`
	Operation    string   `json:"operation"`
	Manifests    []Input  `json:"manifests"`
	Locks        []Input  `json:"locks"`
	Metadata     []Input  `json:"metadata"`
	Sources      []Input  `json:"sources"`
	Requirements []string `json:"requirements"`
	Specifiers   []string `json:"specifiers"`
	Names        []string `json:"names"`
}

type Value struct {
	Kind   string             `json:"kind"`
	Text   string             `json:"text"`
	Line   int                `json:"line"`
	Array  []Value            `json:"array"`
	Inline []InlineAssignment `json:"inline"`
}

type InlineAssignment struct {
	Path  []string `json:"path"`
	Value Value    `json:"value"`
}

type Assignment struct {
	TablePath  []string `json:"tablePath"`
	KeyPath    []string `json:"keyPath"`
	ArrayTable bool     `json:"arrayTable"`
	Line       int      `json:"line"`
	Value      Value    `json:"value"`
}

type Table struct {
	Path  []string `json:"path"`
	Array bool     `json:"array"`
	Line  int      `json:"line"`
}

type VersionSpecifier struct {
	Operator string `json:"operator"`
	Version  string `json:"version"`
}

type Requirement struct {
	Input          string             `json:"input"`
	Name           string             `json:"name"`
	Extras         []string           `json:"extras"`
	Specifier      string             `json:"specifier"`
	Specifiers     []VersionSpecifier `json:"specifiers"`
	Marker         string             `json:"marker"`
	MarkerVariable string             `json:"markerVariable"`
	MarkerValue    string             `json:"markerValue"`
	URL            string             `json:"url"`
	Error          string             `json:"error"`
}

type Specifier struct {
	Input  string `json:"input"`
	Target string `json:"target"`
	Error  string `json:"error"`
}

type Manifest struct {
	Path         string        `json:"path"`
	Tables       []Table       `json:"tables"`
	Assignments  []Assignment  `json:"assignments"`
	Requirements []Requirement `json:"requirements"`
	Specifiers   []Specifier   `json:"specifiers"`
	Error        string        `json:"error"`
}

type LockSourceField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type LockPackage struct {
	NameInput string            `json:"nameInput"`
	Name      string            `json:"name"`
	Version   string            `json:"version"`
	Source    []LockSourceField `json:"source"`
}

type Lock struct {
	Path           string        `json:"path"`
	Version        int           `json:"version"`
	Revision       int           `json:"revision"`
	RequiresPython string        `json:"requiresPython"`
	Packages       []LockPackage `json:"packages"`
	Error          string        `json:"error"`
}

type Distribution struct {
	Path         string        `json:"path"`
	NameInput    string        `json:"nameInput"`
	Name         string        `json:"name"`
	Version      string        `json:"version"`
	Requirements []Requirement `json:"requirements"`
	Error        string        `json:"error"`
}

type Import struct {
	Module  string   `json:"module"`
	Names   []string `json:"names"`
	Line    int      `json:"line"`
	Literal bool     `json:"literal"`
}

type ComputedImport struct {
	Callee          string                  `json:"callee"`
	Callable        string                  `json:"callable"`
	Line            int                     `json:"line"`
	Column          int                     `json:"column"`
	EndLine         int                     `json:"endLine"`
	EndColumn       int                     `json:"endColumn"`
	Argument        string                  `json:"argument"`
	Shape           string                  `json:"shape"`
	Targets         []string                `json:"targets"`
	Configuration   []ComputedConfiguration `json:"configuration"`
	EntryPointGroup string                  `json:"entryPointGroup"`
	EvidenceError   string                  `json:"evidenceError"`
}

type ComputedConfiguration struct {
	Path        string `json:"path"`
	JSONPointer string `json:"jsonPointer"`
}

type Source struct {
	Path            string           `json:"path"`
	SHA256          string           `json:"sha256"`
	Imports         []Import         `json:"imports"`
	ComputedImports []ComputedImport `json:"computedImports"`
	Error           string           `json:"error"`
}

type Name struct {
	Input      string `json:"input"`
	Normalized string `json:"normalized"`
	Error      string `json:"error"`
}

type Response struct {
	Protocol         string         `json:"protocol"`
	PythonVersion    string         `json:"pythonVersion"`
	PackagingVersion string         `json:"packagingVersion"`
	Manifests        []Manifest     `json:"manifests"`
	Locks            []Lock         `json:"locks"`
	Metadata         []Distribution `json:"metadata"`
	Sources          []Source       `json:"sources"`
	Requirements     []Requirement  `json:"requirements"`
	Specifiers       []Specifier    `json:"specifiers"`
	Names            []Name         `json:"names"`
}

type boundedBuffer struct {
	maximum int
	data    bytes.Buffer
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	if buffer.data.Len()+len(data) > buffer.maximum {
		remaining := buffer.maximum - buffer.data.Len()
		if remaining > 0 {
			_, _ = buffer.data.Write(data[:remaining])
		}
		return len(data), errors.New("bounded output exceeded")
	}
	return buffer.data.Write(data)
}

func (buffer *boundedBuffer) Bytes() []byte {
	return buffer.data.Bytes()
}

func Analyze(python string, request Request) (Response, error) {
	return analyze(context.Background(), python, request)
}

func analyze(ctx context.Context, python string, request Request) (Response, error) {
	request = normalizedRequest(request)
	if err := validateRequest(python, request); err != nil {
		return Response{}, err
	}
	data, err := encodeRequest(request)
	if err != nil {
		return Response{}, err
	}
	responseData, err := runAdapter(ctx, python, data)
	if err != nil {
		return Response{}, err
	}
	response, err := decodeResponse(responseData)
	if err != nil {
		return Response{}, err
	}
	if err := validateResponse(response, request); err != nil {
		return Response{}, err
	}
	return response, nil
}

func normalizedRequest(request Request) Request {
	request.Protocol = Protocol
	request.Operation = "analyze"
	if request.Manifests == nil {
		request.Manifests = []Input{}
	}
	if request.Locks == nil {
		request.Locks = []Input{}
	}
	if request.Metadata == nil {
		request.Metadata = []Input{}
	}
	if request.Sources == nil {
		request.Sources = []Input{}
	}
	if request.Requirements == nil {
		request.Requirements = []string{}
	}
	if request.Specifiers == nil {
		request.Specifiers = []string{}
	}
	if request.Names == nil {
		request.Names = []string{}
	}
	return request
}

func validateRequest(python string, request Request) error {
	if python == "" || !filepath.IsAbs(python) {
		return errors.New("python-facts interpreter must be an absolute path")
	}
	if requestExceedsItemLimit(request) {
		return errors.New("python-facts request exceeds the item limit")
	}
	if !validRequestInputs(request) || !validRequestValues(request) {
		return errors.New("python-facts request contains invalid UTF-8")
	}
	return nil
}

func requestExceedsItemLimit(request Request) bool {
	counts := []int{
		len(request.Manifests), len(request.Locks), len(request.Metadata), len(request.Sources),
		len(request.Requirements), len(request.Specifiers), len(request.Names),
	}
	for _, count := range counts {
		if count > maximumItems {
			return true
		}
	}
	return false
}

func validRequestInputs(request Request) bool {
	for _, inputs := range [][]Input{request.Manifests, request.Locks, request.Metadata, request.Sources} {
		for _, input := range inputs {
			if !utf8.ValidString(input.Path) || !utf8.ValidString(input.Source) {
				return false
			}
		}
	}
	return true
}

func validRequestValues(request Request) bool {
	for _, values := range [][]string{request.Requirements, request.Specifiers, request.Names} {
		for _, value := range values {
			if !utf8.ValidString(value) {
				return false
			}
		}
	}
	return true
}

func encodeRequest(request Request) ([]byte, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if len(data) > maximumRequestSize {
		return nil, errors.New("python-facts request exceeds the byte limit")
	}
	return data, nil
}

func runAdapter(parent context.Context, python string, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, python, "-I", "-B", "-c", embeddedAdapterSource())
	command.Dir = filepath.Dir(python)
	command.Env = []string{}
	command.Stdin = bytes.NewReader(data)
	stdout := &boundedBuffer{maximum: maximumResponseSize}
	stderr := &boundedBuffer{maximum: 64 * 1024}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("python-facts adapter exceeded its time limit")
		}
		message := strings.TrimSpace(string(stderr.Bytes()))
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("python-facts adapter failed: %s", message)
	}
	return stdout.Bytes(), nil
}

func embeddedAdapterSource() string {
	encoded := base64.StdEncoding.EncodeToString([]byte(sourceAdapterSource))
	bootstrap := "import base64,sys,types\n_source_adapter=types.ModuleType('source_adapter')\nexec(compile(base64.b64decode('" + encoded + "'),'source_adapter.py','exec'),_source_adapter.__dict__)\nsys.modules['source_adapter']=_source_adapter\n"
	return bootstrap + adapterSource
}

func decodeResponse(data []byte) (Response, error) {
	var response Response
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return Response{}, fmt.Errorf("decode python-facts response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Response{}, errors.New("python-facts response has trailing JSON")
	}
	return response, nil
}

func validateResponse(response Response, request Request) error {
	if response.Protocol != Protocol || response.PackagingVersion != PackagingVersion || !strings.HasPrefix(response.PythonVersion, "3.12.") {
		return errors.New("python-facts response has an invalid tool identity")
	}
	if !responseCountsMatch(response, request) {
		return errors.New("python-facts response has an invalid result count")
	}
	return validateResponseOrder(response, request)
}

func responseCountsMatch(response Response, request Request) bool {
	responseCounts := []int{
		len(response.Manifests), len(response.Locks), len(response.Metadata), len(response.Sources),
		len(response.Requirements), len(response.Specifiers), len(response.Names),
	}
	requestCounts := []int{
		len(request.Manifests), len(request.Locks), len(request.Metadata), len(request.Sources),
		len(request.Requirements), len(request.Specifiers), len(request.Names),
	}
	return slices.Equal(responseCounts, requestCounts)
}

func validateResponseOrder(response Response, request Request) error {
	checks := []struct {
		actual   []string
		expected []string
		message  string
	}{
		{selectedValues(response.Manifests, func(value Manifest) string { return value.Path }), selectedValues(request.Manifests, func(value Input) string { return value.Path }), "python-facts response reordered manifest inputs"},
		{selectedValues(response.Locks, func(value Lock) string { return value.Path }), selectedValues(request.Locks, func(value Input) string { return value.Path }), "python-facts response reordered lock inputs"},
		{selectedValues(response.Metadata, func(value Distribution) string { return value.Path }), selectedValues(request.Metadata, func(value Input) string { return value.Path }), "python-facts response reordered metadata inputs"},
		{selectedValues(response.Sources, func(value Source) string { return value.Path }), selectedValues(request.Sources, func(value Input) string { return value.Path }), "python-facts response reordered source inputs"},
		{selectedValues(response.Requirements, func(value Requirement) string { return value.Input }), request.Requirements, "python-facts response reordered requirement inputs"},
		{selectedValues(response.Specifiers, func(value Specifier) string { return value.Input }), request.Specifiers, "python-facts response reordered specifier inputs"},
		{selectedValues(response.Names, func(value Name) string { return value.Input }), request.Names, "python-facts response reordered package-name inputs"},
	}
	for _, check := range checks {
		if !slices.Equal(check.actual, check.expected) {
			return errors.New(check.message)
		}
	}
	return nil
}

func selectedValues[T any](values []T, selectValue func(T) string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, selectValue(value))
	}
	return result
}

func Interpreter(policyRoot string) string {
	return filepath.Join(policyRoot, ".tools", "python", runtime.GOOS+"-"+architecture(runtime.GOARCH), executable("python"))
}

func DefaultInterpreter() (string, error) {
	starts := []string{}
	if executablePath, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(executablePath))
	}
	if workingDirectory, err := os.Getwd(); err == nil {
		starts = append(starts, workingDirectory)
	}
	if _, sourcePath, _, ok := runtime.Caller(0); ok {
		starts = append(starts, filepath.Dir(sourcePath))
	}
	seen := map[string]bool{}
	for _, start := range starts {
		for directory := start; directory != filepath.Dir(directory); directory = filepath.Dir(directory) {
			if seen[directory] {
				continue
			}
			seen[directory] = true
			candidate := Interpreter(directory)
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
				absolute, err := filepath.Abs(candidate)
				return absolute, err
			}
		}
	}
	return "", errors.New("the policy-owned CPython 3.12 interpreter is unavailable")
}

func SortedInputs(inputs []Input) []Input {
	result := slices.Clone(inputs)
	slices.SortFunc(result, func(left, right Input) int { return strings.Compare(left.Path, right.Path) })
	return result
}

func architecture(value string) string {
	switch value {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	default:
		return value
	}
}

func executable(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
