package architecture

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

type pythonImportReference struct {
	Module  string
	Names   []string
	Line    int
	Literal bool
}

type pythonImportToken struct {
	Value string
	Line  int
}

type pythonModuleIndex struct {
	files     map[string][]string
	packages  map[string]bool
	packageOf map[string]string
	ambiguous map[string]bool
}

func newPythonModuleIndex(project repository.PythonProject) pythonModuleIndex {
	index := pythonModuleIndex{
		files: map[string][]string{}, packages: map[string]bool{}, packageOf: map[string]string{}, ambiguous: map[string]bool{},
	}
	for _, candidate := range project.PackageCandidates {
		index.packages[candidate.Name] = true
	}
	for _, source := range project.Files {
		module, packageName := repository.PythonModuleName(project, source)
		index.packageOf[source] = packageName
		if module == "" {
			continue
		}
		index.files[module] = append(index.files[module], source)
		parts := strings.Split(module, ".")
		for length := 1; length <= len(parts); length++ {
			index.packages[strings.Join(parts[:length], ".")] = true
		}
	}
	for module := range index.files {
		sort.Strings(index.files[module])
		if pythonConflictingModulePaths(index.files[module]) {
			index.ambiguous[module] = true
		}
	}
	return index
}

func pythonConflictingModulePaths(paths []string) bool {
	locations := map[string]bool{}
	for _, path := range paths {
		location := strings.TrimSuffix(strings.TrimSuffix(path, ".pyi"), ".py")
		locations[location] = true
	}
	return len(locations) > 1
}

func pythonImportReferences(data []byte) []pythonImportReference {
	code := pythonCode(data)
	references := pythonStaticImportReferences(code)
	return append(references, pythonDynamicImportReferences(data, code)...)
}

type pythonCodeScanner struct {
	source []byte
	code   []byte
	index  int
	quote  byte
	triple bool
}

func pythonCode(data []byte) []byte {
	scanner := pythonCodeScanner{source: data, code: append([]byte{}, data...)}
	for scanner.index < len(scanner.code) {
		scanner.scan()
	}
	return scanner.code
}

func (scanner *pythonCodeScanner) scan() {
	if scanner.quote != 0 {
		scanner.consumeString()
		return
	}
	switch scanner.source[scanner.index] {
	case '#':
		scanner.consumeComment()
	case '\'', '"':
		scanner.openString()
	default:
		scanner.index++
	}
}

func (scanner *pythonCodeScanner) consumeString() {
	if scanner.endsTripleString() {
		scanner.closeTripleString()
		return
	}
	if scanner.endsString() {
		scanner.closeString()
		return
	}
	if scanner.consumeEscapedCharacter() {
		return
	}
	scanner.maskStringCharacter()
}

func (scanner *pythonCodeScanner) endsTripleString() bool {
	return scanner.triple && scanner.index+2 < len(scanner.code) &&
		scanner.source[scanner.index] == scanner.quote && scanner.source[scanner.index+1] == scanner.quote && scanner.source[scanner.index+2] == scanner.quote
}

func (scanner *pythonCodeScanner) closeTripleString() {
	scanner.code[scanner.index], scanner.code[scanner.index+1], scanner.code[scanner.index+2] = ' ', ' ', ' '
	scanner.index += 3
	scanner.quote = 0
}

func (scanner *pythonCodeScanner) endsString() bool {
	return !scanner.triple && scanner.source[scanner.index] == scanner.quote
}

func (scanner *pythonCodeScanner) closeString() {
	scanner.code[scanner.index] = ' '
	scanner.index++
	scanner.quote = 0
}

func (scanner *pythonCodeScanner) consumeEscapedCharacter() bool {
	if scanner.source[scanner.index] != '\\' || scanner.index+1 >= len(scanner.code) {
		return false
	}
	scanner.code[scanner.index] = ' '
	scanner.index++
	if scanner.source[scanner.index] == '\n' {
		return true
	}
	scanner.code[scanner.index] = ' '
	scanner.index++
	return true
}

func (scanner *pythonCodeScanner) maskStringCharacter() {
	if scanner.source[scanner.index] != '\n' {
		scanner.code[scanner.index] = ' '
	}
	scanner.index++
}

