package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/agents"
	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
)

const upgradeDiagnosticProtocol = "code-polishy-upgrade-diagnostics/v1"
const maximumUpgradeDiagnosticErrorBytes = 1 << 20

var errUpgradeDiagnosticOutputLimit = errors.New("upgrade diagnostic output exceeds its byte bound")

type upgradeDiagnosticSnapshot struct {
	Protocol    string                      `json:"protocol"`
	Diagnostics []release.UpgradeDiagnostic `json:"diagnostics"`
}

type boundedUpgradeOutput struct {
	buffer  bytes.Buffer
	maximum int
}

func (output *boundedUpgradeOutput) Write(data []byte) (int, error) {
	remaining := output.maximum - output.buffer.Len()
	if len(data) <= remaining {
		return output.buffer.Write(data)
	}
	if remaining > 0 {
		_, _ = output.buffer.Write(data[:remaining])
	}
	return len(data), errUpgradeDiagnosticOutputLimit
}

func (output *boundedUpgradeOutput) Bytes() []byte {
	return output.buffer.Bytes()
}

func (output *boundedUpgradeOutput) String() string {
	return output.buffer.String()
}

type upgradePlanOptions struct {
	indexURL, indexSHA256, prefix string
}

type upgradeApplyOptions struct {
	planPath          string
	acceptNewFindings bool
}

type upgradeSnapshotOptions struct {
	version       string
	releaseDigest string
}

type upgradePlanningResult struct {
	outgoing, incoming release.Lock
	capability         release.CapabilityDelta
	diagnostics        release.UpgradeDiagnosticDelta
	planPath, nextRoot string
}

func handleUpgradeMeta(invocation invocation) int {
	if len(invocation.arguments) == 0 {
		return commandUsageError("upgrade", "upgrade requires plan or apply")
	}
	switch invocation.arguments[0] {
	case "plan":
		return planUpgrade(invocation, invocation.arguments[1:])
	case "apply":
		return applyUpgrade(invocation, invocation.arguments[1:])
	case "snapshot":
		return snapshotUpgrade(invocation, invocation.arguments[1:])
	default:
		return commandUsageError("upgrade", "upgrade requires plan or apply")
	}
}

func planUpgrade(invocation invocation, arguments []string) int {
	options, err := parseUpgradePlanOptions(arguments)
	if err != nil {
		return commandUsageError("upgrade", err.Error())
	}
	result, err := prepareUpgradePlan(invocation, options)
	if err != nil {
		return operationalError(err)
	}
	fmt.Printf("PASS upgrade plan: Code Polishy %s -> %s\n", result.outgoing.CodePolishyVersion, result.incoming.CodePolishyVersion)
	fmt.Println(capabilityDeltaHuman(result.capability))
	printUpgradeDiagnosticDelta(result.diagnostics)
	fmt.Printf("PLAN: %s\n", result.planPath)
	fmt.Printf("NEXT: %s --repo-root %s upgrade apply --plan %s\n", filepath.Join(result.nextRoot, filepath.FromSlash(release.BinaryPath)), invocation.repoRoot, result.planPath)
	if upgradeDeltaHasNewErrors(result.diagnostics) {
		fmt.Println("New error diagnostics require cleanup, a new plan, or apply --accept-new-findings.")
	}
	return 0
}

