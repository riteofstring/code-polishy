package repository

import "testing"

func TestReleaseArchitectureMatchesHostBundleNames(t *testing.T) {
	if got := releaseArchitecture("amd64"); got != "x64" {
		t.Fatalf("amd64=%q", got)
	}
	if got := releaseArchitecture("arm64"); got != "arm64" {
		t.Fatalf("arm64=%q", got)
	}
}
