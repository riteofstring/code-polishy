package repository

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type pythonTOMLValueKind uint8

const (
	pythonTOMLOther pythonTOMLValueKind = iota
	pythonTOMLString
	pythonTOMLArray
	pythonTOMLInlineTable
)

type pythonTOMLValue struct {
	kind   pythonTOMLValueKind
	text   string
	array  []pythonTOMLValue
	inline []pythonTOMLInlineAssignment
	line   int
}

type pythonTOMLInlineAssignment struct {
	path  []string
	value pythonTOMLValue
}

type pythonTOMLAssignment struct {
	table      string
	tablePath  []string
	arrayTable bool
	key        string
	keyPath    []string
	value      pythonTOMLValue
	line       int
}

type pythonTOMLTable struct {
	name  string
	array bool
}

type pythonTOMLDocument struct {
	assignments []pythonTOMLAssignment
	tables      []pythonTOMLTable
}

type pythonTOMLParser struct {
	data  []byte
	index int
	line  int
}

type pythonTOMLDocumentState struct {
	document   pythonTOMLDocument
	table      string
	tablePath  []string
	arrayTable bool
	scope      int
	nextScope  int
	seenTables map[string]bool
	seenKeys   map[string]bool
}

func parsePythonTOML(data []byte) (pythonTOMLDocument, error) {
	parser := pythonTOMLParser{data: data, line: 1}
	state := pythonTOMLDocumentState{nextScope: 1, seenTables: map[string]bool{}, seenKeys: map[string]bool{}}
	for {
		parser.skipDocumentSpace()
		if parser.done() {
			return state.document, nil
		}
		if err := state.statement(&parser); err != nil {
			return pythonTOMLDocument{}, err
		}
	}
}

func (state *pythonTOMLDocumentState) statement(parser *pythonTOMLParser) error {
	if parser.peek() == '[' {
		return state.tableStatement(parser)
	}
	return state.assignmentStatement(parser)
}

func (state *pythonTOMLDocumentState) tableStatement(parser *pythonTOMLParser) error {
	name, path, array, err := parser.table()
	if err != nil {
		return err
	}
	identity := strings.Join(path, "\x00")
	if !array && state.seenTables[identity] {
		return parser.errorf("table %q is declared more than once", name)
	}
	if !array {
		state.seenTables[identity] = true
	}
	state.table = name
	state.tablePath = path
	state.arrayTable = array
	state.scope = state.nextScope
	state.nextScope++
	state.document.tables = append(state.document.tables, pythonTOMLTable{name: name, array: array})
	return nil
}

func (state *pythonTOMLDocumentState) assignmentStatement(parser *pythonTOMLParser) error {
	line := parser.line
	keyPath, err := parser.keyPath()
	if err != nil {
		return err
	}
	key := strings.Join(keyPath, ".")
	value, err := parser.assignmentValue(key)
	if err != nil {
		return err
	}
	if state.duplicateKey(keyPath) {
		return parser.errorf("key %q is declared more than once", key)
	}
	state.document.assignments = append(state.document.assignments, pythonTOMLAssignment{
		table: state.table, tablePath: append([]string{}, state.tablePath...), arrayTable: state.arrayTable,
		key: key, keyPath: keyPath, value: value, line: line,
	})
	return nil
}

func (state *pythonTOMLDocumentState) duplicateKey(key []string) bool {
	identity := fmt.Sprintf("%d\x00%s", state.scope, strings.Join(key, "\x00"))
	if state.seenKeys[identity] {
		return true
	}
	state.seenKeys[identity] = true
	return false
}

func (parser *pythonTOMLParser) assignmentValue(key string) (pythonTOMLValue, error) {
	parser.skipHorizontal()
	if parser.take() != '=' {
		return pythonTOMLValue{}, parser.errorf("key %q has no value", key)
	}
	parser.skipHorizontal()
	value, err := parser.value()
	if err != nil {
		return pythonTOMLValue{}, err
	}
	if err := parser.finishStatement(); err != nil {
		return pythonTOMLValue{}, err
	}
	return value, nil
}

func (parser *pythonTOMLParser) done() bool {
	return parser.index >= len(parser.data)
}

func (parser *pythonTOMLParser) peek() byte {
	if parser.done() {
		return 0
	}
	return parser.data[parser.index]
}

