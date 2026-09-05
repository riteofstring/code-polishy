package engine

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestGitEvidenceReportsRetainVerifiedIdentityAcrossJSONAndSARIF(t *testing.T) {
	t.Parallel()
	engine := gitEvidenceEngine(t)
	findings, evidence, _ := engine.onlineSupplyChain(t.Context(), []string{"uv.lock"}, nil)
	if len(findings) != 0 || len(evidence) != 1 {
		t.Fatalf("online evidence = %+v, %+v", findings, evidence)
	}
	report := engine.normalizeReport(Report{Command: "supply-chain", GitEvidence: evidence})
	evidence[0].RetrievedAt = evidence[0].RetrievedAt.Add(time.Minute)
	report = engine.combine(report, Report{GitEvidence: evidence})
	report.Command = "supply-chain"
	if len(report.GitEvidence) != 1 || !report.GitEvidence[0].RetrievedAt.Equal(evidence[0].RetrievedAt) {
		t.Fatal("combining reports must retain the latest retrieval of the same verified artifact")
	}
	encoded, err := JSONReport(report)
	if err != nil {
		t.Fatal(err)
	}
	decoded := Report{}
	if err := json.Unmarshal(encoded, &decoded); err != nil || len(decoded.GitEvidence) != 1 || decoded.GitEvidence[0].SHA256 != evidence[0].SHA256 {
		t.Fatalf("JSON evidence lost its identity: %+v, %v", decoded.GitEvidence, err)
	}
	sarif, err := SARIF(report)
	if err != nil || !strings.Contains(string(sarif), evidence[0].SHA256) || !strings.Contains(string(sarif), `"gitEvidence"`) {
		t.Fatalf("SARIF evidence lost its identity: %v", err)
	}
	report.GitEvidence[0].SHA256 = "invalid"
	if _, err := JSONReport(report); err == nil {
		t.Fatal("invalid evidence identity satisfied the shipped report schema")
	}
}

func TestGitEvidenceChangesDuringConfiguredSecurityChecksPreventAcceptance(t *testing.T) {
	t.Parallel()
	engine := gitEvidenceEngine(t)
	engine.Repository.Config.Checks = []policy.Command{{Name: "security", Argv: []string{"assess"}, Cwd: ".", RunOn: []string{"security"}, TimeoutSeconds: 30}}
	engine.Runner = gitEvidenceChangingRunner{}
	findings, receipts, _ := engine.onlineSupplyChain(t.Context(), []string{"uv.lock"}, nil)
	if len(findings) != 1 || findings[0].Check != "supplyChain.gitEvidence" || findings[0].Subject != "verification-state" || len(receipts) != 0 {
		t.Fatalf("changed evidence was accepted after a configured security check: %+v, %+v", findings, receipts)
	}
}

type gitEvidenceChangingRunner struct{}

