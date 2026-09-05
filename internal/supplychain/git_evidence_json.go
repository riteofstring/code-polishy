package supplychain

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

func validateGitEvidenceJSONKeys(data []byte, destination any) error {
	keys := map[string]bool{}
	gitEvidenceFieldNames(reflect.TypeOf(destination), keys)
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := walkGitEvidenceJSON(decoder, keys, 0); err != nil {
		return gitEvidenceFailure("invalid", "Git evidence contains duplicate, unknown, or malformed JSON fields")
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return gitEvidenceFailure("invalid", "Git evidence contains malformed JSON")
	}
	return requireGitEvidenceFields(document, reflect.TypeOf(destination))
}

func requireGitEvidenceFields(document any, value reflect.Type) error {
	switch value.Kind() {
	case reflect.Pointer:
		return requireGitEvidenceFields(document, value.Elem())
	case reflect.Struct:
		if value == reflect.TypeFor[time.Time]() {
			return nil
		}
		return requireGitEvidenceObject(document, value)
	case reflect.Slice:
		array, valid := document.([]any)
		if !valid {
			return gitEvidenceFailure("invalid", "Git evidence requires explicit arrays")
		}
		for _, item := range array {
			if err := requireGitEvidenceFields(item, value.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func requireGitEvidenceObject(document any, value reflect.Type) error {
	object, valid := document.(map[string]any)
	if !valid || len(object) != value.NumField() {
		return gitEvidenceFailure("invalid", "Git evidence requires every field in the versioned document contract")
	}
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		item, exists := object[name]
		if !exists {
			return gitEvidenceFailure("invalid", "Git evidence omitted a required field")
		}
		if err := requireGitEvidenceFields(item, field.Type); err != nil {
			return err
		}
	}
	return nil
}

func gitEvidenceFieldNames(value reflect.Type, keys map[string]bool) {
	switch value.Kind() {
	case reflect.Pointer, reflect.Slice:
		gitEvidenceFieldNames(value.Elem(), keys)
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			field := value.Field(index)
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "" || name == "-" {
				continue
			}
			keys[name] = true
			gitEvidenceFieldNames(field.Type, keys)
		}
	}
}

func walkGitEvidenceJSON(decoder *json.Decoder, keys map[string]bool, depth int) error {
	if depth > 16 {
		return gitEvidenceFailure("invalid", "Git evidence exceeds its nesting limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return gitEvidenceFailure("invalid", "Git evidence contains an invalid delimiter")
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			if err := gitEvidenceObjectKey(decoder, keys, seen); err != nil {
				return err
			}
		}
		if err := walkGitEvidenceJSON(decoder, keys, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func gitEvidenceObjectKey(decoder *json.Decoder, allowed, seen map[string]bool) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	key, valid := token.(string)
	if !valid || !allowed[key] || seen[key] {
		return gitEvidenceFailure("invalid", "Git evidence has a repeated or unsupported field")
	}
	seen[key] = true
	return nil
}
