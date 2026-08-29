package quality

import (
	"context"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const documentationMaximumBytes int64 = 4 * 1024 * 1024

type markdownDocument struct {
	path     string
	text     string
	headings map[string]markdownHeading
	links    []markdownLink
	anchors  []markdownAnchorIssue
}

type markdownHeading struct {
	line   int
	column int
}

type markdownLink struct {
	destination string
	image       bool
	line        int
	column      int
}

type markdownAnchorIssue struct {
	line   int
	column int
}

type markdownCorpus struct {
	files     map[string]bool
	folded    map[string][]string
	documents map[string]markdownDocument
}

func DocumentationFindings(ctx context.Context, repo repository.Repository, addedOrModified, deleted []string) []policy.Finding {
	changed, _, findings := documentationCandidates(repo, addedOrModified, deleted)
	corpus, corpusFindings := documentationCorpusFor(repo, changed)
	findings = append(findings, corpusFindings...)
	formatPaths := []string{}
	for _, path := range changed {
		document, exists := corpus.documents[path]
		if exists {
			findings = append(findings, markdownTextFindings(document)...)
			formatPaths = append(formatPaths, path)
		}
	}
	findings = append(findings, markdownLinkFindings(repo, corpus)...)
	findings = append(findings, DocumentationFormatFindings(ctx, repo, formatPaths)...)
	return stableDocumentationFindings(findings)
}

func DocumentationFormatFindings(ctx context.Context, repo repository.Repository, paths []string) []policy.Finding {
	return markdownFormat(ctx, repo, paths, false)
}

func DocumentationFormatWrite(ctx context.Context, repo repository.Repository, paths []string) []policy.Finding {
	return markdownFormat(ctx, repo, paths, true)
}

func documentationCandidates(repo repository.Repository, addedOrModified, deleted []string) ([]string, []string, []policy.Finding) {
	changed, changedFindings := documentationCandidatePaths(repo, addedOrModified, "changed")
	deletedPaths, deletedFindings := documentationCandidatePaths(repo, deleted, "deleted")
	return changed, deletedPaths, append(changedFindings, deletedFindings...)
}

func documentationCandidatePaths(repo repository.Repository, paths []string, kind string) ([]string, []policy.Finding) {
	selected := []string{}
	findings := []policy.Finding{}
	for _, candidate := range paths {
		normalized, err := repo.NormalizePath(candidate)
		if err != nil {
			findings = append(findings, policy.Finding{
				Check: "quality.documentationPath", Path: filepath.ToSlash(candidate), Subject: kind,
				Message: "Markdown candidate path must stay inside the repository",
			})
			continue
		}
		if !markdownPath(normalized) {
			findings = append(findings, policy.Finding{
				Check: "quality.documentationPath", Path: normalized, Subject: kind,
				Message: "documentation contract accepts only Markdown paths",
			})
			continue
		}
		selected = append(selected, normalized)
	}
	return uniqueMarkdownPaths(selected), findings
}

func documentationCorpusFor(repo repository.Repository, extra []string) (markdownCorpus, []policy.Finding) {
	corpus := markdownCorpus{files: map[string]bool{}, folded: map[string][]string{}, documents: map[string]markdownDocument{}}
	files, err := repo.RawFiles()
	if err != nil {
		return corpus, []policy.Finding{{
			Check: "quality.documentationCorpus", Path: "repository", Subject: "inventory",
			Message: err.Error(),
		}}
	}
	for _, path := range files {
		if repo.IsExcluded(path) {
			continue
		}
		corpus.files[path] = true
		folded := strings.ToLower(path)
		corpus.folded[folded] = append(corpus.folded[folded], path)
	}
	for key := range corpus.folded {
		sort.Strings(corpus.folded[key])
	}
	paths := []string{}
	for _, path := range files {
		if !repo.IsExcluded(path) && markdownPath(path) {
			paths = append(paths, path)
		}
	}
	paths = append(paths, extra...)
	findings := []policy.Finding{}
	for _, path := range uniqueMarkdownPaths(paths) {
		document, documentFindings := readMarkdownDocument(repo, path)
		findings = append(findings, documentFindings...)
		if len(documentFindings) == 0 {
			corpus.documents[path] = document
			for _, issue := range document.anchors {
				findings = append(findings, policy.Finding{
					Check: "quality.documentationAnchor", Path: path, Subject: markdownSubject(issue.line, issue.column),
					Message: "heading does not produce a usable fragment anchor",
				})
			}
		}
	}
	return corpus, findings
}

func readMarkdownDocument(repo repository.Repository, path string) (markdownDocument, []policy.Finding) {
	abs := filepath.Join(repo.Root, filepath.FromSlash(path))
	info, err := os.Lstat(abs)
	if err != nil {
		return markdownDocument{}, []policy.Finding{{
			Check: "quality.documentationPath", Path: path, Subject: "regular file",
			Message: "Markdown document is missing or unreadable",
		}}
	}
	if !info.Mode().IsRegular() {
		return markdownDocument{}, []policy.Finding{{
			Check: "quality.documentationPath", Path: path, Subject: "regular file",
			Message: "Markdown document must be a contained regular file",
		}}
	}
	if info.Size() > documentationMaximumBytes {
		return markdownDocument{}, []policy.Finding{{
			Check: "quality.documentationSize", Path: path, Subject: strconv.FormatInt(info.Size(), 10),
			Message: fmt.Sprintf("Markdown document exceeds the %d byte limit", documentationMaximumBytes),
		}}
	}
	resolved, err := repo.Resolve(path)
	if err != nil {
		return markdownDocument{}, []policy.Finding{{
			Check: "quality.documentationPath", Path: path, Subject: "contained path",
			Message: "Markdown document must resolve inside the repository",
		}}
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return markdownDocument{}, []policy.Finding{{
			Check: "quality.documentationPath", Path: path, Subject: "read",
			Message: "Markdown document is unreadable",
		}}
	}
	if !utf8.Valid(data) {
		return markdownDocument{}, []policy.Finding{{
			Check: "quality.documentationText", Path: path, Subject: "UTF-8",
			Message: "Markdown document must be valid UTF-8 text",
		}}
	}
	return parseMarkdownDocument(path, string(data)), nil
}

func markdownTextFindings(document markdownDocument) []policy.Finding {
	findings := []policy.Finding{}
	lines := strings.Split(document.text, "\n")
	for index, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimRight(line, " \t")
		if len(trimmed) == len(line) {
			continue
		}
		findings = append(findings, policy.Finding{
			Check: "quality.documentationWhitespace", Path: document.path,
			Subject: markdownSubject(index+1, utf8.RuneCountInString(trimmed)+1),
			Message: "Markdown line has trailing whitespace",
		})
	}
	if document.text != "" && !strings.HasSuffix(document.text, "\n") {
		line := len(lines)
		column := utf8.RuneCountInString(lines[len(lines)-1]) + 1
		findings = append(findings, policy.Finding{
			Check: "quality.documentationFinalNewline", Path: document.path,
			Subject: markdownSubject(line, column),
			Message: "Markdown document must end with a newline",
		})
	}
	return findings
}

