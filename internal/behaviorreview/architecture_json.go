package behaviorreview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

type architectureJSONField struct {
	typeOf   reflect.Type
	optional bool
}

func validateArchitectureJSONShape(data []byte, typeOf reflect.Type) error {
	if typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("null is not a structured architecture evidence value")
	}
	switch typeOf.Kind() {
	case reflect.Struct:
		return validateArchitectureJSONObject(data, typeOf)
	case reflect.Slice:
		var values []json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		for _, value := range values {
			if err := validateArchitectureJSONShape(value, typeOf.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateArchitectureJSONObject(data []byte, typeOf reflect.Type) error {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	fields := architectureJSONFields(typeOf)
	for name, value := range values {
		field, exists := fields[name]
		if !exists {
			return fmt.Errorf("unknown JSON field %q", name)
		}
		if err := validateArchitectureJSONShape(value, field.typeOf); err != nil {
			return fmt.Errorf("field %s: %w", name, err)
		}
	}
	for name, field := range fields {
		if _, exists := values[name]; !exists && !field.optional {
			return fmt.Errorf("missing JSON field %q", name)
		}
	}
	return nil
}

func architectureJSONFields(typeOf reflect.Type) map[string]architectureJSONField {
	fields := map[string]architectureJSONField{}
	for index := 0; index < typeOf.NumField(); index++ {
		field := typeOf.Field(index)
		if field.Anonymous {
			for name, nested := range architectureJSONFields(field.Type) {
				fields[name] = nested
			}
			continue
		}
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" {
			name = field.Name
		}
		if name != "-" {
			fields[name] = architectureJSONField{typeOf: field.Type, optional: strings.Contains(options, "omitempty")}
		}
	}
	return fields
}
