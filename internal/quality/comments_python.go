package quality

import (
	"fmt"
	"regexp"
	"strings"
)

var pythonEncodingPlain = regexp.MustCompile(`^#[ \t]*coding[:=][ \t]*[-_.A-Za-z0-9]+[ \t]*$`)
var pythonEncodingEmacs = regexp.MustCompile(`^#[ \t]*-\*-[ \t]*coding[:=][ \t]*[-_.A-Za-z0-9]+[ \t]*-\*-[ \t]*$`)

func pythonEncodingAllowed(data []byte, annotation sourceAnnotation) bool {
	source := newSourceText(data)
	line, _ := source.location(annotation.offset)
	if !pythonEncodingLineAllowed(data, source, line, annotation) {
		return false
	}
	return pythonEncodingPlain.MatchString(annotation.text) || pythonEncodingEmacs.MatchString(annotation.text)
}

func pythonEncodingLineAllowed(data []byte, source sourceText, line int, annotation sourceAnnotation) bool {
	if line < 1 || line > 2 {
		return false
	}
	lineStart := pythonEncodingLineStart(data, source, line)
	if !pythonOnlyHorizontal(data[lineStart:annotation.offset]) {
		return false
	}
	return line != 2 || pythonFirstLineAllowsEncoding(data)
}

func pythonEncodingLineStart(data []byte, source sourceText, line int) int {
	if line == 1 && pythonByteOrderMark(data) {
		return 3
	}
	return source.lineStarts[line-1]
}

func pythonOnlyHorizontal(data []byte) bool {
	for _, value := range data {
		if !pythonHorizontalSpace(value) {
			return false
		}
	}
	return true
}

func pythonFirstLineAllowsEncoding(data []byte) bool {
	line := data[:lineEnd(data, 0)]
	if pythonByteOrderMark(line) {
		line = line[3:]
	}
	for len(line) > 0 && pythonHorizontalSpace(line[0]) {
		line = line[1:]
	}
	return len(line) == 0 || line[0] == '#'
}

func pythonByteOrderMark(data []byte) bool {
	return len(data) >= 3 && string(data[:3]) == "\xef\xbb\xbf"
}

func pythonHorizontalSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\f'
}

func pythonLineEnding(value byte) bool {
	return value == '\r' || value == '\n'
}

type pythonTokenKind uint8

const (
	pythonName pythonTokenKind = iota
	pythonString
	pythonColon
	pythonOther
)

type pythonToken struct {
	kind   pythonTokenKind
	offset int
	text   string
	depth  int
	plain  bool
}

type pythonStatement struct {
	indent int
	tokens []pythonToken
}

type pythonScope struct {
	indent int
	seen   bool
}

type pythonPendingScope struct {
	indent int
}

type pythonTokenizer struct {
	data        []byte
	annotations []sourceAnnotation
	statements  []pythonStatement
	current     pythonStatement
	lineStart   bool
	indent      int
	depth       int
	index       int
}

func scanPythonSource(data []byte) ([]sourceAnnotation, error) {
	annotations, statements, err := scanPythonTokens(data)
	if err != nil {
		return nil, err
	}
	docstrings, err := pythonDocstrings(statements)
	if err != nil {
		return nil, err
	}
	return append(annotations, docstrings...), nil
}

func scanPythonTokens(data []byte) ([]sourceAnnotation, []pythonStatement, error) {
	scanner := newPythonTokenizer(data)
	if err := scanner.scan(); err != nil {
		return nil, nil, err
	}
	return scanner.annotations, scanner.statements, nil
}

func newPythonTokenizer(data []byte) pythonTokenizer {
	index := 0
	if pythonByteOrderMark(data) {
		index = 3
	}
	return pythonTokenizer{data: data, lineStart: true, index: index}
}

func (scanner *pythonTokenizer) scan() error {
	for scanner.index < len(scanner.data) {
		if err := scanner.scanNext(); err != nil {
			return err
		}
	}
	if scanner.depth != 0 {
		return fmt.Errorf("unterminated Python delimiter")
	}
	scanner.flush()
	return nil
}