func parseMarkdownDocument(path, text string) markdownDocument {
	document := markdownDocument{path: path, text: text, headings: map[string]markdownHeading{}}
	document.links = markdownLinks(text)
	lines := strings.Split(text, "\n")
	fence := markdownFence{}
	previous := markdownHeadingSource{}
	duplicates := map[string]int{}
	for index, original := range lines {
		line := strings.TrimSuffix(original, "\r")
		if fence.open {
			if markdownFenceCloser(line, fence) {
				fence = markdownFence{}
			}
			previous = markdownHeadingSource{}
			continue
		}
		if opened, found := markdownFenceOpener(line); found {
			fence = opened
			previous = markdownHeadingSource{}
			continue
		}
		if content, column, found := markdownATXHeading(line); found {
			document.addHeading(content, index+1, column, duplicates)
			previous = markdownHeadingSource{}
			continue
		}
		if markdownSetextUnderline(line) && previous.text != "" {
			document.addHeading(previous.text, previous.line, previous.column, duplicates)
			previous = markdownHeadingSource{}
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			previous = markdownHeadingSource{}
			continue
		}
		previous = markdownHeadingSource{text: trimmed, line: index + 1, column: markdownFirstColumn(line)}
	}
	return document
}

type markdownHeadingSource struct {
	text   string
	line   int
	column int
}

func (document *markdownDocument) addHeading(content string, line, column int, duplicates map[string]int) {
	anchor := markdownAnchor(content)
	if anchor == "" {
		document.anchors = append(document.anchors, markdownAnchorIssue{line: line, column: column})
		return
	}
	duplicate := duplicates[anchor]
	duplicates[anchor]++
	if duplicate > 0 {
		anchor += "-" + strconv.Itoa(duplicate)
	}
	document.headings[anchor] = markdownHeading{line: line, column: column}
}

