package workflow

import (
	"errors"
	"fmt"
	"os"
)

func Read(path string, read func(string) ([]byte, error)) (Facts, error) {
	data, err := read(path)
	if err != nil {
		return Facts{}, err
	}
	configuration, err := readConfiguration(read)
	if err != nil {
		return Facts{}, err
	}
	return Parse(path, data, configuration)
}

func readConfiguration(read func(string) ([]byte, error)) ([]byte, error) {
	var configuration []byte
	var selected string
	for _, path := range []string{".github/actionlint.yaml", ".github/actionlint.yml"} {
		data, err := read(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		if selected != "" {
			return nil, fmt.Errorf("ambiguous workflow configuration: both %s and %s exist", selected, path)
		}
		selected, configuration = path, data
	}
	return configuration, nil
}
