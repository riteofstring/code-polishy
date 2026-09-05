package repository

import (
	"bytes"
	"fmt"
	"os/exec"
)

func (repo Repository) ReadRegularFileAtLimit(revision, path string, limit int) ([]byte, bool, error) {
	if limit <= 0 {
		return nil, false, fmt.Errorf("committed file byte limit must be positive")
	}
	normalized, present, err := repo.regularFileAtPath(revision, path)
	if err != nil || !present {
		return nil, present, err
	}
	output := committedFileBuffer{limit: limit}
	command := exec.Command("git", "-C", repo.Root, "cat-file", "blob", revision+":"+normalized)
	command.Stdout = &output
	if err := command.Run(); err != nil {
		return nil, true, fmt.Errorf("read %s at %s within %d bytes: %w", normalized, revision, limit, err)
	}
	return output.buffer.Bytes(), true, nil
}

type committedFileBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (output *committedFileBuffer) Write(data []byte) (int, error) {
	if len(data) > output.limit-output.buffer.Len() {
		return 0, fmt.Errorf("committed file exceeds %d bytes", output.limit)
	}
	return output.buffer.Write(data)
}
