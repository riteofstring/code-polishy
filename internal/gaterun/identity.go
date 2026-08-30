package gaterun

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func NewIdentity(input IdentityInput) (Identity, error) {
	identity, err := identityFromInput(input)
	if err != nil {
		return Identity{}, err
	}
	if err := validateIdentity(identity); err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func (identity Identity) Digest() (string, error) {
	if err := validateIdentity(identity); err != nil {
		return "", err
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return "", operational("encode gate run identity", err)
	}
	return ContentSHA256(data), nil
}

func (identity Identity) Command(index int) (CommandRef, error) {
	if err := validateIdentity(identity); err != nil {
		return CommandRef{}, err
	}
	if index < 0 || index >= len(identity.Commands) {
		return CommandRef{}, fmt.Errorf("%w: command index %d is outside the execution plan", ErrInvalidInput, index)
	}
	runSHA256, err := identity.Digest()
	if err != nil {
		return CommandRef{}, err
	}
	commandSHA256, err := commandDigest(runSHA256, index, identity.Commands[index])
	if err != nil {
		return CommandRef{}, err
	}
	return CommandRef{Index: index, SHA256: commandSHA256, Spec: cloneCommand(identity.Commands[index])}, nil
}

func ContentSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func identityFromInput(input IdentityInput) (Identity, error) {
	environment, err := fingerprintEnvironment(input.Commands, input.Environment)
	if err != nil {
		return Identity{}, err
	}
	ambient, err := fingerprintAmbientEnvironment(input.AmbientEnvironment)
	if err != nil {
		return Identity{}, err
	}
	return Identity{
		Version: Version, Gate: input.Gate, RequestedBase: input.RequestedBase,
		ExactBase: input.ExactBase, Candidate: input.Candidate, PolicyLevel: input.PolicyLevel,
		Release: input.Release, ConfigurationSHA256: input.ConfigurationSHA256,
		Platform: input.Platform, Commands: cloneCommands(input.Commands), Environment: environment, AmbientEnvironment: ambient,
	}, nil
}

func fingerprintEnvironment(commands []CommandSpec, inputs []EnvironmentInput) ([]EnvironmentFingerprint, error) {
	declared, err := declaredEnvironmentNames(commands)
	if err != nil {
		return nil, err
	}
	provided := map[string]EnvironmentInput{}
	for _, input := range inputs {
		if err := addEnvironmentInput(provided, input); err != nil {
			return nil, err
		}
	}
	if len(declared) != len(provided) {
		return nil, fmt.Errorf("%w: declared environment values do not match the command plan", ErrInvalidInput)
	}
	return fingerprintsForNames(declared, provided)
}

func fingerprintAmbientEnvironment(inputs []EnvironmentInput) ([]EnvironmentFingerprint, error) {
	provided := map[string]EnvironmentInput{}
	for _, input := range inputs {
		if err := addEnvironmentInput(provided, input); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, len(provided))
	for name := range provided {
		names = append(names, name)
	}
	sort.Strings(names)
	return fingerprintsForNames(names, provided)
}

func addEnvironmentInput(provided map[string]EnvironmentInput, input EnvironmentInput) error {
	if !validEnvironmentName(input.Name) || input.Name != strings.TrimSpace(input.Name) {
		return fmt.Errorf("%w: environment name %q is invalid", ErrInvalidInput, input.Name)
	}
	if !input.Present && input.Value != "" {
		return fmt.Errorf("%w: absent environment %q has a value", ErrInvalidInput, input.Name)
	}
	if _, duplicate := provided[input.Name]; duplicate {
		return fmt.Errorf("%w: environment %q is duplicated", ErrInvalidInput, input.Name)
	}
	provided[input.Name] = input
	return nil
}

func fingerprintsForNames(declared []string, provided map[string]EnvironmentInput) ([]EnvironmentFingerprint, error) {
	fingerprints := make([]EnvironmentFingerprint, 0, len(declared))
	for _, name := range declared {
		input, found := provided[name]
		if !found {
			return nil, fmt.Errorf("%w: declared environment %q is missing", ErrInvalidInput, name)
		}
		fingerprint := EnvironmentFingerprint{Name: name, Present: input.Present}
		if input.Present {
			fingerprint.SHA256 = ContentSHA256([]byte(input.Value))
		}
		fingerprints = append(fingerprints, fingerprint)
	}
	return fingerprints, nil
}

func declaredEnvironmentNames(commands []CommandSpec) ([]string, error) {
	declared := map[string]bool{}
	for _, command := range commands {
		for _, name := range command.Environment {
			if !validEnvironmentName(name) {
				return nil, fmt.Errorf("%w: command %q declares invalid environment %q", ErrInvalidInput, command.Name, name)
			}
			declared[name] = true
		}
	}
	result := make([]string, 0, len(declared))
	for name := range declared {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

func validateIdentity(identity Identity) error {
	if identity.Version != Version || !validGate(identity.Gate) {
		return fmt.Errorf("%w: gate identity version or kind is invalid", ErrInvalidInput)
	}
	if err := validateIdentityContext(identity); err != nil {
		return err
	}
	return validateIdentityPlan(identity)
}

func validateIdentityContext(identity Identity) error {
	if !validReference(identity.RequestedBase) || !validRevision(identity.ExactBase) || !validRevision(identity.Candidate) {
		return fmt.Errorf("%w: gate identity revisions are invalid", ErrInvalidInput)
	}
	if !validToken(identity.PolicyLevel) || !validRelease(identity.Release) || !validSHA256(identity.ConfigurationSHA256) || !validPlatform(identity.Platform) {
		return fmt.Errorf("%w: gate identity policy, release, configuration, or platform is invalid", ErrInvalidInput)
	}
	return nil
}

func validateIdentityPlan(identity Identity) error {
	if identity.Commands == nil || identity.Environment == nil || identity.AmbientEnvironment == nil {
		return fmt.Errorf("%w: gate identity collections are missing", ErrInvalidInput)
	}
	if err := validateCommands(identity.Commands); err != nil {
		return err
	}
	if err := validateEnvironmentFingerprints(identity.Commands, identity.Environment); err != nil {
		return err
	}
	return validateAmbientEnvironmentFingerprints(identity.AmbientEnvironment)
}

func validateCommands(commands []CommandSpec) error {
	for index, command := range commands {
		if err := validateCommand(command); err != nil {
			return fmt.Errorf("%w: command %d: %v", ErrInvalidInput, index, err)
		}
	}
	return nil
}

func validateCommand(command CommandSpec) error {
	if !validCommandIdentity(command) {
		return fmt.Errorf("command category or identity is invalid")
	}
	if !validCommandInvocation(command) {
		return fmt.Errorf("command invocation is invalid")
	}
	return validateCommandCollections(command)
}

func validCommandIdentity(command CommandSpec) bool {
	return validCategory(command.Category) && validToken(command.Scope) && validToken(command.Cost) && validToken(command.Name)
}

func validCommandInvocation(command CommandSpec) bool {
	return len(command.Argv) != 0 && command.Argv[0] != "" && command.Cwd != "" && command.TimeoutSeconds > 0
}

func validateCommandCollections(command CommandSpec) error {
	for _, values := range [][]string{
		command.Provides, command.Argv, command.Paths, command.Modules, command.RunOn,
		command.Environment, command.ExclusiveResources, command.PassFilePaths,
	} {
		if !validStrings(values) {
			return fmt.Errorf("command collection is invalid")
		}
	}
	if hasDuplicate(command.Environment) {
		return fmt.Errorf("command environment is duplicated")
	}
	return nil
}

func validateEnvironmentFingerprints(commands []CommandSpec, fingerprints []EnvironmentFingerprint) error {
	declared, err := declaredEnvironmentNames(commands)
	if err != nil {
		return err
	}
	if len(declared) != len(fingerprints) {
		return fmt.Errorf("%w: environment fingerprint count does not match the command plan", ErrInvalidInput)
	}
	for index, fingerprint := range fingerprints {
		if fingerprint.Name != declared[index] || fingerprint.Present && !validSHA256(fingerprint.SHA256) || !fingerprint.Present && fingerprint.SHA256 != "" {
			return fmt.Errorf("%w: environment fingerprint %d is invalid", ErrInvalidInput, index)
		}
	}
	return nil
}

func validateAmbientEnvironmentFingerprints(fingerprints []EnvironmentFingerprint) error {
	previous := ""
	for index, fingerprint := range fingerprints {
		if !validEnvironmentName(fingerprint.Name) || fingerprint.Name <= previous || fingerprint.Present && !validSHA256(fingerprint.SHA256) || !fingerprint.Present && fingerprint.SHA256 != "" {
			return fmt.Errorf("%w: ambient environment fingerprint %d is invalid", ErrInvalidInput, index)
		}
		previous = fingerprint.Name
	}
	return nil
}

func commandDigest(runSHA256 string, index int, command CommandSpec) (string, error) {
	if !validSHA256(runSHA256) || index < 0 {
		return "", fmt.Errorf("%w: command identity input is invalid", ErrInvalidInput)
	}
	payload := struct {
		Version   int         `json:"version"`
		RunSHA256 string      `json:"run_sha256"`
		Index     int         `json:"index"`
		Command   CommandSpec `json:"command"`
	}{Version: Version, RunSHA256: runSHA256, Index: index, Command: command}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", operational("encode gate command identity", err)
	}
	return ContentSHA256(data), nil
}

func validGate(gate GateKind) bool {
	return gate == MergeGate || gate == CheckpointGate
}

func validCategory(category CommandCategory) bool {
	switch category {
	case OrdinaryTest, Check, Build, SupplyChain, ArtifactSecurity, BehaviorProof:
		return true
	default:
		return false
	}
}

func validRelease(release ReleaseIdentity) bool {
	return validToken(release.Version) && validSHA256(release.Digest)
}

func validPlatform(platform Platform) bool {
	return validToken(platform.OS) && validToken(platform.Arch)
}

func validReference(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && !strings.HasPrefix(value, "-") && !strings.ContainsAny(value, "\x00\r\n\t ")
}

func validRevision(value string) bool {
	return (len(value) == 40 || len(value) == 64) && validHexadecimal(value)
}

func validSHA256(value string) bool {
	return len(value) == 64 && validHexadecimal(value)
}

func validHexadecimal(value string) bool {
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func validToken(value string) bool {
	if value == "" || len(value) > 160 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-", character) {
			return false
		}
	}
	return true
}

func validEnvironmentName(value string) bool {
	if value == "" || !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_", rune(value[0])) {
		return false
	}
	for _, character := range value[1:] {
		if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_0123456789", character) {
			return false
		}
	}
	return true
}

func validStrings(values []string) bool {
	for _, value := range values {
		if value == "" || strings.ContainsAny(value, "\x00\r\n") {
			return false
		}
	}
	return true
}

func hasDuplicate(values []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}

func cloneCommands(commands []CommandSpec) []CommandSpec {
	result := make([]CommandSpec, len(commands))
	for index, command := range commands {
		result[index] = cloneCommand(command)
	}
	return result
}

func cloneCommand(command CommandSpec) CommandSpec {
	command.Provides = cloneStrings(command.Provides)
	command.Argv = cloneStrings(command.Argv)
	command.Paths = cloneStrings(command.Paths)
	command.Modules = cloneStrings(command.Modules)
	command.RunOn = cloneStrings(command.RunOn)
	command.Environment = cloneStrings(command.Environment)
	command.ExclusiveResources = cloneStrings(command.ExclusiveResources)
	command.PassFilePaths = cloneStrings(command.PassFilePaths)
	return command
}

func cloneStrings(values []string) []string {
	return append([]string{}, values...)
}
