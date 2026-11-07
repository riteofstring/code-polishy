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
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/release"
)

func scannerCLIFixture(t *testing.T) (invocation, []byte, release.Lock) {
	t.Helper()
	root, repo := t.TempDir(), t.TempDir()
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
	lock := release.Lock{LockVersion: release.LockVersion, CodePolishyVersion: "test", ReleaseDigest: strings.Repeat("a", 64), Features: []string{"javascript-bundle"}, Publication: &release.LockPublication{IndexURL: "https://example.test/index", IndexSHA256: strings.Repeat("a", 64)}}
	for _, host := range []string{"darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "windows-x64"} {
		lock.Publication.Archives = append(lock.Publication.Archives, release.LockedArchive{Host: host, URL: "https://example.test/" + host, SHA256: strings.Repeat("a", 64), Size: 1})
	}
	if err := release.WriteLock(repo, lock); err != nil {
		t.Fatal(err)
	}
	return invocation{repoRoot: repo, policyRoot: root}, envelope, lock
}

func TestScannerUpdatePreviewsWithoutMutationAndAppliesExactAuthenticatedCandidate(t *testing.T) {
	invocation, envelope, original := scannerCLIFixture(t)
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
