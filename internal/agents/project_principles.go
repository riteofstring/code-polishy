package agents

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	goldmarkutil "github.com/yuin/goldmark/util"
)

const (
	maximumCanonicalAgentsBytes       = 5 << 10
	projectPrinciplesHeading          = "## Project principles"
	maximumProjectPrinciplesBytes     = 5 << 10
	maximumProjectPrinciples          = 12
	maximumProjectPrincipleBytes      = 400
	maximumProjectPrincipleTitleWords = 8
)

type agentsDocument struct {
	managed     []byte
	principles  []byte
	hasBoundary bool
}

type projectPrincipleLine struct {
	text  string
	start int
}

type projectPrincipleListState struct {
	number     int
	itemStart  int
	itemIndent string
}

func validateCanonicalAgents(template []byte) error {
	if len(template) == 0 {
		return errors.New("canonical AGENTS.md must not be empty")
	}
	crlfBytes := len(template) + bytes.Count(template, []byte("\n"))
	if crlfBytes > maximumCanonicalAgentsBytes {
		return fmt.Errorf("canonical AGENTS.md uses %d bytes with CRLF, maximum %d", crlfBytes, maximumCanonicalAgentsBytes)
	}
	document, err := splitAgentsDocument(template)
	if err != nil {
		return fmt.Errorf("canonical AGENTS.md: %w", err)
	}
	if document.hasBoundary {
		return errors.New("canonical AGENTS.md must not contain project principles")
	}
	return nil
}

func splitAgentsDocument(contents []byte) (agentsDocument, error) {
	headingStart := -1
	separatorStart := -1
	previousStart := -1
	previousText := []byte(nil)
	for start := 0; start < len(contents); {
		relativeEnd := bytes.IndexByte(contents[start:], '\n')
		end := len(contents)
		if relativeEnd >= 0 {
			end = start + relativeEnd + 1
		}
		text := bytes.TrimSuffix(contents[start:end], []byte("\n"))
		text = bytes.TrimSuffix(text, []byte("\r"))
		if bytes.Equal(text, []byte(projectPrinciplesHeading)) {
			if headingStart >= 0 {
				return agentsDocument{}, errors.New("project principles heading must appear exactly once")
			}
			if previousStart < 0 || len(previousText) != 0 {
				return agentsDocument{}, errors.New("project principles must be the final section after one blank line")
			}
			headingStart = start
			separatorStart = previousStart
		}
		previousStart = start
		previousText = text
		start = end
	}
	if headingStart < 0 {
		return agentsDocument{managed: contents}, nil
	}
	document := agentsDocument{managed: contents[:separatorStart], principles: contents[separatorStart:], hasBoundary: true}
	if err := validateProjectPrinciples(document.principles); err != nil {
		return agentsDocument{}, err
	}
	return document, nil
}

func validateProjectPrinciples(suffix []byte) error {
	if len(suffix) == 0 {
		return nil
	}
	section, lines, err := parseProjectPrinciples(suffix)
	if err != nil {
		return err
	}
	return validateProjectPrincipleList(section, lines[2:])
}

func parseProjectPrinciples(suffix []byte) ([]byte, []projectPrincipleLine, error) {
	section, err := projectPrinciplesSection(suffix)
	if err != nil {
		return nil, nil, err
	}
	lines, err := projectPrincipleLines(section)
	if err != nil {
		return nil, nil, err
	}
	if len(lines) < 3 || lines[0].text != projectPrinciplesHeading || lines[1].text != "" {
		return nil, nil, fmt.Errorf("project principles must begin with %q followed by one blank line", projectPrinciplesHeading)
	}
	return section, lines, nil
}

func projectPrinciplesSection(suffix []byte) ([]byte, error) {
	separator, err := projectPrinciplesSeparator(suffix)
	if err != nil {
		return nil, err
	}
	if len(suffix) > maximumProjectPrinciplesBytes {
		return nil, fmt.Errorf("project principles use %d bytes, maximum %d", len(suffix), maximumProjectPrinciplesBytes)
	}
	section := suffix[separator:]
	if !utf8.Valid(section) {
		return nil, errors.New("project principles must contain valid UTF-8")
	}
	return section, nil
}

func projectPrinciplesSeparator(suffix []byte) (int, error) {
	if bytes.HasPrefix(suffix, []byte("\r\n")) {
		return 2, nil
	}
	if suffix[0] == '\n' {
		return 1, nil
	}
	return 0, errors.New("project principles must follow the canonical guidance after one blank line")
}

func validateProjectPrincipleList(section []byte, lines []projectPrincipleLine) error {
	state := projectPrincipleListState{itemStart: -1}
	for _, line := range lines {
		if err := state.addLine(section, line); err != nil {
			return err
		}
	}
	return state.finish(section)
}

