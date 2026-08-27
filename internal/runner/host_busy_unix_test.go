//go:build unix

package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHostRuntimeRetriesTransientBusyExecutable(t *testing.T) {
	helper := filepath.Join(t.TempDir(), "busy-helper")
	openHelper, err := os.OpenFile(helper, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = openHelper.Close() })
	if _, err := openHelper.WriteString("#!/bin/sh\nexit 0\n"); err != nil {
		t.Fatal(err)
	}
	if err := openHelper.Sync(); err != nil {
		t.Fatal(err)
	}

	closed := make(chan error, 1)
	go func() {
		time.Sleep(75 * time.Millisecond)
		closed <- openHelper.Close()
	}()
	result, runErr := Run(context.Background(), HostCommand{
		Path: helper, Argv: []string{helper}, Environment: os.Environ(),
	})
	if closeErr := <-closed; closeErr != nil {
		t.Fatal(closeErr)
	}
	if runErr != nil || !result.Started || !result.Quiescent || result.ExitStatus != 0 {
		t.Fatalf("result=%+v error=%v", result, runErr)
	}
}
