package portability

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

var unixHomePattern = regexp.MustCompile(`^/(?:Users|home)/[^/]+(?:/|$)`)
var windowsHomePattern = regexp.MustCompile(`^[A-Za-z]:[\\/](?:Users|Documents and Settings)[\\/][^\\/]+(?:[\\/]|$)`)
var siblingSegmentPattern = regexp.MustCompile(`^(?:\.\.[\\/])+[A-Za-z0-9][A-Za-z0-9._-]*(?:[\\/]|$)`)

func CoverageFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, input := range repo.Config.Portability.ExternalInputs {
		matched := []string{}
		for _, path := range files {
			if policy.MatchesAny(path, input.SourcePaths) && repo.IsExecutableSource(path) && !repo.IsGenerated(path) && !repo.IsTest(path) {
				matched = append(matched, path)
			}
		}
		if len(matched) == 0 {
			findings = append(findings, policy.Finding{
				Check: "policy.portabilityCoverage", Path: policy.ConfigFilename, Subject: input.Name,
				Message: "external input sourcePaths do not match any governed non-generated production source",
			})
			continue
		}
		for _, path := range matched {
			if !slices.Contains(repo.OwnerModuleNames(path), input.Module) {
				findings = append(findings, policy.Finding{
					Check: "policy.portabilityCoverage", Path: path, Subject: input.Name,
					Message: fmt.Sprintf("external input source must be owned by module %q", input.Module),
				})
			}
		}
	}
	return findings
}

func Advisories(repo repository.Repository, files []string) []policy.Advisory {
	advisories := []policy.Advisory{}
	for _, path := range files {
		if repo.IsGenerated(path) || repo.IsTest(path) || !repo.IsExecutableSource(path) {
			continue
		}
		data, err := repo.Read(path)
		if err != nil || bytes.IndexByte(data, 0) >= 0 {
			continue
		}
		advisories = append(advisories, fileAdvisories(repo, path, data)...)
	}
	slices.SortFunc(advisories, func(left, right policy.Advisory) int {
		return strings.Compare(advisoryKey(left), advisoryKey(right))
	})
	return advisories
}

func fileAdvisories(repo repository.Repository, path string, data []byte) []policy.Advisory {
	advisories := []policy.Advisory{}
	for lineIndex, rawLine := range bytes.Split(data, []byte("\n")) {
		lineNumber := lineIndex + 1
		line := string(rawLine)
		for _, value := range quotedLiterals(line, hashCommentLanguage(repo.Language(path))) {
			if machinePath(value) {
				advisories = append(advisories, policy.Advisory{
					Check: "portability.machinePath", Path: path, Subject: strconv.Itoa(lineNumber),
					Message: fmt.Sprintf("line %d contains a machine-specific home path; resolve it from explicit configuration or a runtime-owned location", lineNumber),
				})
			}
			if siblingReference(line, value) && !governedSiblingReference(repo.Config.Portability.ExternalInputs, path, value) {
				advisories = append(advisories, policy.Advisory{
					Check: "portability.siblingReference", Path: path, Subject: strconv.Itoa(lineNumber),
					Message: fmt.Sprintf("line %d contains an implicit sibling-folder reference; declare and test the external input or use explicit configuration", lineNumber),
				})
			}
		}
	}
	return uniqueAdvisories(advisories)
}

func quotedLiterals(line string, hashComments bool) []string {
	values := []string{}
	for index := 0; index < len(line); index++ {
		if lineCommentAt(line, index, hashComments) {
			break
		}
		quote := line[index]
		if !isQuote(quote) {
			continue
		}
		value, end, closed := consumeQuotedLiteral(line, index, quote)
		index = end
		if closed {
			values = append(values, value)
		}
	}
	return values
}

func lineCommentAt(line string, index int, hashComments bool) bool {
	return hashComments && line[index] == '#' ||
		index+1 < len(line) && line[index] == '/' && line[index+1] == '/'
}

func isQuote(value byte) bool {
	return value == '\'' || value == '"' || value == '`'
}

func consumeQuotedLiteral(line string, start int, quote byte) (string, int, bool) {
	for index := start + 1; index < len(line); index++ {
		if line[index] == '\\' {
			index++
			continue
		}
		if line[index] == quote {
			return literalValue(line[start:index+1], quote), index, true
		}
	}
	return "", len(line) - 1, false
}

func literalValue(literal string, quote byte) string {
	if quote == '"' {
		if value, err := strconv.Unquote(literal); err == nil {
			return value
		}
	}
	value := literal[1 : len(literal)-1]
	value = strings.ReplaceAll(value, `\\`, `\`)
	value = strings.ReplaceAll(value, `\'`, `'`)
	value = strings.ReplaceAll(value, "\\`", "`")
	return value
}

func hashCommentLanguage(language string) bool {
	return language == "python" || language == "shell" || language == "ruby"
}

func machinePath(value string) bool {
	return unixHomePattern.MatchString(value) || windowsHomePattern.MatchString(value)
}

func siblingReference(line, value string) bool {
	if !siblingSegmentPattern.MatchString(value) || strings.ContainsAny(value, "*$\n\r") {
		return false
	}
	context, _, found := strings.Cut(line, value)
	if !found {
		context = line
	}
	normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(context))
	for _, rootName := range []string{"rootdir", "reporoot", "repositoryroot", "projectroot", "process.cwd"} {
		if strings.Contains(normalized, rootName) {
			return true
		}
	}
	return false
}

func governedSiblingReference(inputs []policy.ExternalInput, path, value string) bool {
	portableValue := strings.ReplaceAll(value, `\`, "/")
	for _, input := range inputs {
		if input.SiblingFallback == portableValue && policy.MatchesAny(path, input.SourcePaths) {
			return true
		}
	}
	return false
}

func uniqueAdvisories(advisories []policy.Advisory) []policy.Advisory {
	result := make([]policy.Advisory, 0, len(advisories))
	seen := map[string]bool{}
	for _, advisory := range advisories {
		key := advisoryKey(advisory)
		if !seen[key] {
			seen[key] = true
			result = append(result, advisory)
		}
	}
	return result
}

func advisoryKey(advisory policy.Advisory) string {
	return advisory.Check + "\x00" + advisory.Path + "\x00" + advisory.Subject + "\x00" + advisory.Message
}