func parseUpgradePlanOptions(arguments []string) (upgradePlanOptions, error) {
	flags := flag.NewFlagSet("upgrade plan", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	indexURL := flags.String("index", "", "release publication index URL")
	indexSHA256 := flags.String("sha256", "", "release publication index SHA-256")
	prefix := flags.String("prefix", "", "shared installation prefix")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *indexURL == "" || *indexSHA256 == "" {
		return upgradePlanOptions{}, errors.New("upgrade plan requires --index URL and --sha256 DIGEST; --prefix PATH is optional")
	}
	return upgradePlanOptions{*indexURL, *indexSHA256, *prefix}, nil
}

func prepareUpgradePlan(invocation invocation, options upgradePlanOptions) (upgradePlanningResult, error) {
	installPrefix, err := resolveUpgradePrefix(invocation.policyRoot, options.prefix)
	if err != nil {
		return upgradePlanningResult{}, err
	}
	outgoing, outgoingBytes, err := readUpgradeOutgoingLock(invocation.repoRoot)
	if err != nil {
		return upgradePlanningResult{}, err
	}
	incoming, err := release.InstallIndexedRelease(context.Background(), options.indexURL, options.indexSHA256, installPrefix)
	if err != nil {
		return upgradePlanningResult{}, err
	}
	if bytes.Equal(release.RenderLock(outgoing), release.RenderLock(incoming.Lock)) {
		return upgradePlanningResult{}, errors.New("the publication index resolves to the repository's current release")
	}
	capability, err := release.PrepareCapabilityUpgrade(invocation.repoRoot, incoming.Root, incoming.Lock)
	if err != nil {
		return upgradePlanningResult{}, err
	}
	outgoingRoot := release.Directory(installPrefix, outgoing)
	if err := verifyUpgradeRelease(outgoingRoot, outgoing); err != nil {
		return upgradePlanningResult{}, fmt.Errorf("outgoing release: %w", err)
	}
	outgoingSnapshot, outgoingSnapshotData, err := runUpgradeDiagnostics(invocation, outgoingRoot, outgoing, false)
	if err != nil {
		return upgradePlanningResult{}, err
	}
	incomingSnapshot, incomingSnapshotData, err := runUpgradeDiagnostics(invocation, incoming.Root, incoming.Lock, true)
	if err != nil {
		return upgradePlanningResult{}, err
	}
	delta := compareUpgradeDiagnostics(outgoingSnapshot.Diagnostics, incomingSnapshot.Diagnostics)
	plan := release.UpgradePlan{
		Protocol: release.UpgradePlanProtocol, Outgoing: outgoing,
		OutgoingLockSHA256: release.LockBytesSHA256(outgoingBytes), Incoming: incoming.Lock,
		InstallPrefix: installPrefix, CapabilityDelta: capability.Delta, DiagnosticDelta: delta,
		OutgoingDiagnosticsSHA256: release.LockBytesSHA256(outgoingSnapshotData),
		IncomingDiagnosticsSHA256: release.LockBytesSHA256(incomingSnapshotData),
	}
	path, err := release.WriteUpgradePlan(invocation.repoRoot, plan)
	if err != nil {
		return upgradePlanningResult{}, err
	}
	return upgradePlanningResult{outgoing, incoming.Lock, capability.Delta, delta, path, incoming.Root}, nil
}

func applyUpgrade(invocation invocation, arguments []string) int {
	options, err := parseUpgradeApplyOptions(arguments)
	if err != nil {
		return commandUsageError("upgrade", err.Error())
	}
	plan, currentBytes, incomingRoot, err := prepareUpgradeApplication(invocation, options.planPath)
	if err != nil {
		return operationalError(err)
	}
	if err := revalidateUpgradeEvidence(invocation, plan, incomingRoot); err != nil {
		return operationalError(err)
	}
	if upgradeDeltaHasNewErrors(plan.DiagnosticDelta) && !options.acceptNewFindings {
		return operationalError(errors.New("incoming release adds or changes error diagnostics; fix them, create a new plan, or pass --accept-new-findings"))
	}
	message, err := agents.SyncWithLock(invocation.repoRoot, incomingRoot, currentBytes, release.RenderLock(plan.Incoming))
	if err != nil {
		return operationalError(err)
	}
	if err := release.RequireLockedRelease(invocation.repoRoot, incomingRoot); err != nil {
		return operationalError(fmt.Errorf("upgrade cutover verification failed: %w", err))
	}
	fmt.Printf("PASS upgraded repository to Code Polishy %s\n", plan.Incoming.CodePolishyVersion)
	fmt.Println(message)
	fmt.Println("No merge gate or application test suite was run.")
	return 0
}

func parseUpgradeApplyOptions(arguments []string) (upgradeApplyOptions, error) {
	flags := flag.NewFlagSet("upgrade apply", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	planPath := flags.String("plan", "", "managed upgrade plan path")
	accept := flags.Bool("accept-new-findings", false, "acknowledge new incoming error diagnostics")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *planPath == "" {
		return upgradeApplyOptions{}, errors.New("upgrade apply requires --plan PATH and accepts --accept-new-findings")
	}
	return upgradeApplyOptions{planPath: *planPath, acceptNewFindings: *accept}, nil
}

func prepareUpgradeApplication(invocation invocation, planPath string) (release.UpgradePlan, []byte, string, error) {
	plan, err := release.ReadUpgradePlan(invocation.repoRoot, filepath.ToSlash(planPath))
	if err != nil {
		return release.UpgradePlan{}, nil, "", err
	}
	current, currentBytes, err := readUpgradeOutgoingLock(invocation.repoRoot)
	if err != nil {
		return release.UpgradePlan{}, nil, "", err
	}
	if release.LockBytesSHA256(currentBytes) != plan.OutgoingLockSHA256 || !bytes.Equal(release.RenderLock(current), release.RenderLock(plan.Outgoing)) {
		return release.UpgradePlan{}, nil, "", errors.New("repository lock changed after upgrade planning; create a new plan")
	}
	incomingRoot := release.Directory(plan.InstallPrefix, plan.Incoming)
	if err := verifyUpgradeRelease(incomingRoot, plan.Incoming); err != nil {
		return release.UpgradePlan{}, nil, "", fmt.Errorf("incoming release: %w", err)
	}
	outgoingRoot := release.Directory(plan.InstallPrefix, plan.Outgoing)
	if err := verifyUpgradeRelease(outgoingRoot, plan.Outgoing); err != nil {
		return release.UpgradePlan{}, nil, "", fmt.Errorf("outgoing release: %w", err)
	}
	return plan, currentBytes, incomingRoot, nil
}

func revalidateUpgradeEvidence(invocation invocation, plan release.UpgradePlan, incomingRoot string) error {
	outgoingRoot := release.Directory(plan.InstallPrefix, plan.Outgoing)
	outgoingSnapshot, outgoingData, err := runUpgradeDiagnostics(invocation, outgoingRoot, plan.Outgoing, false)
	if err != nil {
		return err
	}
	incomingSnapshot, incomingData, err := runUpgradeDiagnostics(invocation, incomingRoot, plan.Incoming, true)
	if err != nil {
		return err
	}
	if release.LockBytesSHA256(outgoingData) != plan.OutgoingDiagnosticsSHA256 || release.LockBytesSHA256(incomingData) != plan.IncomingDiagnosticsSHA256 ||
		!sameUpgradeDiagnosticDelta(compareUpgradeDiagnostics(outgoingSnapshot.Diagnostics, incomingSnapshot.Diagnostics), plan.DiagnosticDelta) {
		return errors.New("repository diagnostics changed after upgrade planning; create a new plan")
	}
	return nil
}

func snapshotUpgrade(invocation invocation, arguments []string) int {
	options, err := parseUpgradeSnapshotOptions(arguments)
	if err != nil {
		return commandUsageError("upgrade", err.Error())
	}
	if err := verifyUpgradeSnapshotRelease(invocation.policyRoot, options); err != nil {
		return operationalError(err)
	}
	snapshot, data, err := collectUpgradeDiagnostics(invocation)
	if err != nil {
		return operationalError(err)
	}
	if snapshot.Protocol != upgradeDiagnosticProtocol {
		return operationalError(errors.New("invalid upgrade diagnostic protocol"))
	}
	if _, err := os.Stdout.Write(data); err != nil {
		return operationalError(err)
	}
	return 0
}

func parseUpgradeSnapshotOptions(arguments []string) (upgradeSnapshotOptions, error) {
	flags := flag.NewFlagSet("upgrade snapshot", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	version := flags.String("version", "", "expected release version")
	digest := flags.String("release-digest", "", "expected release digest")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *version == "" || *digest == "" {
		return upgradeSnapshotOptions{}, errors.New("internal upgrade snapshot requires an exact release identity")
	}
	return upgradeSnapshotOptions{version: *version, releaseDigest: *digest}, nil
}

func verifyUpgradeSnapshotRelease(policyRoot string, options upgradeSnapshotOptions) error {
	manifest, present, err := release.ReadManifest(policyRoot)
	if err != nil || !present {
		if err == nil {
			err = errors.New("incoming release manifest is unavailable")
		}
		return err
	}
	if err := manifest.Verify(policyRoot); err != nil || manifest.CodePolishyVersion != options.version || manifest.ReleaseDigest != options.releaseDigest {
		if err == nil {
			err = errors.New("incoming release identity does not match the snapshot request")
		}
		return err
	}
	return nil
}

func collectUpgradeDiagnostics(invocation invocation) (upgradeDiagnosticSnapshot, []byte, error) {
	policyEngine, err := engine.Open(invocation.repoRoot, invocation.policyRoot, invocation.configPath)
	if err != nil {
		return upgradeDiagnosticSnapshot{}, nil, err
	}
	policyEngine.RouteProgress(io.Discard)
	selection, err := policyEngine.Select("all", nil)
	if err != nil {
		return upgradeDiagnosticSnapshot{}, nil, err
	}
	report := policyEngine.Check(context.Background(), selection, "check")
	snapshot := upgradeDiagnosticSnapshot{Protocol: upgradeDiagnosticProtocol, Diagnostics: diagnosticsFromFindings(report.Findings)}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return upgradeDiagnosticSnapshot{}, nil, err
	}
	data = append(data, '\n')
	return snapshot, data, nil
}

func runUpgradeDiagnostics(invocation invocation, root string, locked release.Lock, incoming bool) (upgradeDiagnosticSnapshot, []byte, error) {
	binary := filepath.Join(root, filepath.FromSlash(release.BinaryPath))
	arguments := upgradeDiagnosticArguments(invocation, root, locked, incoming)
	output, diagnostic, err := executeUpgradeDiagnostics(binary, arguments)
	if err := validateUpgradeDiagnosticExit(err, incoming); err != nil {
		return upgradeDiagnosticSnapshot{}, nil, fmt.Errorf("run Code Polishy %s diagnostics: %w: %s", locked.CodePolishyVersion, err, strings.TrimSpace(diagnostic))
	}
	if incoming {
		return parseIncomingUpgradeDiagnostics(output)
	}
	return parseOutgoingUpgradeDiagnostics(output)
}

func upgradeDiagnosticArguments(invocation invocation, root string, locked release.Lock, incoming bool) []string {
	arguments := []string{"--repo-root", invocation.repoRoot, "--policy-root", root}
	if invocation.configPath != "" {
		arguments = append(arguments, "--config", invocation.configPath)
	}
	if incoming {
		arguments = append(arguments, "upgrade", "snapshot", "--version", locked.CodePolishyVersion, "--release-digest", locked.ReleaseDigest)
	} else {
		arguments = append(arguments, "check", "--all", "--format", "json")
	}
	return arguments
}

func executeUpgradeDiagnostics(binary string, arguments []string) ([]byte, string, error) {
	command := exec.Command(binary, arguments...)
	output := &boundedUpgradeOutput{maximum: release.MaximumUpgradePlanBytes}
	diagnostic := &boundedUpgradeOutput{maximum: maximumUpgradeDiagnosticErrorBytes}
	command.Stdout, command.Stderr = output, diagnostic
	err := command.Run()
	return output.Bytes(), diagnostic.String(), err
}

func validateUpgradeDiagnosticExit(err error, incoming bool) error {
	if err == nil {
		return nil
	}
	var exitError *exec.ExitError
	if !incoming && errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return nil
	}
	return err
}

func parseIncomingUpgradeDiagnostics(data []byte) (upgradeDiagnosticSnapshot, []byte, error) {
	var snapshot upgradeDiagnosticSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil || snapshot.Protocol != upgradeDiagnosticProtocol {
		return upgradeDiagnosticSnapshot{}, nil, errors.New("incoming release returned an invalid diagnostic snapshot")
	}
	return snapshot, data, nil
}

func parseOutgoingUpgradeDiagnostics(data []byte) (upgradeDiagnosticSnapshot, []byte, error) {
	var report engine.Report
	if err := json.Unmarshal(data, &report); err != nil || report.Protocol != engine.ReportProtocol || report.Command != "check" {
		return upgradeDiagnosticSnapshot{}, nil, errors.New("outgoing release returned an invalid check report")
	}
	snapshot := upgradeDiagnosticSnapshot{Protocol: upgradeDiagnosticProtocol, Diagnostics: diagnosticsFromFindings(report.Findings)}
	canonical, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return upgradeDiagnosticSnapshot{}, nil, err
	}
	return snapshot, append(canonical, '\n'), nil
}

