package repository

import (
	"fmt"
	"net/url"
	"strings"
)

func NormalizePythonPackageName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("package name is empty")
	}
	for index, character := range name {
		if !pythonPackageNameCharacter(character) {
			return "", fmt.Errorf("package name %q contains an unsupported character", name)
		}
		if pythonPackageNameEdge(name, index) && !pythonPackageNameAlphaNumeric(character) {
			return "", fmt.Errorf("package name %q must start and end with a letter or digit", name)
		}
	}
	return normalizedPythonPackageName(name), nil
}

func pythonPackageNameCharacter(character rune) bool {
	return pythonPackageNameAlphaNumeric(character) || strings.ContainsRune("-_.", character)
}

func pythonPackageNameAlphaNumeric(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
}

func pythonPackageNameEdge(name string, index int) bool {
	return index == 0 || index+1 == len(name)
}

func normalizedPythonPackageName(name string) string {
	normalized := strings.Builder{}
	separator := false
	for _, character := range strings.ToLower(name) {
		if strings.ContainsRune("-_.", character) {
			separator = true
			continue
		}
		if separator && normalized.Len() > 0 {
			normalized.WriteByte('-')
		}
		separator = false
		normalized.WriteRune(character)
	}
	return normalized.String()
}

func ParsePythonRequirement(value string) (PythonRequirement, error) {
	parser := pythonRequirementParser{input: value}
	requirement, err := pythonRequirementHeader(&parser)
	if err != nil {
		return PythonRequirement{}, err
	}
	if err := pythonRequirementSource(&parser, &requirement); err != nil {
		return PythonRequirement{}, err
	}
	if err := pythonRequirementMarker(&parser, &requirement); err != nil {
		return PythonRequirement{}, err
	}
	parser.skipSpace()
	if !parser.done() {
		return PythonRequirement{}, fmt.Errorf("unexpected requirement content %q", parser.remaining())
	}
	return requirement, nil
}

func pythonRequirementHeader(parser *pythonRequirementParser) (PythonRequirement, error) {
	parser.skipSpace()
	name, err := parser.identifier()
	if err != nil {
		return PythonRequirement{}, err
	}
	normalized, err := NormalizePythonPackageName(name)
	if err != nil {
		return PythonRequirement{}, err
	}
	requirement := PythonRequirement{Raw: parser.input, Name: normalized, Kind: PythonRegistryRequirement}
	parser.skipSpace()
	if parser.peek() != '[' {
		return requirement, nil
	}
	extras, err := parser.extras()
	if err != nil {
		return PythonRequirement{}, err
	}
	requirement.Extras = extras
	parser.skipSpace()
	return requirement, nil
}

func pythonRequirementSource(parser *pythonRequirementParser, requirement *PythonRequirement) error {
	if parser.peek() == '@' {
		parser.take()
		parser.skipSpace()
		return pythonRequirementDirectURL(parser, requirement)
	}
	if parser.peek() == ';' || parser.done() {
		return nil
	}
	return pythonRequirementVersionSpecifiers(parser, requirement)
}

func pythonRequirementDirectURL(parser *pythonRequirementParser, requirement *PythonRequirement) error {
	urlValue, err := parser.directURL()
	if err != nil {
		return err
	}
	return pythonDirectRequirementURL(urlValue, requirement)
}

func pythonDirectRequirementURL(value string, requirement *PythonRequirement) error {
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "git+") {
		return pythonGitRequirementURL(value, requirement)
	}
	if strings.HasPrefix(lower, "file:") {
		return pythonFileRequirementURL(value, requirement)
	}
	if err := pythonOrdinaryRequirementURL(value); err != nil {
		return err
	}
	requirement.Kind = PythonURLRequirement
	requirement.URL = value
	return nil
}

func pythonGitRequirementURL(value string, requirement *PythonRequirement) error {
	git, err := parsePythonDirectGitURL(value)
	if err != nil {
		return err
	}
	requirement.Kind = PythonGitRequirement
	requirement.Git = git
	requirement.URL = git.Identity()
	return nil
}

