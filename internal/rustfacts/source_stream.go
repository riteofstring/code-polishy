package rustfacts

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/riteofstring/code-polishy/internal/repository"
)

type sourceStream struct {
	repo    repository.Repository
	inputs  []SourceInput
	sources []Source
	pending []byte
	err     error
}

func (stream *sourceStream) Write(data []byte) (int, error) {
	if stream.err != nil {
		return 0, stream.err
	}
	written := len(data)
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		terminated := end >= 0
		if !terminated {
			end = len(data)
		}
		if len(stream.pending)+end >= maximumSourceRequest {
			stream.err = errors.New("rust syntax source record exceeds 8388608 bytes")
			return 0, stream.err
		}
		stream.pending = append(stream.pending, data[:end]...)
		if !terminated {
			break
		}
		if stream.err = stream.consume(); stream.err != nil {
			return 0, stream.err
		}
		stream.pending = stream.pending[:0]
		data = data[end+1:]
	}
	return written, nil
}

func (stream *sourceStream) consume() error {
	index := len(stream.sources)
	if index >= len(stream.inputs) {
		return errors.New("rust syntax stream contains an unrequested source record")
	}
	sources, err := ParseSources(stream.repo, stream.inputs[index:index+1], stream.pending)
	if err != nil {
		return err
	}
	stream.sources = append(stream.sources, sources[0])
	return nil
}

func (stream *sourceStream) finish() ([]Source, error) {
	if stream.err != nil {
		return nil, stream.err
	}
	if len(stream.pending) != 0 || len(stream.sources) != len(stream.inputs) {
		return nil, errors.New("rust syntax stream ended before its complete source inventory")
	}
	seen := map[string]bool{}
	for _, source := range stream.sources {
		if seen[source.Path] {
			return nil, fmt.Errorf("rust syntax stream repeats source %s", source.Path)
		}
		seen[source.Path] = true
	}
	return stream.sources, nil
}

type sourceDiagnostics struct {
	bytes.Buffer
}

func (diagnostics *sourceDiagnostics) Write(data []byte) (int, error) {
	remaining := (64 << 10) - diagnostics.Len()
	if len(data) > remaining {
		_, _ = diagnostics.Buffer.Write(data[:remaining])
		return remaining, errors.New("rust syntax diagnostics exceed 65536 bytes")
	}
	return diagnostics.Buffer.Write(data)
}
