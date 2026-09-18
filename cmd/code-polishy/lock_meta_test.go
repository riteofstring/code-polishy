package main

import "testing"

func TestLockOptionsRequireACompletePublicationIdentity(t *testing.T) {
	for _, arguments := range [][]string{
		{"--index", "https://example.invalid/index.json"},
		{"--sha256", "digest"},
		{"unexpected"},
		{"--unknown"},
	} {
		if _, err := parseLockOptions(arguments); err == nil {
			t.Fatalf("arguments %v were accepted", arguments)
		}
	}
	options, err := parseLockOptions([]string{"--index", "https://example.invalid/index.json", "--sha256", "digest"})
	if err != nil || options.indexURL == "" || options.indexSHA256 == "" {
		t.Fatalf("complete publication options: options=%+v err=%v", options, err)
	}
}
