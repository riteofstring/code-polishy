package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func scannerSigningFixture(t *testing.T) (string, []byte, ScannerManifest) {
	t.Helper()
	root, _ := installedRelease(t, map[string]string{BinaryPath: "scanner fixture engine"}, nil)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "artifact-security"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artifact-security/scanner-contract.txt"), []byte("code-polishy-trivy/v1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "artifact-security/publisher.pem"), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	secret, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	manifest := ScannerManifest{Version: "1.0.0", Contract: "code-polishy-trivy/v1", TrivyVersion: "0.76.1", SourceImage: "docker.io/aquasec/trivy@sha256:" + strings.Repeat("a", 64), ReassessBy: time.Now().Add(time.Hour), OpenVEX: json.RawMessage(`{"statements":[]}`), Evidence: "Reproducible fixed advisory fixture for scanner tests."}
	return root, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: secret}), manifest
}

func signScannerFixture(t *testing.T, key []byte, manifest ScannerManifest) *SignedScanner {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := SignScanner(data, key)
	if err != nil {
		t.Fatal(err)
	}
	return &signed
}

func TestScannerPublisherAndContractAreIndependentOfConsumerPin(t *testing.T) {
	root, key, manifest := scannerSigningFixture(t)
	signed := signScannerFixture(t, key, manifest)
	if _, err := VerifyScanner(root, signed); err != nil {
		t.Fatal(err)
	}
	_, foreign, _ := scannerSigningFixture(t)
	for name, candidate := range map[string]*SignedScanner{"unsigned": {Payload: signed.Payload}, "foreign": signScannerFixture(t, foreign, manifest), "tampered": {Payload: append(append([]byte{}, signed.Payload...), ' '), Signature: signed.Signature}} {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyScanner(root, candidate); err == nil {
				t.Fatal("unapproved package accepted")
			}
		})
	}
	for _, change := range []func(*ScannerManifest){func(m *ScannerManifest) { m.Contract = "different/v1" }, func(m *ScannerManifest) { m.ReassessBy = time.Now().Add(-time.Second) }, func(m *ScannerManifest) { m.SourceImage = "docker.io/aquasec/trivy:latest" }} {
		candidate := manifest
		change(&candidate)
		if _, err := VerifyScanner(root, signScannerFixture(t, key, candidate)); err == nil {
			t.Fatal("incompatible package accepted")
		}
	}
}

