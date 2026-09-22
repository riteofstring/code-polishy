package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"
)

type parsedSource struct {
	Comments []commentFact
	Literals []literalFact
}

type sourceComment struct {
	offset int
	line   int
	column int
	raw    string
}

var siblingPattern = regexp.MustCompile(`^(?:\.\.[\\/])+[A-Za-z0-9][A-Za-z0-9._-]*(?:[\\/]|$)`)

func dialectFor(name string, data []byte) shellDialect {
	first := strings.ToLower(string(bytes.SplitN(data, []byte("\n"), 2)[0]))
	if strings.Contains(first, "bash") || strings.EqualFold(filepath.Ext(name), ".bash") {
		return dialectBash
	}
	return dialectPOSIX
}

func parseSource(name string, data []byte, dialect shellDialect, inventory []inventoryEntry) (parsedSource, error) {
	variant := syntax.LangPOSIX
	if dialect == dialectBash {
		variant = syntax.LangBash
	}
	parsed, err := syntax.NewParser(syntax.KeepComments(true), syntax.Variant(variant)).Parse(bytes.NewReader(data), name)
	if err != nil {
		return parsedSource{}, err
	}
	comments, sourceCalls, firstCode := inspectSyntax(parsed, len(data))
	return parsedSource{Comments: commentFacts(name, data, comments, sourceCalls, firstCode, shellInventory(inventory)), Literals: literalFacts(name, data)}, nil
}

func inspectSyntax(parsed *syntax.File, size int) ([]sourceComment, map[int]bool, int) {
	comments := []sourceComment{}
	sourceCalls := map[int]bool{}
	firstCode := size + 1
	for _, statement := range parsed.Stmts {
		firstCode = min(firstCode, int(statement.Pos().Offset()))
	}
	syntax.Walk(parsed, func(node syntax.Node) bool {
		switch typed := node.(type) {
		case *syntax.Comment:
			comments = append(comments, sourceComment{offset: int(typed.Hash.Offset()), line: int(typed.Hash.Line()), column: int(typed.Hash.Col()), raw: "#" + typed.Text})
		case *syntax.CallExpr:
			if len(typed.Assigns) == 0 && len(typed.Args) >= 2 {
				command := typed.Args[0].Lit()
				if command == "source" || command == "." {
					sourceCalls[int(typed.Pos().Offset())] = true
				}
			}
		}
		return true
	})
	sort.Slice(comments, func(left, right int) bool { return comments[left].offset < comments[right].offset })
	return comments, sourceCalls, firstCode
}

func commentFacts(name string, data []byte, comments []sourceComment, sourceCalls map[int]bool, firstCode int, allowed map[string]bool) []commentFact {
	facts := make([]commentFact, 0, len(comments))
	for index, comment := range comments {
		raw, complete := boundedUTF8(comment.raw, 65536)
		kind := "Line"
		if comment.offset == 0 && strings.HasPrefix(raw, "#!") {
			kind = "Shebang"
		}
		machine := false
		if complete {
			machine = shellMachineDirective(data, comment, sourceCalls, allowed)
		}
		facts = append(facts, commentFact{
			Path: name, Line: comment.line, Column: comment.column, Kind: kind, Raw: raw, Complete: complete,
			BeforeCode: comment.offset < firstCode, Preamble: index == 0 && strings.TrimSpace(string(data[:comment.offset])) == "",
			ByteZero: comment.offset == 0, MachineDirective: machine,
		})
	}
	return facts
}

func shellInventory(inventory []inventoryEntry) map[string]bool {
	result := map[string]bool{}
	for _, entry := range inventory {
		if entry.Source && entry.Language == "shell" {
			result[entry.Path] = true
		}
	}
	return result
}

func shellMachineDirective(data []byte, comment sourceComment, sourceCalls map[int]bool, shellFiles map[string]bool) bool {
	if comment.offset == 0 && strings.HasPrefix(comment.raw, "#!") && strings.TrimSpace(strings.TrimPrefix(comment.raw, "#!")) != "" {
		return true
	}
	if shellCheckSourceDirective(data, comment, sourceCalls, shellFiles) {
		return true
	}
	return batchDirective(data, comment)
}