func markdownLinkFindings(repo repository.Repository, corpus markdownCorpus) []policy.Finding {
	findings := []policy.Finding{}
	paths := make([]string, 0, len(corpus.documents))
	for path := range corpus.documents {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, source := range paths {
		for _, link := range corpus.documents[source].links {
			findings = append(findings, markdownLinkFinding(repo, corpus, source, link)...)
		}
	}
	return findings
}

func markdownLinkFinding(repo repository.Repository, corpus markdownCorpus, source string, link markdownLink) []policy.Finding {
	target, fragment, external, err := markdownDestination(link.destination)
	if err != nil {
		return []policy.Finding{markdownLinkProblem(source, link, err.Error())}
	}
	if external {
		return nil
	}
	if target == "" && fragment == "" {
		return []policy.Finding{markdownLinkProblem(source, link, "local link has no target")}
	}
	resolved, problem := markdownResolvedTarget(repo, corpus, source, target)
	if problem != "" {
		return []policy.Finding{markdownLinkProblem(source, link, problem)}
	}
	if fragment == "" || link.image {
		return nil
	}
	return markdownFragmentFindings(corpus, source, resolved, fragment, link)
}

func markdownResolvedTarget(repo repository.Repository, corpus markdownCorpus, source, target string) (string, string) {
	if target == "" {
		return source, ""
	}
	return markdownLocalTarget(repo, corpus, source, target)
}

func markdownFragmentFindings(corpus markdownCorpus, source, resolved, fragment string, link markdownLink) []policy.Finding {
	if !markdownPath(resolved) {
		return []policy.Finding{markdownLinkProblem(source, link, "fragment target must be a Markdown document")}
	}
	document, exists := corpus.documents[resolved]
	if !exists {
		return []policy.Finding{markdownLinkProblem(source, link, "fragment target cannot provide Markdown headings")}
	}
	anchor := markdownAnchor(fragment)
	if anchor == "" {
		return []policy.Finding{markdownLinkProblem(source, link, "fragment is not a usable Markdown heading anchor")}
	}
	if _, exists := document.headings[anchor]; !exists {
		return []policy.Finding{markdownLinkProblem(source, link, "Markdown fragment target is missing: #"+anchor)}
	}
	return nil
}

func markdownLocalTarget(repo repository.Repository, corpus markdownCorpus, source, target string) (string, string) {
	if strings.HasPrefix(target, "/") {
		return "", "local target escapes the repository"
	}
	resolved := pathpkg.Clean(pathpkg.Join(pathpkg.Dir(source), target))
	if resolved == "." || resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", "local target escapes the repository"
	}
	if problem := markdownCorpusTargetProblem(repo, corpus, resolved); problem != "" {
		return "", problem
	}
	if problem := markdownFilesystemTargetProblem(repo, resolved); problem != "" {
		return "", problem
	}
	return resolved, ""
}

func markdownCorpusTargetProblem(repo repository.Repository, corpus markdownCorpus, resolved string) string {
	if !corpus.files[resolved] {
		absolute := filepath.Join(repo.Root, filepath.FromSlash(resolved))
		if info, err := os.Lstat(absolute); err == nil && !info.Mode().IsRegular() {
			return "local target is not a regular file"
		}
		return "local target is missing: " + resolved
	}
	if names := corpus.folded[strings.ToLower(resolved)]; len(names) > 1 {
		return "local target is ambiguous across case-sensitive filesystems"
	}
	return ""
}

func markdownFilesystemTargetProblem(repo repository.Repository, resolved string) string {
	abs := filepath.Join(repo.Root, filepath.FromSlash(resolved))
	info, err := os.Lstat(abs)
	if err != nil {
		return "local target is missing: " + resolved
	}
	if !info.Mode().IsRegular() {
		return "local target is not a regular file"
	}
	if _, err := repo.Resolve(resolved); err != nil {
		return "local target resolves outside the repository"
	}
	return ""
}

func markdownLinkProblem(source string, link markdownLink, message string) policy.Finding {
	kind := "link"
	if link.image {
		kind = "image"
	}
	return policy.Finding{
		Check: "quality.documentationLink", Path: source, Subject: markdownSubject(link.line, link.column),
		Message: kind + " " + message,
	}
}

func markdownPath(path string) bool {
	return policy.IsMarkdownPath(path)
}

