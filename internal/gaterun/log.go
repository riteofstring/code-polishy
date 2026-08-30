package gaterun

import (
	"fmt"
	"sync"
)

type boundedBuffer struct {
	mutex     sync.Mutex
	headLimit int
	tailLimit int
	head      []byte
	tail      []byte
	total     int64
}

type capturedStream struct {
	head      []byte
	tail      []byte
	total     int64
	truncated bool
}

func newBoundedBuffer(limit int) *boundedBuffer {
	headLimit := limit / 2
	return &boundedBuffer{headLimit: headLimit, tailLimit: limit - headLimit, head: []byte{}, tail: []byte{}}
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	buffer.total += int64(len(data))
	buffer.appendTail(data[buffer.appendHead(data):])
	return len(data), nil
}

func (buffer *boundedBuffer) appendHead(data []byte) int {
	remaining := buffer.headLimit - len(buffer.head)
	if remaining <= 0 {
		return 0
	}
	if len(data) > remaining {
		data = data[:remaining]
	}
	buffer.head = append(buffer.head, data...)
	return len(data)
}

func (buffer *boundedBuffer) appendTail(data []byte) {
	if buffer.tailLimit == 0 || len(data) == 0 {
		return
	}
	if len(data) >= buffer.tailLimit {
		buffer.tail = append(buffer.tail[:0], data[len(data)-buffer.tailLimit:]...)
		return
	}
	overflow := len(buffer.tail) + len(data) - buffer.tailLimit
	if overflow > 0 {
		buffer.tail = append(buffer.tail[:0], buffer.tail[overflow:]...)
	}
	buffer.tail = append(buffer.tail, data...)
}

func (buffer *boundedBuffer) snapshot() capturedStream {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	head := append([]byte{}, buffer.head...)
	tail := append([]byte{}, buffer.tail...)
	return capturedStream{head: head, tail: tail, total: buffer.total, truncated: buffer.total > int64(buffer.headLimit+buffer.tailLimit)}
}

func (stream capturedStream) rendered() []byte {
	if !stream.truncated {
		return append(append([]byte{}, stream.head...), stream.tail...)
	}
	omitted := stream.total - int64(len(stream.head)+len(stream.tail))
	marker := []byte(fmt.Sprintf("\n[... %d bytes omitted; terminal output follows ...]\n", omitted))
	output := append([]byte{}, stream.head...)
	output = append(output, marker...)
	return append(output, stream.tail...)
}

type commandLogDocument struct {
	Version         int    `json:"version"`
	StreamLimit     int    `json:"stream_limit"`
	Stdout          []byte `json:"stdout"`
	Stderr          []byte `json:"stderr"`
	StdoutTail      []byte `json:"stdout_tail"`
	StderrTail      []byte `json:"stderr_tail"`
	StdoutBytes     int64  `json:"stdout_bytes"`
	StderrBytes     int64  `json:"stderr_bytes"`
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
	file, display, err := run.logPath(reference.SHA256, attempt, true)
	if err != nil {
		return nil, err
	}
	run.openLogs[index] = true
	log := &CommandLog{stdout: newBoundedBuffer(limit), stderr: newBoundedBuffer(limit)}
	log.close = func(closed *CommandLog) (LogResult, error) {
		defer delete(run.openLogs, index)
		return writeCommandLog(file, display, limit, closed)
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

func writeCommandLog(file artifactFile, display string, limit int, log *CommandLog) (LogResult, error) {
	stdout := log.stdout.snapshot()
	stderr := log.stderr.snapshot()
	document := commandLogDocument{
		Version: Version, StreamLimit: limit, Stdout: stdout.head, Stderr: stderr.head, StdoutTail: stdout.tail, StderrTail: stderr.tail,
		StdoutBytes: stdout.total, StderrBytes: stderr.total, StdoutTruncated: stdout.truncated, StderrTruncated: stderr.truncated,
	}
	data, err := marshalArtifact(document, "encode gate command log")
	if err != nil {
		return LogResult{}, err
	}
	if err := writeArtifactAtomic(file, data); err != nil {
		return LogResult{}, err
	}
	return LogResult{
		Path: display, SHA256: ContentSHA256(data), Stdout: stdout.rendered(), Stderr: stderr.rendered(),
		StdoutTail: stdout.tail, StderrTail: stderr.tail, StdoutBytes: stdout.total, StderrBytes: stderr.total,
		StdoutTruncated: stdout.truncated, StderrTruncated: stderr.truncated, StreamLimit: limit,
	}, nil
}

func (run *Run) logPath(commandSHA256 string, attempt int, create bool) (artifactFile, string, error) {
	if !validSHA256(commandSHA256) || attempt < 1 {
		return artifactFile{}, "", fmt.Errorf("%w: command log identity is invalid", ErrInvalidInput)
	}
	directory, err := secureRunSubdirectory(run.directory, logsDirectory, create)
	if err != nil {
		return artifactFile{}, "", err
	}
	return artifactFilePath(directory, commandSHA256+fmt.Sprintf("-%d.json", attempt))
}

func readCommandLog(file artifactFile) (commandLogDocument, []byte, error) {
	data, err := readArtifact(file, maximumCommandLogBytes(), "gate command log")
	if err != nil {
		return commandLogDocument{}, nil, err
	}
	var document commandLogDocument
	if err := decodeStrict(data, &document, "gate command log"); err != nil {
		return commandLogDocument{}, nil, err
	}
	if !validCommandLogDocument(document) {
		return commandLogDocument{}, nil, fmt.Errorf("%w: gate command log is malformed", ErrInvalidArtifact)
	}
	return document, data, nil
}

func validCommandLogDocument(document commandLogDocument) bool {
	if document.Version != Version || document.StreamLimit < 1 || document.StreamLimit > MaximumStreamLimit {
		return false
	}
	return validCapturedStream(document.Stdout, document.StdoutTail, document.StdoutBytes, document.StdoutTruncated, document.StreamLimit) &&
		validCapturedStream(document.Stderr, document.StderrTail, document.StderrBytes, document.StderrTruncated, document.StreamLimit)
}

func validCapturedStream(head, tail []byte, total int64, truncated bool, limit int) bool {
	headLimit := limit / 2
	if len(head) > headLimit || len(tail) > limit-headLimit || total < int64(len(head)+len(tail)) {
		return false
	}
	if truncated {
		return total > int64(limit)
	}
	return total == int64(len(head)+len(tail))
}

func maximumCommandLogBytes() int {
	return encodedBytesMaximum(MaximumStreamLimit)*2 + maximumLogHeaderBytes + 16
}

func encodedBytesMaximum(size int) int {
	return (size + 2) / 3 * 4
}
