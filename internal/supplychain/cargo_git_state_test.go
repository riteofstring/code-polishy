package supplychain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestGitEvidenceStateRetainsBothCargoAndPythonLocks(t *testing.T) {
	t.Parallel()
	cargo := newCargoGitEvidenceFixture(t, true)
	cargo.write(t)
	python := newGitEvidenceFixture(t, true)
	python.key = cargo.key
	python.statement.Scope = "python/uv.lock"
	refreshGitEvidenceFixture(&python)
	python.write(t)
	for _, pair := range [][2]string{{"uv.lock", "python/uv.lock"}, {"evidence/git.dsse.json", "evidence/python.dsse.json"}} {
		data, err := os.ReadFile(filepath.Join(python.repo.Root, pair[0]))
		if err != nil {
			t.Fatal(err)
		}
		writeSupplyFile(t, cargo.repo.Root, pair[1], string(data))
	}
	cargo.repo.Config.SupplyChain.GitEvidence.Attestations = append(cargo.repo.Config.SupplyChain.GitEvidence.Attestations, policy.GitEvidenceAttestation{Scope: "python/uv.lock", Provider: "ci", Path: "evidence/python.dsse.json"})
	state, err := ReadGitEvidenceState(cargo.repo, python.now)
	if err != nil || len(state.Receipts) != 2 || state.Receipts[0].Scope != "Cargo.lock" || state.Receipts[1].Scope != "python/uv.lock" {
		t.Fatalf("mixed ecosystem evidence state = %+v, %v", state, err)
	}
	writeSupplyFile(t, cargo.repo.Root, "evidence/python.dsse.json", "invalid")
	changed, err := ReadGitEvidenceState(cargo.repo, python.now)
	if err != nil || changed.IdentitySHA256 == state.IdentitySHA256 || len(changed.Receipts) != 1 || changed.Receipts[0].Scope != "Cargo.lock" {
		t.Fatalf("invalid Python evidence discarded valid Cargo evidence: %+v, %v", changed, err)
	}
}

func TestGitEvidenceStateSharesTheInputBoundAcrossCargoAndPython(t *testing.T) {
	t.Parallel()
	fixture := newCargoGitEvidenceFixture(t, true)
	writeSupplyFile(t, fixture.repo.Root, "python/uv.lock", "[[package]]\nname='local'\nversion='0.1.0'\nsource={editable='.'}\n")
	for index := range 511 {
		directory := fmt.Sprintf("workspace-%03d/", index)
		writeSupplyFile(t, fixture.repo.Root, directory+"Cargo.lock", "version=4\n[[package]]\nname='local'\nversion='0.1.0'\n")
	}
	state, err := ReadGitEvidenceState(fixture.repo, fixture.now)
	if err == nil || !strings.Contains(err.Error(), "input limit") || len(state.Receipts) != 0 {
		t.Fatalf("mixed lock inventory exceeded its combined bound: %+v, %v", state, err)
	}
}

func TestCargoGitEvidenceRejectsUnsupportedInventoryButLeavesOptInOptional(t *testing.T) {
	t.Parallel()
	fixture := newCargoGitEvidenceFixture(t, true)
	fixture.write(t)
	data, err := os.ReadFile(filepath.Join(fixture.repo.Root, "Cargo.lock"))
	if err != nil {
		t.Fatal(err)
	}
	lock := strings.ReplaceAll(string(data), "registry+https://github.com/rust-lang/crates.io-index", "registry+https://private.example.test/index")
	writeSupplyFile(t, fixture.repo.Root, "Cargo.lock", lock)
	state, err := ReadGitEvidenceState(fixture.repo, fixture.now)
	if err == nil || strings.Contains(err.Error(), "private.example.test") || len(state.Receipts) != 0 {
		t.Fatalf("unsupported Cargo inventory satisfied complete evidence: %+v, %v", state, err)
	}
	fixture.repo.Config.SupplyChain.GitEvidence.Required = false
	state, err = ReadGitEvidenceState(fixture.repo, fixture.now)
	if err != nil || state.IdentitySHA256 != "" || len(state.Receipts) != 0 {
		t.Fatalf("disabled Cargo evidence became required or credited: %+v, %v", state, err)
	}
}
