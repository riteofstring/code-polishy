package policy

import (
	"strings"
	"testing"
)

func TestRustQualityLimitsHaveIndependentNonBypassableBounds(t *testing.T) {
	t.Parallel()
	for name, quality := range map[string]string{
		"defaults": `{}`,
		"tighter":  `{"complexity":{"rust":5,"rustTest":8}}`,
	} {
		t.Run(name, func(t *testing.T) {
			input := strings.Replace(minimalConfig(), `"quality":{}`, `"quality":`+quality, 1)
			config, err := Load(writeConfig(t, input), "")
			if err != nil {
				t.Fatal(err)
			}
			production, test := 12, 20
			if name == "tighter" {
				production, test = 5, 8
			}
			if config.Quality.Complexity.Rust != production || config.Quality.Complexity.RustTest != test || config.Quality.Complexity.TypeScript != 10 {
				t.Fatalf("Rust limits did not retain their independent policy: %+v", config.Quality.Complexity)
			}
		})
	}
	for _, quality := range []string{`{"complexity":{"rust":13}}`, `{"complexity":{"rustTest":21}}`, `{"complexity":{"rust":-1}}`} {
		input := strings.Replace(minimalConfig(), `"quality":{}`, `"quality":`+quality, 1)
		if _, err := Load(writeConfig(t, input), ""); err == nil {
			t.Fatalf("invalid Rust limit was accepted: %s", quality)
		}
	}
}
