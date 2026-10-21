package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

var ErrTrailingData = errors.New("JSON contains more than one value")

func Decode(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrTrailingData
	}
	return nil
}
