package release

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const UpgradePlanProtocol = "code-polishy-upgrade-plan/v1"
const MaximumUpgradePlanBytes = 64 << 20

type UpgradeDiagnostic struct {
	Fingerprint string `json:"fingerprint"`
	RuleID      string `json:"ruleId"`
	Severity    string `json:"severity"`
	Path        string `json:"path"`
	Message     string `json:"message"`
}

type UpgradeDiagnosticChange struct {
	Before UpgradeDiagnostic `json:"before"`
	After  UpgradeDiagnostic `json:"after"`
}

type UpgradeDiagnosticDelta struct {
	Added   []UpgradeDiagnostic       `json:"added"`
	Removed []UpgradeDiagnostic       `json:"removed"`
	Changed []UpgradeDiagnosticChange `json:"changed"`
}

type UpgradePlan struct {
	Protocol                  string                 `json:"protocol"`
	Outgoing                  Lock                   `json:"outgoing"`
	OutgoingLockSHA256        string                 `json:"outgoingLockSha256"`
	Incoming                  Lock                   `json:"incoming"`
	InstallPrefix             string                 `json:"installPrefix"`
	CapabilityDelta           CapabilityDelta        `json:"capabilityDelta"`
	DiagnosticDelta           UpgradeDiagnosticDelta `json:"diagnosticDelta"`
	OutgoingDiagnosticsSHA256 string                 `json:"outgoingDiagnosticsSha256"`
	IncomingDiagnosticsSHA256 string                 `json:"incomingDiagnosticsSha256"`
}

func LockBytesSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func WriteUpgradePlan(repoRoot string, plan UpgradePlan) (string, error) {
	data, err := renderUpgradePlan(plan)
	if err != nil {
		return "", err
	}
	root, err := openCapabilityUpgradeRoot(repoRoot, true)
	if err != nil {
		return "", err
	}
	defer root.Close()
	unlock, err := acquireCapabilityUpgradeLock(root)
	if err != nil {
		return "", err
	}
	defer unlock()
	relative := upgradePlanRelativePath(plan.Incoming)
	directory, err := openCapabilityUpgradeDirectory(root, filepath.Dir(relative), true)
	if err != nil {
		return "", err
	}
	defer directory.Close()
	if err := publishUpgradePlanData(directory, data); err != nil {
		return "", err
	}
	published, err := readCapabilityFile(root, relative, MaximumUpgradePlanBytes)
	if err != nil || !bytes.Equal(published, data) {
		if err == nil {
			err = errors.New("upgrade plan changed during publication")
		}
		return "", err
	}
	if _, err := parseUpgradePlan(published); err != nil {
		return "", err
	}
	return CapabilityUpgradeDirectory + "/" + relative, nil
}

func ReadUpgradePlan(repoRoot, path string) (UpgradePlan, error) {
	prefix := CapabilityUpgradeDirectory + "/"
	if !strings.HasPrefix(path, prefix) {
		return UpgradePlan{}, errors.New("upgrade plan must be a managed repository-relative path")
	}
	relative := strings.TrimPrefix(path, prefix)
	root, err := openCapabilityUpgradeRoot(repoRoot, false)
	if err != nil {
		return UpgradePlan{}, err
	}
	defer root.Close()
	data, err := readCapabilityFile(root, relative, MaximumUpgradePlanBytes)
	if err != nil {
		return UpgradePlan{}, err
	}
	plan, err := parseUpgradePlan(data)
	if err != nil {
		return UpgradePlan{}, err
	}
	if relative != upgradePlanRelativePath(plan.Incoming) {
		return UpgradePlan{}, errors.New("upgrade plan path does not match its incoming lock")
	}
	return plan, nil
}

func renderUpgradePlan(plan UpgradePlan) ([]byte, error) {
	if err := validateUpgradePlan(plan); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(data)+1 > MaximumUpgradePlanBytes {
		return nil, fmt.Errorf("upgrade plan exceeds %d bytes", MaximumUpgradePlanBytes)
	}
	return append(data, '\n'), nil
}