func (scanner *pythonCodeScanner) consumeComment() {
	for scanner.index < len(scanner.code) && scanner.source[scanner.index] != '\n' {
		scanner.code[scanner.index] = ' '
		scanner.index++
	}
}

func (scanner *pythonCodeScanner) openString() {
	scanner.quote = scanner.source[scanner.index]
	scanner.triple = scanner.index+2 < len(scanner.code) && scanner.source[scanner.index+1] == scanner.quote && scanner.source[scanner.index+2] == scanner.quote
	scanner.code[scanner.index] = ' '
	scanner.index++
	if scanner.triple {
		scanner.code[scanner.index], scanner.code[scanner.index+1] = ' ', ' '
		scanner.index += 2
	}
}

func pythonStaticImportReferences(code []byte) []pythonImportReference {
	references := []pythonImportReference{}
	tokens := pythonImportTokens(code)
	for index := 0; index < len(tokens); {
		switch tokens[index].Value {
		case "from":
			reference, next, found := pythonFromImportReference(tokens, index)
			if found {
				references = append(references, reference)
				index = next
				continue
			}
		case "import":
			imports, next, found := pythonDirectImportReferences(tokens, index)
			if found {
				references = append(references, imports...)
				index = next
				continue
			}
		}
		index++
	}
	return references
}

type pythonImportTokenScanner struct {
	code  []byte
	index int
	line  int
}

func pythonImportTokens(code []byte) []pythonImportToken {
	scanner := pythonImportTokenScanner{code: code, line: 1}
	tokens := []pythonImportToken{}
	for scanner.index < len(scanner.code) {
		if scanner.consumeContinuedLine() || scanner.consumeNewline(&tokens) || scanner.consumeIdentifier(&tokens) {
			continue
		}
		scanner.consumeSymbol(&tokens)
		scanner.index++
	}
	return tokens
}

func (scanner *pythonImportTokenScanner) consumeContinuedLine() bool {
	if scanner.index+2 < len(scanner.code) && scanner.code[scanner.index] == '\\' && scanner.code[scanner.index+1] == '\r' && scanner.code[scanner.index+2] == '\n' {
		scanner.index += 3
		scanner.line++
		return true
	}
	if scanner.index+1 < len(scanner.code) && scanner.code[scanner.index] == '\\' && scanner.code[scanner.index+1] == '\n' {
		scanner.index += 2
		scanner.line++
		return true
	}
	return false
}

func (scanner *pythonImportTokenScanner) consumeNewline(tokens *[]pythonImportToken) bool {
	if scanner.code[scanner.index] != '\n' {
		return false
	}
	*tokens = append(*tokens, pythonImportToken{Value: "\n", Line: scanner.line})
	scanner.index++
	scanner.line++
	return true
}

func (scanner *pythonImportTokenScanner) consumeIdentifier(tokens *[]pythonImportToken) bool {
	if !pythonIdentifierByte(scanner.code[scanner.index]) || scanner.code[scanner.index] >= '0' && scanner.code[scanner.index] <= '9' {
		return false
	}
	start := scanner.index
	for scanner.index < len(scanner.code) && pythonIdentifierByte(scanner.code[scanner.index]) {
		scanner.index++
	}
	*tokens = append(*tokens, pythonImportToken{Value: string(scanner.code[start:scanner.index]), Line: scanner.line})
	return true
}

func (scanner *pythonImportTokenScanner) consumeSymbol(tokens *[]pythonImportToken) {
	if strings.ContainsRune(".,;()*", rune(scanner.code[scanner.index])) {
		*tokens = append(*tokens, pythonImportToken{Value: string(scanner.code[scanner.index]), Line: scanner.line})
	}
}

func pythonFromImportReference(tokens []pythonImportToken, start int) (pythonImportReference, int, bool) {
	module, next, found := pythonTokenImportPath(tokens, start+1)
	if !found || next >= len(tokens) || tokens[next].Value != "import" {
		return pythonImportReference{}, start + 1, false
	}
	names, next, found := pythonImportedNames(tokens, next+1)
	if !found {
		return pythonImportReference{}, start + 1, false
	}
	return pythonImportReference{Module: module, Names: names, Line: tokens[start].Line, Literal: true}, next, true
}

