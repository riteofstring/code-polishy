package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"unicode"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type jsonMemberLocation struct {
	key        string
	keyStart   int
	valueStart int
	valueEnd   int
}

func renderPackMigrationConfig(data []byte, selections []policy.PackSelection) ([]byte, error) {
	members, closeOffset, err := topLevelJSONMembers(data)
	if err != nil {
		return nil, err
	}
	for _, member := range members {
		if member.key != "packs" {
			continue
		}
		indent := lineIndent(data, member.keyStart)
		value, err := renderPackSelections(selections, indent)
		if err != nil {
			return nil, err
		}
		return joinJSONParts(data[:member.valueStart], value, data[member.valueEnd:]), nil
	}
	indent := []byte("  ")
	value, err := renderPackSelections(selections, indent)
	if err != nil {
		return nil, err
	}
	insertOffset := closeOffset
	separator := []byte("\n  \"packs\": ")
	if len(members) > 0 {
		insertOffset = members[len(members)-1].valueEnd
		separator = []byte(",\n  \"packs\": ")
	}
	if !bytes.Contains(data[:closeOffset], []byte("\n")) {
		separator = []byte("\"packs\":")
		if len(members) > 0 {
			separator = []byte(",\"packs\":")
		}
		value, err = json.Marshal(selections)
		if err != nil {
			return nil, err
		}
	}
	return joinJSONParts(data[:insertOffset], separator, value, data[insertOffset:]), nil
}

func renderPackSelections(selections []policy.PackSelection, indent []byte) ([]byte, error) {
	value, err := json.MarshalIndent(selections, "", "  ")
	if err != nil {
		return nil, err
	}
	return bytes.ReplaceAll(value, []byte("\n"), append([]byte("\n"), indent...)), nil
}

func joinJSONParts(parts ...[]byte) []byte {
	size := 0
	for _, part := range parts {
		size += len(part)
	}
	result := make([]byte, 0, size)
	for _, part := range parts {
		result = append(result, part...)
	}
	return result
}

func topLevelJSONMembers(data []byte) ([]jsonMemberLocation, int, error) {
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &decoded); err != nil || decoded == nil {
		return nil, 0, errors.New("configuration must be one JSON object")
	}
	index := skipJSONSpace(data, 0)
	if index >= len(data) || data[index] != '{' {
		return nil, 0, errors.New("configuration must be one JSON object")
	}
	index++
	members := []jsonMemberLocation{}
	for {
		index = skipJSONSpace(data, index)
		if index >= len(data) {
			return nil, 0, errors.New("configuration object is incomplete")
		}
		if data[index] == '}' {
			return members, index, nil
		}
		keyStart := index
		keyEnd, err := scanJSONString(data, index)
		if err != nil {
			return nil, 0, err
		}
		var key string
		if err := json.Unmarshal(data[keyStart:keyEnd], &key); err != nil {
			return nil, 0, err
		}
		index = skipJSONSpace(data, keyEnd)
		if index >= len(data) || data[index] != ':' {
			return nil, 0, errors.New("configuration object member lacks a colon")
		}
		valueStart := skipJSONSpace(data, index+1)
		valueEnd, err := scanJSONValue(data, valueStart)
		if err != nil {
			return nil, 0, err
		}
		members = append(members, jsonMemberLocation{key: key, keyStart: keyStart, valueStart: valueStart, valueEnd: valueEnd})
		index = skipJSONSpace(data, valueEnd)
		if index < len(data) && data[index] == ',' {
			index++
			continue
		}
		if index >= len(data) || data[index] != '}' {
			return nil, 0, errors.New("configuration object member lacks a delimiter")
		}
	}
}

func scanJSONValue(data []byte, start int) (int, error) {
	if start >= len(data) {
		return 0, errors.New("configuration object member lacks a value")
	}
	if data[start] == '"' {
		return scanJSONString(data, start)
	}
	if data[start] != '{' && data[start] != '[' {
		index := start
		for index < len(data) && data[index] != ',' && data[index] != '}' && !unicode.IsSpace(rune(data[index])) {
			index++
		}
		if index == start {
			return 0, errors.New("configuration object member lacks a value")
		}
		return index, nil
	}
	stack := []byte{data[start]}
	for index := start + 1; index < len(data); index++ {
		switch data[index] {
		case '"':
			end, err := scanJSONString(data, index)
			if err != nil {
				return 0, err
			}
			index = end - 1
		case '{', '[':
			stack = append(stack, data[index])
		case '}', ']':
			expected := byte('{')
			if data[index] == ']' {
				expected = '['
			}
			if len(stack) == 0 || stack[len(stack)-1] != expected {
				return 0, errors.New("configuration contains mismatched JSON delimiters")
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return index + 1, nil
			}
		}
	}
	return 0, errors.New("configuration contains an incomplete JSON value")
}

func scanJSONString(data []byte, start int) (int, error) {
	if start >= len(data) || data[start] != '"' {
		return 0, errors.New("configuration object key must be a JSON string")
	}
	escaped := false
	for index := start + 1; index < len(data); index++ {
		if escaped {
			escaped = false
			continue
		}
		if data[index] == '\\' {
			escaped = true
			continue
		}
		if data[index] == '"' {
			return index + 1, nil
		}
	}
	return 0, errors.New("configuration contains an incomplete JSON string")
}

func skipJSONSpace(data []byte, index int) int {
	for index < len(data) && (data[index] == ' ' || data[index] == '\t' || data[index] == '\r' || data[index] == '\n') {
		index++
	}
	return index
}

func lineIndent(data []byte, offset int) []byte {
	lineStart := bytes.LastIndexByte(data[:offset], '\n') + 1
	indent := data[lineStart:offset]
	for _, value := range indent {
		if value != ' ' && value != '\t' {
			return nil
		}
	}
	return append([]byte{}, indent...)
}

func validatePackMigrationConfig(data []byte, source string) (policy.Config, error) {
	config, err := policy.Parse(data, source)
	if err != nil {
		return policy.Config{}, fmt.Errorf("validate migrated configuration: %w", err)
	}
	return config, nil
}