func TestScannerUpdatePreservesReleaseAndRejectsStaleOrInterruptedCutover(t *testing.T) {
	root, key, manifest := scannerSigningFixture(t)
	repo := t.TempDir()
	old := scannerReleaseLockFixture(t, root)
	if err := WriteLock(repo, old); err != nil {
		t.Fatal(err)
	}
	signed := signScannerFixture(t, key, manifest)
	if err := WriteScannerLock(repo, root, old, signed); err != nil {
		t.Fatal(err)
	}
	current, present, err := ReadLock(repo)
	if err != nil || !present || current.Scanner == nil {
		t.Fatalf("lock = %+v %v", current, err)
	}
	current.Scanner = nil
	if !sameCapabilityLock(current, old) {
		t.Fatal("scanner update changed release selection")
	}
	before, err := os.ReadFile(filepath.Join(repo, LockFilename))
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteScannerLock(repo, root, old, signed); err == nil {
		t.Fatal("stale update accepted")
	}
	malformed := *signed
	malformed.Signature = nil
	actual, _, _ := ReadLock(repo)
	if err := WriteScannerLock(repo, root, actual, &malformed); err == nil {
		t.Fatal("unauthorized update accepted")
	}
	after, err := os.ReadFile(filepath.Join(repo, LockFilename))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed update changed authoritative lock")
	}
	if err := os.WriteFile(filepath.Join(repo, ".scanner-abandoned.tmp"), []byte("interrupted preparation"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteScannerLock(repo, root, actual, signed); err != nil {
		t.Fatal(err)
	}
}

func TestScannerBoundsLeaveEncodingRoomInsideTheBootstrapLockLimit(t *testing.T) {
	root, key, manifest := scannerSigningFixture(t)
	manifest.Evidence = strings.Repeat("e", MaximumScannerPayloadBytes)
	if _, err := VerifyScanner(root, signScannerFixture(t, key, manifest)); err == nil {
		t.Fatal("oversized signed payload accepted")
	}
	signed := &SignedScanner{Payload: make([]byte, MaximumScannerPayloadBytes), Signature: make([]byte, ed25519.SignatureSize)}
	encoded := jsonValue(signed)
	if len(encoded) > MaximumScannerBytes {
		t.Fatal("payload encoding exceeds acquisition bound")
	}
	lock := indexedLockFixture("9.9.9", exampleDigest)
	lock.Scanner = signed
	if len(RenderLock(lock)) > MaximumLockBytes {
		t.Fatal("scanner package exceeds bootstrap lock bound")
	}
}

func TestExpiredScannerCanBeReplacedWithoutAuthorizingItsAssessments(t *testing.T) {
	root, key, manifest := scannerSigningFixture(t)
	expired := manifest
	expired.ReassessBy = time.Now().Add(-time.Second)
	selected := scannerReleaseLockFixture(t, root)
	selected.Scanner = signScannerFixture(t, key, expired)
	repo := t.TempDir()
	if err := WriteLock(repo, selected); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyScanner(root, selected.Scanner); err == nil {
		t.Fatal("expired assessments remained authorized")
	}
	if err := WriteScannerLock(repo, root, selected, signScannerFixture(t, key, manifest)); err != nil {
		t.Fatalf("expiry blocked recovery: %v", err)
	}
}

func installScannerPublicKeyFixture(t *testing.T, root, keyRoot, contract string) {
	t.Helper()
	key, err := os.ReadFile(filepath.Join(keyRoot, "artifact-security/publisher.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "artifact-security"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artifact-security/publisher.pem"), key, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artifact-security/scanner-contract.txt"), []byte(contract+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest, _, err := ReadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Entries = append(manifest.Entries, Entry{Path: "artifact-security/publisher.pem", SHA256: digestBytes(key)}, Entry{Path: "artifact-security/scanner-contract.txt", SHA256: digestBytes([]byte(contract + "\n"))})
	slices.SortFunc(manifest.Entries, func(a, b Entry) int { return strings.Compare(a.Path, b.Path) })
	manifest.EntryCount = len(manifest.Entries)
	manifest.ContentDigest = EntriesDigest(manifest.Entries)
	writeCapabilityManifest(t, root, manifest)
}

func TestToolUpgradePreservesOnlyScannerSelectionsTheIncomingReleaseTrusts(t *testing.T) {
	for _, expired := range []bool{false, true} {
		for _, mode := range []string{"compatible", "foreign-publisher", "incompatible-contract"} {
			name := mode
			if expired {
				name = "expired/" + mode
			}
			t.Run(name, func(t *testing.T) {
				repo, oldRoot, nextRoot, old, next := capabilityUpgradeFixture(t)
				keyRoot, key, manifest := scannerSigningFixture(t)
				if expired {
					manifest.ReassessBy = time.Now().Add(-time.Hour)
				}
				installScannerPublicKeyFixture(t, oldRoot, keyRoot, "code-polishy-trivy/v1")
				incomingKeyRoot := keyRoot
				if mode == "foreign-publisher" {
					incomingKeyRoot, _, _ = scannerSigningFixture(t)
				}
				contract := "code-polishy-trivy/v1"
				if mode == "incompatible-contract" {
					contract = "code-polishy-trivy/v2"
				}
				installScannerPublicKeyFixture(t, nextRoot, incomingKeyRoot, contract)
				old.Scanner = signScannerFixture(t, key, manifest)
				if err := WriteLock(repo, old); err != nil {
					t.Fatal(err)
				}
				result, err := writeReleaseLock(repo, nextRoot, next)
				if mode != "compatible" {
					if err == nil {
						t.Fatal("upgrade retained an untrusted or incompatible scanner")
					}
					current, _, readErr := ReadLock(repo)
					if readErr != nil || !sameCapabilityLock(current, old) {
						t.Fatal("rejected upgrade changed lock")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if result.Lock.Scanner == nil || string(result.Lock.Scanner.Payload) != string(old.Scanner.Payload) || string(result.Lock.Scanner.Signature) != string(old.Scanner.Signature) {
					t.Fatal("upgrade replaced scanner selection")
				}
				_, err = VerifyScanner(nextRoot, result.Lock.Scanner)
				if (err != nil) != expired {
					t.Fatalf("scanner assessment authorization changed: expired=%v error=%v", expired, err)
				}
				if expired {
					manifest.Version = "1.0.1"
					manifest.ReassessBy = time.Now().Add(time.Hour)
					if err := WriteScannerLock(repo, nextRoot, result.Lock, signScannerFixture(t, key, manifest)); err != nil {
						t.Fatalf("compatible upgrade blocked scanner renewal: %v", err)
					}
				}
			})
		}
	}
}

func TestScannerSignatureUsesThePublishedDomainAndCannotAddPolicyOverrides(t *testing.T) {
	root, key, manifest := scannerSigningFixture(t)
	payload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(key)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	private := parsed.(ed25519.PrivateKey)
	independent := SignedScanner{Payload: payload, Signature: ed25519.Sign(private, append([]byte("code-polishy-scanner-manifest/v1\x00"), payload...))}
	if _, err := VerifyScanner(root, &independent); err != nil {
		t.Fatalf("published signing format failed: %v", err)
	}
	independent.Signature = ed25519.Sign(private, payload)
	if _, err := VerifyScanner(root, &independent); err == nil {
		t.Fatal("signature from a different message domain was accepted")
	}
	for name, value := range map[string]any{"failSeverities": []string{}, "databaseMaximumAgeHours": 999, "environment": []string{"ALLOW_UNSAFE=true"}} {
		var fields map[string]any
		if err := json.Unmarshal(payload, &fields); err != nil {
			t.Fatal(err)
		}
		fields[name] = value
		altered, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		signed, err := SignScanner(altered, key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := VerifyScanner(root, &signed); err == nil {
			t.Fatalf("publisher package overrode Code Polishy policy through %s", name)
		}
	}
}

func scannerReleaseLockFixture(t *testing.T, root string) Lock {
	t.Helper()
	manifest, _, err := ReadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	return indexedManifestLock(manifest)
}

func TestScannerAdoptionRequiresTheExecutingReleaseEvenWithAFreshExpectedLock(t *testing.T) {
	root, key, manifest := scannerSigningFixture(t)
	for _, mode := range []string{"different-release", "source-checkout"} {
		t.Run(mode, func(t *testing.T) {
			selected := scannerReleaseLockFixture(t, root)
			policyRoot := root
			if mode == "different-release" {
				selected.ReleaseDigest = strings.Repeat("b", 64)
			} else {
				path := filepath.Join(root, ManifestFilename)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Error(err)
					}
				})
			}
			repo := t.TempDir()
			if err := WriteLock(repo, selected); err != nil {
				t.Fatal(err)
			}
			if err := WriteScannerLock(repo, policyRoot, selected, signScannerFixture(t, key, manifest)); err == nil {
				t.Fatal("scanner adoption accepted a different executing release")
			}
			current, _, err := ReadLock(repo)
			if err != nil || !sameCapabilityLock(current, selected) {
				t.Fatalf("rejected adoption changed lock: %+v %v", current, err)
			}
		})
	}
}
