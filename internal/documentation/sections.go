package versioneddocs

import (
	"fmt"
	"strings"
)

type documentHeading struct {
	line  int
	level int
	name  string
	path  string
}

func (document Document) Section(name string) (Document, error) {
	lines := strings.SplitAfter(string(document.Content), "\n")
	headings := documentHeadings(lines)
	matches := []int{}
	for index, heading := range headings {
		if heading.name == name || heading.path == name {
			matches = append(matches, index)
		}
	}
	if len(matches) != 1 {
		return Document{}, document.sectionError(name, headings, matches)
	}
	index := matches[0]
	end := len(lines)
	for _, heading := range headings[index+1:] {
		if heading.level <= headings[index].level {
			end = heading.line
			break
		}
	}
	document.Content = []byte(strings.Join(lines[headings[index].line:end], ""))
	return document, nil
}

func (document Document) sectionError(name string, headings []documentHeading, matches []int) error {
	reason := "missing"
	choices := headings
	if len(matches) > 1 {
		reason, choices = "ambiguous", []documentHeading{}
		for _, index := range matches {
			choices = append(choices, headings[index])
		}
	}
	commands := []string{}
	for _, heading := range choices[:min(5, len(choices))] {
		commands = append(commands, fmt.Sprintf("./code-polishyw docs read %s --section '%s'", document.Topic.ID, strings.ReplaceAll(heading.path, "'", "'\\''")))
	}
	commands = append(commands, "./code-polishyw docs read "+document.Topic.ID)
	return failure(ErrorInvalidQuery, name, fmt.Errorf("%s heading %q in %s; available reads: %s", reason, name, document.Topic.ID, strings.Join(commands, "; ")))
}

func documentHeadings(lines []string) []documentHeading {
	headings, parents := []documentHeading{}, []documentHeading{}
	fence := ""
	for index, line := range lines {
		line = strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) > 3 {
			continue
		}
		nextFence, fenced := documentFence(trimmed, fence)
		fence = nextFence
		if fenced {
			continue
		}
		level, name, valid := parseDocumentHeading(trimmed)
		if !valid {
			continue
		}
		for len(parents) > 0 && parents[len(parents)-1].level >= level {
			parents = parents[:len(parents)-1]
		}
		path := name
		if len(parents) > 0 {
			path = parents[len(parents)-1].path + " > " + name
		}
		heading := documentHeading{line: index, level: level, name: name, path: path}
		headings, parents = append(headings, heading), append(parents, heading)
	}
	return headings
}

func parseDocumentHeading(trimmed string) (int, string, bool) {
	level := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
	if level < 1 || level > 6 || len(trimmed) > level && trimmed[level] != ' ' && trimmed[level] != '\t' {
		return 0, "", false
	}
	name := strings.TrimSpace(trimmed[level:])
	withoutClosing := strings.TrimRight(name, "#")
	if withoutClosing != name && (withoutClosing == "" || strings.HasSuffix(withoutClosing, " ") || strings.HasSuffix(withoutClosing, "\t")) {
		name = strings.TrimSpace(withoutClosing)
	}
	return level, name, true
}

func documentFence(line, fence string) (string, bool) {
	if fence != "" {
		if strings.HasPrefix(line, fence) && strings.Trim(line, string(fence[0])+" \t") == "" {
			fence = ""
		}
		return fence, true
	}
	if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
		return line[:len(line)-len(strings.TrimLeft(line, string(line[0])))], true
	}
	return "", false
}
