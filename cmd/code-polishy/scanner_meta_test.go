package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/riteofstring/code-polishy/internal/release"
)

func scannerCLIFixture(t *testing.T) (invocation, []byte, release.Lock, []byte) {
	t.Helper()
	root, repo := installedRelease(t, strings.Repeat("a", 40)), t.TempDir()
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
	manifest := release.ScannerManifest{Version: "1.0.0", Contract: "code-polishy-trivy/v1", TrivyVersion: "0.76.1", SourceImage: "docker.io/aquasec/trivy@sha256:" + strings.Repeat("a", 64), ReassessBy: time.Now().Add(time.Hour), OpenVEX: json.RawMessage(`{"@context":"https://openvex.dev/ns/v0.2.0","@id":"https://example.test/fixture","author":"Fixture","timestamp":"2026-01-01T00:00:00Z","version":1,"statements":[]}`), Evidence: "Disposable fixture signs no production assessment."}
	payload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := release.SignScanner(payload, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: secret}))
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	policyManifest, _, err := release.ReadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"artifact-security/publisher.pem", "artifact-security/scanner-contract.txt"} {
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		policyManifest.Entries = append(policyManifest.Entries, release.Entry{Path: relative, SHA256: hex.EncodeToString(digest[:])})
	}
	slices.SortFunc(policyManifest.Entries, func(left, right release.Entry) int { return strings.Compare(left.Path, right.Path) })
	policyManifest.EntryCount = len(policyManifest.Entries)
	policyManifest.ContentDigest = release.EntriesDigest(policyManifest.Entries)
	encoded, err := json.Marshal(policyManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, release.ManifestFilename), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	lock := publishedTestLock(policyManifest)
	if err := release.WriteLock(repo, lock); err != nil {
		t.Fatal(err)
	}
	return invocation{repoRoot: repo, policyRoot: root}, envelope, lock, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: secret})
}

func TestScannerUpdatePreviewsWithoutMutationAndAppliesExactAuthenticatedCandidate(t *testing.T) {
	invocation, envelope, original, _ := scannerCLIFixture(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(envelope) }))
	defer server.Close()
	transport := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	defer func() { http.DefaultTransport = transport }()
	sum := sha256.Sum256(envelope)
	invocation.arguments = []string{"--manifest", server.URL, "--sha256", hex.EncodeToString(sum[:])}
	if status := run(append([]string{"--repo-root", invocation.repoRoot, "--policy-root", invocation.policyRoot, "scanner-update"}, invocation.arguments...)); status != 0 {
		t.Fatalf("preview status %d", status)
	}
	after, _, err := release.ReadLock(invocation.repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if string(release.RenderLock(after)) != string(release.RenderLock(original)) {
		t.Fatal("preview changed lock")
	}
	invocation.arguments = append(invocation.arguments, "--apply")
	if status := run(append([]string{"--repo-root", invocation.repoRoot, "--policy-root", invocation.policyRoot, "scanner-update"}, invocation.arguments...)); status != 0 {
		t.Fatalf("apply status %d", status)
	}
	after, _, err = release.ReadLock(invocation.repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if after.Scanner == nil {
		t.Fatal("apply did not record signed candidate")
	}
	if _, err := release.VerifyScanner(invocation.policyRoot, after.Scanner); err != nil {
		t.Fatal(err)
	}
	before := release.RenderLock(after)
	invocation.arguments[3] = strings.Repeat("b", 64)
	if status := run(append([]string{"--repo-root", invocation.repoRoot, "--policy-root", invocation.policyRoot, "scanner-update"}, invocation.arguments...)); status == 0 {
		t.Fatal("wrong digest was accepted")
	}
	after, _, err = release.ReadLock(invocation.repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(release.RenderLock(after)) {
		t.Fatal("failed adoption changed lock")
	}
}

func TestScannerUpdateRejectsAReleaseCutoverDuringAcquisition(t *testing.T) {
	invocation, envelope, original, _ := scannerCLIFixture(t)
	concurrent := original
	concurrent.CodePolishyVersion = "9.9.10"
	concurrent.ReleaseDigest = strings.Repeat("b", 64)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unlock, err := release.LockRepository(invocation.repoRoot)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer unlock()
		if err := release.WriteLock(invocation.repoRoot, concurrent); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(envelope)
	}))
	defer server.Close()
	transport := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	defer func() { http.DefaultTransport = transport }()
	sum := sha256.Sum256(envelope)
	invocation.arguments = []string{"--manifest", server.URL, "--sha256", hex.EncodeToString(sum[:]), "--apply"}
	if status := handleScannerUpdate(invocation); status == 0 {
		t.Fatal("scanner adoption crossed a concurrent release cutover")
	}
	current, present, err := release.ReadLock(invocation.repoRoot)
	if err != nil || !present || string(release.RenderLock(current)) != string(release.RenderLock(concurrent)) {
		t.Fatalf("scanner adoption changed the concurrent release lock: %+v %v", current, err)
	}
}

func TestScannerPublicationCanReplaceAnExpiredBootstrapButRejectsExpiredCandidates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		time.Sleep(time.Until(time.Date(2026, time.November, 14, 0, 0, 0, 0, time.UTC)))
		invocation, envelope, _, private := scannerCLIFixture(t)
		if err := os.WriteFile(filepath.Join(invocation.policyRoot, "artifact-security/scanner.json"), envelope, 0600); err != nil {
			t.Fatal(err)
		}
		var bootstrap release.SignedScanner
		if err := json.Unmarshal(envelope, &bootstrap); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Hour)
		if _, err := release.VerifyScanner(invocation.policyRoot, &bootstrap); err == nil {
			t.Fatal("bootstrap did not expire")
		}
		directory := t.TempDir()
		key := filepath.Join(directory, "private.pem")
		if err := os.WriteFile(key, private, 0600); err != nil {
			t.Fatal(err)
		}
		for _, expired := range []bool{false, true} {
			var candidate release.ScannerManifest
			if err := json.Unmarshal(bootstrap.Payload, &candidate); err != nil {
				t.Fatal(err)
			}
			candidate.Version = "1.0.1"
			name := "expired"
			if !expired {
				candidate.ReassessBy = time.Now().Add(time.Hour)
				name = "fresh"
			}
			payload, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			input, output := filepath.Join(directory, name+"-input.json"), filepath.Join(directory, name+"-signed.json")
			if err := os.WriteFile(input, payload, 0600); err != nil {
				t.Fatal(err)
			}
			err = signScannerFile(invocation.policyRoot, input, key, output)
			if expired {
				if err == nil {
					t.Fatal("publication signer accepted expired candidate")
				}
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatal("expired candidate produced publication output")
				}
				continue
			}
			if err != nil {
				t.Fatalf("expired bootstrap blocked fresh publication candidate: %v", err)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			var signed release.SignedScanner
			if err := json.Unmarshal(data, &signed); err != nil {
				t.Fatal(err)
			}
			if _, err := release.VerifyScanner(invocation.policyRoot, &signed); err != nil {
				t.Fatal(err)
			}
		}
	})
}
