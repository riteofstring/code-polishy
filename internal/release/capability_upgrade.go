package release

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"unicode"
	"unicode/utf8"
)

func WriteReleaseLock(repoRoot, incomingRoot string) (LockUpgradeResult, error) {
	manifest, installed, err := ReadManifest(incomingRoot)
	if err != nil {
		return LockUpgradeResult{}, err
	}
	if !installed {
		return LockUpgradeResult{}, fmt.Errorf("a source checkout cannot satisfy a target release lock")
	}
	incoming := LockFor(manifest)
	if err := manifest.Satisfies(incoming); err != nil {
		return LockUpgradeResult{}, err
	}
	root, err := openCapabilityUpgradeRoot(repoRoot, true)
	if err != nil {
		return LockUpgradeResult{}, err
	}
	defer root.Close()
	unlock, err := acquireCapabilityUpgradeLock(root)
	if err != nil {
		return LockUpgradeResult{}, err
	}
	defer unlock()
	return writeCapabilityUpgrade(repoRoot, incomingRoot, root, incoming)
}

func captureCapabilityUpgrade(incomingRoot string, outgoing *Lock, incoming Lock) capabilityUpgradeRecord {
	record := capabilityUpgradeRecord{Protocol: "capability-upgrade-record/v1", Delta: newCapabilityDelta(outgoing, incoming)}
	after, err := ReadCapabilityCatalog(incomingRoot, incoming)
	if err != nil {
		record.Delta.Reason = "The incoming release capability catalog could not be authenticated."
		return record
	}
	record.Incoming = after.proof
	record.Delta.IncomingCatalogSHA256 = after.SHA256
	if outgoing == nil {
		record.Delta.Reason = "There was no outgoing release lock to compare."
		return record
	}
	outgoingRoot, err := outgoingCapabilityRoot(incomingRoot, incoming, *outgoing)
	if err != nil {
		record.Delta.Reason = "The exact outgoing release is unavailable beside the incoming installed release."
		return record
	}
	before, err := ReadCapabilityCatalog(outgoingRoot, *outgoing)
	if err != nil {
		record.Delta.Reason = "The outgoing release capability catalog could not be authenticated; no changes were inferred."
		return record
	}
	record.Outgoing = before.proof
	record.Delta = compareCapabilityCatalogs(before, after)
	return record
}

func outgoingCapabilityRoot(incomingRoot string, incoming, outgoing Lock) (string, error) {
	root, err := filepath.EvalSymlinks(incomingRoot)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	prefix := filepath.Dir(filepath.Dir(root))
	if root != Directory(prefix, incoming) {
		return "", fmt.Errorf("incoming release does not identify an installation prefix")
	}
	return Directory(prefix, outgoing), nil
}

func ReadCapabilityUpgrade(repoRoot string, incoming Lock) CapabilityDelta {
	unavailable := capabilityDeltaUnavailable(nil, incoming, "No verified capability upgrade record is available for the current lock.")
	unavailable.ArtifactPath = ""
	root, err := openCapabilityUpgradeRoot(repoRoot, false)
	if err != nil {
		return unavailable
	}
	defer root.Close()
	data, err := readCapabilityFile(root, capabilityUpgradeRelativePath(incoming), MaximumCapabilityUpgradeBytes)
	if err != nil {
		return unavailable
	}
	record, err := parseCapabilityUpgradeRecord(data, incoming)
	if err != nil {
		unavailable.Reason = "The stored capability upgrade record is invalid or cannot authenticate its catalogs."
		return unavailable
	}
	return record.Delta
}

