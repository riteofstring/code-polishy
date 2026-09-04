package quality

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	goldmarktext "github.com/yuin/goldmark/text"
)

func parseMarkdownDocument(path, text string) markdownDocument {
	document := markdownDocument{path: path, text: text, headings: map[string]markdownHeading{}}
	source := []byte(text)
	root := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(goldmarktext.NewReader(source))
	duplicates := map[string]int{}
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch typed := node.(type) {
		case *ast.Heading:
			line, column := markdownASTPosition(source, typed.Pos(), false)
			document.addHeading(markdownNodeText(typed, source), line, column, duplicates)
		case *ast.Link:
			line, column := markdownASTPosition(source, typed.Pos(), false)
			document.links = append(document.links, markdownLink{destination: string(typed.Destination), line: line, column: column})
		case *ast.Image:
			line, column := markdownASTPosition(source, typed.Pos(), true)
			document.links = append(document.links, markdownLink{destination: string(typed.Destination), image: true, line: line, column: column})
		}
		return ast.WalkContinue, nil
	})
	return document
}

func markdownNodeText(node ast.Node, source []byte) string {
	var result strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch typed := child.(type) {
		case *ast.Text:
			result.Write(typed.Value(source))
			if typed.SoftLineBreak() {
				result.WriteByte('\n')
			}
		case *ast.String:
			result.Write(typed.Value)
		case *ast.AutoLink:
			result.Write(typed.Label(source))
		case *ast.RawHTML:
			result.Write(typed.Segments.Value(source))
		default:
			result.WriteString(markdownNodeText(child, source))
		}
	}
	return result.String()
}

func markdownASTPosition(source []byte, offset int, image bool) (int, int) {
	if image && offset > 0 && source[offset-1] == '!' {
		offset--
	}
	if offset < 0 || offset > len(source) {
		offset = 0
	}
	line := bytes.Count(source[:offset], []byte{'\n'}) + 1
	lineStart := bytes.LastIndexByte(source[:offset], '\n') + 1
	return line, utf8.RuneCount(source[lineStart:offset]) + 1
}
