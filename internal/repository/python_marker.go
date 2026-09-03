package repository

import (
	"fmt"
	"strings"
)

type pythonMarkerTokenKind uint8

const (
	pythonMarkerIdentifier pythonMarkerTokenKind = iota
	pythonMarkerString
	pythonMarkerOperator
	pythonMarkerLeftParen
	pythonMarkerRightParen
)

type pythonMarkerToken struct {
	kind pythonMarkerTokenKind
	text string
}

func parsePythonMarker(value string) (string, error) {
	tokens, err := pythonMarkerTokens(value)
	if err != nil {
		return "", err
	}
	parser := pythonMarkerParser{tokens: tokens}
	if err := parser.expression(); err != nil {
		return "", err
	}
	if parser.index != len(tokens) {
		return "", fmt.Errorf("marker has unexpected content %q", tokens[parser.index].text)
	}
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		parts = append(parts, token.text)
	}
	return strings.Join(parts, " "), nil
}

func pythonMarkerTokens(value string) ([]pythonMarkerToken, error) {
	tokens := []pythonMarkerToken{}
	scanner := pythonMarkerScanner{value: value}
	for {
		token, found, err := scanner.next()
		if err != nil {
			return nil, err
		}
		if !found {
			break
		}
		tokens = append(tokens, token)
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("marker is empty")
	}
	return tokens, nil
}

type pythonMarkerScanner struct {
	value string
	index int
}

func (scanner *pythonMarkerScanner) next() (pythonMarkerToken, bool, error) {
	scanner.skipSpace()
	if scanner.done() {
		return pythonMarkerToken{}, false, nil
	}
	if token, found := scanner.parenthesis(); found {
		return token, true, nil
	}
	if scanner.quoted() {
		token, err := scanner.string()
		return token, true, err
	}
	if operator := scanner.operator(); operator != "" {
		return pythonMarkerToken{kind: pythonMarkerOperator, text: operator}, true, nil
	}
	return scanner.word()
}

func (scanner *pythonMarkerScanner) done() bool {
	return scanner.index >= len(scanner.value)
}

func (scanner *pythonMarkerScanner) peek() byte {
	if scanner.done() {
		return 0
	}
	return scanner.value[scanner.index]
}

func (scanner *pythonMarkerScanner) skipSpace() {
	for scanner.peek() == ' ' || scanner.peek() == '\t' {
		scanner.index++
	}
}

func (scanner *pythonMarkerScanner) parenthesis() (pythonMarkerToken, bool) {
	if scanner.peek() == '(' {
		scanner.index++
		return pythonMarkerToken{kind: pythonMarkerLeftParen, text: "("}, true
	}
	if scanner.peek() == ')' {
		scanner.index++
		return pythonMarkerToken{kind: pythonMarkerRightParen, text: ")"}, true
	}
	return pythonMarkerToken{}, false
}

func (scanner *pythonMarkerScanner) quoted() bool {
	return scanner.peek() == '\'' || scanner.peek() == '"'
}

func (scanner *pythonMarkerScanner) string() (pythonMarkerToken, error) {
	start := scanner.index
	quote := scanner.peek()
	scanner.index++
	for !scanner.done() {
		if scanner.peek() == '\\' {
			scanner.index += 2
			continue
		}
		if scanner.peek() == quote {
			scanner.index++
			return pythonMarkerToken{kind: pythonMarkerString, text: scanner.value[start:scanner.index]}, nil
		}
		if pythonMarkerLineEnding(scanner.peek()) {
			return pythonMarkerToken{}, fmt.Errorf("marker string contains a line ending")
		}
		scanner.index++
	}
	return pythonMarkerToken{}, fmt.Errorf("marker string is unterminated")
}

func pythonMarkerLineEnding(value byte) bool {
	return value == '\r' || value == '\n'
}

func (scanner *pythonMarkerScanner) operator() string {
	for _, candidate := range []string{"===", "~=", "==", "!=", "<=", ">=", "<", ">"} {
		if strings.HasPrefix(scanner.value[scanner.index:], candidate) {
			scanner.index += len(candidate)
			return candidate
		}
	}
	return ""
}

