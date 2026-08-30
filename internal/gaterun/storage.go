package gaterun

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	reportsDirectory      = ".code-polishy-reports"
	executionsDirectory   = "executions"
	reportFilename        = "report.json"
	latestFilename        = "latest.json"
	logsDirectory         = "logs"
	receiptsDirectory     = "receipts"
	maximumReportBytes    = 8 << 20
	maximumReceiptBytes   = 64 << 10
	maximumLogHeaderBytes = 32 << 10
)

var errUnsafeArtifact = errors.New("unsafe gate run artifact")

type artifactRoot struct {
	path   string
	handle artifactRootHandle
}

type artifactDirectory struct {
	root       artifactRoot
	components []string
}

type artifactFile struct {
	directory artifactDirectory
	name      string
}

func newRunDirectory(repositoryRoot string, identity Identity) (artifactRoot, artifactDirectory, artifactDirectory, string, error) {
	runSHA256, err := identity.Digest()
	if err != nil {
		return artifactRoot{}, artifactDirectory{}, artifactDirectory{}, "", err
	}
	root, directory, err := managedRunDirectory(repositoryRoot, identity.Gate, runSHA256, true)
	if err != nil {
		return artifactRoot{}, artifactDirectory{}, artifactDirectory{}, "", err
	}
	executions, err := secureRunSubdirectory(directory, executionsDirectory, true)
	if err != nil {
		return artifactRoot{}, artifactDirectory{}, artifactDirectory{}, "", err
	}
	execution, executionID, err := newExecutionDirectory(executions)
	if err != nil {
		return artifactRoot{}, artifactDirectory{}, artifactDirectory{}, "", err
	}
	return root, directory, execution, executionID, nil
}

func existingRunDirectory(repositoryRoot string, identity Identity) (artifactRoot, artifactDirectory, error) {
	runSHA256, err := identity.Digest()
	if err != nil {
		return artifactRoot{}, artifactDirectory{}, err
	}
	return managedRunDirectory(repositoryRoot, identity.Gate, runSHA256, false)
}

func managedRunDirectory(repositoryRoot string, gate GateKind, runSHA256 string, create bool) (artifactRoot, artifactDirectory, error) {
	if !validGate(gate) || !validSHA256(runSHA256) {
		return artifactRoot{}, artifactDirectory{}, fmt.Errorf("%w: gate run path is invalid", ErrInvalidInput)
	}
	root, err := resolveRepositoryRoot(repositoryRoot)
	if err != nil {
		return artifactRoot{}, artifactDirectory{}, err
	}
	directory, err := secureChildDirectory(root, []string{reportsDirectory, string(gate), runSHA256}, create)
	if err != nil {
		return artifactRoot{}, artifactDirectory{}, err
	}
	return root, directory, nil
}

func resolveRepositoryRoot(repositoryRoot string) (artifactRoot, error) {
	if strings.TrimSpace(repositoryRoot) == "" {
		return artifactRoot{}, fmt.Errorf("%w: repository root is required", ErrInvalidInput)
	}
	abs, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return artifactRoot{}, operational("resolve gate repository root", err)
	}
	path, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return artifactRoot{}, operational("resolve gate repository root", err)
	}
	handle, err := makeArtifactRoot(path)
	if err != nil {
		if errors.Is(err, errUnsafeArtifact) {
			return artifactRoot{}, fmt.Errorf("%w: repository root is not a directory", ErrInvalidInput)
		}
		return artifactRoot{}, operational("open gate repository root", err)
	}
	return artifactRoot{path: path, handle: handle}, nil
}

func secureChildDirectory(root artifactRoot, components []string, create bool) (artifactDirectory, error) {
	if len(components) == 0 || !validArtifactComponents(components) {
		return artifactDirectory{}, fmt.Errorf("%w: gate artifact path component is invalid", ErrInvalidInput)
	}
	directory := artifactDirectory{root: root, components: append([]string{}, components...)}
	if err := ensureArtifactDirectory(directory, create); err != nil {
		return artifactDirectory{}, err
	}
	return directory, nil
}

func secureRunSubdirectory(parent artifactDirectory, name string, create bool) (artifactDirectory, error) {
	if !validArtifactComponent(name) || len(parent.components) == 0 {
		return artifactDirectory{}, fmt.Errorf("%w: gate artifact directory is invalid", ErrInvalidArtifact)
	}
	components := append(append([]string{}, parent.components...), name)
	directory := artifactDirectory{root: parent.root, components: components}
	if err := ensureArtifactDirectory(directory, create); err != nil {
		return artifactDirectory{}, err
	}
	return directory, nil
}

func ensureArtifactDirectory(directory artifactDirectory, create bool) error {
	if err := platformEnsureArtifactDirectory(directory, create); err != nil {
		return classifyArtifactError("open gate artifact directory", "gate artifact directory", err)
	}
	return nil
}

