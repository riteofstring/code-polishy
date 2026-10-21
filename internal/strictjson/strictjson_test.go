package strictjson

import (
	"errors"
	"testing"
)

type document struct {
	Name string `json:"name"`
}

func TestDecodeAcceptsExactlyOneKnownDocument(t *testing.T) {
	t.Parallel()
	var decoded document
	if err := Decode([]byte(" {\"name\":\"exact\"}\n"), &decoded); err != nil || decoded.Name != "exact" {
		t.Fatalf("decoded = %+v, err = %v", decoded, err)
	}
}

func TestDecodeRejectsUnknownFieldsAndTrailingValues(t *testing.T) {
	t.Parallel()
	for name, input := range map[string]string{
		"unknown field":  `{"name":"x","trusted":true}`,
		"trailing value": `{"name":"x"} {"name":"y"}`,
		"trailing token": `{"name":"x"}]`,
		"truncated":      `{"name":`,
	} {
		t.Run(name, func(t *testing.T) {
			var decoded document
			if err := Decode([]byte(input), &decoded); err == nil {
				t.Fatalf("accepted %s", input)
			}
		})
	}
	var decoded document
	if err := Decode([]byte(`{"name":"x"} 1`), &decoded); !errors.Is(err, ErrTrailingData) {
		t.Fatalf("trailing value error = %v", err)
	}
}