func (parser *pythonTOMLParser) take() byte {
	value := parser.peek()
	if value != 0 {
		parser.index++
		if value == '\n' {
			parser.line++
		}
	}
	return value
}

func (parser *pythonTOMLParser) errorf(format string, arguments ...any) error {
	return fmt.Errorf("line %d: %s", parser.line, fmt.Sprintf(format, arguments...))
}

func (parser *pythonTOMLParser) skipDocumentSpace() {
	for !parser.done() {
		switch parser.peek() {
		case ' ', '\t', '\r', '\n':
			parser.take()
		case '#':
			parser.skipComment()
		default:
			return
		}
	}
}

func (parser *pythonTOMLParser) skipHorizontal() {
	for parser.peek() == ' ' || parser.peek() == '\t' || parser.peek() == '\r' {
		parser.take()
	}
}

func (parser *pythonTOMLParser) skipValueSpace() {
	for !parser.done() {
		switch parser.peek() {
		case ' ', '\t', '\r', '\n':
			parser.take()
		case '#':
			parser.skipComment()
		default:
			return
		}
	}
}

func (parser *pythonTOMLParser) skipComment() {
	for !parser.done() && parser.peek() != '\n' {
		parser.take()
	}
}

func (parser *pythonTOMLParser) finishStatement() error {
	parser.skipHorizontal()
	if parser.peek() == '#' {
		parser.skipComment()
	}
	if parser.done() {
		return nil
	}
	if parser.peek() != '\n' {
		return parser.errorf("unexpected content after value")
	}
	parser.take()
	return nil
}

func (parser *pythonTOMLParser) table() (string, []string, bool, error) {
	parser.take()
	array := parser.peek() == '['
	if array {
		parser.take()
	}
	parser.skipHorizontal()
	path, err := parser.keyPath()
	if err != nil {
		return "", nil, false, err
	}
	name := strings.Join(path, ".")
	parser.skipHorizontal()
	if parser.take() != ']' || array && parser.take() != ']' {
		return "", nil, false, parser.errorf("table %q is not closed", name)
	}
	if err := parser.finishStatement(); err != nil {
		return "", nil, false, err
	}
	return name, path, array, nil
}

func (parser *pythonTOMLParser) keyPath() ([]string, error) {
	keys := []string{}
	for {
		parser.skipHorizontal()
		key, err := parser.key()
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
		parser.skipHorizontal()
		if parser.peek() != '.' {
			return keys, nil
		}
		parser.take()
	}
}

func (parser *pythonTOMLParser) key() (string, error) {
	if parser.peek() == '"' || parser.peek() == '\'' {
		value, err := parser.stringValue()
		if err != nil {
			return "", err
		}
		return value.text, nil
	}
	start := parser.index
	for pythonTOMLBareKeyCharacter(parser.peek()) {
		parser.take()
	}
	if start == parser.index {
		return "", parser.errorf("expected a TOML key")
	}
	return string(parser.data[start:parser.index]), nil
}

func pythonTOMLBareKeyCharacter(value byte) bool {
	return pythonAlphaNumeric(value) || value == '_' || value == '-'
}

func (parser *pythonTOMLParser) value() (pythonTOMLValue, error) {
	line := parser.line
	switch parser.peek() {
	case '"', '\'':
		value, err := parser.stringValue()
		if err != nil {
			return pythonTOMLValue{}, err
		}
		value.line = line
		return value, nil
	case '[':
		return parser.arrayValue(line)
	case '{':
		return parser.inlineTable(line)
	default:
		if err := parser.bareValue(); err != nil {
			return pythonTOMLValue{}, err
		}
		return pythonTOMLValue{kind: pythonTOMLOther, line: line}, nil
	}
}

func (parser *pythonTOMLParser) arrayValue(line int) (pythonTOMLValue, error) {
	parser.take()
	array := []pythonTOMLValue{}
	parser.skipValueSpace()
	if parser.peek() == ']' {
		parser.take()
		return pythonTOMLValue{kind: pythonTOMLArray, array: array, line: line}, nil
	}
	for {
		parser.skipValueSpace()
		value, err := parser.value()
		if err != nil {
			return pythonTOMLValue{}, err
		}
		array = append(array, value)
		parser.skipValueSpace()
		if parser.peek() == ']' {
			parser.take()
			return pythonTOMLValue{kind: pythonTOMLArray, array: array, line: line}, nil
		}
		if parser.take() != ',' {
			return pythonTOMLValue{}, parser.errorf("array values must be comma-separated")
		}
		parser.skipValueSpace()
		if parser.peek() == ']' {
			parser.take()
			return pythonTOMLValue{kind: pythonTOMLArray, array: array, line: line}, nil
		}
	}
}

