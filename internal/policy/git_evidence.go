package policy

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

type GitEvidence struct {
	Required     bool                     `json:"required,omitempty"`
	Providers    []GitEvidenceProvider    `json:"providers,omitempty"`
	Attestations []GitEvidenceAttestation `json:"attestations,omitempty"`
}

type GitEvidenceProvider struct {
	Name         string               `json:"name"`
	Kind         string               `json:"kind"`
	Issuer       string               `json:"issuer"`
	PublicKey    string               `json:"publicKey"`
	PolicySHA256 string               `json:"policySha256"`
	Scanners     []GitEvidenceScanner `json:"scanners"`
}

type GitEvidenceScanner struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type GitEvidenceAttestation struct {
	Scope    string `json:"scope"`
	Provider string `json:"provider"`
	Path     string `json:"path"`
}

func ValidateGitEvidence(configuration GitEvidence) error {
	if len(configuration.Providers) > 32 || len(configuration.Attestations) > 128 {
		return fmt.Errorf("supplyChain.gitEvidence exceeds 32 providers or 128 attestations")
	}
	providers := map[string]bool{}
	for _, provider := range configuration.Providers {
		if err := validateGitEvidenceProvider(provider, providers); err != nil {
			return err
		}
		providers[provider.Name] = true
	}
	scopes, paths := map[string]bool{}, map[string]bool{}
	for _, attestation := range configuration.Attestations {
		if !providers[attestation.Provider] {
			return fmt.Errorf("git evidence attestation references an unknown provider")
		}
		if !gitEvidencePath(attestation.Scope) || !gitEvidencePath(attestation.Path) {
			return fmt.Errorf("git evidence scope and artifact must be exact contained paths")
		}
		if scopes[attestation.Scope] || paths[attestation.Path] {
			return fmt.Errorf("git evidence requires one distinct artifact per lock scope")
		}
		scopes[attestation.Scope], paths[attestation.Path] = true, true
	}
	return nil
}

func validateGitEvidenceProvider(provider GitEvidenceProvider, names map[string]bool) error {
	if !identifierPattern.MatchString(provider.Name) || len(provider.Name) > 128 || names[provider.Name] {
		return fmt.Errorf("git evidence provider requires a unique bounded canonical name")
	}
	if provider.Kind != "ed25519-ci/v1" || !gitEvidenceIssuer(provider.Issuer) || !gitEvidenceSHA256(provider.PolicySHA256) {
		return fmt.Errorf("git evidence provider requires ed25519-ci/v1, a credential-free HTTPS issuer, and an exact policy identity")
	}
	if len(provider.PublicKey) != 44 {
		return fmt.Errorf("git evidence provider requires one canonical base64 Ed25519 public key")
	}
	key, err := base64.StdEncoding.Strict().DecodeString(provider.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize || base64.StdEncoding.EncodeToString(key) != provider.PublicKey {
		return fmt.Errorf("git evidence provider requires one canonical base64 Ed25519 public key")
	}
	return validateGitEvidenceScanners(provider.Scanners)
}

func validateGitEvidenceScanners(scanners []GitEvidenceScanner) error {
	if len(scanners) == 0 || len(scanners) > 16 {
		return fmt.Errorf("git evidence provider requires 1..16 exact scanner identities")
	}
	seen := map[string]bool{}
	for _, scanner := range scanners {
		if !validGitEvidenceScanner(scanner) {
			return fmt.Errorf("git evidence scanner requires bounded exact name and version")
		}
		identity := scanner.Name + "\x00" + scanner.Version + "\x00" + scanner.SHA256
		if seen[identity] {
			return fmt.Errorf("git evidence repeats a scanner identity")
		}
		seen[identity] = true
	}
	return nil
}

func gitEvidenceIssuer(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && gitEvidenceText(value, 2048) && parsed.Scheme == "https" &&
		parsed.Hostname() != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && gitEvidenceIssuerPath(parsed)
}

func gitEvidenceIssuerPath(parsed *url.URL) bool {
	return parsed.Opaque == "" && !parsed.ForceQuery && parsed.RawPath == "" &&
		(parsed.Path == "" || path.Clean(parsed.Path) == parsed.Path)
}

func validGitEvidenceScanner(scanner GitEvidenceScanner) bool {
	return gitEvidenceText(scanner.Name, 128) && len(scanner.Version) <= 128 && artifactVersionPattern.MatchString(scanner.Version) && gitEvidenceSHA256(scanner.SHA256)
}

func gitEvidencePath(value string) bool {
	return gitEvidenceText(value, 4096) && value == path.Clean(value) && value != "." &&
		value != ".." && !strings.HasPrefix(value, "../") && !path.IsAbs(value) && !strings.ContainsAny(value, "\\:*?[]{}")
}

func gitEvidenceText(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) &&
		strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}

func gitEvidenceSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && value == strings.ToLower(value)
}