func (scanner *pythonTokenizer) scanNext() error {
	if scanner.consumeIndent() || scanner.consumeLineEnding() {
		return nil
	}
	scanner.lineStart = false
	if scanner.consumeHorizontalSpace() || scanner.consumeComment() || scanner.consumeLineContinuation() {
		return nil
	}
	if isPythonIdentifierStart(scanner.data[scanner.index]) {
		return scanner.scanIdentifier()
	}
	if pythonQuoteAt(scanner.data, scanner.index) {
		return scanner.scanPlainString()
	}
	return scanner.scanPunctuation()
}

func (scanner *pythonTokenizer) consumeIndent() bool {
	if !scanner.lineStart || !pythonHorizontalSpace(scanner.data[scanner.index]) {
		return false
	}
	if scanner.data[scanner.index] == '\t' {
		scanner.indent += 8 - scanner.indent%8
	} else {
		scanner.indent++
	}
	scanner.index++
	return true
}

func (scanner *pythonTokenizer) consumeLineEnding() bool {
	if !pythonLineEnding(scanner.data[scanner.index]) {
		return false
	}
	if scanner.depth == 0 {
		scanner.flush()
	}
	scanner.index = nextLine(scanner.data, scanner.index)
	scanner.lineStart = true
	scanner.indent = 0
	return true
}

func (scanner *pythonTokenizer) consumeHorizontalSpace() bool {
	if !pythonHorizontalSpace(scanner.data[scanner.index]) {
		return false
	}
	scanner.index++
	return true
}

func (scanner *pythonTokenizer) consumeComment() bool {
	if scanner.data[scanner.index] != '#' {
		return false
	}
	end := lineEnd(scanner.data, scanner.index)
	scanner.annotations = append(scanner.annotations, sourceAnnotation{offset: scanner.index, text: string(scanner.data[scanner.index:end])})
	scanner.index = end
	return true
}

func (scanner *pythonTokenizer) consumeLineContinuation() bool {
	if scanner.data[scanner.index] != '\\' {
		return false
	}
	next := scanner.index + 1
	if next >= len(scanner.data) || !pythonLineEnding(scanner.data[next]) {
		return false
	}
	scanner.index = nextLine(scanner.data, next)
	scanner.lineStart = true
	scanner.indent = 0
	return true
}

func (scanner *pythonTokenizer) scanIdentifier() error {
	start := scanner.index
	end := pythonIdentifierEnd(scanner.data, start)
	if !pythonQuoteAt(scanner.data, end) {
		scanner.emit(pythonToken{kind: pythonName, offset: start, text: string(scanner.data[start:end]), depth: scanner.depth})
		scanner.index = end
		return nil
	}
	return scanner.scanPrefixedString(start, end)
}

func (scanner *pythonTokenizer) scanPrefixedString(start, quote int) error {
	prefix := strings.ToLower(string(scanner.data[start:quote]))
	if !pythonStringPrefix[prefix] {
		return fmt.Errorf("invalid Python string prefix at byte %d", start)
	}
	end, annotations, err := scanPythonString(scanner.data, quote, strings.Contains(prefix, "f"))
	if err != nil {
		return err
	}
	scanner.annotations = append(scanner.annotations, annotations...)
	scanner.emit(pythonToken{kind: pythonString, offset: start, depth: scanner.depth, plain: !strings.ContainsAny(prefix, "fb")})
	scanner.index = end
	return nil
}

func (scanner *pythonTokenizer) scanPlainString() error {
	start := scanner.index
	end, annotations, err := scanPythonString(scanner.data, start, false)
	if err != nil {
		return err
	}
	scanner.annotations = append(scanner.annotations, annotations...)
	scanner.emit(pythonToken{kind: pythonString, offset: start, depth: scanner.depth, plain: true})
	scanner.index = end
	return nil
}

func (scanner *pythonTokenizer) scanPunctuation() error {
	value := scanner.data[scanner.index]
	scanner.emit(pythonToken{kind: pythonPunctuationKind(value), offset: scanner.index, text: string(value), depth: scanner.depth})
	depth, err := pythonDelimiterDepth(scanner.depth, value, scanner.index)
	if err != nil {
		return err
	}
	scanner.depth = depth
	if value == ';' && scanner.depth == 0 {
		scanner.current.tokens = scanner.current.tokens[:len(scanner.current.tokens)-1]
		scanner.flush()
	}
	scanner.index++
	return nil
}

