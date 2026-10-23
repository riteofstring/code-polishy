package engine

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/supplychain"
)

type securityScanRunner struct {
	delegate runner.Runner
	repo     repository.Repository
	commands map[string]policy.Command
	now      func() time.Time
	scans    []policy.SecurityScan
	tools    map[string]string
}

type securityScanRecord struct {
	InputSHA256     string        `json:"inputSha256"`
	Succeeded       bool          `json:"succeeded"`
	ScannedAt       time.Time     `json:"scannedAt"`
	LastSucceededAt time.Time     `json:"lastSucceededAt"`
	Result          runner.Result `json:"result"`
	Stdout          string        `json:"stdout"`
	Stderr          string        `json:"stderr"`
}

func (engine *Engine) securityNow() time.Time {
	if engine.securityClock != nil {
		return engine.securityClock().UTC()
	}
	return time.Now().UTC()
}

func (scanRunner *securityScanRunner) verification() *policy.SecurityVerification {
	now := scanRunner.now()
	status := &policy.SecurityVerification{Status: "current", CheckedAt: now, RefreshAfter: now.Add(policy.SecurityEvidenceLifetime), Scans: append([]policy.SecurityScan{}, scanRunner.scans...)}
	for _, scan := range status.Scans {
		if scan.Status == "unavailable" {
			status.Status = "unavailable"
		}
		if scan.RefreshAfter.Before(status.RefreshAfter) {
			status.RefreshAfter = scan.RefreshAfter
		}
	}
	if !now.Before(status.RefreshAfter) {
		status.Status = "unavailable"
	}
	return status
}

func securityEvidenceFinding(err error) policy.Finding {
	return policy.Finding{Check: "policy.securityEvidence", VerificationPhase: policy.VerificationSecurity, Path: "repository", Message: "current security evidence is unavailable: " + err.Error()}
}

func newSecurityScanRunner(engine *Engine, commands []policy.Command) *securityScanRunner {
	selected := map[string]policy.Command{}
	for _, command := range commands {
		if command.ReportProtocol == policy.OSVVulnerabilityReportProtocol || strings.HasPrefix(command.Name, "pnpm-audit-") {
			selected[command.Name] = command
		}
	}
	return &securityScanRunner{delegate: engine.Runner, repo: engine.Repository, commands: selected, now: engine.securityNow, tools: map[string]string{}}
}

func (scanRunner *securityScanRunner) Execute(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	expected, selected := scanRunner.commands[invocation.Command.Name]
	if !selected || !samePolicyCommand(expected, invocation.Command) {
		return scanRunner.delegate.Execute(ctx, invocation)
	}
	files := supplychain.SecurityScanInputs(invocation.Command)
	if len(files) == 0 && invocation.Snapshot == nil {
		return scanRunner.scanUncached(ctx, invocation)
	}
	tool, err := scanRunner.toolIdentity(invocation.Command.Argv[0])
	if err != nil {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
	}
	if tool == "" {
		return scanRunner.scanUncached(ctx, invocation)
	}
	invocation, digest, err := scanRunner.captureInput(invocation)
	if err != nil {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
	}
	if digest == "" {
		return scanRunner.scanUncached(ctx, invocation)
	}
	return scanRunner.cached(ctx, invocation, digest, tool)
}

func (scanRunner *securityScanRunner) captureInput(invocation runner.Invocation) (runner.Invocation, string, error) {
	var err error
	if invocation.Snapshot == nil {
		invocation.Snapshot, err = supplychain.CaptureAuditSnapshot(scanRunner.repo, invocation.Command)
		if err != nil {
			return invocation, "", err
		}
	}
	invocation.Command, err = runner.SnapshotEnvironment(invocation.Command)
	if err != nil {
		return invocation, "", err
	}
	digest, err := invocation.Snapshot.Identity(invocation.Command)
	return invocation, digest, err
}

func (scanRunner *securityScanRunner) scanUncached(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	path := ".code-polishy-reports/security-scans/uncached/" + rand.Text() + "/scan.json"
	return scanRunner.scan(ctx, invocation, "", path, securityScanRecord{})
}

func (scanRunner *securityScanRunner) cached(ctx context.Context, invocation runner.Invocation, digest, tool string) (runner.Result, runner.Output, error) {
	key, err := scanRunner.identity(invocation.Command, digest, tool)
	if err != nil {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
	}
	path := ".code-polishy-reports/security-scans/" + key + "/scan.json"
	var previous securityScanRecord
	present, err := readSecurityEvidence(scanRunner.repo, path, &previous)
	if err != nil {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
	}
	now := scanRunner.now()
	if present && previous.InputSHA256 != key {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, errors.New("security scan evidence does not match its inputs")
	}
	if reusableSecurityScan(ctx, invocation, previous, now) {
		output := runner.Output{Stdout: []byte(previous.Stdout), Stderr: []byte(previous.Stderr)}
		err := replaySecurityOutput(invocation, output)
		if err == nil && previous.Result.ExitStatus != 0 {
			err = fmt.Errorf("reused scanner report has exit status %d", previous.Result.ExitStatus)
		}
		scanRunner.record(invocation.Command, path, previous, "reused")
		result := previous.Result
		result.ExecutionDuration, result.ResourceWait = 0, 0
		return result, output, err
	}
	return scanRunner.scan(ctx, invocation, key, path, previous)
}

