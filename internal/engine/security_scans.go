package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	inputs   map[string]string
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
	return policy.Finding{Check: "policy.securityEvidence", Path: "repository", Message: "current security evidence is unavailable: " + err.Error()}
}

func newSecurityScanRunner(engine *Engine, commands []policy.Command) *securityScanRunner {
	selected := map[string]policy.Command{}
	for _, command := range commands {
		if strings.HasPrefix(command.Name, "osv-scan-") || strings.HasPrefix(command.Name, "pnpm-audit-") {
			selected[command.Name] = command
		}
	}
	return &securityScanRunner{delegate: engine.Runner, repo: engine.Repository, commands: selected, now: engine.securityNow, inputs: map[string]string{}, tools: map[string]string{}}
}

func (scanRunner *securityScanRunner) Execute(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	expected, selected := scanRunner.commands[invocation.Command.Name]
	if !selected || !samePolicyCommand(expected, invocation.Command) {
		return scanRunner.delegate.Execute(ctx, invocation)
	}
	key, reusable, err := scanRunner.identity(invocation.Command)
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
	if reusable && reusableSecurityScan(ctx, invocation, previous, now) {
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
	result, output, runErr := scanRunner.delegate.Execute(ctx, actual)
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
	if strings.HasPrefix(command.Name, "osv-scan-") {
		tool = "osv-scanner"
	}
	version, _ := os.ReadFile(filepath.Join(scanRunner.repo.PolicyRoot, "tools", tool+"-version.txt"))
	var refreshAfter time.Time
	if !record.LastSucceededAt.IsZero() {
		refreshAfter = record.LastSucceededAt.Add(policy.SecurityEvidenceLifetime)
	}
	scanRunner.scans = append(scanRunner.scans, policy.SecurityScan{Name: command.Name, Status: status, ScannerVersion: strings.TrimSpace(string(version)), LastSucceededAt: record.LastSucceededAt, RefreshAfter: refreshAfter, ReportPath: path})
}

func (scanRunner *securityScanRunner) identity(command policy.Command) (string, bool, error) {
	files := supplychain.SecurityScanInputs(command)
	inputs := map[string]string{}
	for _, path := range files {
		if scanRunner.inputs[path] == "" {
			data, err := scanRunner.repo.ReadContainedFile(path, 128<<20)
			if err != nil {
				return "", false, fmt.Errorf("security input %s: %w", path, err)
			}
			scanRunner.inputs[path] = gaterun.ContentSHA256(data)
		}
		inputs[path] = scanRunner.inputs[path]
	}
	tool, err := scanRunner.toolIdentity(command.Argv[0])
	if err != nil {
		return "", false, err
	}
	environment := runner.EnvironmentWithPath(command.Environment, scanRunner.repo.CommandEnvironment().PathEntries)
	payload, err := json.Marshal(struct {
		Protocol, Tool string
		Inputs         map[string]string
		Configuration  policy.Config
		Command        policy.Command
		Environment    []string
	}{"security-scan/v3", tool, inputs, scanRunner.repo.Config, command, environment})
	if err != nil {
		return "", false, err
	}
	reusable := len(files) > 0 && strings.HasPrefix(tool, "release:")
	return gaterun.ContentSHA256(payload), reusable, nil
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
	} else {
		file, err := os.Open(executable)
		if err != nil {
			return "", err
		}
		defer file.Close()
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			return "", err
		}
		identity = hex.EncodeToString(hash.Sum(nil))
	}
	scanRunner.tools[executable] = identity
	return identity, nil
}
