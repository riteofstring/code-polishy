package gaterun

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIdentityDigestBindsCommandAndEnvironmentInputs(t *testing.T) {
	input := testIdentityInput([]CommandSpec{testCommand(OrdinaryTest, "unit", "FOO")})
	input.Environment = []EnvironmentInput{{Name: "FOO", Value: "top-secret", Present: true}}
	identity, err := NewIdentity(input)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := identity.Digest()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "top-secret") || !strings.Contains(string(encoded), ContentSHA256([]byte("top-secret"))) {
		t.Fatalf("identity exposes or omits environment fingerprint: %s", encoded)
	}
	cases := []struct {
		name   string
		mutate func(*IdentityInput)
	}{
		{name: "candidate", mutate: func(value *IdentityInput) { value.Candidate = strings.Repeat("c", 40) }},
		{name: "configuration", mutate: func(value *IdentityInput) { value.ConfigurationSHA256 = strings.Repeat("d", 64) }},
		{name: "environment", mutate: func(value *IdentityInput) { value.Environment[0].Value = "other" }},
		{name: "command", mutate: func(value *IdentityInput) { value.Commands[0].Argv = []string{"tool", "changed"} }},
		{name: "category", mutate: func(value *IdentityInput) { value.Commands[0].Category = Check }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			changed := cloneIdentityInput(input)
			test.mutate(&changed)
			updated, err := NewIdentity(changed)
			if err != nil {
				t.Fatal(err)
			}
			updatedDigest, err := updated.Digest()
			if err != nil {
				t.Fatal(err)
			}
			if updatedDigest == digest {
				t.Fatalf("%s did not change the identity digest", test.name)
			}
		})
	}
}

func TestIdentityRequiresExactDeclaredEnvironmentValues(t *testing.T) {
	input := testIdentityInput([]CommandSpec{testCommand(OrdinaryTest, "unit", "FOO")})
	for _, environment := range [][]EnvironmentInput{
		nil,
		{{Name: "EXTRA", Value: "value", Present: true}},
		{{Name: "FOO", Value: "value", Present: true}, {Name: "FOO", Value: "other", Present: true}},
		{{Name: "FOO", Value: "value", Present: false}},
	} {
		input.Environment = environment
		if _, err := NewIdentity(input); err == nil {
			t.Fatalf("NewIdentity accepted environment %+v", environment)
		}
	}
}

func testIdentityInput(commands []CommandSpec) IdentityInput {
	return IdentityInput{
		Gate: MergeGate, RequestedBase: "origin/main", ExactBase: strings.Repeat("a", 40), Candidate: strings.Repeat("b", 40),
		PolicyLevel: "recommended", Release: ReleaseIdentity{Version: "0.19.0", Digest: strings.Repeat("e", 64)},
		ConfigurationSHA256: strings.Repeat("f", 64), Platform: Platform{OS: "linux", Arch: "amd64"}, Commands: cloneCommands(commands),
		Environment: []EnvironmentInput{},
	}
}

func testCommand(category CommandCategory, name string, environment ...string) CommandSpec {
	return CommandSpec{
		Category: category, Scope: "module", Cost: "quick", Name: name, Argv: []string{"tool", name}, Cwd: ".",
		Provides: []string{}, Paths: []string{}, Modules: []string{"gaterun"}, RunOn: []string{"recommended"},
		Environment: append([]string{}, environment...), ExclusiveResources: []string{}, TimeoutSeconds: 60, PassFilePaths: []string{},
	}
}

func cloneIdentityInput(input IdentityInput) IdentityInput {
	input.Commands = cloneCommands(input.Commands)
	input.Environment = append([]EnvironmentInput{}, input.Environment...)
	return input
}
