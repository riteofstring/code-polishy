package architecture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func pythonComputedTargets(repo repository.Repository, project repository.PythonProject, declaration policy.PythonComputedImport) ([]string, string) {
	if declaration.EntryPointGroup != "" {
		return pythonComputedEntryPointTargets(project, declaration)
	}
	targets := append([]string{}, declaration.Targets...)
	for _, input := range declaration.Configuration {
		values, message := pythonComputedConfigurationTargets(repo, input)
		if message != "" {
			return nil, message
		}
		targets = append(targets, values...)
	}
	seen := map[string]bool{}
	result := []string{}
	for _, target := range targets {
		if target != declaration.Namespace && !strings.HasPrefix(target, declaration.Namespace+".") {
			return nil, fmt.Sprintf("computed Python import target %q escapes namespace %q", target, declaration.Namespace)
		}
		if !seen[target] {
			seen[target] = true
			result = append(result, target)
		}
	}
	if len(result) == 0 {
		return nil, "computed Python import target set is empty"
	}
	slices.Sort(result)
	return result, ""
}

func pythonComputedEntryPointTargets(project repository.PythonProject, declaration policy.PythonComputedImport) ([]string, string) {
	table := "project.entry-points." + declaration.EntryPointGroup
	seen := map[string]bool{}
	targets := []string{}
	for _, reference := range project.DynamicReferences {
		if reference.Table == table && !seen[reference.Module] {
			seen[reference.Module] = true
			targets = append(targets, reference.Module)
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Sprintf("computed Python import entry-point group %q is absent or empty", declaration.EntryPointGroup)
	}
	slices.Sort(targets)
	return targets, ""
}

func pythonComputedConfigurationTargets(repo repository.Repository, input policy.PythonComputedImportInput) ([]string, string) {
	data, err := repo.Read(input.Path)
	if err != nil {
		return nil, fmt.Sprintf("computed import configuration %s is unreadable: %v", input.Path, err)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != input.SHA256 {
		return nil, fmt.Sprintf("computed import configuration %s changed and its declaration is stale", input.Path)
	}
	value, err := decodeComputedJSON(data)
	if err != nil {
		return nil, fmt.Sprintf("computed import configuration %s is malformed: %v", input.Path, err)
	}
	value, err = computedJSONPointer(value, input.JSONPointer)
	if err != nil {
		return nil, fmt.Sprintf("computed import configuration %s pointer %s is invalid: %v", input.Path, input.JSONPointer, err)
	}
	switch selected := value.(type) {
	case string:
		if selected == "" {
			return nil, "computed import configuration selects an empty module"
		}
		return []string{selected}, ""
	case []any:
		result := make([]string, 0, len(selected))
		for _, item := range selected {
			text, ok := item.(string)
			if !ok || text == "" {
				return nil, "computed import configuration must select only nonempty module names"
			}
			result = append(result, text)
		}
		return result, ""
	default:
		return nil, "computed import configuration must select a module name or an array of module names"
	}
}

func decodeComputedJSON(data []byte) (any, error) {
	if len(data) == 0 || len(data) > 2*1024*1024 {
		return nil, errors.New("JSON input has an invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := decodeComputedJSONValue(decoder, 0, new(int))
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("JSON input has trailing data")
	}
	return value, nil
}

func decodeComputedJSONValue(decoder *json.Decoder, depth int, count *int) (any, error) {
	(*count)++
	if depth > 64 || *count > 100000 {
		return nil, errors.New("JSON input exceeds its structural limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return token, nil
	}
	switch delimiter {
	case '{':
		return decodeComputedJSONObject(decoder, depth, count)
	case '[':
		return decodeComputedJSONArray(decoder, depth, count)
	default:
		return nil, errors.New("JSON input has an invalid delimiter")
	}
}

func decodeComputedJSONObject(decoder *json.Decoder, depth int, count *int) (map[string]any, error) {
	result := map[string]any{}
	seen := map[string]bool{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok || seen[key] {
			return nil, errors.New("JSON object has an invalid or duplicate key")
		}
		seen[key] = true
		value, err := decodeComputedJSONValue(decoder, depth+1, count)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	_, err := decoder.Token()
	return result, err
}

func decodeComputedJSONArray(decoder *json.Decoder, depth int, count *int) ([]any, error) {
	result := []any{}
	for decoder.More() {
		value, err := decodeComputedJSONValue(decoder, depth+1, count)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	_, err := decoder.Token()
	return result, err
}

func computedJSONPointer(value any, pointer string) (any, error) {
	if pointer == "" {
		return value, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, errors.New("JSON pointer must start with a slash")
	}
	for _, raw := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		segment, err := decodeComputedJSONPointerSegment(raw)
		if err != nil {
			return nil, err
		}
		value, err = computedJSONPointerStep(value, segment)
		if err != nil {
			return nil, err
		}
	}
	return value, nil
}

func decodeComputedJSONPointerSegment(raw string) (string, error) {
	var segment strings.Builder
	for index := 0; index < len(raw); index++ {
		if raw[index] != '~' {
			segment.WriteByte(raw[index])
			continue
		}
		if index+1 >= len(raw) || raw[index+1] != '0' && raw[index+1] != '1' {
			return "", errors.New("JSON pointer has an invalid escape")
		}
		index++
		if raw[index] == '0' {
			segment.WriteByte('~')
		} else {
			segment.WriteByte('/')
		}
	}
	return segment.String(), nil
}

func computedJSONPointerStep(value any, segment string) (any, error) {
	switch current := value.(type) {
	case map[string]any:
		selected, found := current[segment]
		if !found {
			return nil, errors.New("JSON pointer does not exist")
		}
		return selected, nil
	case []any:
		if segment == "" || len(segment) > 1 && segment[0] == '0' {
			return nil, errors.New("JSON pointer array index is invalid")
		}
		index, err := strconv.Atoi(segment)
		if err != nil || index < 0 || index >= len(current) {
			return nil, errors.New("JSON pointer array index is outside the value")
		}
		return current[index], nil
	default:
		return nil, errors.New("JSON pointer traverses a scalar")
	}
}