func (state *projectPrincipleListState) addLine(section []byte, line projectPrincipleLine) error {
	number, body, numbered := numberedProjectPrinciple(line.text)
	if numbered {
		return state.startItem(section, line.start, number, body)
	}
	return state.continueItem(line.text)
}

func (state *projectPrincipleListState) startItem(section []byte, start, number int, body string) error {
	if state.itemStart >= 0 {
		if err := validateProjectPrincipleSize(section, state.itemStart, start, state.number); err != nil {
			return err
		}
	}
	state.number++
	if number != state.number {
		return fmt.Errorf("project principles must use consecutive numbering; expected %d, found %d", state.number, number)
	}
	if state.number > maximumProjectPrinciples {
		return fmt.Errorf("project principles contain more than %d items", maximumProjectPrinciples)
	}
	if err := validateProjectPrincipleStart(body, state.number); err != nil {
		return err
	}
	state.itemStart = start
	state.itemIndent = strings.Repeat(" ", len(strconv.Itoa(number))+2)
	return nil
}

func (state *projectPrincipleListState) continueItem(text string) error {
	if state.itemStart < 0 {
		return errors.New("project principles must contain one flat numbered list")
	}
	if text == "" {
		return fmt.Errorf("project principle %d must be one paragraph", state.number)
	}
	if !validProjectPrincipleIndent(text, state.itemIndent) {
		return fmt.Errorf("project principle %d continuation lines must use list indentation", state.number)
	}
	return validateProjectPrincipleText(strings.TrimPrefix(text, state.itemIndent), state.number)
}

func validProjectPrincipleIndent(text, indent string) bool {
	return strings.HasPrefix(text, indent) && !strings.HasPrefix(text, indent+" ")
}

func (state projectPrincipleListState) finish(section []byte) error {
	if state.itemStart < 0 {
		return errors.New("project principles must contain at least one item")
	}
	return validateProjectPrincipleSize(section, state.itemStart, len(section), state.number)
}

func projectPrincipleLines(section []byte) ([]projectPrincipleLine, error) {
	if len(section) == 0 || section[len(section)-1] != '\n' {
		return nil, errors.New("project principles must end with a newline")
	}
	lines := []projectPrincipleLine{}
	start := 0
	for start < len(section) {
		relativeEnd := bytes.IndexByte(section[start:], '\n')
		if relativeEnd < 0 {
			return nil, errors.New("project principles must end with a newline")
		}
		end := start + relativeEnd + 1
		text := section[start : end-1]
		if len(text) > 0 && text[len(text)-1] == '\r' {
			text = text[:len(text)-1]
		}
		if bytes.ContainsRune(text, '\r') {
			return nil, errors.New("project principles contain an invalid line ending")
		}
		lines = append(lines, projectPrincipleLine{text: string(text), start: start})
		start = end
	}
	return lines, nil
}

func numberedProjectPrinciple(line string) (int, string, bool) {
	separator := strings.Index(line, ". ")
	if separator <= 0 {
		return 0, "", false
	}
	number, err := strconv.Atoi(line[:separator])
	if err != nil || number < 1 || line[:separator] != strconv.Itoa(number) {
		return 0, "", false
	}
	return number, line[separator+2:], true
}

func validateProjectPrincipleStart(body string, number int) error {
	if !strings.HasPrefix(body, "**") {
		return fmt.Errorf("project principle %d must begin with a bold title", number)
	}
	closing := strings.Index(body[2:], "**")
	if closing < 1 {
		return fmt.Errorf("project principle %d must begin with a bold title", number)
	}
	closing += 2
	title := body[2:closing]
	if strings.TrimSpace(title) != title || strings.ContainsAny(title, "*_`|\\\r\n") || containsProjectPrincipleLink(title) {
		return fmt.Errorf("project principle %d has an invalid bold title", number)
	}
	if words := len(strings.Fields(title)); words > maximumProjectPrincipleTitleWords {
		return fmt.Errorf("project principle %d title uses %d words, maximum %d", number, words, maximumProjectPrincipleTitleWords)
	}
	remainder := body[closing+2:]
	if !strings.HasPrefix(remainder, " ") || strings.TrimSpace(remainder) == "" {
		return fmt.Errorf("project principle %d title must be followed by prose on the same line", number)
	}
	return validateProjectPrincipleText(strings.TrimPrefix(remainder, " "), number)
}

func validateProjectPrincipleText(text string, number int) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || trimmed != text {
		return fmt.Errorf("project principle %d has invalid paragraph spacing", number)
	}
	for _, prefix := range []string{"#", ">", "```", "~~~"} {
		if strings.HasPrefix(trimmed, prefix) {
			return fmt.Errorf("project principle %d contains nested Markdown", number)
		}
	}
	if isMarkdownDivider(trimmed) {
		return fmt.Errorf("project principle %d contains nested Markdown", number)
	}
	if startsMarkdownListItem(trimmed) {
		return fmt.Errorf("project principle %d contains a nested list", number)
	}
	if strings.ContainsRune(trimmed, '|') || strings.Contains(trimmed, "**") || strings.Contains(trimmed, "__") ||
		containsProjectPrincipleLink(trimmed) {
		return fmt.Errorf("project principle %d contains unsupported Markdown or a link", number)
	}
	return nil
}

