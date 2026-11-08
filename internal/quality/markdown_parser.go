package quality

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldmarktext "github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

func parseMarkdownDocument(path, text string, renderer markdownRenderer) markdownDocument {
	document := markdownDocument{path: path, text: text, headings: map[string]markdownHeading{}}
	source := []byte(text)
	options := []goldmark.Option{goldmark.WithExtensions(extension.GFM)}
	if renderer.attributes {
		options = append(options, goldmark.WithParserOptions(parser.WithAttribute()))
	}
	root := goldmark.New(options...).Parser().Parse(goldmarktext.NewReader(source))
	duplicates := map[string]int{}
	headings := []*ast.Heading{}
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch typed := node.(type) {
		case *ast.Heading:
			headings = append(headings, typed)
			if id := markdownHeadingID(typed); id != "" {
				line, column := markdownASTPosition(source, typed.Pos(), false)
				document.headings[id] = markdownHeading{line: line, column: column}
			}
		case *ast.Link:
			line, column := markdownASTPosition(source, typed.Pos(), false)
			document.links = append(document.links, markdownLink{destination: string(typed.Destination), line: line, column: column})
		case *ast.Image:
			line, column := markdownASTPosition(source, typed.Pos(), true)
			document.links = append(document.links, markdownLink{destination: string(typed.Destination), image: true, line: line, column: column})
		}
		return ast.WalkContinue, nil
	})
	for _, heading := range headings {
		if markdownHeadingID(heading) != "" {
			continue
		}
		line, column := markdownASTPosition(source, heading.Pos(), false)
		document.addHeading(markdownNodeText(heading, source, renderer.mkdocs), line, column, duplicates, renderer)
	}
	return document
}

func markdownHeadingID(node *ast.Heading) string {
	value, _ := node.AttributeString("id")
	id, _ := value.([]byte)
	return string(id)
}

func markdownNodeText(node ast.Node, source []byte, mkdocs bool) string {
	var result strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch typed := child.(type) {
		case *ast.Text:
			result.Write(markdownTextContent(typed.Value(source), !mkdocs || node.Kind() == ast.KindCodeSpan))
			if typed.SoftLineBreak() {
				result.WriteByte('\n')
			}
		case *ast.String:
			result.Write(typed.Value)
		case *ast.AutoLink:
			result.Write(typed.Label(source))
		case *ast.RawHTML:
			if !mkdocs {
				result.Write(typed.Segments.Value(source))
			}
		case *ast.Image:
			if !mkdocs {
				result.WriteString(markdownNodeText(child, source, mkdocs))
			}
		default:
			result.WriteString(markdownNodeText(child, source, mkdocs))
		}
	}
	return result.String()
}

func markdownTextContent(value []byte, literal bool) []byte {
	if literal {
		return value
	}
	return util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(value)))
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