func parseUpgradePlan(data []byte) (UpgradePlan, error) {
	if len(data) == 0 || len(data) > MaximumUpgradePlanBytes {
		return UpgradePlan{}, errors.New("upgrade plan exceeds its byte bound")
	}
	var plan UpgradePlan
	if err := decodeExactly(data, "upgrade plan", &plan); err != nil {
		return UpgradePlan{}, err
	}
	canonical, err := renderUpgradePlan(plan)
	if err != nil || !bytes.Equal(canonical, data) {
		return UpgradePlan{}, errors.New("upgrade plan is not canonical")
	}
	return plan, nil
}

func validateUpgradePlan(plan UpgradePlan) error {
	if err := validateUpgradePlanIdentity(plan); err != nil {
		return err
	}
	if err := validateUpgradePlanLocks(plan); err != nil {
		return err
	}
	return validateUpgradePlanEvidence(plan)
}

func validateUpgradePlanIdentity(plan UpgradePlan) error {
	if plan.Protocol != UpgradePlanProtocol || !digestPattern.MatchString(plan.OutgoingLockSHA256) ||
		!digestPattern.MatchString(plan.OutgoingDiagnosticsSHA256) || !digestPattern.MatchString(plan.IncomingDiagnosticsSHA256) {
		return errors.New("upgrade plan has an invalid identity")
	}
	if !filepath.IsAbs(plan.InstallPrefix) || filepath.Clean(plan.InstallPrefix) != plan.InstallPrefix {
		return errors.New("upgrade plan installation prefix must be an absolute clean path")
	}
	return nil
}

func validateUpgradePlanLocks(plan UpgradePlan) error {
	if _, err := parseLock(RenderLock(plan.Outgoing), "outgoing upgrade lock"); err != nil {
		return err
	}
	if _, err := parseLock(RenderLock(plan.Incoming), "incoming upgrade lock"); err != nil {
		return err
	}
	if plan.Incoming.LockVersion != LockVersion || plan.Incoming.Publication == nil || sameCapabilityLock(plan.Outgoing, plan.Incoming) {
		return errors.New("upgrade plan does not identify a distinct published release")
	}
	return nil
}

func validateUpgradePlanEvidence(plan UpgradePlan) error {
	if !sameCapabilityLock(plan.CapabilityDelta.Incoming, plan.Incoming) ||
		plan.CapabilityDelta.Outgoing == nil || !sameCapabilityLock(*plan.CapabilityDelta.Outgoing, plan.Outgoing) {
		return errors.New("upgrade plan capability delta does not match its locks")
	}
	if !canonicalDiagnosticDelta(plan.DiagnosticDelta) {
		return errors.New("upgrade plan diagnostic delta is not canonical")
	}
	return nil
}

func canonicalDiagnosticDelta(delta UpgradeDiagnosticDelta) bool {
	return slices.IsSortedFunc(delta.Added, compareUpgradeDiagnostic) &&
		slices.IsSortedFunc(delta.Removed, compareUpgradeDiagnostic) &&
		slices.IsSortedFunc(delta.Changed, func(left, right UpgradeDiagnosticChange) int {
			return strings.Compare(left.After.Fingerprint, right.After.Fingerprint)
		}) && delta.Added != nil && delta.Removed != nil && delta.Changed != nil
}

func compareUpgradeDiagnostic(left, right UpgradeDiagnostic) int {
	return strings.Compare(left.Fingerprint, right.Fingerprint)
}

func upgradePlanRelativePath(incoming Lock) string {
	return capabilityContentSHA256(RenderLock(incoming)) + "/plan.json"
}

func publishUpgradePlanData(directory *os.Root, data []byte) error {
	if info, err := directory.Lstat("plan.json"); err == nil && !info.Mode().IsRegular() || err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("upgrade plan must be a regular file")
	}
	temporary := ".plan-" + rand.Text() + ".tmp"
	file, err := directory.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("stage upgrade plan: %w", err)
	}
	defer directory.Remove(temporary)
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	if err := errors.Join(writeErr, syncErr, file.Close()); err != nil {
		return fmt.Errorf("write upgrade plan: %w", err)
	}
	if err := directory.Rename(temporary, "plan.json"); err != nil {
		return fmt.Errorf("publish upgrade plan: %w", err)
	}
	return nil
}
