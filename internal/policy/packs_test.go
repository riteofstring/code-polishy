package policy

import (
	"strings"
	"testing"
)

func TestPackSelectionRequiresOneExactIdentityPerName(t *testing.T) {
	valid := strings.Replace(minimalConfig(), `"modules":`, `"packs":[{"name":"community-rust","version":"1.2.3","digest":"`+strings.Repeat("a", 64)+`"}],"modules":`, 1)
	config, err := Parse([]byte(valid), ConfigFilename)
	if err != nil || len(config.Packs) != 1 {
		t.Fatalf("valid pack selection failed: %+v %v", config.Packs, err)
	}
	for _, mutation := range []string{
		strings.Replace(valid, `"version":"1.2.3"`, `"version":"latest"`, 1),
		strings.Replace(valid, strings.Repeat("a", 64), "sha256:missing", 1),
		strings.Replace(valid, `],"modules":`, `,{"name":"community-rust","version":"1.2.4","digest":"`+strings.Repeat("b", 64)+`"}],"modules":`, 1),
	} {
		if _, err := Parse([]byte(mutation), ConfigFilename); err == nil {
			t.Fatal("invalid pack selection passed")
		}
	}
}