func containsProjectPrincipleLink(text string) bool {
	return strings.ContainsAny(text, "[]<>") || strings.Contains(text, "://") ||
		strings.Contains(text, "www.") || strings.Contains(text, "mailto:") ||
		containsBareEmailLink(text)
}

func containsBareEmailLink(text string) bool {
	for index := 0; index < len(text); index++ {
		if index > 0 && !strings.ContainsRune(" \t\r\n\v\f*_~(", rune(text[index-1])) {
			continue
		}
		candidate := []byte(text[index:])
		stop := goldmarkutil.FindEmailIndex(candidate)
		if stop < 0 {
			continue
		}
		at := bytes.IndexByte(candidate[:stop], '@')
		if at < 0 || !bytes.ContainsRune(candidate[at:stop-1], '.') {
			continue
		}
		if stop < len(candidate) && (candidate[stop] == '-' || candidate[stop] == '_') {
			continue
		}
		return true
	}
	return false
}

func isMarkdownDivider(text string) bool {
	if isSingleColumnTableDelimiter(text) {
		return true
	}
	marker, count, valid := markdownDividerRun(text)
	if !valid {
		return false
	}
	return markdownDividerLongEnough(marker, count)
}

func markdownDividerRun(text string) (byte, int, bool) {
	marker := byte(0)
	count := 0
	for index := 0; index < len(text); index++ {
		character := text[index]
		if strings.ContainsRune(" \t", rune(character)) {
			continue
		}
		if !strings.ContainsRune("=-_*", rune(character)) {
			return 0, 0, false
		}
		if marker != 0 && marker != character {
			return 0, 0, false
		}
		marker = character
		count++
	}
	return marker, count, true
}

func markdownDividerLongEnough(marker byte, count int) bool {
	if marker == '=' || marker == '-' {
		return count > 0
	}
	return count >= 3
}

func isSingleColumnTableDelimiter(text string) bool {
	aligned := false
	if strings.HasPrefix(text, ":") {
		text = strings.TrimPrefix(text, ":")
		aligned = true
	}
	if strings.HasSuffix(text, ":") {
		text = strings.TrimSuffix(text, ":")
		aligned = true
	}
	return aligned && text != "" && strings.Trim(text, "-") == ""
}

func startsMarkdownListItem(text string) bool {
	if len(text) == 0 {
		return false
	}
	if strings.ContainsRune("-+*", rune(text[0])) {
		return followsMarkdownListMarker(text, 1)
	}
	return startsMarkdownOrderedListItem(text)
}

func startsMarkdownOrderedListItem(text string) bool {
	digits := leadingMarkdownListDigits(text)
	if digits == 0 || digits >= len(text) {
		return false
	}
	if !strings.ContainsRune(".)", rune(text[digits])) {
		return false
	}
	return followsMarkdownListMarker(text, digits+1)
}

func leadingMarkdownListDigits(text string) int {
	digits := 0
	for digits < len(text) && digits < 9 && text[digits] >= '0' && text[digits] <= '9' {
		digits++
	}
	return digits
}

func followsMarkdownListMarker(text string, end int) bool {
	return end == len(text) || end < len(text) && strings.ContainsRune(" \t", rune(text[end]))
}

func validateProjectPrincipleSize(section []byte, start, end, number int) error {
	size := end - start
	if size > maximumProjectPrincipleBytes {
		return fmt.Errorf("project principle %d uses %d bytes, maximum %d", number, size, maximumProjectPrincipleBytes)
	}
	return nil
}

func renderSynchronizedAgents(existing, template []byte) ([]byte, bool, error) {
	document, err := splitAgentsDocument(existing)
	if err != nil {
		return nil, false, err
	}
	if !document.hasBoundary {
		if matchesCanonicalGuidance(existing, template) {
			return nil, false, nil
		}
		return append([]byte{}, template...), true, nil
	}
	if matchesCanonicalGuidance(document.managed, template) {
		return nil, false, nil
	}
	canonical := template
	if usesCRLF(document.managed) {
		canonical = bytes.ReplaceAll(template, []byte("\n"), []byte("\r\n"))
	}
	updated := append([]byte{}, canonical...)
	updated = append(updated, document.principles...)
	return updated, true, nil
}

func usesCRLF(contents []byte) bool {
	if !bytes.Contains(contents, []byte("\r\n")) {
		return false
	}
	return !bytes.Contains(bytes.ReplaceAll(contents, []byte("\r\n"), nil), []byte("\n"))
}