func (parser *pythonTOMLParser) inlineTable(line int) (pythonTOMLValue, error) {
	parser.take()
	parser.skipHorizontal()
	if parser.peek() == '}' {
		parser.take()
		return pythonTOMLValue{kind: pythonTOMLInlineTable, inline: []pythonTOMLInlineAssignment{}, line: line}, nil
	}
	fields := []pythonTOMLInlineAssignment{}
	seen := map[string]bool{}
	for {
		path, err := parser.keyPath()
		if err != nil {
			return pythonTOMLValue{}, err
		}
		key := strings.Join(path, ".")
		identity := strings.Join(path, "\x00")
		if seen[identity] {
			return pythonTOMLValue{}, parser.errorf("inline table key %q is declared more than once", key)
		}
		seen[identity] = true
		parser.skipHorizontal()
		if parser.take() != '=' {
			return pythonTOMLValue{}, parser.errorf("inline table key has no value")
		}
		parser.skipHorizontal()
		if parser.peek() == '\n' || parser.peek() == '\r' {
			return pythonTOMLValue{}, parser.errorf("inline table cannot span lines")
		}
		value, err := parser.value()
		if err != nil {
			return pythonTOMLValue{}, err
		}
		fields = append(fields, pythonTOMLInlineAssignment{path: path, value: value})
		parser.skipHorizontal()
		if parser.peek() == '}' {
			parser.take()
			return pythonTOMLValue{kind: pythonTOMLInlineTable, inline: fields, line: line}, nil
		}
		if parser.take() != ',' {
			return pythonTOMLValue{}, parser.errorf("inline table values must be comma-separated")
		}
		parser.skipHorizontal()
	}
}

func (parser *pythonTOMLParser) bareValue() error {
	start := parser.index
	for !parser.done() && !strings.ContainsRune(" \t\r\n#,]}", rune(parser.peek())) {
		parser.take()
	}
	value := string(parser.data[start:parser.index])
	if parser.peek() == ' ' && validPythonTOMLDate(value) {
		checkpoint := parser.index
		parser.take()
		timeStart := parser.index
		for !parser.done() && !strings.ContainsRune(" \t\r\n#,]}", rune(parser.peek())) {
			parser.take()
		}
		if validPythonTOMLDateTime(value + " " + string(parser.data[timeStart:parser.index])) {
			return nil
		}
		parser.index = checkpoint
	}
	if !validPythonTOMLBareValue(value) {
		return parser.errorf("TOML value %q is malformed", value)
	}
	return nil
}

func validPythonTOMLBareValue(value string) bool {
	if pythonTOMLSpecialBareValues[value] {
		return true
	}
	return validPythonTOMLInteger(value) || validPythonTOMLFloat(value) || validPythonTOMLDateTime(value) || validPythonTOMLDate(value) || validPythonTOMLTime(value)
}

var pythonTOMLSpecialBareValues = map[string]bool{
	"true": true, "false": true, "inf": true, "+inf": true, "-inf": true,
	"nan": true, "+nan": true, "-nan": true,
}

func validPythonTOMLInteger(value string) bool {
	if len(value) >= 3 && value[0] == '0' {
		switch value[1] {
		case 'x':
			return validPythonTOMLDigitSequence(value[2:], pythonTOMLHexDigit)
		case 'o':
			return validPythonTOMLDigitSequence(value[2:], pythonTOMLOctalDigit)
		case 'b':
			return validPythonTOMLDigitSequence(value[2:], pythonTOMLBinaryDigit)
		}
	}
	return validPythonTOMLDecimalInteger(value, false)
}

func validPythonTOMLFloat(value string) bool {
	if pythonTOMLFloatSpecialValue(value) {
		return true
	}
	if value == "" {
		return false
	}
	parser := pythonTOMLFloatParser{value: value}
	if !parser.integer() {
		return false
	}
	if !parser.fraction() {
		return false
	}
	if !parser.exponent() {
		return false
	}
	return parser.done() && (parser.hasFraction || parser.hasExponent)
}

