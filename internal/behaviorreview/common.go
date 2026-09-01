package behaviorreview

import (
	"context"
	"fmt"
	"strings"
)

func allValid(values ...bool) bool {
	for _, value := range values {
		if !value {
			return false
		}
	}
	return true
}

func reviewContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func validIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 80 {
		return false
	}
	if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", rune(value[0])) {
		return false
	}
	return identifierTailIsValid(value[1:])
}

func identifierTailIsValid(value string) bool {
	const allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-"
	for _, character := range value {
		if !strings.ContainsRune(allowed, character) {
			return false
		}
	}
	return true
}

func validRevision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	return hexadecimal(value)
}

func validSHA256(value string) bool {
	return len(value) == 64 && hexadecimal(value)
}

func hexadecimal(value string) bool {
	const hexadecimalCharacters = "0123456789abcdefABCDEF"
	for _, character := range value {
		if !strings.ContainsRune(hexadecimalCharacters, character) {
			return false
		}
	}
	return true
}

func staleReceipt(message string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%w: %s", ErrStaleReceipt, message)
	}
	return fmt.Errorf("%w: %s: %v", ErrStaleReceipt, message, cause)
}
