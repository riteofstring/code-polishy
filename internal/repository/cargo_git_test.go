package repository

import (
	"strings"
	"testing"
)

func TestCargoGitInventoryPreservesDeclarationWithoutInventingResolution(t *testing.T) {
	t.Parallel()
	for _, declaration := range []CargoDependencyDeclaration{
		{Git: "https://example.test/team/tool"},
		{Git: "https://example.test/team/tool", Branch: "release/stable"},
		{Git: "https://example.test/team/tool", Tag: "v1.2.3"},
		{Git: "https://example.test/team/tool", Revision: "ABCDEF0"},
	} {
		source, err := ParseCargoGitDeclaration(declaration)
		if err != nil || source.Commit != "" || source.ValidateExactPin() == nil {
			t.Fatalf("historical declaration became an admitted pin: %+v, %v", source, err)
		}
		if declaration.Branch != "" && source.DeclaredRef != declaration.Branch {
			t.Fatalf("branch declaration changed: %+v", source)
		}
	}
	commit := strings.Repeat("a", 40)
	source, err := ParseCargoGitDeclaration(CargoDependencyDeclaration{Git: "https://example.test/team/tool", Revision: commit})
	if err != nil || source.Commit != commit || source.ValidateExactPin() != nil {
		t.Fatalf("full revision was not retained: %+v, %v", source, err)
	}
}

func TestCargoGitLockRetainsDefaultRefAndResolvedCommitSeparately(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	source, err := ParseCargoGitLockSource("git+https://example.test/team/tool#" + commit)
	if err != nil || source.DeclaredRef != "" || source.Commit != commit || !strings.Contains(source.InventoryIdentity(), "commit="+commit) {
		t.Fatalf("default-ref Cargo lock source = %+v, %v", source, err)
	}
	source, err = ParseCargoGitLockSource("git+https://example.test/team/tool?branch=release/stable#" + commit)
	if err != nil || source.DeclaredRef != "release/stable" || source.RefKind != "branch" || source.Commit != commit {
		t.Fatalf("branch Cargo lock source = %+v, %v", source, err)
	}
}

func TestCargoGitInventoryRejectsCredentialsAndAmbiguousReferences(t *testing.T) {
	t.Parallel()
	for _, declaration := range []CargoDependencyDeclaration{
		{Git: "https://TOKEN@example.test/tool", Revision: strings.Repeat("a", 40)},
		{Git: "https://example.test/tool?token=TOKEN", Revision: strings.Repeat("a", 40)},
		{Git: "https://example.test/tool", Tag: "v1", Branch: "main"},
		{Git: "https://example.test/tool", Revision: "main~1"},
	} {
		if _, err := ParseCargoGitDeclaration(declaration); err == nil {
			t.Fatal("unsafe Cargo declaration accepted")
		}
	}
	for _, source := range []string{
		"git+https://TOKEN@example.test/tool#" + strings.Repeat("a", 40),
		"git+https://example.test/tool#abcdef0",
		"git+https://example.test/tool?tag=v1&branch=main#" + strings.Repeat("a", 40),
	} {
		if _, err := ParseCargoGitLockSource(source); err == nil {
			t.Fatal("unsafe Cargo lock source accepted")
		}
	}
}