func pythonDirectImportReferences(tokens []pythonImportToken, start int) ([]pythonImportReference, int, bool) {
	references := []pythonImportReference{}
	next := start + 1
	for {
		module, afterModule, found := pythonTokenImportPath(tokens, next)
		if !found {
			return nil, start + 1, false
		}
		next = afterModule
		if next < len(tokens) && tokens[next].Value == "as" {
			next++
			if next >= len(tokens) || !pythonIdentifier(tokens[next].Value) {
				return nil, start + 1, false
			}
			next++
		}
		references = append(references, pythonImportReference{Module: module, Line: tokens[start].Line, Literal: true})
		if next >= len(tokens) || pythonImportTerminator(tokens[next].Value) {
			return references, next, true
		}
		if tokens[next].Value != "," {
			return nil, start + 1, false
		}
		next++
	}
}

func pythonTokenImportPath(tokens []pythonImportToken, start int) (string, int, bool) {
	if start >= len(tokens) {
		return "", start, false
	}
	dots, start := pythonImportDots(tokens, start)
	component, found := pythonImportPathComponent(tokens, start)
	if !found {
		return pythonRelativeImportPath(dots, start)
	}
	return pythonQualifiedImportPath(tokens, dots, component, start+1)
}

func pythonImportDots(tokens []pythonImportToken, start int) (string, int) {
	dots := ""
	for start < len(tokens) && tokens[start].Value == "." {
		dots += "."
		start++
	}
	return dots, start
}

func pythonImportPathComponent(tokens []pythonImportToken, index int) (string, bool) {
	if index >= len(tokens) {
		return "", false
	}
	value := tokens[index].Value
	return value, pythonIdentifier(value) && value != "import"
}

func pythonRelativeImportPath(dots string, index int) (string, int, bool) {
	if dots == "" {
		return "", index, false
	}
	return dots, index, true
}

func pythonQualifiedImportPath(tokens []pythonImportToken, dots, first string, index int) (string, int, bool) {
	parts := []string{first}
	for index < len(tokens) && tokens[index].Value == "." {
		component, found := pythonImportPathComponent(tokens, index+1)
		if !found {
			return "", index, false
		}
		parts = append(parts, component)
		index += 2
	}
	return dots + strings.Join(parts, "."), index, true
}

type pythonImportedNameParser struct {
	tokens        []pythonImportToken
	index         int
	parenthesized bool
	names         []string
}

func pythonImportedNames(tokens []pythonImportToken, start int) ([]string, int, bool) {
	parser := pythonImportedNameParser{tokens: tokens, index: start, parenthesized: start < len(tokens) && tokens[start].Value == "("}
	if parser.parenthesized {
		parser.index++
	}
	return parser.parse()
}

func (parser *pythonImportedNameParser) parse() ([]string, int, bool) {
	for {
		if names, next, found, done := parser.completeBeforeName(); done {
			return names, next, found
		}
		name, found := parser.readName()
		if !found || !parser.readAlias() {
			return nil, parser.index, false
		}
		parser.names = append(parser.names, name)
		if names, next, found, done := parser.completeAfterName(); done {
			return names, next, found
		}
	}
}

func (parser *pythonImportedNameParser) completeBeforeName() ([]string, int, bool, bool) {
	if parser.parenthesized {
		parser.index = pythonSkipImportNewlines(parser.tokens, parser.index)
		if parser.index < len(parser.tokens) && parser.tokens[parser.index].Value == ")" {
			return parser.names, parser.index + 1, len(parser.names) > 0, true
		}
	}
	if parser.index >= len(parser.tokens) || pythonImportTerminator(parser.tokens[parser.index].Value) {
		return parser.names, parser.index, !parser.parenthesized && len(parser.names) > 0, true
	}
	return nil, 0, false, false
}

func (parser *pythonImportedNameParser) readName() (string, bool) {
	name := parser.tokens[parser.index].Value
	if name != "*" && !pythonIdentifier(name) {
		return "", false
	}
	parser.index++
	return name, true
}

