package supplychain

import (
	"fmt"
	"unicode/utf8"
)

const (
	maximumDependencyInputBytes = 8 << 20
	maximumDependencyInputs     = 4096
	maximumDependencyTotalBytes = 128 << 20
)

type dependencyInputReader struct {
	read  func(string) ([]byte, error)
	cache map[string][]byte
	bytes int
}

func boundedInventorySource(source inventorySource) inventorySource {
	reader := &dependencyInputReader{read: source.read, cache: map[string][]byte{}}
	return inventorySource{files: source.files, read: reader.Read}
}

func (reader *dependencyInputReader) Read(path string) ([]byte, error) {
	if data, found := reader.cache[path]; found {
		return data, nil
	}
	if len(reader.cache) >= maximumDependencyInputs {
		return nil, fmt.Errorf("dependency inventory exceeds the input count limit")
	}
	data, err := reader.read(path)
	if err != nil {
		return nil, err
	}
	if len(data) > maximumDependencyInputBytes || !utf8.Valid(data) {
		return nil, fmt.Errorf("parse %s: dependency input exceeds the UTF-8 byte limit or contains invalid text", path)
	}
	if len(data) > maximumDependencyTotalBytes-reader.bytes {
		return nil, fmt.Errorf("dependency inventory exceeds the aggregate input byte limit")
	}
	reader.bytes += len(data)
	reader.cache[path] = data
	return data, nil
}
