package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func ValidateUniqueJSON(data []byte, maximumDepth int) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := walkUniqueJSON(decoder, 0, maximumDepth); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON content")
	}
	return nil
}

func walkUniqueJSON(decoder *json.Decoder, depth, maximumDepth int) error {
	if depth > maximumDepth {
		return fmt.Errorf("JSON nesting exceeds %d levels", maximumDepth)
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
		return fmt.Errorf("invalid JSON delimiter")
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			if err := uniqueJSONKey(decoder, seen); err != nil {
				return err
			}
		}
		if err := walkUniqueJSON(decoder, depth+1, maximumDepth); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func uniqueJSONKey(decoder *json.Decoder, seen map[string]bool) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	key, valid := token.(string)
	if !valid || seen[key] {
		return fmt.Errorf("duplicate or invalid JSON object key")
	}
	seen[key] = true
	return nil
}