func (scanner *pythonTokenizer) flush() {
	if len(scanner.current.tokens) == 0 {
		return
	}
	scanner.statements = append(scanner.statements, scanner.current)
	scanner.current = pythonStatement{}
}

func (scanner *pythonTokenizer) emit(token pythonToken) {
	if len(scanner.current.tokens) == 0 {
		scanner.current.indent = scanner.indent
	}
	scanner.current.tokens = append(scanner.current.tokens, token)
}

func pythonPunctuationKind(value byte) pythonTokenKind {
	if value == ':' {
		return pythonColon
	}
	return pythonOther
}

func pythonDelimiterDepth(depth int, value byte, index int) (int, error) {
	switch value {
	case '(', '[', '{':
		return depth + 1, nil
	case ')', ']', '}':
		if depth == 0 {
			return 0, fmt.Errorf("unexpected Python closing delimiter at byte %d", index)
		}
		return depth - 1, nil
	default:
		return depth, nil
	}
}

func pythonIdentifierEnd(data []byte, index int) int {
	for index < len(data) && isPythonIdentifierContinue(data[index]) {
		index++
	}
	return index
}

var pythonStringPrefix = map[string]bool{
	"r": true, "u": true, "b": true, "br": true, "rb": true,
	"f": true, "fr": true, "rf": true,
}