func diagnosticsFromFindings(findings []policy.Finding) []release.UpgradeDiagnostic {
	diagnostics := make([]release.UpgradeDiagnostic, 0, len(findings))
	for _, finding := range findings {
		diagnostics = append(diagnostics, release.UpgradeDiagnostic{
			Fingerprint: finding.Fingerprint, RuleID: finding.Check, Severity: string(finding.Severity),
			Path: finding.Path, Message: finding.Message,
		})
	}
	slices.SortFunc(diagnostics, func(left, right release.UpgradeDiagnostic) int {
		return strings.Compare(left.Fingerprint, right.Fingerprint)
	})
	return diagnostics
}

func compareUpgradeDiagnostics(before, after []release.UpgradeDiagnostic) release.UpgradeDiagnosticDelta {
	delta := release.UpgradeDiagnosticDelta{Added: []release.UpgradeDiagnostic{}, Removed: []release.UpgradeDiagnostic{}, Changed: []release.UpgradeDiagnosticChange{}}
	previous := make(map[string]release.UpgradeDiagnostic, len(before))
	current := make(map[string]release.UpgradeDiagnostic, len(after))
	for _, diagnostic := range before {
		previous[diagnostic.Fingerprint] = diagnostic
	}
	for _, diagnostic := range after {
		current[diagnostic.Fingerprint] = diagnostic
	}
	for fingerprint, diagnostic := range current {
		prior, exists := previous[fingerprint]
		if !exists {
			delta.Added = append(delta.Added, diagnostic)
		} else if prior != diagnostic {
			delta.Changed = append(delta.Changed, release.UpgradeDiagnosticChange{Before: prior, After: diagnostic})
		}
	}
	for fingerprint, diagnostic := range previous {
		if _, exists := current[fingerprint]; !exists {
			delta.Removed = append(delta.Removed, diagnostic)
		}
	}
	slices.SortFunc(delta.Added, func(left, right release.UpgradeDiagnostic) int {
		return strings.Compare(left.Fingerprint, right.Fingerprint)
	})
	slices.SortFunc(delta.Removed, func(left, right release.UpgradeDiagnostic) int {
		return strings.Compare(left.Fingerprint, right.Fingerprint)
	})
	slices.SortFunc(delta.Changed, func(left, right release.UpgradeDiagnosticChange) int {
		return strings.Compare(left.After.Fingerprint, right.After.Fingerprint)
	})
	return delta
}