func (parser *pythonImportedNameParser) readAlias() bool {
	if parser.index >= len(parser.tokens) || parser.tokens[parser.index].Value != "as" {
		return true
	}
	parser.index++
	if parser.index >= len(parser.tokens) || !pythonIdentifier(parser.tokens[parser.index].Value) {
		return false
	}
	parser.index++
	return true
}

func (parser *pythonImportedNameParser) completeAfterName() ([]string, int, bool, bool) {
	parser.skipNewlinesAfterName()
	if parser.nextIs(",") {
		return parser.completeAfterNameComma()
	}
	return parser.completeAfterNameEnd()
}

func (parser *pythonImportedNameParser) skipNewlinesAfterName() {
	if parser.parenthesized {
		parser.index = pythonSkipImportNewlines(parser.tokens, parser.index)
	}
}

func (parser *pythonImportedNameParser) nextIs(value string) bool {
	return parser.index < len(parser.tokens) && parser.tokens[parser.index].Value == value
}

func (parser *pythonImportedNameParser) completeAfterNameComma() ([]string, int, bool, bool) {
	parser.index++
	if !parser.parenthesized && parser.importListEnded() {
		return nil, parser.index, false, true
	}
	return nil, 0, false, false
}

func (parser *pythonImportedNameParser) completeAfterNameEnd() ([]string, int, bool, bool) {
	if parser.parenthesized && parser.nextIs(")") {
		return parser.names, parser.index + 1, true, true
	}
	if !parser.parenthesized && parser.importListEnded() {
		return parser.names, parser.index, true, true
	}
	return nil, parser.index, false, true
}

func (parser *pythonImportedNameParser) importListEnded() bool {
	return parser.index >= len(parser.tokens) || pythonImportTerminator(parser.tokens[parser.index].Value)
}

func pythonSkipImportNewlines(tokens []pythonImportToken, start int) int {
	for start < len(tokens) && tokens[start].Value == "\n" {
		start++
	}
	return start
}

func pythonImportTerminator(value string) bool {
	return value == "\n" || value == ";"
}

func pythonIdentifier(value string) bool {
	for index, character := range value {
		if !(character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || index > 0 && character >= '0' && character <= '9') {
			return false
		}
	}
	return value != ""
}

func pythonDynamicImportReferences(data, code []byte) []pythonImportReference {
	references := []pythonImportReference{}
	for _, name := range pythonDynamicImportNames(code) {
		for offset := 0; offset < len(code); {
			index := bytes.Index(code[offset:], []byte(name))
			if index < 0 {
				break
			}
			index += offset
			offset = index + len(name)
			if !pythonCallBoundary(code, index, len(name)) {
				continue
			}
			argument, literal, found := pythonCallImportArgument(data, code, index+len(name))
			if !found {
				continue
			}
			references = append(references, pythonImportReference{
				Module: argument, Line: bytes.Count(code[:index], []byte("\n")) + 1, Literal: literal,
			})
		}
	}
	return references
}