func (scanner *pythonMarkerScanner) word() (pythonMarkerToken, bool, error) {
	start := scanner.index
	for !scanner.done() && pythonMarkerWordCharacter(scanner.peek()) {
		scanner.index++
	}
	if start == scanner.index {
		return pythonMarkerToken{}, false, fmt.Errorf("marker contains an unsupported character %q", scanner.peek())
	}
	return pythonMarkerToken{kind: pythonMarkerIdentifier, text: scanner.value[start:scanner.index]}, true, nil
}

func pythonMarkerWordCharacter(value byte) bool {
	return pythonAlphaNumeric(value) || value == '_' || value == '-' || value == '.'
}

type pythonMarkerParser struct {
	tokens []pythonMarkerToken
	index  int
}

func (parser *pythonMarkerParser) expression() error {
	if err := parser.conjunction(); err != nil {
		return err
	}
	for parser.matchIdentifier("or") {
		if err := parser.conjunction(); err != nil {
			return err
		}
	}
	return nil
}

func (parser *pythonMarkerParser) conjunction() error {
	if err := parser.primary(); err != nil {
		return err
	}
	for parser.matchIdentifier("and") {
		if err := parser.primary(); err != nil {
			return err
		}
	}
	return nil
}

func (parser *pythonMarkerParser) primary() error {
	if parser.matchKind(pythonMarkerLeftParen) {
		if err := parser.expression(); err != nil {
			return err
		}
		if !parser.matchKind(pythonMarkerRightParen) {
			return fmt.Errorf("marker parenthesis is unclosed")
		}
		return nil
	}
	return parser.comparison()
}

func (parser *pythonMarkerParser) comparison() error {
	left, found := parser.nextOperand()
	if !found {
		return fmt.Errorf("marker comparison is missing its left operand")
	}
	operator, found := parser.markerOperator()
	if !found {
		return fmt.Errorf("marker comparison is missing its operator")
	}
	right, found := parser.nextOperand()
	if !found {
		return fmt.Errorf("marker comparison %q is missing its right operand", operator)
	}
	if !pythonMarkerVariable(left) && !pythonMarkerVariable(right) {
		return fmt.Errorf("marker comparison must name an environment variable")
	}
	return nil
}

func (parser *pythonMarkerParser) nextOperand() (pythonMarkerToken, bool) {
	if parser.index >= len(parser.tokens) {
		return pythonMarkerToken{}, false
	}
	token := parser.tokens[parser.index]
	if token.kind != pythonMarkerIdentifier && token.kind != pythonMarkerString {
		return pythonMarkerToken{}, false
	}
	parser.index++
	if token.kind == pythonMarkerIdentifier && !pythonMarkerVariable(token) {
		parser.index--
		return pythonMarkerToken{}, false
	}
	return token, true
}

func (parser *pythonMarkerParser) markerOperator() (string, bool) {
	if parser.index >= len(parser.tokens) {
		return "", false
	}
	token := parser.tokens[parser.index]
	if token.kind == pythonMarkerOperator {
		parser.index++
		return token.text, true
	}
	if parser.matchIdentifier("in") {
		return "in", true
	}
	if parser.matchIdentifier("not") {
		if parser.matchIdentifier("in") {
			return "not in", true
		}
		parser.index--
	}
	return "", false
}

func (parser *pythonMarkerParser) matchKind(kind pythonMarkerTokenKind) bool {
	if parser.index >= len(parser.tokens) || parser.tokens[parser.index].kind != kind {
		return false
	}
	parser.index++
	return true
}

func (parser *pythonMarkerParser) matchIdentifier(value string) bool {
	if parser.index >= len(parser.tokens) || parser.tokens[parser.index].kind != pythonMarkerIdentifier || parser.tokens[parser.index].text != value {
		return false
	}
	parser.index++
	return true
}

func pythonMarkerVariable(token pythonMarkerToken) bool {
	if token.kind != pythonMarkerIdentifier {
		return false
	}
	return map[string]bool{
		"implementation_name": true, "implementation_version": true, "os_name": true,
		"platform_machine": true, "platform_python_implementation": true, "platform_release": true,
		"platform_system": true, "platform_version": true, "python_full_version": true,
		"python_version": true, "sys_platform": true, "extra": true, "extras": true,
		"dependency_groups": true,
	}[token.text]
}