func markdownFormat(ctx context.Context, repo repository.Repository, paths []string, write bool) []policy.Finding {
	selected := []string{}
	for _, path := range paths {
		if markdownPath(path) && !repo.IsGenerated(path) {
			selected = append(selected, path)
		}
	}
	return runJavaScriptFormat(ctx, repo, uniqueMarkdownPaths(selected), write)
}

func uniqueMarkdownPaths(paths []string) []string {
	set := map[string]bool{}
	for _, path := range paths {
		set[path] = true
	}
	result := make([]string, 0, len(set))
	for path := range set {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func markdownSubject(line, column int) string {
	return strconv.Itoa(line) + ":" + strconv.Itoa(column)
}

func markdownAnchor(value string) string {
	value = markdownUnescape(value)
	var builder strings.Builder
	for _, character := range strings.ToLower(value) {
		switch {
		case unicode.IsLetter(character), unicode.IsNumber(character), character == '-' || character == '_':
			builder.WriteRune(character)
		case unicode.IsSpace(character):
			builder.WriteByte('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func markdownUnescape(value string) string {
	var builder strings.Builder
	for index := 0; index < len(value); {
		if value[index] == '\\' && index+1 < len(value) {
			index++
		}
		builder.WriteByte(value[index])
		index++
	}
	return builder.String()
}

func stableDocumentationFindings(findings []policy.Finding) []policy.Finding {
	sort.Slice(findings, func(left, right int) bool {
		return documentationFindingKey(findings[left]) < documentationFindingKey(findings[right])
	})
	result := make([]policy.Finding, 0, len(findings))
	last := ""
	for _, finding := range findings {
		key := documentationFindingKey(finding)
		if key == last {
			continue
		}
		result = append(result, finding)
		last = key
	}
	return result
}

func documentationFindingKey(finding policy.Finding) string {
	return finding.Check + "\x00" + finding.Path + "\x00" + finding.Subject + "\x00" + finding.Message
}

type markdownFence struct {
	open   bool
	marker byte
	length int
}

func markdownLinks(text string) []markdownLink {
	ignored := markdownIgnored(text)
	definitions, starts := markdownReferenceDefinitions(text, ignored)
	lineStarts := markdownLineStarts(text)
	links := []markdownLink{}
	for position := markdownNextLinkStart(text, ignored, 0); position >= 0; {
		link, next, found := markdownLinkAt(text, ignored, definitions, starts, lineStarts, position)
		if found {
			links = append(links, link)
		}
		position = markdownNextLinkStart(text, ignored, next)
	}
	return links
}

func markdownNextLinkStart(text string, ignored []bool, start int) int {
	for position := start; position < len(text); position++ {
		if ignored[position] {
			continue
		}
		if text[position] == '\\' {
			position++
			continue
		}
		if text[position] == '[' {
			return position
		}
	}
	return -1
}

func markdownLinkAt(
	text string,
	ignored []bool,
	definitions map[string]string,
	starts map[int]bool,
	lineStarts []int,
	opening int,
) (markdownLink, int, bool) {
	closing, found := markdownClosingBracket(text, ignored, opening)
	if !found {
		return markdownLink{}, opening + 1, false
	}
	image, line, column := markdownLinkLocation(text, ignored, lineStarts, opening)
	label := text[opening+1 : closing]
	next := closing + 1
	if destination, end, found := markdownInlineLinkAt(text, ignored, next); found {
		return markdownLink{destination: destination, image: image, line: line, column: column}, end, true
	}
	if reference, end, found := markdownExplicitReferenceAt(text, ignored, label, next); found {
		destination, exists := definitions[markdownReferenceLabel(reference)]
		return markdownLink{destination: destination, image: image, line: line, column: column}, end, exists
	}
	if destination, exists := definitions[markdownReferenceLabel(label)]; exists && !starts[opening] {
		return markdownLink{destination: destination, image: image, line: line, column: column}, next, true
	}
	return markdownLink{}, next, false
}

func markdownLinkLocation(text string, ignored []bool, lineStarts []int, opening int) (bool, int, int) {
	image := opening > 0 && text[opening-1] == '!' && !ignored[opening-1]
	location := opening
	if image {
		location--
	}
	line, column := markdownPosition(text, lineStarts, location)
	return image, line, column
}

func markdownInlineLinkAt(text string, ignored []bool, opening int) (string, int, bool) {
	if opening >= len(text) || ignored[opening] || text[opening] != '(' {
		return "", opening, false
	}
	return markdownInlineDestination(text, ignored, opening)
}

func markdownExplicitReferenceAt(text string, ignored []bool, label string, opening int) (string, int, bool) {
	if opening >= len(text) || ignored[opening] || text[opening] != '[' {
		return "", opening, false
	}
	closing, found := markdownClosingBracket(text, ignored, opening)
	if !found {
		return "", opening, false
	}
	reference := text[opening+1 : closing]
	if reference == "" {
		reference = label
	}
	return reference, closing + 1, true
}

func markdownIgnored(text string) []bool {
	ignored := make([]bool, len(text))
	fence := markdownFence{}
	for offset := 0; offset < len(text); {
		end := strings.IndexByte(text[offset:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += offset
		}
		line := strings.TrimSuffix(text[offset:end], "\r")
		if fence.open {
			markdownIgnoreRange(ignored, offset, end)
			if markdownFenceCloser(line, fence) {
				fence = markdownFence{}
			}
		} else if opened, found := markdownFenceOpener(line); found {
			markdownIgnoreRange(ignored, offset, end)
			fence = opened
		}
		if end == len(text) {
			break
		}
		offset = end + 1
	}
	for position := 0; position < len(text); {
		if ignored[position] || text[position] != '`' {
			position++
			continue
		}
		length := markdownRunLength(text, position, '`')
		end := markdownInlineCodeEnd(text, ignored, position+length, length)
		if end < 0 {
			position += length
			continue
		}
		markdownIgnoreRange(ignored, position, end+length)
		position = end + length
	}
	return ignored
}

func markdownIgnoreRange(ignored []bool, start, end int) {
	for index := start; index < end && index < len(ignored); index++ {
		ignored[index] = true
	}
}

func markdownInlineCodeEnd(text string, ignored []bool, start, length int) int {
	for position := start; position < len(text); {
		if ignored[position] || text[position] != '`' {
			position++
			continue
		}
		if markdownRunLength(text, position, '`') == length {
			return position
		}
		position += markdownRunLength(text, position, '`')
	}
	return -1
}

func markdownReferenceDefinitions(text string, ignored []bool) (map[string]string, map[int]bool) {
	definitions := map[string]string{}
	starts := map[int]bool{}
	pending := markdownReferencePending{}
	for offset := 0; offset < len(text); {
		end := strings.IndexByte(text[offset:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += offset
		}
		line := strings.TrimSuffix(text[offset:end], "\r")
		if markdownLineIgnored(ignored, offset, end) {
			pending = markdownReferencePending{}
		} else if pending.active {
			destination := markdownDestinationText(line)
			if destination != "" {
				if _, exists := definitions[pending.label]; !exists {
					definitions[pending.label] = destination
				}
			}
			pending = markdownReferencePending{}
		} else {
			indent, label, destination, found := markdownReferenceDefinition(line)
			if found {
				key := markdownReferenceLabel(label)
				starts[offset+indent] = true
				if destination == "" {
					pending = markdownReferencePending{active: true, label: key}
				} else if _, exists := definitions[key]; !exists {
					definitions[key] = destination
				}
			}
		}
		if end == len(text) {
			break
		}
		offset = end + 1
	}
	return definitions, starts
}

type markdownReferencePending struct {
	active bool
	label  string
}

func markdownLineIgnored(ignored []bool, start, end int) bool {
	for index := start; index < end; index++ {
		if !ignored[index] {
			return false
		}
	}
	return end > start
}

func markdownReferenceDefinition(line string) (int, string, string, bool) {
	indent := markdownIndent(line)
	if indent > 3 || indent >= len(line) || line[indent] != '[' {
		return 0, "", "", false
	}
	closing := markdownRawClosingBracket(line, indent)
	if closing < 0 || closing+1 >= len(line) || line[closing+1] != ':' {
		return 0, "", "", false
	}
	label := line[indent+1 : closing]
	destination := markdownDestinationText(line[closing+2:])
	if label == "" {
		return 0, "", "", false
	}
	return indent, label, destination, true
}

func markdownDestinationText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if value[0] == '<' {
		if end := strings.IndexByte(value, '>'); end > 0 {
			return value[1:end]
		}
		return value
	}
	for index, character := range value {
		if unicode.IsSpace(character) {
			return value[:index]
		}
	}
	return value
}

func markdownReferenceLabel(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(markdownUnescape(value))), " ")
}

func markdownClosingBracket(text string, ignored []bool, opening int) (int, bool) {
	depth := 1
	for position := opening + 1; position < len(text); {
		if ignored[position] {
			position++
			continue
		}
		if text[position] == '\\' {
			position += 2
			continue
		}
		switch text[position] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return position, true
			}
		}
		position++
	}
	return 0, false
}

func markdownRawClosingBracket(text string, opening int) int {
	for position := opening + 1; position < len(text); {
		if text[position] == '\\' {
			position += 2
			continue
		}
		if text[position] == ']' {
			return position
		}
		position++
	}
	return -1
}

func markdownInlineDestination(text string, ignored []bool, opening int) (string, int, bool) {
	depth := 0
	for position := opening + 1; position < len(text); {
		if ignored[position] {
			position++
			continue
		}
		if text[position] == '\\' {
			position += 2
			continue
		}
		switch text[position] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return markdownDestinationText(text[opening+1 : position]), position + 1, true
			}
			depth--
		}
		position++
	}
	return "", 0, false
}

