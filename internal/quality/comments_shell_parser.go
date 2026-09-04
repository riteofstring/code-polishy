package quality

import (
	"bytes"
	"errors"
	"sort"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"
)

const maximumShellSourceBytes = 4 * 1024 * 1024

func scanShellSource(data []byte) ([]sourceAnnotation, error) {
	if len(data) > maximumShellSourceBytes || !utf8.Valid(data) {
		return nil, errors.New("shell source exceeds the parser byte or encoding limit")
	}
	parsed, err := syntax.NewParser(syntax.KeepComments(true), syntax.Variant(syntax.LangBash)).Parse(bytes.NewReader(data), "")
	if err != nil {
		return nil, err
	}
	comments := []sourceAnnotation{}
	sourceCalls := map[int]bool{}
	syntax.Walk(parsed, func(node syntax.Node) bool {
		switch typed := node.(type) {
		case *syntax.Comment:
			comment := typed
			comments = append(comments, sourceAnnotation{offset: int(comment.Hash.Offset()), text: "#" + comment.Text})
		case *syntax.CallExpr:
			if len(typed.Assigns) == 0 && len(typed.Args) >= 2 {
				command := typed.Args[0].Lit()
				if command == "source" || command == "." {
					sourceCalls[int(typed.Pos().Offset())] = true
				}
			}
		}
		return true
	})
	for index := range comments {
		next := nextLine(data, lineEnd(data, comments[index].offset))
		next = shellSkipHorizontal(data, next)
		comments[index].shellSourceFollows = sourceCalls[next]
	}
	sort.Slice(comments, func(left, right int) bool { return comments[left].offset < comments[right].offset })
	return comments, nil
}
