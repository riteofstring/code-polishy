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