func pythonTOMLFloatSpecialValue(value string) bool {
	return value == "inf" || value == "+inf" || value == "-inf" || value == "nan" || value == "+nan" || value == "-nan"
}

type pythonTOMLFloatParser struct {
	value       string
	index       int
	hasFraction bool
	hasExponent bool
}

func (parser *pythonTOMLFloatParser) done() bool {
	return parser.index == len(parser.value)
}

func (parser *pythonTOMLFloatParser) integer() bool {
	parser.optionalSign()
	start := parser.index
	parser.consumeUntilFloatSeparator()
	return validPythonTOMLUnsignedDecimalInteger(parser.value[start:parser.index], false)
}

func (parser *pythonTOMLFloatParser) fraction() bool {
	if !parser.takeFractionSeparator() {
		return true
	}
	parser.hasFraction = true
	start := parser.index
	parser.consumeUntilExponent()
	return validPythonTOMLDigitSequence(parser.value[start:parser.index], pythonTOMLDecimalDigit)
}

func (parser *pythonTOMLFloatParser) exponent() bool {
	if !parser.takeExponentSeparator() {
		return true
	}
	parser.hasExponent = true
	parser.optionalSign()
	if !validPythonTOMLDigitSequence(parser.value[parser.index:], pythonTOMLDecimalDigit) {
		return false
	}
	parser.index = len(parser.value)
	return true
}

func (parser *pythonTOMLFloatParser) optionalSign() {
	if parser.index < len(parser.value) && pythonTOMLSign(parser.value[parser.index]) {
		parser.index++
	}
}

func (parser *pythonTOMLFloatParser) consumeUntilFloatSeparator() {
	for parser.index < len(parser.value) && !pythonTOMLFloatSeparator(parser.value[parser.index]) {
		parser.index++
	}
}

func (parser *pythonTOMLFloatParser) consumeUntilExponent() {
	for parser.index < len(parser.value) && !pythonTOMLExponentSeparator(parser.value[parser.index]) {
		parser.index++
	}
}

func (parser *pythonTOMLFloatParser) takeFractionSeparator() bool {
	if parser.index >= len(parser.value) || parser.value[parser.index] != '.' {
		return false
	}
	parser.index++
	return true
}

func (parser *pythonTOMLFloatParser) takeExponentSeparator() bool {
	if parser.index >= len(parser.value) || !pythonTOMLExponentSeparator(parser.value[parser.index]) {
		return false
	}
	parser.index++
	return true
}

func pythonTOMLSign(value byte) bool {
	return value == '+' || value == '-'
}

func pythonTOMLFloatSeparator(value byte) bool {
	return value == '.' || pythonTOMLExponentSeparator(value)
}

func pythonTOMLExponentSeparator(value byte) bool {
	return value == 'e' || value == 'E'
}

func validPythonTOMLDecimalInteger(value string, allowLeadingZero bool) bool {
	if value == "" {
		return false
	}
	if value[0] == '+' || value[0] == '-' {
		value = value[1:]
	}
	return validPythonTOMLUnsignedDecimalInteger(value, allowLeadingZero)
}

func validPythonTOMLUnsignedDecimalInteger(value string, allowLeadingZero bool) bool {
	if !validPythonTOMLDigitSequence(value, pythonTOMLDecimalDigit) {
		return false
	}
	if allowLeadingZero || value[0] != '0' {
		return true
	}
	for index := 1; index < len(value); index++ {
		if pythonTOMLDecimalDigit(value[index]) {
			return false
		}
	}
	return true
}

func validPythonTOMLDigitSequence(value string, digit func(byte) bool) bool {
	if value == "" {
		return false
	}
	for index := range value {
		if digit(value[index]) {
			continue
		}
		if value[index] != '_' || index == 0 || index+1 == len(value) || !digit(value[index-1]) || !digit(value[index+1]) {
			return false
		}
	}
	return true
}

func pythonTOMLDecimalDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func pythonTOMLHexDigit(value byte) bool {
	return pythonTOMLDecimalDigit(value) || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}

func pythonTOMLOctalDigit(value byte) bool {
	return value >= '0' && value <= '7'
}

func pythonTOMLBinaryDigit(value byte) bool {
	return value == '0' || value == '1'
}

