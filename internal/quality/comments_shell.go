package quality

import (
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func shellHorizontalSpace(value byte) bool {
	return value == ' ' || value == '\t'
}

func shellSkipHorizontal(data []byte, index int) int {
	for index < len(data) && shellHorizontalSpace(data[index]) {
		index++
	}
	return index
}

func shellcheckSourceAllowed(repo repository.Repository, data []byte, annotation sourceAnnotation) bool {
	const prefix = "# shellcheck source="
	if !strings.HasPrefix(annotation.text, prefix) {
		return false
	}
	path := strings.TrimPrefix(annotation.text, prefix)
	if path == "" || strings.ContainsAny(path, " \t\r\n") {
		return false
	}
	normalized, err := repo.NormalizePath(path)
	if err != nil || normalized != path {
		return false
	}
	if !repo.IsRegularFile(normalized) || repo.SourceCommentLanguage(normalized) != "shell" {
		return false
	}
	return annotation.shellSourceFollows
}

func shellSBATCHAllowed(data []byte, annotation sourceAnnotation) bool {
	const prefix = "#SBATCH"
	if !sourceLineStart(data, annotation.offset) || !strings.HasPrefix(annotation.text, prefix) {
		return false
	}
	argument := strings.TrimPrefix(annotation.text, prefix)
	if argument == "" || !shellHorizontalSpace(argument[0]) || strings.TrimSpace(argument) == "" {
		return false
	}
	for index := 0; index < annotation.offset; {
		end := lineEnd(data, index)
		line := strings.TrimLeft(string(data[index:end]), " \t\f")
		if line != "" && !strings.HasPrefix(line, "#") {
			return false
		}
		index = nextLine(data, end)
	}
	return true
}
