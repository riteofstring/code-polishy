package pythonfacts

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/strictjson"
)

//go:embed source_parser.py
var sourceParserSource string

//go:embed type_facts.py
var typeFactsSource string

//go:embed object_predicates.py
var objectPredicatesSource string

//go:embed type_resolver.py
var typeResolverSource string

//go:embed connection_flow.py
var connectionFlowSource string

//go:embed astroid_contracts.py
var astroidContractsSource string

//go:embed framework_contracts.py
var frameworkContractsSource string

//go:embed repository_contracts.py
var repositoryContractsSource string

type TypeModule struct {
	Path         string          `json:"path"`
	Module       string          `json:"module"`
	Package      string          `json:"package"`
	SourceSHA256 string          `json:"sourceSha256"`
	Facts        json.RawMessage `json:"facts"`
}

type typeCoverage struct {
	Path         string `json:"path"`
	SourceSHA256 string `json:"sourceSha256"`
}

type typeProjectReader struct {
	modules []TypeModule
	current *bytes.Reader
	index   int
	total   int
}

func validateSourceTypeFacts(data json.RawMessage) error {
	var facts struct {
		Scopes   []json.RawMessage `json:"scopes"`
		Bindings []json.RawMessage `json:"bindings"`
		Calls    []json.RawMessage `json:"calls"`
	}
	if err := strictjson.Decode(data, &facts); err != nil {
		return fmt.Errorf("decode compact type facts: %w", err)
	}
	if len(facts.Scopes) == 0 || facts.Bindings == nil || facts.Calls == nil {
		return fmt.Errorf("compact type facts omit required scope, binding, or call collections")
	}
	return nil
}

func TypeSupportSource() string {
	return embeddedPythonModule("object_predicates", objectPredicatesSource) + embeddedPythonModule("type_facts", typeFactsSource) + embeddedPythonModule("type_resolver", typeResolverSource) + embeddedPythonModule("connection_flow", connectionFlowSource) + embeddedPythonModule("astroid_contracts", astroidContractsSource) + embeddedPythonModule("framework_contracts", frameworkContractsSource) + embeddedPythonModule("repository_contracts", repositoryContractsSource)
}

func ParserSupportSource() string {
	return embeddedPythonModule("source_parser", sourceParserSource)
}

func SourceSupportSource() string {
	return ParserSupportSource() + TypeSupportSource() + embeddedPythonModule("source_adapter", sourceAdapterSource)
}

func embeddedPythonModule(name, source string) string {
	return "import sys,types\n_module=types.ModuleType('" + name + "')\nexec(compile(" + strconv.Quote(source) + ",'" + name + ".py','exec'),_module.__dict__)\nsys.modules['" + name + "']=_module\n"
}

func (reader *typeProjectReader) Read(target []byte) (int, error) {
	if reader.current.Len() > 0 {
		return reader.current.Read(target)
	}
	if reader.index == len(reader.modules) {
		return 0, io.EOF
	}
	data, err := json.Marshal(reader.modules[reader.index])
	if err != nil {
		return 0, err
	}
	if len(data)+1 > maximumResponseSize || len(data)+1 > maximumProjectFactSize-reader.total {
		return 0, fmt.Errorf("python type project exceeds its compact fact byte limit")
	}
	reader.total += len(data) + 1
	reader.index++
	reader.current = bytes.NewReader(append(data, '\n'))
	return reader.current.Read(target)
}

func runFactProject(ctx context.Context, python string, input io.Reader, program string) ([]byte, error) {
	return runFactProjectWithLimit(ctx, python, input, program, maximumResponseSize)
}

func runFactProjectWithLimit(ctx context.Context, python string, input io.Reader, program string, outputLimit int) ([]byte, error) {
	programInput, err := ProgramInput(program, input)
	if err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, maximumProjectDuration)
	defer cancel()
	command := exec.CommandContext(bounded, python, "-I", "-B", "-c", ProgramBootstrap)
	command.Dir = filepath.Dir(python)
	command.Env = []string{}
	command.Stdin = programInput
	stdout := &boundedBuffer{maximum: outputLimit}
	stderr := &boundedBuffer{maximum: 64 * 1024}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		if bounded.Err() != nil {
			return nil, fmt.Errorf("python fact project exceeded its time limit")
		}
		return nil, fmt.Errorf("python fact project failed: %s: %w", strings.TrimSpace(string(stderr.Bytes())), err)
	}
	return stdout.Bytes(), nil
}