func newExecutionDirectory(parent artifactDirectory) (artifactDirectory, string, error) {
	for range 32 {
		executionID, err := newExecutionID()
		if err != nil {
			return artifactDirectory{}, "", err
		}
		directory := artifactDirectory{
			root: parent.root, components: append(append([]string{}, parent.components...), executionID),
		}
		err = platformCreateArtifactDirectory(directory)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return artifactDirectory{}, "", classifyArtifactError("create gate execution directory", "gate execution directory", err)
		}
		return directory, executionID, nil
	}
	return artifactDirectory{}, "", fmt.Errorf("%w: could not allocate a gate execution directory", ErrOperational)
}

func newExecutionID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", operational("generate gate execution identity", err)
	}
	return "run-" + hex.EncodeToString(data), nil
}

func temporaryArtifactName() (string, error) {
	identifier, err := newExecutionID()
	if err != nil {
		return "", err
	}
	return ".gaterun-" + identifier, nil
}

func validExecutionID(value string) bool {
	return len(value) == len("run-")+32 && strings.HasPrefix(value, "run-") && validHexadecimal(value[len("run-"):])
}

func validArtifactComponents(values []string) bool {
	for _, value := range values {
		if !validArtifactComponent(value) {
			return false
		}
	}
	return true
}

func validArtifactComponent(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/\\\x00")
}

func artifactFilePath(directory artifactDirectory, name string) (artifactFile, string, error) {
	if !validArtifactComponent(name) {
		return artifactFile{}, "", fmt.Errorf("%w: gate artifact file path is invalid", ErrInvalidInput)
	}
	file := artifactFile{directory: directory, name: name}
	return file, file.display(), nil
}

func (directory artifactDirectory) display() string {
	return strings.Join(directory.components, "/")
}

func (file artifactFile) display() string {
	return file.directory.display() + "/" + file.name
}

func writeArtifactAtomic(file artifactFile, data []byte) error {
	if err := platformWriteArtifactAtomic(file, data); err != nil {
		return classifyArtifactError("write gate artifact", "gate artifact", err)
	}
	return nil
}

func readArtifact(file artifactFile, maximum int, label string) ([]byte, error) {
	if maximum < 1 {
		return nil, fmt.Errorf("%w: %s size limit is invalid", ErrInvalidInput, label)
	}
	data, err := platformReadArtifact(file, maximum)
	if err != nil {
		return nil, classifyArtifactError("read "+label, label, err)
	}
	return data, nil
}

func removeArtifact(file artifactFile) error {
	if err := platformRemoveArtifact(file); err != nil {
		return classifyArtifactError("remove gate artifact", "gate artifact", err)
	}
	return nil
}

func classifyArtifactError(operation, label string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errUnsafeArtifact) {
		return fmt.Errorf("%w: %s is not a contained restrictive artifact", ErrInvalidArtifact, label)
	}
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s is missing", ErrMissingArtifact, label)
	}
	return operational(operation, err)
}

func marshalArtifact(value any, operation string) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, operational(operation, err)
	}
	return append(data, '\n'), nil
}

func decodeStrict(data []byte, value any, label string) error {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return fmt.Errorf("%w: %s JSON is invalid: %v", ErrInvalidArtifact, label, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("%w: %s JSON is invalid: %v", ErrInvalidArtifact, label, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("%w: %s JSON is invalid: %v", ErrInvalidArtifact, label, err)
		}
		return fmt.Errorf("%w: %s JSON has more than one value", ErrInvalidArtifact, label)
	}
	return nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return errors.New("JSON has more than one value")
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		return consumeJSONObject(decoder)
	case '[':
		return consumeJSONArray(decoder)
	default:
		return errors.New("JSON has an unexpected delimiter")
	}
}

func consumeJSONObject(decoder *json.Decoder) error {
	seen := map[string]bool{}
	for decoder.More() {
		if err := consumeJSONObjectEntry(decoder, seen); err != nil {
			return err
		}
	}
	return consumeJSONEnd(decoder, '}')
}

func consumeJSONObjectEntry(decoder *json.Decoder, seen map[string]bool) error {
	key, err := decoder.Token()
	if err != nil {
		return err
	}
	name, ok := key.(string)
	if !ok || seen[name] {
		return errors.New("JSON object has duplicate or invalid key")
	}
	seen[name] = true
	return consumeJSONValue(decoder)
}

func consumeJSONArray(decoder *json.Decoder) error {
	for decoder.More() {
		if err := consumeJSONValue(decoder); err != nil {
			return err
		}
	}
	return consumeJSONEnd(decoder, ']')
}

func consumeJSONEnd(decoder *json.Decoder, expected rune) error {
	end, err := decoder.Token()
	if err != nil || end != json.Delim(expected) {
		return errors.New("JSON container is not closed")
	}
	return nil
}