func parseCapabilityUpgradeRecord(data []byte, incoming Lock) (capabilityUpgradeRecord, error) {
	var record capabilityUpgradeRecord
	if len(data) == 0 || len(data) > MaximumCapabilityUpgradeBytes {
		return record, fmt.Errorf("capability upgrade record exceeds its byte bound")
	}
	if err := decodeExactly(data, "capability upgrade record", &record); err != nil {
		return record, err
	}
	canonical, err := renderCapabilityUpgradeRecord(record)
	if err != nil || !bytes.Equal(canonical, data) || record.Protocol != "capability-upgrade-record/v1" {
		return record, fmt.Errorf("capability upgrade record is not a canonical versioned document")
	}
	if !sameCapabilityLock(record.Delta.Incoming, incoming) || record.Delta.ArtifactPath != capabilityUpgradePath(incoming) {
		return record, fmt.Errorf("capability upgrade record belongs to another lock")
	}
	if err := validateCapabilityUpgradeRecord(record); err != nil {
		return record, err
	}
	return record, nil
}

func validateCapabilityUpgradeRecord(record capabilityUpgradeRecord) error {
	delta := record.Delta
	if err := validateCapabilityDeltaShape(delta); err != nil {
		return err
	}
	before, err := authenticateCapabilityDeltaCatalog(record.Outgoing, delta.Outgoing, delta.OutgoingCatalogSHA256)
	if err != nil {
		return err
	}
	after, err := authenticateCapabilityDeltaCatalog(record.Incoming, &delta.Incoming, delta.IncomingCatalogSHA256)
	if err != nil {
		return err
	}
	return validateCapabilityDeltaResult(record, before, after)
}

func validateCapabilityDeltaShape(delta CapabilityDelta) error {
	if delta.Protocol != "capability-delta/v1" || delta.Added == nil || delta.Removed == nil || delta.Changed == nil {
		return fmt.Errorf("capability delta protocol or change arrays are invalid")
	}
	if _, err := parseLock(RenderLock(delta.Incoming), "incoming capability lock"); err != nil {
		return err
	}
	if delta.Outgoing != nil {
		_, err := parseLock(RenderLock(*delta.Outgoing), "outgoing capability lock")
		return err
	}
	return nil
}

func authenticateCapabilityDeltaCatalog(proof *capabilityCatalogProof, expected *Lock, digest string) (AuthenticatedCapabilityCatalog, error) {
	if proof == nil {
		if digest != "" {
			return AuthenticatedCapabilityCatalog{}, fmt.Errorf("capability delta asserts an unauthenticated catalog digest")
		}
		return AuthenticatedCapabilityCatalog{}, nil
	}
	if expected == nil {
		return AuthenticatedCapabilityCatalog{}, fmt.Errorf("capability evidence omitted its release lock")
	}
	catalog, err := authenticateCapabilityProof(proof, *expected)
	if err != nil || catalog.SHA256 != digest {
		return AuthenticatedCapabilityCatalog{}, fmt.Errorf("capability evidence does not match the delta")
	}
	return catalog, nil
}

func validateCapabilityDeltaResult(record capabilityUpgradeRecord, before, after AuthenticatedCapabilityCatalog) error {
	delta := record.Delta
	if delta.Availability == "unavailable" {
		if !validCapabilityDeltaReason(delta.Reason) || len(delta.Added)+len(delta.Removed)+len(delta.Changed) != 0 || record.Outgoing != nil && record.Incoming != nil {
			return fmt.Errorf("unavailable capability evidence cannot assert changes")
		}
		return nil
	}
	if delta.Availability != "available" || record.Outgoing == nil || record.Incoming == nil {
		return fmt.Errorf("capability delta requires both authenticated release catalogs")
	}
	expected := compareCapabilityCatalogs(before, after)
	actual, _ := json.Marshal(delta)
	canonical, _ := json.Marshal(expected)
	if !bytes.Equal(actual, canonical) {
		return fmt.Errorf("capability delta does not match its authenticated catalogs")
	}
	return nil
}

func validCapabilityDeltaReason(reason string) bool {
	if reason == "" || len(reason) > 512 || !utf8.ValidString(reason) {
		return false
	}
	for _, character := range reason {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func renderCapabilityUpgradeRecord(record capabilityUpgradeRecord) ([]byte, error) {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(data)+1 > MaximumCapabilityUpgradeBytes {
		return nil, fmt.Errorf("capability upgrade record exceeds %d bytes", MaximumCapabilityUpgradeBytes)
	}
	return append(data, '\n'), nil
}