func pythonFileRequirementURL(value string, requirement *PythonRequirement) error {
	filePath, err := parsePythonFileURL(value)
	if err != nil {
		return err
	}
	requirement.Kind = PythonFileRequirement
	requirement.FilePath = filePath
	requirement.URL = value
	return nil
}

func pythonOrdinaryRequirementURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || hasPythonDynamicValue(value) {
		return fmt.Errorf("direct URL is malformed")
	}
	if strings.EqualFold(parsed.Scheme, "git") {
		return fmt.Errorf("git URL must use git+https:// or git+ssh://")
	}
	return nil
}

func pythonRequirementVersionSpecifiers(parser *pythonRequirementParser, requirement *PythonRequirement) error {
	parenthesized := parser.peek() == '('
	if parenthesized {
		parser.take()
		parser.skipSpace()
	}
	specifiers, err := parser.specifiers(parenthesized)
	if err != nil {
		return err
	}
	if parenthesized && parser.take() != ')' {
		return fmt.Errorf("version specifier list is not closed")
	}
	requirement.Specifiers = specifiers
	requirement.Version = pythonSpecifierText(specifiers)
	return nil
}

func pythonRequirementMarker(parser *pythonRequirementParser, requirement *PythonRequirement) error {
	parser.skipSpace()
	if parser.peek() != ';' {
		return nil
	}
	parser.take()
	marker := strings.TrimSpace(parser.remaining())
	if marker == "" {
		return fmt.Errorf("marker is empty")
	}
	markerKey, err := parsePythonMarker(marker)
	if err != nil {
		return err
	}
	requirement.Marker = marker
	requirement.markerKey = markerKey
	parser.index = len(parser.input)
	return nil
}

func pythonSpecifierText(specifiers []PythonVersionSpecifier) string {
	parts := make([]string, 0, len(specifiers))
	for _, specifier := range specifiers {
		parts = append(parts, specifier.Operator+specifier.Version)
	}
	return strings.Join(parts, ",")
}

type pythonRequirementParser struct {
	input string
	index int
}

func (parser *pythonRequirementParser) done() bool {
	return parser.index >= len(parser.input)
}

func (parser *pythonRequirementParser) peek() byte {
	if parser.done() {
		return 0
	}
	return parser.input[parser.index]
}

func (parser *pythonRequirementParser) take() byte {
	value := parser.peek()
	if value != 0 {
		parser.index++
	}
	return value
}

func (parser *pythonRequirementParser) remaining() string {
	return parser.input[parser.index:]
}

func (parser *pythonRequirementParser) skipSpace() {
	for parser.peek() == ' ' || parser.peek() == '\t' {
		parser.take()
	}
}

func (parser *pythonRequirementParser) identifier() (string, error) {
	start := parser.index
	if !pythonIdentifierCharacter(parser.peek()) || !pythonAlphaNumeric(parser.peek()) {
		return "", fmt.Errorf("requirement name must start with a letter or digit")
	}
	for pythonIdentifierCharacter(parser.peek()) {
		parser.take()
	}
	name := parser.input[start:parser.index]
	if !pythonAlphaNumeric(name[len(name)-1]) {
		return "", fmt.Errorf("requirement name must end with a letter or digit")
	}
	return name, nil
}

func (parser *pythonRequirementParser) extras() ([]string, error) {
	parser.take()
	parser.skipSpace()
	extras := []string{}
	seen := map[string]bool{}
	for {
		if parser.peek() == ']' {
			parser.take()
			return extras, nil
		}
		extra, err := parser.identifier()
		if err != nil {
			return nil, fmt.Errorf("invalid extra: %w", err)
		}
		normalized, err := NormalizePythonPackageName(extra)
		if err != nil {
			return nil, err
		}
		if seen[normalized] {
			return nil, fmt.Errorf("extra %q is declared more than once", extra)
		}
		seen[normalized] = true
		extras = append(extras, normalized)
		parser.skipSpace()
		if parser.peek() == ']' {
			parser.take()
			return extras, nil
		}
		if parser.take() != ',' {
			return nil, fmt.Errorf("extras must be comma-separated")
		}
		parser.skipSpace()
	}
}

