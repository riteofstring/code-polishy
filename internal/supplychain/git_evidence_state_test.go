package supplychain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitEvidenceStateBindsFreshnessContentsAndManagedCIArtifacts(t *testing.T) {
	t.Parallel()
	fixture := newGitEvidenceFixture(t, true)
	fixture.write(t)
	path := ".code-polishy-reports/git-evidence/private.dsse.json"
	data, err := os.ReadFile(filepath.Join(fixture.repo.Root, "evidence/git.dsse.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeSupplyFile(t, fixture.repo.Root, path, string(data))
	fixture.repo.Config.SupplyChain.GitEvidence.Attestations[0].Path = path
	first, err := ReadGitEvidenceState(fixture.repo, fixture.now)
	if err != nil || first.IdentitySHA256 == "" || len(first.Receipts) != 1 || first.Receipts[0].Path != path {
		t.Fatalf("managed evidence state = %+v, %v", first, err)
	}
	same, err := ReadGitEvidenceState(fixture.repo, fixture.now.Add(time.Minute))
	if err != nil || same.IdentitySHA256 != first.IdentitySHA256 || len(same.Receipts) != 1 || !same.Receipts[0].RetrievedAt.Equal(fixture.now.Add(time.Minute)) {
		t.Fatalf("fresh unchanged evidence changed identity: %+v, %v", same, err)
	}
	expired, err := ReadGitEvidenceState(fixture.repo, fixture.now.Add(2*time.Hour))
	if err != nil || expired.IdentitySHA256 == first.IdentitySHA256 || len(expired.Receipts) != 0 {
		t.Fatalf("expired evidence retained reusable identity: %+v, %v", expired, err)
	}
	writeSupplyFile(t, fixture.repo.Root, path, strings.Replace(string(data), "payloadType", "PayloadType", 1))
	changed, err := ReadGitEvidenceState(fixture.repo, fixture.now)
	if err != nil || changed.IdentitySHA256 == first.IdentitySHA256 || changed.IdentitySHA256 == expired.IdentitySHA256 || len(changed.Receipts) != 0 {
		t.Fatalf("changed evidence retained reusable identity: %+v, %v", changed, err)
	}
}

func TestGitEvidenceStateRejectsChangedTrustAndLockInputs(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"policy", "commit", "lock bytes", "missing evidence"} {
		t.Run(change, func(t *testing.T) {
			fixture := newGitEvidenceFixture(t, true)
			fixture.write(t)
			first, err := ReadGitEvidenceState(fixture.repo, fixture.now)
			if err != nil || len(first.Receipts) != 1 {
				t.Fatalf("initial evidence = %+v, %v", first, err)
			}
			mutateGitEvidenceState(t, &fixture, change)
			changed, err := ReadGitEvidenceState(fixture.repo, fixture.now)
			if err != nil || changed.IdentitySHA256 == first.IdentitySHA256 || len(changed.Receipts) != 0 {
				t.Fatalf("%s did not invalidate proof: %+v, %v", change, changed, err)
			}
		})
	}
}

func mutateGitEvidenceState(t *testing.T, fixture *gitEvidenceFixture, change string) {
	t.Helper()
	switch change {
	case "policy":
		fixture.repo.Config.SupplyChain.GitEvidence.Providers[0].PolicySHA256 = strings.Repeat("7", 64)
	case "missing evidence":
		if err := os.Remove(filepath.Join(fixture.repo.Root, "evidence/git.dsse.json")); err != nil {
			t.Fatal(err)
		}
	case "commit", "lock bytes":
		data, err := os.ReadFile(filepath.Join(fixture.repo.Root, "uv.lock"))
		if err != nil {
			t.Fatal(err)
		}
		if change == "commit" {
			data = []byte(strings.ReplaceAll(string(data), "0123456789abcdef0123456789abcdef01234567", strings.Repeat("a", 40)))
		} else {
			data = append(data, '\n')
		}
		writeSupplyFile(t, fixture.repo.Root, "uv.lock", string(data))
	}
}
