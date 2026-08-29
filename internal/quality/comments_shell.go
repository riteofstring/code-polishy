package quality

import (
	"fmt"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

type shellHereDoc struct {
	delimiter string
	stripTabs bool
	quoted    bool
}

type shellCommentScanner struct {
	data     []byte
	comments []sourceAnnotation
	pending  []shellHereDoc
}

func scanShellSource(data []byte) ([]sourceAnnotation, error) {
	scanner := shellCommentScanner{data: data}
	_, err := scanner.scan(0, 0)
	if err != nil {
		return nil, err
	}
	return scanner.comments, nil
}

func (scanner *shellCommentScanner) scan(index int, stop byte) (int, error) {
	wordStart := true
	for index < len(scanner.data) {
		if stop != 0 && scanner.data[index] == stop {
			return index + 1, nil
		}
		next, nextWordStart, err := scanner.scanStep(index, wordStart)
		if err != nil {
			return 0, err
		}
		index = next
		wordStart = nextWordStart
	}
	if stop != 0 {
		return 0, fmt.Errorf("unterminated shell substitution")
	}
	return index, nil
}

func (scanner *shellCommentScanner) scanStep(index int, wordStart bool) (int, bool, error) {
	if shellLineEnding(scanner.data[index]) {
		return scanner.scanLineEnding(index)
	}
	if shellHorizontalSpace(scanner.data[index]) {
		return index + 1, true, nil
	}
	return scanner.scanSymbol(index, wordStart)
}

func (scanner *shellCommentScanner) scanLineEnding(index int) (int, bool, error) {
	index = nextLine(scanner.data, index)
	if len(scanner.pending) == 0 {
		return index, true, nil
	}
	next, err := scanner.skipHereDocs(index)
	return next, true, err
}

func (scanner *shellCommentScanner) scanSymbol(index int, wordStart bool) (int, bool, error) {
	value := scanner.data[index]
	if value == '#' {
		next, nextWordStart := scanner.scanComment(index, wordStart)
		return next, nextWordStart, nil
	}
	if value == '\'' {
		return scanner.scanSingleQuoted(index)
	}
	if value == '"' {
		return scanner.scanDoubleQuoted(index)
	}
	if value == '`' {
		return scanner.scanBacktick(index)
	}
	if value == '$' {
		return scanner.scanDollarStep(index)
	}
	if value == '<' {
		return scanner.scanLessThan(index)
	}
	if value == '>' {
		return scanner.scanGreaterThan(index)
	}
	if shellControlOperator(value) {
		return index + 1, true, nil
	}
	if value == '\\' {
		return scanner.scanEscape(index)
	}
	return index + 1, false, nil
}

func (scanner *shellCommentScanner) scanComment(index int, wordStart bool) (int, bool) {
	if !wordStart {
		return index + 1, false
	}
	end := lineEnd(scanner.data, index)
	scanner.comments = append(scanner.comments, sourceAnnotation{offset: index, text: string(scanner.data[index:end])})
	return end, true
}

func (scanner *shellCommentScanner) scanSingleQuoted(index int) (int, bool, error) {
	end, err := scanShellSingleQuote(scanner.data, index)
	return end, false, err
}

func (scanner *shellCommentScanner) scanDoubleQuoted(index int) (int, bool, error) {
	end, err := scanner.scanDoubleQuote(index)
	return end, false, err
}

func (scanner *shellCommentScanner) scanBacktick(index int) (int, bool, error) {
	end, err := scanner.scan(index+1, '`')
	if err != nil {
		return 0, false, fmt.Errorf("unterminated shell command substitution")
	}
	return end, false, nil
}

func (scanner *shellCommentScanner) scanDollarStep(index int) (int, bool, error) {
	end, handled, err := scanner.scanDollar(index)
	if err != nil {
		return 0, false, err
	}
	if handled {
		return end, false, nil
	}
	return index + 1, false, nil
}

func (scanner *shellCommentScanner) scanLessThan(index int) (int, bool, error) {
	if shellHasNext(scanner.data, index, '<') {
		end, err := scanner.readHereDoc(index)
		return end, false, err
	}
	if shellHasNext(scanner.data, index, '(') {
		return scanner.scanProcessSubstitution(index)
	}
	return index + 1, true, nil
}

func (scanner *shellCommentScanner) scanGreaterThan(index int) (int, bool, error) {
	if shellHasNext(scanner.data, index, '(') {
		return scanner.scanProcessSubstitution(index)
	}
	return index + 1, true, nil
}

func (scanner *shellCommentScanner) scanProcessSubstitution(index int) (int, bool, error) {
	end, err := scanner.scan(index+2, ')')
	if err != nil {
		return 0, false, fmt.Errorf("unterminated shell process substitution")
	}
	return end, false, nil
}

func (scanner *shellCommentScanner) scanEscape(index int) (int, bool, error) {
	if index+1 >= len(scanner.data) {
		return 0, false, fmt.Errorf("unterminated shell escape")
	}
	if shellLineEnding(scanner.data[index+1]) {
		return nextLine(scanner.data, index+1), false, nil
	}
	return index + 2, false, nil
}

func shellLineEnding(value byte) bool {
	return value == '\r' || value == '\n'
}

func shellHorizontalSpace(value byte) bool {
	return value == ' ' || value == '\t'
}

func shellControlOperator(value byte) bool {
	return strings.ContainsRune(";|&()", rune(value))
}

func shellHasNext(data []byte, index int, value byte) bool {
	return index+1 < len(data) && data[index+1] == value
}

func scanShellSingleQuote(data []byte, index int) (int, error) {
	index++
	for index < len(data) {
		if data[index] == '\'' {
			return index + 1, nil
		}
		index++
	}
	return 0, fmt.Errorf("unterminated shell single-quoted string")
}

func scanShellANSIQuote(data []byte, index int) (int, error) {
	index += 2
	for index < len(data) {
		if data[index] == '\\' {
			if index+1 >= len(data) {
				return 0, fmt.Errorf("unterminated shell ANSI string")
			}
			index += 2
			continue
		}
		if data[index] == '\'' {
			return index + 1, nil
		}
		index++
	}
	return 0, fmt.Errorf("unterminated shell ANSI string")
}

func (scanner *shellCommentScanner) scanDoubleQuote(index int) (int, error) {
	index++
	for index < len(scanner.data) {
		value := scanner.data[index]
		if value == '"' {
			return index + 1, nil
		}
		if value == '\\' {
			if index+1 >= len(scanner.data) {
				return 0, fmt.Errorf("unterminated shell double-quoted string")
			}
			index += 2
			continue
		}
		if value == '`' {
			end, err := scanner.scan(index+1, '`')
			if err != nil {
				return 0, fmt.Errorf("unterminated shell command substitution")
			}
			index = end
			continue
		}
		if value == '$' {
			end, handled, err := scanner.scanDollar(index)
			if err != nil {
				return 0, err
			}
			if handled {
				index = end
				continue
			}
		}
		index++
	}
	return 0, fmt.Errorf("unterminated shell double-quoted string")
}

func (scanner *shellCommentScanner) scanDollar(index int) (int, bool, error) {
	if index+1 >= len(scanner.data) {
		return index, false, nil
	}
	switch scanner.data[index+1] {
	case '(':
		if index+2 < len(scanner.data) && scanner.data[index+2] == '(' {
			end, err := scanShellArithmetic(scanner.data, index+3)
			return end, true, err
		}
		end, err := scanner.scan(index+2, ')')
		if err != nil {
			return 0, true, fmt.Errorf("unterminated shell command substitution")
		}
		return end, true, nil
	case '{':
		end, err := scanner.scanParameter(index + 2)
		return end, true, err
	case '\'':
		end, err := scanShellANSIQuote(scanner.data, index)
		return end, true, err
	case '"':
		end, err := scanner.scanDoubleQuote(index + 1)
		return end, true, err
	}
	return index, false, nil
}

func scanShellArithmetic(data []byte, index int) (int, error) {
	scanner := shellArithmeticScanner{data: data, index: index, depth: 1}
	return scanner.scan()
}

type shellArithmeticScanner struct {
	data  []byte
	index int
	depth int
}

func (scanner *shellArithmeticScanner) scan() (int, error) {
	for scanner.index < len(scanner.data) {
		next, closed, err := scanner.scanItem()
		if err != nil {
			return 0, err
		}
		if closed {
			return next, nil
		}
		scanner.index = next
	}
	return 0, fmt.Errorf("unterminated shell arithmetic expansion")
}

func (scanner *shellArithmeticScanner) scanItem() (int, bool, error) {
	value := scanner.data[scanner.index]
	if value == '\\' {
		return scanner.scanEscape()
	}
	if value == '\'' || value == '"' {
		return scanner.scanQuoted()
	}
	if value == '(' {
		scanner.depth++
		return scanner.index + 1, false, nil
	}
	if value == ')' {
		return scanner.scanClosing()
	}
	return scanner.index + 1, false, nil
}

func (scanner *shellArithmeticScanner) scanEscape() (int, bool, error) {
	if scanner.index+1 >= len(scanner.data) {
		return 0, false, fmt.Errorf("unterminated shell arithmetic escape")
	}
	return scanner.index + 2, false, nil
}

func (scanner *shellArithmeticScanner) scanQuoted() (int, bool, error) {
	quote := scanner.data[scanner.index]
	index := scanner.index + 1
	for index < len(scanner.data) {
		if scanner.data[index] == '\\' {
			if index+1 >= len(scanner.data) {
				return 0, false, fmt.Errorf("unterminated shell arithmetic string")
			}
			index += 2
			continue
		}
		if scanner.data[index] == quote {
			return index + 1, false, nil
		}
		index++
	}
	return index, false, nil
}

func (scanner *shellArithmeticScanner) scanClosing() (int, bool, error) {
	if scanner.depth == 1 && shellHasNext(scanner.data, scanner.index, ')') {
		return scanner.index + 2, true, nil
	}
	if scanner.depth > 1 {
		scanner.depth--
	}
	return scanner.index + 1, false, nil
}

func (scanner *shellCommentScanner) scanParameter(index int) (int, error) {
	depth := 1
	for index < len(scanner.data) {
		next, nextDepth, closed, err := scanner.scanParameterItem(index, depth)
		if err != nil {
			return 0, err
		}
		if closed {
			return next, nil
		}
		index = next
		depth = nextDepth
	}
	return 0, fmt.Errorf("unterminated shell parameter expansion")
}

func (scanner *shellCommentScanner) scanParameterItem(index, depth int) (int, int, bool, error) {
	value := scanner.data[index]
	if value == '\\' {
		return scanner.scanParameterEscape(index, depth)
	}
	if value == '\'' {
		return scanner.scanParameterSingleQuote(index, depth)
	}
	if value == '"' {
		return scanner.scanParameterDoubleQuote(index, depth)
	}
	if value == '$' {
		return scanner.scanParameterDollar(index, depth)
	}
	if value == '{' {
		return index + 1, depth + 1, false, nil
	}
	if value == '}' {
		return shellParameterClosing(index, depth)
	}
	return index + 1, depth, false, nil
}

func (scanner *shellCommentScanner) scanParameterEscape(index, depth int) (int, int, bool, error) {
	if index+1 >= len(scanner.data) {
		return 0, depth, false, fmt.Errorf("unterminated shell parameter escape")
	}
	return index + 2, depth, false, nil
}

func (scanner *shellCommentScanner) scanParameterSingleQuote(index, depth int) (int, int, bool, error) {
	end, err := scanShellSingleQuote(scanner.data, index)
	return end, depth, false, err
}

func (scanner *shellCommentScanner) scanParameterDoubleQuote(index, depth int) (int, int, bool, error) {
	end, err := scanner.scanDoubleQuote(index)
	return end, depth, false, err
}

func (scanner *shellCommentScanner) scanParameterDollar(index, depth int) (int, int, bool, error) {
	end, handled, err := scanner.scanDollar(index)
	if err != nil {
		return 0, depth, false, err
	}
	if handled {
		return end, depth, false, nil
	}
	return index + 1, depth, false, nil
}

func shellParameterClosing(index, depth int) (int, int, bool, error) {
	depth--
	return index + 1, depth, depth == 0, nil
}

func (scanner *shellCommentScanner) readHereDoc(index int) (int, error) {
	index += 2
	if shellHereStringStart(scanner.data, index) {
		return index + 1, nil
	}
	index, stripTabs := shellHereDocOptions(scanner.data, index)
	index = shellSkipHorizontal(scanner.data, index)
	if !shellHereDocDelimiterStart(scanner.data, index) {
		return 0, fmt.Errorf("shell heredoc has no delimiter")
	}
	delimiter, quoted, end, err := shellHereDocDelimiter(scanner.data, index)
	if err != nil {
		return 0, err
	}
	scanner.pending = append(scanner.pending, shellHereDoc{delimiter: delimiter, stripTabs: stripTabs, quoted: quoted})
	return end, nil
}

func shellHereStringStart(data []byte, index int) bool {
	return index < len(data) && data[index] == '<'
}

func shellHereDocOptions(data []byte, index int) (int, bool) {
	if index < len(data) && data[index] == '-' {
		return index + 1, true
	}
	return index, false
}

func shellSkipHorizontal(data []byte, index int) int {
	for index < len(data) && shellHorizontalSpace(data[index]) {
		index++
	}
	return index
}

func shellHereDocDelimiterStart(data []byte, index int) bool {
	return index < len(data) && !shellLineEnding(data[index])
}

func shellHereDocDelimiter(data []byte, index int) (string, bool, int, error) {
	scanner := shellHereDocDelimiterScanner{data: data, index: index}
	return scanner.scan()
}

type shellHereDocDelimiterScanner struct {
	data   []byte
	index  int
	value  strings.Builder
	quoted bool
}

func (scanner *shellHereDocDelimiterScanner) scan() (string, bool, int, error) {
	for scanner.index < len(scanner.data) && !shellHereDocTerminator(scanner.data[scanner.index]) {
		handled, err := scanner.scanSpecial()
		if err != nil {
			return "", false, 0, err
		}
		if handled {
			continue
		}
		scanner.value.WriteByte(scanner.data[scanner.index])
		scanner.index++
	}
	if scanner.value.Len() == 0 {
		return "", false, 0, fmt.Errorf("shell heredoc has no delimiter")
	}
	return scanner.value.String(), scanner.quoted, scanner.index, nil
}

func (scanner *shellHereDocDelimiterScanner) scanSpecial() (bool, error) {
	if scanner.data[scanner.index] == '\\' {
		return true, scanner.scanEscape()
	}
	if shellQuote(scanner.data[scanner.index]) {
		return true, scanner.scanQuoted()
	}
	return false, nil
}

func (scanner *shellHereDocDelimiterScanner) scanEscape() error {
	if scanner.index+1 >= len(scanner.data) {
		return fmt.Errorf("unterminated shell heredoc delimiter")
	}
	scanner.quoted = true
	scanner.value.WriteByte(scanner.data[scanner.index+1])
	scanner.index += 2
	return nil
}

func (scanner *shellHereDocDelimiterScanner) scanQuoted() error {
	quote := scanner.data[scanner.index]
	scanner.quoted = true
	scanner.index++
	for scanner.index < len(scanner.data) {
		if scanner.scanQuotedEscape(quote) {
			continue
		}
		if scanner.data[scanner.index] == quote {
			scanner.index++
			return nil
		}
		scanner.value.WriteByte(scanner.data[scanner.index])
		scanner.index++
	}
	return fmt.Errorf("unterminated shell heredoc delimiter")
}

func (scanner *shellHereDocDelimiterScanner) scanQuotedEscape(quote byte) bool {
	if quote != '"' || scanner.data[scanner.index] != '\\' || scanner.index+1 >= len(scanner.data) {
		return false
	}
	scanner.value.WriteByte(scanner.data[scanner.index+1])
	scanner.index += 2
	return true
}

func shellHereDocTerminator(value byte) bool {
	return shellHorizontalSpace(value) || shellLineEnding(value) || strings.ContainsRune(";|&<>()", rune(value))
}

func shellQuote(value byte) bool {
	return value == '\'' || value == '"'
}

func (scanner *shellCommentScanner) skipHereDocs(index int) (int, error) {
	for _, hereDoc := range scanner.pending {
		found := false
		bodyStart := index
		for index < len(scanner.data) {
			lineStart := index
			end := lineEnd(scanner.data, index)
			line := scanner.data[index:end]
			if hereDoc.stripTabs {
				line = bytesTrimLeftTabs(line)
			}
			index = nextLine(scanner.data, end)
			if string(line) == hereDoc.delimiter {
				if !hereDoc.quoted {
					if err := scanner.scanHereDocExpansions(bodyStart, lineStart); err != nil {
						return 0, err
					}
				}
				found = true
				break
			}
		}
		if !found {
			return 0, fmt.Errorf("unterminated shell heredoc")
		}
	}
	scanner.pending = nil
	return index, nil
}

func (scanner *shellCommentScanner) scanHereDocExpansions(index, limit int) error {
	pending := scanner.pending
	scanner.pending = nil
	defer func() {
		scanner.pending = pending
	}()
	for index < limit {
		next, err := scanner.scanHereDocExpansion(index, limit)
		if err != nil {
			return err
		}
		index = next
	}
	return nil
}

func (scanner *shellCommentScanner) scanHereDocExpansion(index, limit int) (int, error) {
	if scanner.data[index] == '\\' {
		return shellHereDocEscapeEnd(scanner.data, index, limit), nil
	}
	if scanner.data[index] == '`' {
		return scanner.scanHereDocBacktick(index, limit)
	}
	if scanner.data[index] == '$' {
		return scanner.scanHereDocDollar(index, limit)
	}
	return index + 1, nil
}

func shellHereDocEscapeEnd(data []byte, index, limit int) int {
	if index+1 >= limit || !shellHereDocEscaped(data[index+1]) {
		return index + 1
	}
	return index + 2
}

func shellHereDocEscaped(value byte) bool {
	return value == '\\' || value == '$' || value == '`' || shellLineEnding(value)
}

func (scanner *shellCommentScanner) scanHereDocBacktick(index, limit int) (int, error) {
	end, err := scanner.scan(index+1, '`')
	if err != nil || end > limit {
		return 0, fmt.Errorf("unterminated shell heredoc command substitution")
	}
	return end, nil
}

func (scanner *shellCommentScanner) scanHereDocDollar(index, limit int) (int, error) {
	end, handled, err := scanner.scanDollar(index)
	if err != nil {
		return 0, err
	}
	if !handled {
		return index + 1, nil
	}
	if end > limit {
		return 0, fmt.Errorf("unterminated shell heredoc expansion")
	}
	return end, nil
}

func bytesTrimLeftTabs(value []byte) []byte {
	for len(value) > 0 && value[0] == '\t' {
		value = value[1:]
	}
	return value
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
	return shellSourceCommandFollows(data, annotation.offset)
}

func shellSourceCommandFollows(data []byte, offset int) bool {
	index := nextLine(data, lineEnd(data, offset))
	index, found := shellNextSourceLine(data, index)
	if !found {
		return false
	}
	index, found = shellSourceCommandEnd(data, index)
	if !found {
		return false
	}
	return shellSourceArgumentFollows(data, index)
}

func shellNextSourceLine(data []byte, index int) (int, bool) {
	index = shellSkipHorizontal(data, index)
	return index, index < len(data) && !shellLineEnding(data[index])
}

func shellSourceCommandEnd(data []byte, index int) (int, bool) {
	if strings.HasPrefix(string(data[index:]), "source") {
		return index + len("source"), true
	}
	if data[index] == '.' {
		return index + 1, true
	}
	return 0, false
}

func shellSourceArgumentFollows(data []byte, index int) bool {
	if index >= len(data) || !shellHorizontalSpace(data[index]) {
		return false
	}
	index = shellSkipHorizontal(data, index)
	return index < len(data) && !shellLineEnding(data[index]) && data[index] != '#'
}