func (parser *pythonRequirementParser) specifiers(parenthesized bool) ([]PythonVersionSpecifier, error) {
	specifiers := []PythonVersionSpecifier{}
	for {
		specifier, err := parser.specifier()
		if err != nil {
			return nil, err
		}
		specifiers = append(specifiers, specifier)
		if !parser.moreSpecifiers(parenthesized) {
			return specifiers, nil
		}
	}
}

func (parser *pythonRequirementParser) specifier() (PythonVersionSpecifier, error) {
	parser.skipSpace()
	operator := parser.versionOperator()
	if operator == "" {
		return PythonVersionSpecifier{}, fmt.Errorf("expected a version specifier")
	}
	parser.skipSpace()
	start := parser.index
	for pythonVersionCharacter(parser.peek()) {
		parser.take()
	}
	version := parser.input[start:parser.index]
	if version == "" || operator != "===" && !validPythonVersion(version) {
		return PythonVersionSpecifier{}, fmt.Errorf("version specifier %q is malformed", version)
	}
	return PythonVersionSpecifier{Operator: operator, Version: version}, nil
}

func (parser *pythonRequirementParser) moreSpecifiers(parenthesized bool) bool {
	parser.skipSpace()
	if parser.peek() != ',' {
		return false
	}
	parser.take()
	parser.skipSpace()
	if parser.done() || parser.peek() == ';' {
		return false
	}
	return !parenthesized || parser.peek() != ')'
}

func (parser *pythonRequirementParser) versionOperator() string {
	for _, operator := range []string{"===", "~=", "==", "!=", "<=", ">=", "<", ">"} {
		if strings.HasPrefix(parser.remaining(), operator) {
			parser.index += len(operator)
			return operator
		}
	}
	return ""
}

func (parser *pythonRequirementParser) directURL() (string, error) {
	start := parser.index
	for !parser.done() && parser.peek() != ' ' && parser.peek() != '\t' && parser.peek() != '\r' && parser.peek() != '\n' {
		parser.take()
	}
	value := parser.input[start:parser.index]
	if value == "" {
		return "", fmt.Errorf("direct URL is empty")
	}
	return value, nil
}

func pythonIdentifierCharacter(value byte) bool {
	return pythonAlphaNumeric(value) || value == '-' || value == '_' || value == '.'
}

func pythonAlphaNumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func pythonVersionCharacter(value byte) bool {
	return pythonAlphaNumeric(value) || strings.ContainsRune(".!+_-*", rune(value))
}

func validPythonVersion(value string) bool {
	core, valid := pythonVersionCore(value)
	if !valid {
		return false
	}
	parser := pythonVersionParser{value: core}
	if !parser.release() {
		return false
	}
	parser.preRelease()
	parser.postRelease()
	parser.developmentRelease()
	return parser.done()
}

func pythonVersionCore(value string) (string, bool) {
	if value == "" || value == "*" {
		return "", false
	}
	if strings.HasSuffix(value, ".*") {
		return pythonVersionCore(strings.TrimSuffix(value, ".*"))
	}
	if !pythonVersionCharacters(value) {
		return "", false
	}
	value = strings.ToLower(value)
	value = strings.TrimPrefix(value, "v")
	if value == "" {
		return "", false
	}
	withoutEpoch, valid := pythonVersionWithoutEpoch(value)
	if !valid {
		return "", false
	}
	return pythonVersionWithoutLocal(withoutEpoch)
}

func pythonVersionCharacters(value string) bool {
	for index := range value {
		if !pythonVersionCharacter(value[index]) {
			return false
		}
	}
	return true
}

