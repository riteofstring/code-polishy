package supplychain

import (
	"context"
	"errors"
	"testing"

	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestOSVReportExitAcceptanceFailsClosed(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	scan := osvScan{Root: "."}
	valid := runner.Output{Stdout: []byte(`{
  "results":[{
    "source":{"path":"pnpm-lock.yaml","type":"lockfile"},
    "packages":[{
      "package":{"name":"example","version":"1.2.3","ecosystem":"npm"},
      "groups":[{"ids":["GHSA-abcd-1234-5678"],"max_severity":"high"}]
    }]
  }]
}`)}
	invalid := runner.Output{Stdout: []byte(`{
  "results":[{
    "source":{"path":"pnpm-lock.yaml"},
    "packages":[{"package":{"name":"example"},"vulnerabilities":[{"id":"CVE-2026-1000"}]}]
  }]
}`)}
	failure := errors.New("scanner reported vulnerabilities")
	tests := map[string]struct {
		result runner.Result
		output runner.Output
		err    error
		want   bool
	}{
		"vulnerability report":  {result: runner.Result{ExitStatus: 1, FailureCategory: runner.FailureCommandExit}, output: valid, err: failure, want: true},
		"empty findings":        {result: runner.Result{ExitStatus: 1, FailureCategory: runner.FailureCommandExit}, output: runner.Output{Stdout: []byte(`{"results":[]}`)}, err: failure},
		"malformed output":      {result: runner.Result{ExitStatus: 1, FailureCategory: runner.FailureCommandExit}, output: runner.Output{Stdout: []byte(`{"results":`)}, err: failure},
		"invalid vulnerability": {result: runner.Result{ExitStatus: 1, FailureCategory: runner.FailureCommandExit}, output: invalid, err: failure},
		"operational exit":      {result: runner.Result{ExitStatus: 1, FailureCategory: runner.FailureOperational}, output: valid, err: failure},
		"scanner exit two":      {result: runner.Result{ExitStatus: 2, FailureCategory: runner.FailureCommandExit}, output: valid, err: failure},
		"timeout":               {result: runner.Result{ExitStatus: 124, FailureCategory: runner.FailureTimeout}, output: valid, err: context.DeadlineExceeded},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := acceptedOSVReportExit(t.Context(), repo, scan, test.result, test.output, test.err); got != test.want {
				t.Fatalf("acceptedOSVReportExit() = %v, want %v", got, test.want)
			}
		})
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if acceptedOSVReportExit(canceled, repo, scan, tests["vulnerability report"].result, valid, failure) {
		t.Fatal("canceled vulnerability scan was accepted")
	}
}