func shellCheckSourceDirective(data []byte, comment sourceComment, sourceCalls map[int]bool, shellFiles map[string]bool) bool {
	const prefix = "# shellcheck source="
	if !strings.HasPrefix(comment.raw, prefix) {
		return false
	}
	target := strings.TrimPrefix(comment.raw, prefix)
	if target == "" || strings.ContainsAny(target, " \t\r\n") || exactPath(target) != nil || !shellFiles[target] {
		return false
	}
	next := nextLine(data, lineEnd(data, comment.offset))
	for next < len(data) && (data[next] == ' ' || data[next] == '\t') {
		next++
	}
	return sourceCalls[next]
}

func batchDirective(data []byte, comment sourceComment) bool {
	const prefix = "#SBATCH"
	if !lineStart(data, comment.offset) || !strings.HasPrefix(comment.raw, prefix) {
		return false
	}
	argument := strings.TrimPrefix(comment.raw, prefix)
	if argument == "" || argument[0] != ' ' && argument[0] != '\t' || strings.TrimSpace(argument) == "" {
		return false
	}
	for index := 0; index < comment.offset; {
		end := lineEnd(data, index)
		line := strings.TrimLeft(string(data[index:end]), " \t\f")
		if line != "" && !strings.HasPrefix(line, "#") {
			return false
		}
		index = nextLine(data, end)
	}
	return true
}

func lineStart(data []byte, offset int) bool {
	return offset == 0 || offset > 0 && (data[offset-1] == '\n' || data[offset-1] == '\r')
}

func lineEnd(data []byte, offset int) int {
	for offset < len(data) && data[offset] != '\n' && data[offset] != '\r' {
		offset++
	}
	return offset
}

func nextLine(data []byte, offset int) int {
	if offset < len(data) && data[offset] == '\r' {
		offset++
	}
	if offset < len(data) && data[offset] == '\n' {
		offset++
	}
	return offset
}

func boundedUTF8(value string, limit int) (string, bool) {
	if len(value) <= limit {
		return value, true
	}
	end := limit
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end], false
}

func syntaxFinding(capability, name string, err error) responseFinding {
	line := 1
	column := 1
	message := err.Error()
	var parseError syntax.ParseError
	if errors.As(err, &parseError) {
		line = max(1, int(parseError.Pos.Line()))
		column = max(1, int(parseError.Pos.Col()))
		message = parseError.Text
	}
	var languageError syntax.LangError
	if errors.As(err, &languageError) {
		line = max(1, int(languageError.Pos.Line()))
		column = max(1, int(languageError.Pos.Col()))
		message = languageError.Feature + " is not valid in this shell dialect"
	}
	if len(message) > 4096 {
		message = message[:4096]
	}
	return responseFinding{Capability: capability, Path: name, Line: line, Column: column, Subject: "syntax", Message: message, Rule: "syntax"}
}

func literalFacts(name string, data []byte) []literalFact {
	result := []literalFact{}
	for lineIndex, rawLine := range bytes.Split(data, []byte("\n")) {
		line := string(rawLine)
		for _, literal := range scanLiterals(line) {
			if literal.value == "" || len(literal.value) > 4096 || strings.ContainsAny(literal.value, "\x00\r\n") {
				continue
			}
			result = append(result, literalFact{Path: name, Line: lineIndex + 1, Column: literal.column, Value: literal.value, RootContext: rootedSibling(line, literal.value)})
		}
	}
	return result
}

type scannedLiteral struct {
	value  string
	column int
}

func scanLiterals(line string) []scannedLiteral {
	result := []scannedLiteral{}
	for index := 0; index < len(line); index++ {
		if line[index] == '#' {
			break
		}
		quote := line[index]
		if quote != '\'' && quote != '"' && quote != '`' {
			continue
		}
		value, end, complete := consumeLiteral(line, index, quote)
		column := index + 1
		index = end
		if complete {
			result = append(result, scannedLiteral{value: value, column: column})
		}
	}
	return result
}

func consumeLiteral(line string, start int, quote byte) (string, int, bool) {
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
	return strings.ReplaceAll(value, "\\`", "`")
}

func rootedSibling(line, value string) bool {
	if !siblingPrefix(value) || strings.ContainsAny(value, "*$\n\r") {
		return false
	}
	context, _, found := strings.Cut(line, value)
	if !found {
		context = line
	}
	normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(context))
	for _, root := range []string{"rootdir", "reporoot", "repositoryroot", "projectroot", "process.cwd"} {
		if strings.Contains(normalized, root) {
			return true
		}
	}
	return false
}

func siblingPrefix(value string) bool {
	return siblingPattern.MatchString(value)
}
