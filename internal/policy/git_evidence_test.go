package policy

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGitEvidenceConfigurationDeclaresExactTrustWithoutScheduling(t *testing.T) {
	t.Parallel()
	declaration := gitEvidenceConfiguration(t)
	config, err := Parse(withGitEvidence(t, declaration), "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config.SupplyChain.GitEvidence, declaration) {
		t.Fatalf("configured trust changed: %+v", config.SupplyChain.GitEvidence)
	}
	baseline, err := Parse([]byte(minimalConfig()), "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config.Checks, baseline.Checks) || !reflect.DeepEqual(config.Tests.Suites, baseline.Tests.Suites) {
		t.Fatal("declaring a signed CI artifact scheduled repository execution")
	}
}

func TestGitEvidenceConfigurationRejectsAmbiguousAndUnsafeTrust(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*GitEvidence){
		"unknown provider":   func(c *GitEvidence) { c.Attestations[0].Provider = "unknown" },
		"duplicate provider": func(c *GitEvidence) { c.Providers = append(c.Providers, c.Providers[0]) },
		"duplicate scope":    func(c *GitEvidence) { c.Attestations = append(c.Attestations, c.Attestations[0]) },
		"unsigned provider":  func(c *GitEvidence) { c.Providers[0].Kind = "checked-in-json" },
		"bad key":            func(c *GitEvidence) { c.Providers[0].PublicKey = "bad" },
		"key whitespace":     func(c *GitEvidence) { c.Providers[0].PublicKey += "\n" },
		"HTTP issuer":        func(c *GitEvidence) { c.Providers[0].Issuer = "http://ci.example.test" },
		"credential issuer":  func(c *GitEvidence) { c.Providers[0].Issuer = "https://secret@ci.example.test" },
		"query issuer":       func(c *GitEvidence) { c.Providers[0].Issuer += "?secret=value" },
		"escaping issuer":    func(c *GitEvidence) { c.Providers[0].Issuer += "/../other" },
		"unversioned policy": func(c *GitEvidence) { c.Providers[0].PolicySHA256 = "current" },
		"no scanner":         func(c *GitEvidence) { c.Providers[0].Scanners = nil },
		"scanner range":      func(c *GitEvidence) { c.Providers[0].Scanners[0].Version = "^1.0.0" },
		"no scanner digest":  func(c *GitEvidence) { c.Providers[0].Scanners[0].SHA256 = "" },
		"escaping artifact":  func(c *GitEvidence) { c.Attestations[0].Path = "../secret.json" },
		"glob artifact":      func(c *GitEvidence) { c.Attestations[0].Path = "evidence/*.json" },
		"absolute scope":     func(c *GitEvidence) { c.Attestations[0].Scope = "/uv.lock" },
		"too many providers": func(c *GitEvidence) { c.Providers = make([]GitEvidenceProvider, 33) },
	} {
		t.Run(name, func(t *testing.T) {
			declaration := gitEvidenceConfiguration(t)
			mutate(&declaration)
			if _, err := Parse(withGitEvidence(t, declaration), ""); err == nil {
				t.Fatal("unsafe trust declaration was accepted")
			}
		})
	}
}

func TestGitEvidenceFailuresCannotBeSuppressedByGeneralExceptions(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	for _, check := range []string{"supplyChain.gitEvidence", "supplyChain.gitVulnerability"} {
		finding := Finding{Check: check, Path: "uv.lock", Subject: "exact", Message: "rejected"}
		exception := Exception{Check: check, Path: finding.Path, Subject: finding.Subject, Expires: Date{Time: now.AddDate(0, 0, 1)}}
		kept, suppressed := ApplyExceptions([]Finding{finding}, []Exception{exception}, now)
		if len(kept) != 1 || len(suppressed) != 0 {
			t.Fatalf("Git admission evidence was waived: %+v, %+v", kept, suppressed)
		}
	}
}

func gitEvidenceConfiguration(t *testing.T) GitEvidence {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return GitEvidence{
		Providers:    []GitEvidenceProvider{{Name: "private-ci", Kind: "ed25519-ci/v1", Issuer: "https://ci.example.test/attestor", PublicKey: base64.StdEncoding.EncodeToString(public), PolicySHA256: strings.Repeat("1", 64), Scanners: []GitEvidenceScanner{{Name: "scanner", Version: "1.2.3", SHA256: strings.Repeat("2", 64)}}}},
		Attestations: []GitEvidenceAttestation{{Scope: "uv.lock", Provider: "private-ci", Path: "evidence/git.dsse.json"}},
	}
}

func withGitEvidence(t *testing.T, declaration GitEvidence) []byte {
	t.Helper()
	var config map[string]any
	if err := json.Unmarshal([]byte(minimalConfig()), &config); err != nil {
		t.Fatal(err)
	}
	config["supplyChain"] = map[string]any{"gitEvidence": declaration}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
