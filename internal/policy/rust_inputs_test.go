package policy

import (
	"strings"
	"testing"
)

func TestRustConfigurationPathsAreSensitive(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"Cargo.toml", "Cargo.lock", ".cargo/config", ".cargo/config.toml",
		"rust-toolchain", "rust-toolchain.toml", "rustfmt.toml", ".rustfmt.toml", "clippy.toml", ".clippy.toml",
	} {
		for _, prefix := range []string{"", "crates/graphics/", "./crates/graphics/", `crates\graphics\`} {
			if !IsSensitiveControlInput(prefix + path) {
				t.Errorf("Rust control input %q was not recognized", prefix+path)
			}
		}
	}
	for _, path := range []string{"config.toml", "src/config.rs", "shaders/main.wgsl", "notes/rustfmt.toml.md", ".cargo/config.example", "cargo/config.toml"} {
		if IsSensitiveControlInput(path) {
			t.Errorf("ordinary path %q was classified as a Rust control input", path)
		}
	}
}

func TestWGSLRulesCannotHideShaderSource(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct {
		scope   string
		message string
	}{
		"reserved language": {scope: `{"languages":[{"name":"wgsl","paths":["shaders/**"]}]}`, message: "unique custom language identifier"},
		"shader data":       {scope: `{"data":["shaders/**/*.wgsl"]}`, message: "must not match executable source"},
	} {
		t.Run(name, func(t *testing.T) {
			configured := strings.Replace(minimalConfig(), `"quality":{}`, `"quality":{},"scope":`+testCase.scope, 1)
			if _, err := Load(writeConfig(t, configured), ""); err == nil || !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("configuration error = %v, want %q", err, testCase.message)
			}
		})
	}
}