func isPythonIdentifierStart(value byte) bool {
	return value == '_' || value >= 0x80 || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func isPythonIdentifierContinue(value byte) bool {
	return isPythonIdentifierStart(value) || value >= '0' && value <= '9'
}

func pythonQuoteAt(data []byte, index int) bool {
	return index < len(data) && (data[index] == '\'' || data[index] == '"')
}

type pythonStringScanner struct {
	data        []byte
	quote       byte
	triple      bool
	formatted   bool
	index       int
	annotations []sourceAnnotation
}

func scanPythonString(data []byte, index int, formatted bool) (int, []sourceAnnotation, error) {
	scanner := newPythonStringScanner(data, index, formatted)
	return scanner.scan()
}

func newPythonStringScanner(data []byte, index int, formatted bool) pythonStringScanner {
	quote := data[index]
	triple := pythonTripleQuoteAt(data, index, quote)
	index++
	if triple {
		index += 2
	}
	return pythonStringScanner{data: data, quote: quote, triple: triple, formatted: formatted, index: index}
}

func (scanner *pythonStringScanner) scan() (int, []sourceAnnotation, error) {
	for scanner.index < len(scanner.data) {
		end, annotations, handled, err := scanner.scanFormatted()
		if err != nil {
			return 0, nil, err
		}
		if handled {
			scanner.annotations = append(scanner.annotations, annotations...)
			scanner.index = end
			continue
		}
		if scanner.consumeEscape() {
			continue
		}
		if scanner.singleLineNewline() {
			return 0, nil, fmt.Errorf("unterminated Python string")
		}
		if end, closed := scanner.terminator(); closed {
			return end, scanner.annotations, nil
		}
		scanner.index++
	}
	return 0, nil, fmt.Errorf("unterminated Python string")
}

func (scanner *pythonStringScanner) scanFormatted() (int, []sourceAnnotation, bool, error) {
	if !scanner.formatted {
		return 0, nil, false, nil
	}
	if scanner.data[scanner.index] == '{' {
		return scanner.scanOpeningBrace()
	}
	if scanner.data[scanner.index] == '}' {
		return scanner.scanClosingBrace()
	}
	return 0, nil, false, nil
}

func (scanner *pythonStringScanner) scanOpeningBrace() (int, []sourceAnnotation, bool, error) {
	if scanner.index+1 < len(scanner.data) && scanner.data[scanner.index+1] == '{' {
		return scanner.index + 2, nil, true, nil
	}
	end, annotations, err := scanPythonFStringField(scanner.data, scanner.index+1)
	return end, annotations, true, err
}

func (scanner *pythonStringScanner) scanClosingBrace() (int, []sourceAnnotation, bool, error) {
	if scanner.index+1 < len(scanner.data) && scanner.data[scanner.index+1] == '}' {
		return scanner.index + 2, nil, true, nil
	}
	return 0, nil, true, fmt.Errorf("unescaped Python f-string closing brace")
}

func (scanner *pythonStringScanner) consumeEscape() bool {
	if scanner.data[scanner.index] != '\\' {
		return false
	}
	if scanner.index+1 >= len(scanner.data) {
		scanner.index++
		return true
	}
	scanner.index += 2
	return true
}

func (scanner *pythonStringScanner) singleLineNewline() bool {
	return !scanner.triple && pythonLineEnding(scanner.data[scanner.index])
}

func (scanner *pythonStringScanner) terminator() (int, bool) {
	if scanner.data[scanner.index] != scanner.quote {
		return 0, false
	}
	if !scanner.triple {
		return scanner.index + 1, true
	}
	if pythonTripleQuoteAt(scanner.data, scanner.index, scanner.quote) {
		return scanner.index + 3, true
	}
	return 0, false
}

func pythonTripleQuoteAt(data []byte, index int, quote byte) bool {
	return index+2 < len(data) && data[index] == quote && data[index+1] == quote && data[index+2] == quote
}

type pythonFStringFieldScanner struct {
	data        []byte
	index       int
	depth       int
	formatSpec  bool
	annotations []sourceAnnotation
}

func scanPythonFStringField(data []byte, index int) (int, []sourceAnnotation, error) {
	scanner := pythonFStringFieldScanner{data: data, index: index, depth: 1}
	return scanner.scan()
}

func (scanner *pythonFStringFieldScanner) scan() (int, []sourceAnnotation, error) {
	for scanner.index < len(scanner.data) {
		if scanner.scanComment() {
			continue
		}
		handled, err := scanner.scanString()
		if err != nil {
			return 0, nil, err
		}
		if handled {
			continue
		}
		handled, err = scanner.scanFormatSpec()
		if err != nil {
			return 0, nil, err
		}
		if handled {
			continue
		}
		closed, err := scanner.scanDelimiter()
		if err != nil {
			return 0, nil, err
		}
		if closed {
			return scanner.index, scanner.annotations, nil
		}
		scanner.index++
	}
	return 0, nil, fmt.Errorf("unterminated Python f-string replacement field")
}

func (scanner *pythonFStringFieldScanner) scanComment() bool {
	if scanner.formatSpec || scanner.data[scanner.index] != '#' {
		return false
	}
	end := lineEnd(scanner.data, scanner.index)
	scanner.annotations = append(scanner.annotations, sourceAnnotation{offset: scanner.index, text: string(scanner.data[scanner.index:end])})
	scanner.index = end
	return true
}

func (scanner *pythonFStringFieldScanner) scanString() (bool, error) {
	if pythonQuoteAt(scanner.data, scanner.index) {
		return scanner.scanQuotedString(scanner.index, false)
	}
	if !isPythonIdentifierStart(scanner.data[scanner.index]) {
		return false, nil
	}
	end := pythonIdentifierEnd(scanner.data, scanner.index)
	if !pythonQuoteAt(scanner.data, end) {
		scanner.index = end
		return true, nil
	}
	prefix := strings.ToLower(string(scanner.data[scanner.index:end]))
	if !pythonStringPrefix[prefix] {
		return false, fmt.Errorf("invalid Python string prefix at byte %d", scanner.index)
	}
	return scanner.scanQuotedString(end, strings.Contains(prefix, "f"))
}

func (scanner *pythonFStringFieldScanner) scanQuotedString(index int, formatted bool) (bool, error) {
	end, annotations, err := scanPythonString(scanner.data, index, formatted)
	if err != nil {
		return false, err
	}
	scanner.annotations = append(scanner.annotations, annotations...)
	scanner.index = end
	return true, nil
}

func (scanner *pythonFStringFieldScanner) scanFormatSpec() (bool, error) {
	if scanner.data[scanner.index] == ':' && scanner.depth == 1 {
		scanner.formatSpec = true
		scanner.index++
		return true, nil
	}
	if !scanner.formatSpec || scanner.data[scanner.index] != '{' {
		return false, nil
	}
	if scanner.index+1 < len(scanner.data) && scanner.data[scanner.index+1] == '{' {
		scanner.index += 2
		return true, nil
	}
	end, annotations, err := scanPythonFStringField(scanner.data, scanner.index+1)
	if err != nil {
		return false, err
	}
	scanner.annotations = append(scanner.annotations, annotations...)
	scanner.index = end
	return true, nil
}

func (scanner *pythonFStringFieldScanner) scanDelimiter() (bool, error) {
	value := scanner.data[scanner.index]
	if pythonOpeningDelimiter(value) {
		scanner.depth++
		return false, nil
	}
	if value == '}' {
		scanner.depth--
		if scanner.depth == 0 {
			scanner.index++
			return true, nil
		}
		return false, nil
	}
	if !pythonClosingDelimiter(value) {
		return false, nil
	}
	if scanner.depth == 1 {
		return false, fmt.Errorf("unexpected Python f-string closing delimiter")
	}
	scanner.depth--
	return false, nil
}

func pythonOpeningDelimiter(value byte) bool {
	return value == '{' || value == '(' || value == '['
}

func pythonClosingDelimiter(value byte) bool {
	return value == ')' || value == ']'
}

type pythonDocstringScanner struct {
	annotations []sourceAnnotation
	scopes      []pythonScope
	pending     *pythonPendingScope
}

func pythonDocstrings(statements []pythonStatement) ([]sourceAnnotation, error) {
	scanner := pythonDocstringScanner{scopes: []pythonScope{{indent: 0}}}
	for _, statement := range statements {
		if err := scanner.scanStatement(statement); err != nil {
			return nil, err
		}
	}
	if scanner.pending != nil {
		return nil, fmt.Errorf("python class or function has no body")
	}
	return scanner.annotations, nil
}

func (scanner *pythonDocstringScanner) scanStatement(statement pythonStatement) error {
	scanner.popScopes(statement.indent)
	if err := scanner.openPendingScope(statement); err != nil {
		return err
	}
	scanner.recordScopeDocstring(statement)
	scanner.recordDefinitionDocstring(statement)
	return nil
}

func (scanner *pythonDocstringScanner) popScopes(indent int) {
	for len(scanner.scopes) > 1 && scanner.scopes[len(scanner.scopes)-1].indent > indent {
		scanner.scopes = scanner.scopes[:len(scanner.scopes)-1]
	}
}

func (scanner *pythonDocstringScanner) openPendingScope(statement pythonStatement) error {
	if scanner.pending == nil {
		return nil
	}
	if statement.indent <= scanner.pending.indent {
		return fmt.Errorf("python class or function has no indented body")
	}
	scanner.scopes = append(scanner.scopes, pythonScope{indent: statement.indent})
	scanner.pending = nil
	return nil
}

func (scanner *pythonDocstringScanner) recordScopeDocstring(statement pythonStatement) {
	for index := len(scanner.scopes) - 1; index >= 0; index-- {
		if scanner.scopes[index].indent != statement.indent || scanner.scopes[index].seen {
			continue
		}
		if annotation, found := pythonStringStatement(statement.tokens); found {
			scanner.annotations = append(scanner.annotations, annotation)
		}
		scanner.scopes[index].seen = true
		return
	}
}

func (scanner *pythonDocstringScanner) recordDefinitionDocstring(statement pythonStatement) {
	definition, colon := pythonDefinition(statement.tokens)
	if !definition {
		return
	}
	if colon+1 < len(statement.tokens) {
		if annotation, found := pythonStringStatement(statement.tokens[colon+1:]); found {
			scanner.annotations = append(scanner.annotations, annotation)
		}
		return
	}
	scanner.pending = &pythonPendingScope{indent: statement.indent}
}

func pythonDefinition(tokens []pythonToken) (bool, int) {
	definition := false
	colon := -1
	for index, token := range tokens {
		if token.depth != 0 {
			continue
		}
		if token.kind == pythonName && (token.text == "def" || token.text == "class") {
			definition = true
		}
		if token.kind == pythonColon {
			colon = index
		}
	}
	return definition && colon >= 0, colon
}

func pythonStringStatement(tokens []pythonToken) (sourceAnnotation, bool) {
	annotation := sourceAnnotation{}
	found := false
	for _, token := range tokens {
		if token.kind == pythonString && token.plain {
			if !found {
				annotation = sourceAnnotation{offset: token.offset}
				found = true
			}
			continue
		}
		if token.kind == pythonOther && (token.text == "(" || token.text == ")") {
			continue
		}
		return sourceAnnotation{}, false
	}
	return annotation, found
}
