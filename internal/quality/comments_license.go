package quality

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/spdx"
)

const maximumCopyrightNoticeBytes = 256

var copyrightNoticePrefix = regexp.MustCompile(`^(?:Copyright |© |\([Cc]\) |[0-9]{4}[ ,-])`)

func licenseNoticeComment(language, text string) bool {
	text = strings.TrimSuffix(text, "\r")
	var body string
	var found bool
	switch language {
	case "go", "javascript", "typescript", "rust":
		body, found = strings.CutPrefix(text, "// ")
	case "python", "shell", "powershell":
		body, found = strings.CutPrefix(text, "# ")
	case "css":
		body, found = cutDelimited(text, "/* ", " */")
	case "html":
		body, found = cutDelimited(text, "<!-- ", " -->")
	}
	return found && licenseNotice(body)
}

func cutDelimited(text, prefix, suffix string) (string, bool) {
	body, found := strings.CutPrefix(text, prefix)
	if !found {
		return "", false
	}
	return strings.CutSuffix(body, suffix)
}

func licenseNotice(body string) bool {
	tag, value, found := strings.Cut(body, ": ")
	if !found || value == "" || strings.TrimSpace(value) != value || !utf8.ValidString(value) ||
		strings.ContainsFunc(value, unicode.IsControl) {
		return false
	}
	switch tag {
	case "SPDX-License-Identifier":
		_, err := spdx.Parse(value)
		return err == nil
	case "SPDX-FileCopyrightText":
		return len(value) <= maximumCopyrightNoticeBytes && copyrightNoticePrefix.MatchString(value)
	}
	return false
}

func sourceHeaderEnd(data []byte, annotations []sourceAnnotation) int {
	ordered := append([]sourceAnnotation{}, annotations...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].offset < ordered[right].offset })
	index := 0
	for _, annotation := range ordered {
		index = skipSourceWhitespace(data, index)
		if annotation.offset != index || annotation.text == "" {
			return index
		}
		index += len(annotation.text)
	}
	return skipSourceWhitespace(data, index)
}

func skipSourceWhitespace(data []byte, index int) int {
	for index < len(data) && strings.ContainsRune(" \t\f\r\n", rune(data[index])) {
		index++
	}
	return index
}

func licenseNoticeAllowed(language string, data []byte, headerEnd int, annotation sourceAnnotation) bool {
	return annotation.offset < headerEnd && sourceLineStart(data, annotation.offset) && licenseNoticeComment(language, annotation.text)
}