func (gitEvidenceChangingRunner) Run(_ context.Context, root string, _ policy.Command) error {
	file, err := os.OpenFile(filepath.Join(root, ".code-polishy-reports/git-evidence/proof.json"), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = file.WriteString("\n")
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func TestGateRunRejectsChangedUntrackedGitEvidence(t *testing.T) {
	t.Parallel()
	engine := gitEvidenceEngine(t)
	head, err := engine.Repository.CleanHead()
	if err != nil {
		t.Fatal(err)
	}
	commands := []MergeGateExecutionCommand{{Category: gaterun.Check, Scope: "repository", Cost: "quick", Command: policy.Command{Name: "evidence", Argv: []string{"verify"}, Cwd: ".", TimeoutSeconds: 30}}}
	identity, err := gateRunIdentity(engine, gaterun.MergeGate, "main", head, head, "recommended", commands, testGateRunBehaviorReview(), nil)
	if err != nil || identity.GitEvidenceSHA256 == "" {
		t.Fatalf("gate identity omitted evidence: %+v, %v", identity, err)
	}
	storeGitEvidenceGate(t, engine.Repository.Root, identity)
	if _, err := gaterun.LoadReport(engine.Repository.Root, identity); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(engine.Repository.Root, ".code-polishy-reports/git-evidence/proof.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, engine.Repository.Root, ".code-polishy-reports/git-evidence/proof.json", string(append(data, '\n')), 0o600)
	current, err := engine.Repository.CleanHead()
	if err != nil || current != head {
		t.Fatalf("external evidence changed the committed candidate: %s, %v", current, err)
	}
	changed, err := gateRunIdentity(engine, gaterun.MergeGate, "main", head, head, "recommended", commands, testGateRunBehaviorReview(), nil)
	if err != nil || changed.GitEvidenceSHA256 == identity.GitEvidenceSHA256 {
		t.Fatalf("changed evidence retained gate identity: %+v, %v", changed, err)
	}
	if _, err := gaterun.LoadReport(engine.Repository.Root, changed); err == nil {
		t.Fatal("changed external evidence reused an earlier passed gate")
	}
	controller := &gateRunController{candidate: head, gitEvidenceSHA256: identity.GitEvidenceSHA256}
	if err := controller.candidateIntegrityError(engine); err == nil || !strings.Contains(err.Error(), "evidence changed") {
		t.Fatalf("evidence changed during a gate without failing publication: %v", err)
	}
}

func storeGitEvidenceGate(t *testing.T, root string, identity gaterun.Identity) {
	t.Helper()
	run, err := gaterun.Start(gaterun.StartOptions{RepositoryRoot: root, Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	log, err := run.OpenCommandLog(0, gaterun.LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := log.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run.RecordAttempt(0, gaterun.AttemptInput{Status: gaterun.Passed}, result); err != nil {
		t.Fatal(err)
	}
	if _, err := run.Finalize(gaterun.FinalizeOptions{Status: gaterun.RunPassed, BehaviorReview: identity.BehaviorReview}); err != nil {
		t.Fatal(err)
	}
}

func gitEvidenceEngine(t *testing.T) *Engine {
	t.Helper()
	root := t.TempDir()
	commit := strings.Repeat("a", 40)
	lock := fmt.Sprintf("[[package]]\nname = \"private-kit\"\nversion = \"1.0.0\"\nsource = { git = \"ssh://git@private.example.test/kit.git?rev=%s#%s\" }\n", commit, commit)
	writeEngineFile(t, root, "uv.lock", lock, 0o600)
	writeEngineFile(t, root, ".gitignore", ".code-polishy-reports/\n", 0o600)
	initializeEngineGitRepository(t, root)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	provider := policy.GitEvidenceProvider{Name: "ci", Kind: "ed25519-ci/v1", Issuer: "https://ci.example.test/attestor", PublicKey: base64.StdEncoding.EncodeToString(public), PolicySHA256: strings.Repeat("b", 64), Scanners: []policy.GitEvidenceScanner{{Name: "private-scanner", Version: "1.0.0", SHA256: strings.Repeat("c", 64)}}}
	statement := engineGitStatement(provider, commit, lock)
	payload, err := json.Marshal(statement)
	if err != nil {
		t.Fatal(err)
	}
	media := "application/vnd.code-polishy.git-evidence.v1+json"
	message := []byte(fmt.Sprintf("DSSEv1 %d %s %d %s", len(media), media, len(payload), payload))
	envelope := map[string]any{"payloadType": media, "payload": base64.StdEncoding.EncodeToString(payload), "signatures": []map[string]string{{"keyid": "ci", "sig": base64.StdEncoding.EncodeToString(ed25519.Sign(private, message))}}}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	path := ".code-polishy-reports/git-evidence/proof.json"
	writeEngineFile(t, root, path, string(encoded), 0o600)
	config := policy.Config{SupplyChain: policy.SupplyChain{MinimumReleaseAgeDays: 30, AllowedLicenses: []string{"MIT"}, GitEvidence: policy.GitEvidence{Providers: []policy.GitEvidenceProvider{provider}, Attestations: []policy.GitEvidenceAttestation{{Scope: "uv.lock", Provider: "ci", Path: path}}}}}
	return &Engine{Repository: repository.Repository{Root: root, PolicyRoot: root, Config: config}, Runner: &recordingEngineRunner{}}
}

func engineGitStatement(provider policy.GitEvidenceProvider, commit, lock string) map[string]any {
	now := time.Now().UTC().Add(-time.Minute)
	source := "git+ssh://private.example.test/kit.git@" + commit
	inventory := fmt.Sprintf(`[{"ecosystem":"git","name":"private-kit","version":"1.0.0","source":%q}]`, source)
	return map[string]any{
		"protocol": "git-evidence/v1", "issuer": provider.Issuer, "policySha256": provider.PolicySHA256, "scope": "uv.lock",
		"lockSha256": gaterun.ContentSHA256([]byte(lock)), "inventorySha256": gaterun.ContentSHA256([]byte(inventory)), "issuedAt": now, "expiresAt": now.Add(time.Hour),
		"subjects": []map[string]any{{"ecosystem": "git", "name": "private-kit", "version": "1.0.0", "repository": "git+ssh://private.example.test/kit.git", "commit": commit, "subdirectory": "", "treeSha256": strings.Repeat("d", 64), "observation": map[string]any{"kind": "first-observed", "record": provider.Issuer + "/records/kit", "timestamp": now.AddDate(0, 0, -90)}}},
		"scan":     map[string]any{"coverage": "complete", "target": "git-contents-and-resolved-lock/v1", "scanner": provider.Scanners[0], "completedAt": now, "advisoryVersion": "snapshot-v1", "advisoryUpdated": now.Add(-time.Hour), "vulnerabilities": []any{}, "licenses": []map[string]string{{"ecosystem": "git", "name": "private-kit", "version": "1.0.0", "source": source, "expression": "MIT"}}},
	}
}
