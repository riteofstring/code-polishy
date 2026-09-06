package supplychain

import (
	"strings"
	"testing"
)

func TestPackageLockDependencyFormats(t *testing.T) {
	for _, test := range []struct {
		name, document string
		count          int
	}{
		{"v3", `{"lockfileVersion":3,"packages":{"":{"dependencies":{"parent":"^1.0.0"}},"node_modules/parent":{"version":"1.2.0","dependencies":{"child":"~2.0.0"}},"node_modules/child":{"version":"2.0.1"}}}`, 2},
		{"v2 modern precedence", `{"lockfileVersion":2,"packages":{"":{"dependencies":{"parent":"^1.0.0"}},"node_modules/parent":{"version":"1.2.0","dependencies":{"child":"~2.0.0"}},"node_modules/child":{"version":"2.0.1"}},"dependencies":{"legacy":{"version":"9.0.0","dependencies":{"nested":{"version":"8.0.0"}}}}}`, 2},
		{"v1", `{"lockfileVersion":1,"dependencies":{"parent":{"version":"1.2.0","requires":{"child":"~2.0.0"},"dependencies":{"child":{"version":"2.0.1"}}}}}`, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			packages, err := parsePackageLock([]byte(test.document), "package-lock.json", "npm")
			if err != nil || len(packages) != test.count {
				t.Fatalf("packages=%+v err=%v", packages, err)
			}
			if packages[0].Name != "child" || packages[0].Version != "2.0.1" || packages[1].Name != "parent" || packages[1].Version != "1.2.0" {
				t.Fatalf("inventory=%+v", packages)
			}
		})
	}
}

func TestPackageLockRejectsMalformedDependencyMaps(t *testing.T) {
	for _, document := range []string{
		`{"packages":{"":{"dependencies":{"parent":{"version":"1.0.0"}}}}}`,
		`{"dependencies":{"parent":"^1.0.0"}}`,
		`{"packages":[]}`,
	} {
		if _, err := parsePackageLock([]byte(document), "package-lock.json", "npm"); err == nil || !strings.Contains(err.Error(), "cannot unmarshal") {
			t.Fatalf("error=%v", err)
		}
	}
}

func TestDependencyReviewAcceptsModernNPMInventoryAndRetainsDecoderCause(t *testing.T) {
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json", `{"name":"example","version":"1.0.0","packageManager":"npm@11.0.0","dependencies":{"parent":"1.2.0"}}`)
	lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"parent":"1.2.0"}},"node_modules/parent":{"version":"1.2.0","dependencies":{"child":"^2.0.0"}},"node_modules/child":{"version":"2.0.1"}}}`
	writeSupplyFile(t, repo.Root, "package-lock.json", lock)
	base := commitReviewInventory(t, repo)
	changes, err := DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 0 {
		t.Fatalf("unchanged modern lock changes=%+v err=%v", changes, err)
	}
	writeSupplyFile(t, repo.Root, "package-lock.json", strings.Replace(lock, `"version":"2.0.1"`, `"version":"2.0.2"`, 1))
	changes, err = DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 1 || changes[0].Package != "child" || changes[0].From != "2.0.1" || changes[0].To != "2.0.2" {
		t.Fatalf("transitive update=%+v err=%v", changes, err)
	}
	writeSupplyFile(t, repo.Root, "package-lock.json", `{"packages":{"":{"dependencies":{"parent":42}}}}`)
	if _, err := DependencyChanges(t.Context(), repo, base); err == nil || !strings.Contains(err.Error(), "cannot unmarshal number") {
		t.Fatalf("decoder cause=%v", err)
	}
}