func sameUpgradeDiagnosticDelta(left, right release.UpgradeDiagnosticDelta) bool {
	first, _ := json.Marshal(left)
	second, _ := json.Marshal(right)
	return bytes.Equal(first, second)
}

func upgradeDeltaHasNewErrors(delta release.UpgradeDiagnosticDelta) bool {
	for _, diagnostic := range delta.Added {
		if diagnostic.Severity == string(policy.FindingError) {
			return true
		}
	}
	for _, diagnostic := range delta.Changed {
		if diagnostic.After.Severity == string(policy.FindingError) {
			return true
		}
	}
	return false
}

func printUpgradeDiagnosticDelta(delta release.UpgradeDiagnosticDelta) {
	fmt.Printf("DIAGNOSTIC DELTA: added %d; removed %d; changed %d.\n", len(delta.Added), len(delta.Removed), len(delta.Changed))
	shown := 0
	for _, group := range []struct {
		label       string
		diagnostics []release.UpgradeDiagnostic
	}{{"ADDED", delta.Added}, {"REMOVED", delta.Removed}} {
		for _, diagnostic := range group.diagnostics {
			if shown == 8 {
				break
			}
			fmt.Printf("  %s %s %s: %s\n", group.label, diagnostic.RuleID, diagnostic.Path, diagnostic.Message)
			shown++
		}
	}
	for _, change := range delta.Changed {
		if shown == 8 {
			break
		}
		fmt.Printf("  CHANGED %s %s: %s\n", change.After.RuleID, change.After.Path, change.After.Message)
		shown++
	}
	if remaining := len(delta.Added) + len(delta.Removed) + len(delta.Changed) - shown; remaining > 0 {
		fmt.Printf("  %d more changes are recorded in the plan.\n", remaining)
	}
}