func validPythonTOMLDateTime(value string) bool {
	if len(value) < 19 || !validPythonTOMLDate(value[:10]) {
		return false
	}
	if value[10] != 'T' && value[10] != 't' && value[10] != ' ' {
		return false
	}
	timeLength, valid := validPythonTOMLTimePrefix(value[11:])
	if !valid {
		return false
	}
	offset := value[11+timeLength:]
	return offset == "" || offset == "Z" || offset == "z" || validPythonTOMLOffset(offset)
}

func validPythonTOMLDate(value string) bool {
	year, month, day, valid := pythonTOMLDateValues(value)
	if !valid {
		return false
	}
	return pythonTOMLDateDayValid(year, month, day)
}

func pythonTOMLDateValues(value string) (int, int, int, bool) {
	if !pythonTOMLDateLayout(value) {
		return 0, 0, 0, false
	}
	year, validYear := pythonTOMLDecimalValue(value[:4])
	month, validMonth := pythonTOMLDecimalValue(value[5:7])
	day, validDay := pythonTOMLDecimalValue(value[8:])
	return year, month, day, validYear && validMonth && validDay
}

func pythonTOMLDateLayout(value string) bool {
	return len(value) == 10 && value[4] == '-' && value[7] == '-'
}

func pythonTOMLDateDayValid(year, month, day int) bool {
	if month < 1 || month > 12 || day < 1 {
		return false
	}
	days := [12]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if month == 2 && pythonTOMLLeapYear(year) {
		days[1] = 29
	}
	return day <= days[month-1]
}

func pythonTOMLLeapYear(year int) bool {
	if year%400 == 0 {
		return true
	}
	if year%4 != 0 {
		return false
	}
	return year%100 != 0
}

func validPythonTOMLTime(value string) bool {
	length, valid := validPythonTOMLTimePrefix(value)
	return valid && length == len(value)
}

func validPythonTOMLTimePrefix(value string) (int, bool) {
	if !pythonTOMLTimeValuesValid(value) {
		return 0, false
	}
	return pythonTOMLTimeFraction(value, 8)
}

func pythonTOMLTimeValuesValid(value string) bool {
	if !pythonTOMLTimeLayout(value) {
		return false
	}
	hour, validHour := pythonTOMLDecimalValue(value[:2])
	minute, validMinute := pythonTOMLDecimalValue(value[3:5])
	second, validSecond := pythonTOMLDecimalValue(value[6:8])
	return validHour && validMinute && validSecond && hour <= 23 && minute <= 59 && second <= 60
}

func pythonTOMLTimeLayout(value string) bool {
	return len(value) >= 8 && value[2] == ':' && value[5] == ':'
}

func pythonTOMLTimeFraction(value string, index int) (int, bool) {
	if index >= len(value) || value[index] != '.' {
		return index, true
	}
	index++
	start := index
	for index < len(value) && pythonTOMLDecimalDigit(value[index]) {
		index++
	}
	if start == index {
		return 0, false
	}
	return index, true
}

func validPythonTOMLOffset(value string) bool {
	if len(value) != 6 || (value[0] != '+' && value[0] != '-') || value[3] != ':' {
		return false
	}
	hour, validHour := pythonTOMLDecimalValue(value[1:3])
	minute, validMinute := pythonTOMLDecimalValue(value[4:])
	return validHour && validMinute && hour <= 23 && minute <= 59
}

func pythonTOMLDecimalValue(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	result := 0
	for index := range value {
		if !pythonTOMLDecimalDigit(value[index]) {
			return 0, false
		}
		result = result*10 + int(value[index]-'0')
	}
	return result, true
}

func (parser *pythonTOMLParser) stringValue() (pythonTOMLValue, error) {
	quote, multiline := parser.stringStart()
	value := strings.Builder{}
	for !parser.done() {
		if parser.stringEnd(quote, multiline) {
			parser.consumeStringEnd(multiline)
			return pythonTOMLValue{kind: pythonTOMLString, text: value.String(), line: parser.line}, nil
		}
		character, err := parser.stringCharacter(quote, multiline)
		if err != nil {
			return pythonTOMLValue{}, err
		}
		value.WriteString(character)
	}
	return pythonTOMLValue{}, parser.errorf("string is unterminated")
}

func (parser *pythonTOMLParser) stringStart() (byte, bool) {
	quote := parser.take()
	multiline := parser.multilineStringStart(quote)
	if !multiline {
		return quote, false
	}
	parser.take()
	parser.take()
	if parser.peek() == '\n' {
		parser.take()
	}
	return quote, true
}

