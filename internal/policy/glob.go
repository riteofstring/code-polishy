package policy

import (
	"regexp"
	"strings"
)

func MatchesAny(path string, patterns []string) bool {
	path = strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "./")
	for _, pattern := range patterns {
		if Match(path, pattern) {
			return true
		}
	}
	return false
}

func Match(path, pattern string) bool {
	path = strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "./")
	pattern = strings.TrimPrefix(strings.ReplaceAll(pattern, "\\", "/"), "./")
	var expression strings.Builder
	expression.WriteString("^")
	for index := 0; index < len(pattern); {
		character := pattern[index]
		switch character {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				index += 2
				if index < len(pattern) && pattern[index] == '/' {
					expression.WriteString("(?:.*/)?")
					index++
				} else {
					expression.WriteString(".*")
				}
			} else {
				expression.WriteString("[^/]*")
				index++
			}
		case '?':
			expression.WriteString("[^/]")
			index++
		default:
			expression.WriteString(regexp.QuoteMeta(string(character)))
			index++
		}
	}
	expression.WriteString("$")
	matched, err := regexp.MatchString(expression.String(), path)
	return err == nil && matched
}

func IsTestPath(path string) bool {
	return MatchesAny(path, DefaultTestPatterns)
}