func readUpgradeOutgoingLock(repoRoot string) (release.Lock, []byte, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, release.LockFilename))
	if err != nil {
		return release.Lock{}, nil, err
	}
	locked, present, err := release.ReadLock(repoRoot)
	if err != nil || !present {
		if err == nil {
			err = errors.New("repository has no Code Polishy lock")
		}
		return release.Lock{}, nil, err
	}
	return locked, data, nil
}

func verifyUpgradeRelease(root string, locked release.Lock) error {
	manifest, present, err := release.ReadManifest(root)
	if err != nil || !present {
		if err == nil {
			err = errors.New("release manifest is unavailable")
		}
		return err
	}
	if err := manifest.Verify(root); err != nil {
		return err
	}
	return manifest.Satisfies(locked)
}

func resolveUpgradePrefix(policyRoot, requested string) (string, error) {
	if requested == "" {
		if manifest, present, err := release.ReadManifest(policyRoot); err == nil && present {
			candidate := filepath.Dir(filepath.Dir(policyRoot))
			if filepath.Clean(release.Directory(candidate, release.LockFor(manifest))) == filepath.Clean(policyRoot) {
				requested = candidate
			}
		}
	}
	if requested == "" {
		if runtime.GOOS == "windows" {
			requested = filepath.Join(os.Getenv("LOCALAPPDATA"), "CodePolishy")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			requested = filepath.Join(home, ".local", "share", "code-polishy")
		}
	}
	absolute, err := filepath.Abs(requested)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}
