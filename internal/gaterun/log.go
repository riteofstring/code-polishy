package gaterun

import (
	"bytes"
	"fmt"
	"sync"
)

type boundedBuffer struct {
	mutex     sync.Mutex
	limit     int
	buffer    bytes.Buffer
	truncated bool
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining <= 0 {
		buffer.truncated = buffer.truncated || len(data) > 0
		return len(data), nil
	}
	if len(data) > remaining {
		buffer.buffer.Write(data[:remaining])
		buffer.truncated = true
		return len(data), nil
	}
	buffer.buffer.Write(data)
	return len(data), nil
}

func (buffer *boundedBuffer) snapshot() ([]byte, bool) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return append([]byte{}, buffer.buffer.Bytes()...), buffer.truncated
}

type commandLogDocument struct {
	Version         int    `json:"version"`
	StreamLimit     int    `json:"stream_limit"`
	Stdout          []byte `json:"stdout"`
	Stderr          []byte `json:"stderr"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
}

func (run *Run) OpenCommandLog(index int, options LogOptions) (*CommandLog, error) {
	if run == nil || run.finalized {
		return nil, fmt.Errorf("%w: gate run is unavailable", ErrInvalidInput)
	}
	if run.openLogs[index] {
		return nil, fmt.Errorf("%w: command %d already has an open log", ErrInvalidInput, index)
	}
	entry, err := run.commandOutcome(index)
	if err != nil {
		return nil, err
	}
	if entry.Reused {
		return nil, fmt.Errorf("%w: reused command %d cannot open a log", ErrInvalidInput, index)
	}
	limit, err := streamLimit(options)
	if err != nil {
		return nil, err
	}
	reference, err := run.identity.Command(index)
	if err != nil {
		return nil, err
	}
	attempt := len(entry.Attempts) + 1
	path, display, err := run.logPath(reference.SHA256, attempt, true)
	if err != nil {
		return nil, err
	}
	run.openLogs[index] = true
	log := &CommandLog{
		stdout: &boundedBuffer{limit: limit}, stderr: &boundedBuffer{limit: limit},
	}
	log.close = func(closed *CommandLog) (LogResult, error) {
		defer delete(run.openLogs, index)
		return writeCommandLog(path, display, limit, closed)
	}
	return log, nil
}

func streamLimit(options LogOptions) (int, error) {
	if options.StreamLimit == 0 {
		return DefaultStreamLimit, nil
	}
	if options.StreamLimit < 1 || options.StreamLimit > MaximumStreamLimit {
		return 0, fmt.Errorf("%w: stream limit must be between 1 and %d", ErrInvalidInput, MaximumStreamLimit)
	}
	return options.StreamLimit, nil
}

func writeCommandLog(path, display string, limit int, log *CommandLog) (LogResult, error) {
	stdout, stdoutTruncated := log.stdout.snapshot()
	stderr, stderrTruncated := log.stderr.snapshot()
	document := commandLogDocument{
		Version: Version, StreamLimit: limit, Stdout: stdout, Stderr: stderr,
		StdoutTruncated: stdoutTruncated, StderrTruncated: stderrTruncated,
	}
	data, err := marshalArtifact(document, "encode gate command log")
	if err != nil {
		return LogResult{}, err
	}
	if err := writeArtifactAtomic(path, data); err != nil {
		return LogResult{}, err
	}
	return LogResult{
		Path: display, SHA256: ContentSHA256(data), Stdout: stdout, Stderr: stderr,
		StdoutTruncated: stdoutTruncated, StderrTruncated: stderrTruncated, StreamLimit: limit,
	}, nil
}

func (run *Run) logPath(commandSHA256 string, attempt int, create bool) (string, string, error) {
	if !validSHA256(commandSHA256) || attempt < 1 {
		return "", "", fmt.Errorf("%w: command log identity is invalid", ErrInvalidInput)
	}
	directory, err := secureRunSubdirectory(run.repositoryRoot, run.directory, logsDirectory, create)
	if err != nil {
		return "", "", err
	}
	return artifactFilePath(run.repositoryRoot, directory, commandSHA256+fmt.Sprintf("-%d.json", attempt))
}

func readCommandLog(path string) (commandLogDocument, []byte, error) {
	data, err := readArtifact(path, maximumCommandLogBytes(), "gate command log")
	if err != nil {
		return commandLogDocument{}, nil, err
	}
	var document commandLogDocument
	if err := decodeStrict(data, &document, "gate command log"); err != nil {
		return commandLogDocument{}, nil, err
	}
	if document.Version != Version || document.StreamLimit < 1 || document.StreamLimit > MaximumStreamLimit ||
		len(document.Stdout) > document.StreamLimit || len(document.Stderr) > document.StreamLimit {
		return commandLogDocument{}, nil, fmt.Errorf("%w: gate command log is malformed", ErrInvalidArtifact)
	}
	return document, data, nil
}

func maximumCommandLogBytes() int {
	return encodedBytesMaximum(MaximumStreamLimit)*2 + maximumLogHeaderBytes
}

func encodedBytesMaximum(size int) int {
	return (size + 2) / 3 * 4
}
