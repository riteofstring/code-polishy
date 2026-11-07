package release

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/strictjson"
	policyschema "github.com/riteofstring/code-polishy/schema"
)

const scannerSignatureDomain = "code-polishy-scanner-manifest/v1"
const MaximumScannerBytes = 8 << 10
const MaximumScannerPayloadBytes = 4 << 10

type SignedScanner struct {
	Payload   []byte `json:"payload"`
	Signature []byte `json:"signature"`
}

type ScannerManifest struct {
	Version      string          `json:"packageVersion"`
	Contract     string          `json:"contract"`
	TrivyVersion string          `json:"version"`
	SourceImage  string          `json:"sourceImage"`
	ReassessBy   time.Time       `json:"reassessBy"`
	OpenVEX      json.RawMessage `json:"openVex"`
	Evidence     string          `json:"evidence"`
}

func verifyScannerSignature(root string, signed *SignedScanner) (string, error) {
	directory, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer directory.Close()
	key, err := readCapabilityFile(directory, "artifact-security/publisher.pem", 4096)
	if err != nil {
		return "", err
	}
	block, rest := pem.Decode(key)
	if block == nil || len(rest) != 0 {
		return "", errors.New("invalid scanner publisher public key")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	public, ok := parsed.(ed25519.PublicKey)
	if err != nil || !ok || signed == nil || len(signed.Payload) > MaximumScannerPayloadBytes || !ed25519.Verify(public, scannerMessage(signed.Payload), signed.Signature) {
		return "", errors.New("scanner manifest has no approved publisher signature")
	}
	contract, err := readCapabilityFile(directory, "artifact-security/scanner-contract.txt", 64)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(contract)), nil
}

func VerifyScanner(root string, signed *SignedScanner) (ScannerManifest, error) {
	contract, err := verifyScannerSignature(root, signed)
	if err != nil {
		return ScannerManifest{}, err
	}
	if err := policyschema.ValidateUniqueJSON(signed.Payload, 16); err != nil {
		return ScannerManifest{}, err
	}
	var manifest ScannerManifest
	if err := strictjson.Decode(signed.Payload, &manifest); err != nil {
		return manifest, err
	}
	return manifest, manifest.validate(contract)
}

func (manifest ScannerManifest) validate(contract string) error {
	switch {
	case !scannerContractPattern.MatchString(contract) || manifest.Contract != contract:
		return errors.New("unsupported scanner execution contract; use a compatible Code Polishy release or reassessed package")
	case !versionPattern.MatchString(manifest.Version) || !versionPattern.MatchString(manifest.TrivyVersion):
		return errors.New("scanner package and binary versions must be exact")
	case !strings.HasPrefix(manifest.SourceImage, "docker.io/aquasec/trivy@sha256:") || !digestPattern.MatchString(strings.TrimPrefix(manifest.SourceImage, "docker.io/aquasec/trivy@sha256:")):
		return errors.New("scanner image must identify an immutable official Trivy image index")
	case !manifest.ReassessBy.After(time.Now().UTC()):
		return errors.New("scanner assessments expired; use scanner-update to adopt a reviewed revision")
	case len(manifest.Evidence) < 20 || len(manifest.OpenVEX) == 0:
		return errors.New("scanner manifest requires assessment evidence and explicit OpenVEX")
	}

	return nil
}

func scannerMessage(payload []byte) []byte {
	return append([]byte(scannerSignatureDomain+"\x00"), payload...)
}

func SignScanner(payload, key []byte) (SignedScanner, error) {
	block, _ := pem.Decode(key)
	if block == nil {
		return SignedScanner{}, errors.New("signing requires a PKCS8 Ed25519 private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	private, ok := parsed.(ed25519.PrivateKey)
	if err != nil || !ok {
		return SignedScanner{}, errors.New("signing requires a PKCS8 Ed25519 private key")
	}
	return SignedScanner{Payload: payload, Signature: ed25519.Sign(private, scannerMessage(payload))}, nil
}

func AcquireScanner(ctx context.Context, root, source, digest string) (*SignedScanner, ScannerManifest, error) {
	url, err := parseReleaseHTTPSURL(source)
	if err != nil {
		return nil, ScannerManifest{}, err
	}
	data, err := downloadReleaseBytes(ctx, releaseHTTPClient(), url, MaximumScannerBytes)
	if err != nil {
		return nil, ScannerManifest{}, err
	}
	if !digestPattern.MatchString(digest) || digestBytes(data) != digest {
		return nil, ScannerManifest{}, errors.New("scanner package checksum mismatch")
	}
	if err := policyschema.ValidateUniqueJSON(data, 16); err != nil {
		return nil, ScannerManifest{}, err
	}
	var signed SignedScanner
	if err := strictjson.Decode(data, &signed); err != nil {
		return nil, ScannerManifest{}, err
	}
	manifest, err := VerifyScanner(root, &signed)
	return &signed, manifest, err
}

func WriteScannerLock(repoRoot, policyRoot string, incoming Lock, selected *SignedScanner) error {
	unlock, err := LockRepository(repoRoot)
	if err != nil {
		return err
	}
	defer unlock()
	current, present, err := ReadLock(repoRoot)
	if err != nil {
		return err
	}
	if !present || !sameCapabilityLock(current, incoming) {
		return errors.New("release lock changed during scanner update")
	}
	incoming.Scanner = selected
	if _, err := VerifyScanner(policyRoot, selected); err != nil {
		return err
	}
	return WriteLock(repoRoot, incoming)
}

func LockRepository(repoRoot string) (func(), error) {
	root, err := openCapabilityUpgradeRoot(repoRoot, true)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return acquireCapabilityUpgradeLock(root)
}