func pythonVersionWithoutEpoch(value string) (string, bool) {
	epoch, after, found := strings.Cut(value, "!")
	if !found {
		return value, true
	}
	if !pythonDigits(epoch) || after == "" || strings.Contains(after, "!") {
		return "", false
	}
	return after, true
}

func pythonVersionWithoutLocal(value string) (string, bool) {
	release, suffix, found := strings.Cut(value, "+")
	if !found {
		return value, true
	}
	if suffix == "" || strings.Contains(suffix, "+") || !pythonLocalVersion(suffix) {
		return "", false
	}
	return release, true
}

type pythonVersionParser struct {
	value string
	index int
}

func (parser *pythonVersionParser) done() bool {
	return parser.index == len(parser.value)
}

func (parser *pythonVersionParser) release() bool {
	for {
		if !parser.releaseSegment() {
			return false
		}
		if !parser.releaseSeparator() {
			return true
		}
	}
}

func (parser *pythonVersionParser) releaseSegment() bool {
	start := parser.index
	for parser.currentDigit() {
		parser.index++
	}
	return parser.index != start
}

func (parser *pythonVersionParser) releaseSeparator() bool {
	if parser.index+1 >= len(parser.value) {
		return false
	}
	if parser.value[parser.index] != '.' || !pythonVersionDigit(parser.value[parser.index+1]) {
		return false
	}
	parser.index++
	return true
}

func (parser *pythonVersionParser) currentDigit() bool {
	return parser.index < len(parser.value) && pythonVersionDigit(parser.value[parser.index])
}

func pythonVersionDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func (parser *pythonVersionParser) preRelease() {
	if found, next := pythonVersionLabel(parser.value, parser.index, []string{"alpha", "beta", "preview", "pre", "rc", "a", "b", "c"}); found {
		parser.index = next
	}
}

func (parser *pythonVersionParser) postRelease() {
	if parser.hyphenPostRelease() {
		return
	}
	if found, next := pythonVersionLabel(parser.value, parser.index, []string{"post", "rev", "r"}); found {
		parser.index = next
	}
}

func (parser *pythonVersionParser) hyphenPostRelease() bool {
	if parser.index+1 >= len(parser.value) {
		return false
	}
	if parser.value[parser.index] != '-' || !pythonVersionDigit(parser.value[parser.index+1]) {
		return false
	}
	parser.index += 2
	for parser.currentDigit() {
		parser.index++
	}
	return true
}

func (parser *pythonVersionParser) developmentRelease() {
	if found, next := pythonVersionLabel(parser.value, parser.index, []string{"dev"}); found {
		parser.index = next
	}
}

func pythonVersionLabel(value string, index int, labels []string) (bool, int) {
	start := index
	index = pythonVersionSeparators(value, index)
	label, found := pythonVersionMatchingLabel(value[index:], labels)
	if !found {
		return false, start
	}
	index += len(label)
	index = pythonVersionSeparators(value, index)
	return true, pythonVersionDigits(value, index)
}

func pythonVersionSeparators(value string, index int) int {
	for index < len(value) && pythonVersionSeparator(value[index]) {
		index++
	}
	return index
}

func pythonVersionSeparator(value byte) bool {
	return value == '.' || value == '-' || value == '_'
}

func pythonVersionMatchingLabel(value string, labels []string) (string, bool) {
	for _, label := range labels {
		if strings.HasPrefix(value, label) {
			return label, true
		}
	}
	return "", false
}

func pythonVersionDigits(value string, index int) int {
	for index < len(value) && pythonVersionDigit(value[index]) {
		index++
	}
	return index
}

func pythonDigits(value string) bool {
	if value == "" {
		return false
	}
	for index := range value {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func pythonLocalVersion(value string) bool {
	segment := ""
	for _, character := range value {
		if character == '.' || character == '-' || character == '_' {
			if segment == "" {
				return false
			}
			segment = ""
			continue
		}
		if !pythonAlphaNumeric(byte(character)) {
			return false
		}
		segment += string(character)
	}
	return segment != ""
}