func reusableSecurityScan(ctx context.Context, invocation runner.Invocation, record securityScanRecord, now time.Time) bool {
	if !record.Succeeded || record.ScannedAt.IsZero() || !record.LastSucceededAt.Equal(record.ScannedAt) || record.ScannedAt.After(now) || !now.Before(record.ScannedAt.Add(policy.SecurityEvidenceLifetime)) || ctx.Err() != nil {
		return false
	}
	if record.Result.FailureCategory != "" && record.Result.FailureCategory != runner.FailureCommandExit {
		return false
	}
	var runErr error
	if record.Result.ExitStatus != 0 {
		runErr = fmt.Errorf("scanner report has exit status %d", record.Result.ExitStatus)
	}
	if invocation.Accept != nil {
		return invocation.Accept(record.Result, runner.Output{Stdout: []byte(record.Stdout), Stderr: []byte(record.Stderr)}, runErr)
	}
	return runErr == nil
}

func (scanRunner *securityScanRunner) scan(ctx context.Context, invocation runner.Invocation, key, path string, previous securityScanRecord) (runner.Result, runner.Output, error) {
	actual := invocation
	actual.Capture = runner.CaptureOutput
	if actual.Stdout == nil && actual.Stderr == nil {
		actual.Stdout, actual.Stderr = io.Discard, io.Discard
	}
	result, output, runErr := runner.ExecuteSnapshot(ctx, scanRunner.delegate, actual)
	succeeded := runErr == nil
	if invocation.Accept != nil {
		succeeded = invocation.Accept(result, output, runErr)
	}
	succeeded = succeeded && ctx.Err() == nil && (result.FailureCategory == "" || result.FailureCategory == runner.FailureCommandExit)
	record := securityScanRecord{InputSHA256: key, Succeeded: succeeded, ScannedAt: scanRunner.now(), LastSucceededAt: previous.LastSucceededAt, Result: result, Stdout: string(output.Stdout), Stderr: string(output.Stderr)}
	status := "unavailable"
	if succeeded {
		record.LastSucceededAt = record.ScannedAt
		status = "scanned"
	}
	if err := writeSecurityEvidence(scanRunner.repo, path, record); err != nil {
		result.FailureCategory = runner.FailureOperational
		return result, output, errors.Join(runErr, err)
	}
	scanRunner.record(invocation.Command, path, record, status)
	return result, output, runErr
}

func replaySecurityOutput(invocation runner.Invocation, output runner.Output) error {
	for _, stream := range []struct {
		writer io.Writer
		data   []byte
	}{{invocation.Stdout, output.Stdout}, {invocation.Stderr, output.Stderr}} {
		if stream.writer != nil {
			if _, err := stream.writer.Write(stream.data); err != nil {
				return err
			}
		}
	}
	return nil
}

func (scanRunner *securityScanRunner) record(command policy.Command, path string, record securityScanRecord, status string) {
	tool := strings.Split(command.Name, "-")[0]
	if command.ReportProtocol == policy.OSVVulnerabilityReportProtocol {
		tool = "osv-scanner"
	}
	version, _ := os.ReadFile(filepath.Join(scanRunner.repo.PolicyRoot, "tools", tool+"-version.txt"))
	var refreshAfter time.Time
	if !record.LastSucceededAt.IsZero() {
		refreshAfter = record.LastSucceededAt.Add(policy.SecurityEvidenceLifetime)
	}
	scanRunner.scans = append(scanRunner.scans, policy.SecurityScan{Name: command.Name, Status: status, ScannerVersion: strings.TrimSpace(string(version)), LastSucceededAt: record.LastSucceededAt, RefreshAfter: refreshAfter, ReportPath: path})
}

func (scanRunner *securityScanRunner) identity(command policy.Command, digest, tool string) (string, error) {
	payload, err := json.Marshal(struct {
		Protocol, Tool, Input string
		Command               policy.Command
		Overrides             []string
	}{"security-scan/v5", tool, digest, command, command.EnvironmentOverrides})
	if err != nil {
		return "", err
	}
	return gaterun.ContentSHA256(payload), nil
}

func (scanRunner *securityScanRunner) toolIdentity(executable string) (string, error) {
	if identity, ok := scanRunner.tools[executable]; ok {
		return identity, nil
	}
	manifest, present, err := release.ReadManifest(scanRunner.repo.PolicyRoot)
	if err != nil {
		return "", err
	}
	identity := ""
	if present {
		identity = "release:" + manifest.ReleaseDigest
	}
	scanRunner.tools[executable] = identity
	return identity, nil
}
