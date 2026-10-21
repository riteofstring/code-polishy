package testartifact

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestCleanupCandidatesKeepActiveAndUnownedArtifacts(t *testing.T) {
	root := t.TempDir()
	completed, err := Start(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := completed.Complete(); err != nil {
		t.Fatal(err)
	}
	active, err := Start(root, "")
	if err != nil {
		t.Fatal(err)
	}
	writeArtifactFile(t, filepath.Join(root, RootName), "notes.txt", "keep me")
	removable, kept, err := CleanupCandidates(root)
	want := []string{RootName + "/" + completed.ID()}
	if err != nil || !reflect.DeepEqual(removable, want) || len(kept) != 2 ||
		!slices.Contains(kept, RootName+"/"+active.ID()) || !slices.Contains(kept, RootName+"/notes.txt") {
		t.Fatalf("removable=%v kept=%v error=%v", removable, kept, err)
	}
	writeArtifactFile(t, completed.Directory(), leaseFilename, "held")
	removable, kept, err = CleanupCandidates(root)
	if err != nil || len(removable) != 0 || len(kept) != 3 {
		t.Fatalf("leased execution selected: removable=%v kept=%v error=%v", removable, kept, err)
	}
	if err := os.Remove(filepath.Join(completed.Directory(), leaseFilename)); err != nil {
		t.Fatal(err)
	}
	writeArtifactFile(t, completed.Directory(), manifestFilename, "{}")
	removable, _, err = CleanupCandidates(root)
	if err != nil || len(removable) != 0 {
		t.Fatalf("invalid owner selected: removable=%v error=%v", removable, err)
	}
}