func markdownLineStarts(text string) []int {
	starts := []int{0}
	for index := 0; index < len(text); index++ {
		if text[index] == '\n' {
			starts = append(starts, index+1)
		}
	}
	return starts
}

func markdownPosition(text string, starts []int, position int) (int, int) {
	index := sort.Search(len(starts), func(index int) bool {
		return starts[index] > position
	}) - 1
	if index < 0 {
		index = 0
	}
	return index + 1, utf8.RuneCountInString(text[starts[index]:position]) + 1
}

func markdownFenceOpener(line string) (markdownFence, bool) {
	indent := markdownIndent(line)
	if indent > 3 || indent >= len(line) {
		return markdownFence{}, false
	}
	marker := line[indent]
	if marker != '`' && marker != '~' {
		return markdownFence{}, false
	}
	length := markdownRunLength(line, indent, marker)
	if length < 3 {
		return markdownFence{}, false
	}
	if marker == '`' && strings.Contains(line[indent+length:], "`") {
		return markdownFence{}, false
	}
	return markdownFence{open: true, marker: marker, length: length}, true
}

func markdownFenceCloser(line string, fence markdownFence) bool {
	indent := markdownIndent(line)
	if indent > 3 || indent >= len(line) || line[indent] != fence.marker {
		return false
	}
	length := markdownRunLength(line, indent, fence.marker)
	return length >= fence.length && strings.TrimSpace(line[indent+length:]) == ""
}