func pythonDynamicImportNames(code []byte) []string {
	names := map[string]bool{"importlib.import_module": true, "__import__": true}
	tokens := pythonImportTokens(code)
	for index := 0; index < len(tokens); {
		next := index + 1
		switch tokens[index].Value {
		case "import":
			next = pythonImportlibAliases(tokens, index, names)
		case "from":
			next = pythonImportlibFunctionAliases(tokens, index, names)
		}
		if next > index+1 {
			index = next
			continue
		}
		index++
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func pythonImportlibAliases(tokens []pythonImportToken, start int, names map[string]bool) int {
	staged := map[string]bool{}
	index := start + 1
	for {
		module, binding, aliased, next, found := pythonImportlibAlias(tokens, index)
		if !found {
			return start + 1
		}
		pythonRecordImportlibAlias(staged, module, binding, aliased)
		index = next
		if pythonImportListDone(tokens, index) {
			pythonCommitImportNames(names, staged)
			return index
		}
		if tokens[index].Value != "," {
			return start + 1
		}
		index++
	}
}

func pythonCommitImportNames(names, staged map[string]bool) {
	for name := range staged {
		names[name] = true
	}
}

func pythonImportlibAlias(tokens []pythonImportToken, index int) (string, string, bool, int, bool) {
	module, next, found := pythonTokenImportPath(tokens, index)
	if !found {
		return "", "", false, index, false
	}
	binding := strings.Split(module, ".")[0]
	aliased := false
	if next < len(tokens) && tokens[next].Value == "as" {
		aliased = true
		next++
		if next >= len(tokens) || !pythonIdentifier(tokens[next].Value) {
			return "", "", false, next, false
		}
		binding = tokens[next].Value
		next++
	}
	return module, binding, aliased, next, true
}

func pythonRecordImportlibAlias(names map[string]bool, module, binding string, aliased bool) {
	switch {
	case module == "importlib" || strings.HasPrefix(module, "importlib.") && !aliased:
		names[binding+".import_module"] = true
	case module == "builtins":
		names[binding+".__import__"] = true
	}
}

func pythonImportListDone(tokens []pythonImportToken, index int) bool {
	return index >= len(tokens) || pythonImportTerminator(tokens[index].Value)
}

type pythonImportlibFunctionParser struct {
	tokens        []pythonImportToken
	index         int
	parenthesized bool
	names         map[string]bool
	module        string
	failure       int
}

func pythonImportlibFunctionAliases(tokens []pythonImportToken, start int, names map[string]bool) int {
	module, next, found := pythonTokenImportPath(tokens, start+1)
	if !found || (module != "importlib" && module != "builtins") || next >= len(tokens) || tokens[next].Value != "import" {
		return start + 1
	}
	staged := map[string]bool{}
	parser := pythonImportlibFunctionParser{
		tokens: tokens, index: next + 1, names: staged, module: module, failure: start + 1,
	}
	parser.parenthesized = parser.index < len(tokens) && tokens[parser.index].Value == "("
	if parser.parenthesized {
		parser.index++
	}
	parsed := parser.parse()
	if parsed > start+1 {
		pythonCommitImportNames(names, staged)
	}
	return parsed
}

func (parser *pythonImportlibFunctionParser) parse() int {
	for {
		if next, done := parser.completeBeforeImport(); done {
			return next
		}
		imported, binding, found := parser.readImport()
		if !found {
			return parser.failure
		}
		parser.record(imported, binding)
		if next, done := parser.completeAfterImport(); done {
			return next
		}
	}
}

func (parser *pythonImportlibFunctionParser) completeBeforeImport() (int, bool) {
	if parser.parenthesized {
		parser.index = pythonSkipImportNewlines(parser.tokens, parser.index)
		if parser.index < len(parser.tokens) && parser.tokens[parser.index].Value == ")" {
			return parser.index + 1, true
		}
	}
	if parser.index >= len(parser.tokens) || pythonImportTerminator(parser.tokens[parser.index].Value) {
		return parser.index, true
	}
	return 0, false
}

func (parser *pythonImportlibFunctionParser) readImport() (string, string, bool) {
	imported := parser.tokens[parser.index].Value
	if imported != "*" && !pythonIdentifier(imported) {
		return "", "", false
	}
	parser.index++
	binding := imported
	if parser.index < len(parser.tokens) && parser.tokens[parser.index].Value == "as" {
		parser.index++
		if parser.index >= len(parser.tokens) || !pythonIdentifier(parser.tokens[parser.index].Value) {
			return "", "", false
		}
		binding = parser.tokens[parser.index].Value
		parser.index++
	}
	return imported, binding, true
}

func (parser *pythonImportlibFunctionParser) record(imported, binding string) {
	if parser.module == "importlib" && imported == "import_module" {
		parser.names[binding] = true
	}
	if parser.module == "builtins" && imported == "__import__" {
		parser.names[binding] = true
	}
	if imported == "*" {
		if parser.module == "importlib" {
			parser.names["import_module"] = true
		}
		if parser.module == "builtins" {
			parser.names["__import__"] = true
		}
	}
}

func (parser *pythonImportlibFunctionParser) completeAfterImport() (int, bool) {
	if parser.parenthesized {
		parser.index = pythonSkipImportNewlines(parser.tokens, parser.index)
	}
	if parser.index >= len(parser.tokens) || pythonImportTerminator(parser.tokens[parser.index].Value) {
		return parser.index, true
	}
	if parser.parenthesized && parser.tokens[parser.index].Value == ")" {
		return parser.index + 1, true
	}
	if parser.tokens[parser.index].Value != "," {
		return parser.failure, true
	}
	parser.index++
	if !parser.parenthesized && (parser.index >= len(parser.tokens) || pythonImportTerminator(parser.tokens[parser.index].Value)) {
		return parser.failure, true
	}
	return 0, false
}

func pythonCallBoundary(code []byte, start, length int) bool {
	if start > 0 && (pythonIdentifierByte(code[start-1]) || code[start-1] == '.') {
		return false
	}
	end := start + length
	return end >= len(code) || !pythonIdentifierByte(code[end])
}

func pythonIdentifierByte(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func pythonCallImportArgument(data, code []byte, index int) (string, bool, bool) {
	start, found := pythonCallArgumentStart(data, code, index)
	if !found {
		return pythonParenthesizedCallImportArgument(data, code, index)
	}
	argument, end, literal := pythonPlainImportString(data, start)
	if !literal {
		return "", false, true
	}
	end = pythonSkipCallSpace(data, end)
	if end >= len(code) || code[end] != ')' {
		return "", false, true
	}
	return argument, true, true
}

func pythonCallArgumentStart(data, code []byte, index int) (int, bool) {
	index = pythonSkipCallStartSpace(data, index)
	if index >= len(code) || code[index] != '(' {
		return 0, false
	}
	return pythonSkipCallSpace(data, index+1), true
}

func pythonParenthesizedCallImportArgument(data, code []byte, index int) (string, bool, bool) {
	index = pythonSkipCallStartSpace(data, index)
	if index >= len(code) || code[index] != ')' {
		return "", false, false
	}
	if _, found := pythonCallArgumentStart(data, code, index+1); !found {
		return "", false, false
	}
	return "", false, true
}

func pythonSkipCallStartSpace(data []byte, index int) int {
	for index < len(data) {
		if data[index] == ' ' || data[index] == '\t' {
			index++
			continue
		}
		if data[index] == '\\' && index+1 < len(data) && data[index+1] == '\n' {
			index += 2
			continue
		}
		if data[index] == '\\' && index+2 < len(data) && data[index+1] == '\r' && data[index+2] == '\n' {
			index += 3
			continue
		}
		break
	}
	return index
}

func pythonSkipCallSpace(data []byte, index int) int {
	for index < len(data) {
		if pythonSpace(data[index]) {
			index++
			continue
		}
		if data[index] == '\\' && index+1 < len(data) && data[index+1] == '\n' {
			index += 2
			continue
		}
		if data[index] == '\\' && index+2 < len(data) && data[index+1] == '\r' && data[index+2] == '\n' {
			index += 3
			continue
		}
		break
	}
	return index
}

func pythonPlainImportString(data []byte, index int) (string, int, bool) {
	index = pythonPlainImportStringStart(data, index)
	if index >= len(data) || !pythonImportStringQuote(data[index]) {
		return "", index, false
	}
	if pythonTripleImportString(data, index) {
		return "", index, false
	}
	return pythonReadPlainImportString(data, data[index], index+1)
}

func pythonPlainImportStringStart(data []byte, index int) int {
	if index < len(data) && pythonPlainStringPrefix(data[index]) {
		return index + 1
	}
	return index
}

func pythonImportStringQuote(value byte) bool {
	return value == '\'' || value == '"'
}

func pythonTripleImportString(data []byte, index int) bool {
	return index+2 < len(data) && data[index+1] == data[index] && data[index+2] == data[index]
}

func pythonReadPlainImportString(data []byte, quote byte, index int) (string, int, bool) {
	value := strings.Builder{}
	for index < len(data) {
		if data[index] == quote {
			return value.String(), index + 1, true
		}
		if pythonUnsafeImportStringByte(data[index]) {
			return "", index, false
		}
		value.WriteByte(data[index])
		index++
	}
	return "", index, false
}

func pythonUnsafeImportStringByte(value byte) bool {
	return value == '\\' || value == '\n' || value == '\r'
}

func pythonPlainStringPrefix(value byte) bool {
	return value == 'r' || value == 'R' || value == 'b' || value == 'B' || value == 'u' || value == 'U'
}

func pythonSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r'
}

func pythonUnprovenImport(index pythonModuleIndex, source string, reference pythonImportReference, dependencies map[string]bool) string {
	if !reference.Literal {
		return fmt.Sprintf("line %d dynamic Python import cannot be proven statically", reference.Line)
	}
	module, valid := pythonResolvedImportModule(index, source, reference.Module)
	if !valid {
		return fmt.Sprintf("line %d relative Python import escapes its project", reference.Line)
	}
	if message := pythonAmbiguousImportMessage(index, module, reference.Line); message != "" {
		return message
	}
	if !pythonLocalModule(index, module) {
		return ""
	}
	return pythonProvenImportMessage(index, module, reference, dependencies)
}

func pythonAmbiguousImportMessage(index pythonModuleIndex, module string, line int) string {
	if !pythonAmbiguousImportModule(index, module) {
		return ""
	}
	return fmt.Sprintf("line %d local-looking Python import %q resolves through conflicting source roots", line, module)
}

func pythonProvenImportMessage(index pythonModuleIndex, module string, reference pythonImportReference, dependencies map[string]bool) string {
	if len(reference.Names) == 0 {
		return pythonModuleDependencyMessage(index, module, reference.Line, dependencies)
	}
	for _, name := range reference.Names {
		if message := pythonNamedImportMessage(index, module, name, reference.Line, dependencies); message != "" {
			return message
		}
	}
	return ""
}

func pythonNamedImportMessage(index pythonModuleIndex, module, name string, line int, dependencies map[string]bool) string {
	if name == "*" {
		return pythonModuleDependencyMessage(index, module, line, dependencies)
	}
	qualified := module + "." + name
	if message := pythonAmbiguousImportMessage(index, qualified, line); message != "" {
		return message
	}
	candidates := append([]string{}, index.files[qualified]...)
	candidates = append(candidates, index.files[module]...)
	if len(candidates) == 0 {
		return fmt.Sprintf("line %d local-looking Python import %q cannot be resolved", line, qualified)
	}
	if !pythonGraphContains(dependencies, candidates) {
		return fmt.Sprintf("line %d Ruff did not resolve local-looking Python import %q", line, qualified)
	}
	return ""
}

func pythonAmbiguousImportModule(index pythonModuleIndex, module string) bool {
	parts := strings.Split(module, ".")
	for length := 1; length <= len(parts); length++ {
		if index.ambiguous[strings.Join(parts[:length], ".")] {
			return true
		}
	}
	return false
}

func pythonResolvedImportModule(index pythonModuleIndex, source, module string) (string, bool) {
	if !strings.HasPrefix(module, ".") {
		return module, true
	}
	dots := len(module) - len(strings.TrimLeft(module, "."))
	packageName := index.packageOf[source]
	parts := []string{}
	if packageName != "" {
		parts = strings.Split(packageName, ".")
	}
	if dots > len(parts)+1 {
		return "", false
	}
	parts = parts[:len(parts)-dots+1]
	tail := strings.TrimLeft(module, ".")
	if tail != "" {
		parts = append(parts, strings.Split(tail, ".")...)
	}
	return strings.Join(parts, "."), true
}

func pythonLocalModule(index pythonModuleIndex, module string) bool {
	if module == "" {
		return true
	}
	if index.packages[module] || len(index.files[module]) > 0 {
		return true
	}
	first, _, _ := strings.Cut(module, ".")
	return index.packages[first]
}

func pythonModuleDependencyMessage(index pythonModuleIndex, module string, line int, dependencies map[string]bool) string {
	candidates := index.files[module]
	if len(candidates) == 0 {
		if index.packages[module] {
			return ""
		}
		return fmt.Sprintf("line %d local-looking Python import %q cannot be resolved", line, module)
	}
	if pythonGraphContains(dependencies, candidates) {
		return ""
	}
	return fmt.Sprintf("line %d Ruff did not resolve local-looking Python import %q", line, module)
}

func pythonGraphContains(dependencies map[string]bool, candidates []string) bool {
	for _, candidate := range candidates {
		if dependencies[candidate] {
			return true
		}
	}
	return false
}