func (parser *pythonTOMLParser) multilineStringStart(quote byte) bool {
	if parser.peek() != quote || parser.index+1 >= len(parser.data) {
		return false
	}
	return parser.data[parser.index+1] == quote
}

func (parser *pythonTOMLParser) stringEnd(quote byte, multiline bool) bool {
	if multiline {
		return parser.multilineStringEnd(quote)
	}
	return parser.peek() == quote
}

func (parser *pythonTOMLParser) multilineStringEnd(quote byte) bool {
	if parser.peek() != quote || parser.index+2 >= len(parser.data) {
		return false
	}
	return parser.data[parser.index+1] == quote && parser.data[parser.index+2] == quote
}

func (parser *pythonTOMLParser) consumeStringEnd(multiline bool) {
	parser.take()
	if multiline {
		parser.take()
		parser.take()
	}
}

func (parser *pythonTOMLParser) stringCharacter(quote byte, multiline bool) (string, error) {
	character := parser.take()
	if character == '\n' && !multiline {
		return "", parser.errorf("single-line string is unterminated")
	}
	if quote == '\'' {
		return parser.literalStringCharacter(character)
	}
	return parser.basicStringCharacter(character, multiline)
}

func (parser *pythonTOMLParser) literalStringCharacter(character byte) (string, error) {
	if pythonTOMLStringControl(character) {
		return "", parser.errorf("literal string contains a control character")
	}
	return string(character), nil
}

func (parser *pythonTOMLParser) basicStringCharacter(character byte, multiline bool) (string, error) {
	if character == '\\' {
		return parser.basicEscape(multiline)
	}
	if pythonTOMLStringControl(character) {
		return "", parser.errorf("basic string contains a control character")
	}
	return string(character), nil
}

func pythonTOMLStringControl(character byte) bool {
	return character < 0x20 && character != '\n' && character != '\t'
}

func (parser *pythonTOMLParser) basicEscape(multiline bool) (string, error) {
	if multiline {
		handled, value, err := parser.multilineBasicEscape()
		if handled {
			return value, err
		}
	}
	return parser.basicEscapeCharacter()
}

func (parser *pythonTOMLParser) multilineBasicEscape() (bool, string, error) {
	if !pythonTOMLWhitespace(parser.peek()) {
		return false, "", nil
	}
	seenLine := false
	for pythonTOMLWhitespace(parser.peek()) {
		if parser.take() == '\n' {
			seenLine = true
		}
	}
	if seenLine {
		return true, "", nil
	}
	return true, "", parser.errorf("basic string escape is malformed")
}

func pythonTOMLWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func (parser *pythonTOMLParser) basicEscapeCharacter() (string, error) {
	escape := parser.take()
	if value, found := pythonTOMLBasicEscapes[escape]; found {
		return value, nil
	}
	if escape == 'u' {
		return parser.unicodeEscape(4)
	}
	if escape == 'U' {
		return parser.unicodeEscape(8)
	}
	return "", parser.errorf("basic string escape is malformed")
}

var pythonTOMLBasicEscapes = map[byte]string{
	'b': "\b", 't': "\t", 'n': "\n", 'f': "\f", 'r': "\r", '"': "\"", '\\': "\\",
}

func (parser *pythonTOMLParser) unicodeEscape(length int) (string, error) {
	if parser.index+length > len(parser.data) {
		return "", parser.errorf("unicode escape is incomplete")
	}
	value := 0
	for range length {
		digit, valid := pythonTOMLHexValue(parser.take())
		if !valid {
			return "", parser.errorf("unicode escape is malformed")
		}
		value = value*16 + digit
	}
	if !pythonTOMLUnicodeScalar(value) {
		return "", parser.errorf("unicode escape is outside the Unicode scalar range")
	}
	return string(rune(value)), nil
}

func pythonTOMLHexValue(character byte) (int, bool) {
	if character >= '0' && character <= '9' {
		return int(character - '0'), true
	}
	if character >= 'a' && character <= 'f' {
		return int(character-'a') + 10, true
	}
	if character >= 'A' && character <= 'F' {
		return int(character-'A') + 10, true
	}
	return 0, false
}

func pythonTOMLUnicodeScalar(value int) bool {
	if value > utf8.MaxRune {
		return false
	}
	return value < 0xD800 || value > 0xDFFF
}
