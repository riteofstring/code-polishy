package testreceipt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
)

func (identity Identity) Digest() (string, error) {
	if err := validateIdentity(identity); err != nil {
		return "", err
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

func digest(data []byte) string {
	value := sha256.Sum256(data)
	return hex.EncodeToString(value[:])
}

func validateIdentity(identity Identity) error {
	if !validIdentityHeader(identity) {
		return fmt.Errorf("%w: identity header is malformed", ErrInvalid)
	}
	if identity.Environment == nil || identity.Tools == nil || identity.Inputs == nil {
		return fmt.Errorf("%w: identity collections are absent", ErrInvalid)
	}
	if err := validateEnvironment(identity.Environment); err != nil {
		return err
	}
	if err := validateTools(identity.Tools); err != nil {
		return err
	}
	return validateInputs(identity.Inputs)
}

func validIdentityHeader(identity Identity) bool {
	return identity.Version == IdentityVersion && identity.PolicySchema >= 1 && validToken(identity.Release.Version) &&
		validSHA256(identity.Release.Digest) && validSHA256(identity.ConfigurationSHA256) &&
		validToken(identity.Platform.OS) && validToken(identity.Platform.Arch) && validToken(identity.Suite.Name)
}

func validateEnvironment(values []Environment) error {
	previous := ""
	for _, value := range values {
		if !validEnvironmentName(value.Name) || value.Name <= previous || value.Present && !validSHA256(value.SHA256) || !value.Present && value.SHA256 != "" {
			return fmt.Errorf("%w: environment fingerprint is malformed", ErrInvalid)
		}
		previous = value.Name
	}
	return nil
}

func validateTools(values []Tool) error {
	previous := ""
	for _, value := range values {
		if !validToken(value.Name) || value.Name <= previous || !validToken(value.Version) {
			return fmt.Errorf("%w: tool fingerprint is malformed", ErrInvalid)
		}
		previous = value.Name
	}
	return nil
}

func validateInputs(values []Input) error {
	if !slices.IsSortedFunc(values, func(left, right Input) int { return strings.Compare(left.Path, right.Path) }) {
		return fmt.Errorf("%w: input fingerprints are not sorted", ErrInvalid)
	}
	previous := ""
	for _, value := range values {
		if value.Path == "" || value.Path == "." || path.Clean(value.Path) != value.Path || strings.HasPrefix(value.Path, "/") ||
			strings.Contains(value.Path, "\\") || value.Path <= previous || !validSHA256(value.SHA256) {
			return fmt.Errorf("%w: input fingerprint is malformed", ErrInvalid)
		}
		previous = value.Path
	}
	return nil
}

func sameIdentity(left, right Identity) bool {
	leftDigest, leftErr := left.Digest()
	rightDigest, rightErr := right.Digest()
	return leftErr == nil && rightErr == nil && leftDigest == rightDigest
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func validToken(value string) bool {
	if value == "" || len(value) > 200 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._+-", character) {
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
