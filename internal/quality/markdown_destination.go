package quality

import (
	"fmt"
	"net/url"
	"strings"
)

func markdownDestination(value string) (string, string, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", false, nil
	}
	if markdownWindowsAbsolute(value) {
		return "", "", false, fmt.Errorf("local target escapes the repository")
	}
	if markdownExternalDestination(value) {
		return "", "", true, nil
	}
	pathValue, fragment, found := strings.Cut(value, "#")
	pathValue, _, _ = strings.Cut(pathValue, "?")
	fragment, err := markdownDecodedFragment(fragment, found)
	if err != nil {
		return "", "", false, err
	}
	decoded, err := markdownDecodedPath(pathValue)
	if err != nil {
		return "", "", false, err
	}
	if strings.ContainsRune(fragment, 0) {
		return "", "", false, fmt.Errorf("local target contains a NUL byte")
	}
	return decoded, fragment, false, nil
}

func markdownWindowsAbsolute(value string) bool {
	if len(value) < 3 || value[1] != ':' {
		return false
	}
	letter := value[0]
	if !((letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z')) {
		return false
	}
	return value[2] == '/' || value[2] == '\\'
}

func markdownDecodedFragment(fragment string, present bool) (string, error) {
	if !present {
		return "", nil
	}
	decoded, err := url.PathUnescape(fragment)
	if err != nil {
		return "", fmt.Errorf("fragment is not valid URL encoding")
	}
	return markdownUnescape(decoded), nil
}

func markdownDecodedPath(value string) (string, error) {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return "", fmt.Errorf("local target is not valid URL encoding")
	}
	if strings.ContainsRune(decoded, 0) {
		return "", fmt.Errorf("local target contains a NUL byte")
	}
	if strings.Contains(decoded, "\\") {
		return "", fmt.Errorf("local target uses an ambiguous path separator")
	}
	return markdownUnescape(decoded), nil
}

func markdownExternalDestination(value string) bool {
	if strings.HasPrefix(value, "//") {
		return true
	}
	for index, character := range value {
		if character == ':' {
			return index > 0
		}
		if !markdownSchemeCharacter(character) {
			return false
		}
	}
	return false
}

func markdownSchemeCharacter(character rune) bool {
	if character >= 'a' && character <= 'z' {
		return true
	}
	if character >= 'A' && character <= 'Z' {
		return true
	}
	if character >= '0' && character <= '9' {
		return true
	}
	return character == '+' || character == '-' || character == '.'
}