func markdownIndent(line string) int {
	indent := 0
	for indent < len(line) && indent < 4 && line[indent] == ' ' {
		indent++
	}
	return indent
}

func markdownRunLength(text string, start int, marker byte) int {
	length := 0
	for start+length < len(text) && text[start+length] == marker {
		length++
	}
	return length
}

func markdownATXHeading(line string) (string, int, bool) {
	indent := markdownIndent(line)
	if indent > 3 || indent >= len(line) || line[indent] != '#' {
		return "", 0, false
	}
	length := markdownRunLength(line, indent, '#')
	if !markdownATXMarkerValid(line, indent, length) {
		return "", 0, false
	}
	content := markdownATXContent(line[indent+length:])
	return content, indent + 1, true
}

func markdownATXMarkerValid(line string, indent, length int) bool {
	if length > 6 {
		return false
	}
	end := indent + length
	if end == len(line) {
		return true
	}
	return line[end] == ' ' || line[end] == '\t'
}

func markdownATXContent(value string) string {
	content := strings.TrimSpace(value)
	closing := len(content)
	for closing > 0 && content[closing-1] == '#' {
		closing--
	}
	if closing < len(content) && closing > 0 && (content[closing-1] == ' ' || content[closing-1] == '\t') {
		content = strings.TrimSpace(content[:closing])
	}
	return content
}

func markdownSetextUnderline(line string) bool {
	indent := markdownIndent(line)
	if indent > 3 || indent >= len(line) {
		return false
	}
	marker := line[indent]
	if marker != '=' && marker != '-' {
		return false
	}
	length := markdownRunLength(line, indent, marker)
	return length >= 3 && strings.TrimSpace(line[indent+length:]) == ""
}

func markdownFirstColumn(line string) int {
	for index, character := range line {
		if !unicode.IsSpace(character) {
			return utf8.RuneCountInString(line[:index]) + 1
		}
	}
	return 1
}
