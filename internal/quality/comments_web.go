package quality

import (
	"fmt"
	"strings"
)

func scanCSSSource(data []byte) ([]sourceAnnotation, error) {
	annotations := []sourceAnnotation{}
	for index := 0; index < len(data); {
		next, annotation, err := scanCSSItem(data, index)
		if err != nil {
			return nil, err
		}
		if annotation != nil {
			annotations = append(annotations, *annotation)
		}
		index = next
	}
	return annotations, nil
}

func scanCSSItem(data []byte, index int) (int, *sourceAnnotation, error) {
	if cssQuote(data[index]) {
		end, err := scanCSSString(data, index)
		return end, nil, err
	}
	if data[index] == '\\' {
		return scanCSSEscape(data, index)
	}
	if data[index] == '/' {
		return scanCSSComment(data, index)
	}
	return index + 1, nil, nil
}

func scanCSSEscape(data []byte, index int) (int, *sourceAnnotation, error) {
	if index+1 >= len(data) {
		return 0, nil, fmt.Errorf("unterminated CSS escape")
	}
	return index + 2, nil, nil
}

func scanCSSComment(data []byte, index int) (int, *sourceAnnotation, error) {
	if index+1 >= len(data) || data[index+1] != '*' {
		return index + 1, nil, nil
	}
	end := index + 2
	for end+1 < len(data) && (data[end] != '*' || data[end+1] != '/') {
		end++
	}
	if end+1 >= len(data) {
		return 0, nil, fmt.Errorf("unterminated CSS comment")
	}
	annotation := sourceAnnotation{offset: index, text: string(data[index : end+2])}
	return end + 2, &annotation, nil
}

func cssQuote(value byte) bool {
	return value == '\'' || value == '"'
}

func scanCSSString(data []byte, index int) (int, error) {
	quote := data[index]
	index++
	for index < len(data) {
		if data[index] == '\\' {
			if index+1 >= len(data) {
				return 0, fmt.Errorf("unterminated CSS string")
			}
			index += 2
			continue
		}
		if data[index] == quote {
			return index + 1, nil
		}
		if data[index] == '\r' || data[index] == '\n' {
			return 0, fmt.Errorf("unterminated CSS string")
		}
		index++
	}
	return 0, fmt.Errorf("unterminated CSS string")
}

func scanHTMLSource(data []byte) ([]sourceAnnotation, error) {
	annotations := []sourceAnnotation{}
	for index := 0; index < len(data); {
		step, err := scanHTMLSourceStep(data, index)
		if err != nil {
			return nil, err
		}
		if step.annotation != nil {
			annotations = append(annotations, *step.annotation)
		}
		if step.done {
			return annotations, nil
		}
		index = step.next
	}
	return annotations, nil
}

type htmlSourceStep struct {
	next       int
	annotation *sourceAnnotation
	done       bool
}

func scanHTMLSourceStep(data []byte, index int) (htmlSourceStep, error) {
	if htmlPrefix(data, index, "<!--") {
		return scanHTMLCommentStep(data, index)
	}
	if htmlBogusCommentStart(data, index) {
		return scanHTMLBogusCommentStep(data, index)
	}
	if data[index] != '<' {
		return htmlSourceStep{next: index + 1}, nil
	}
	return scanHTMLTagStep(data, index)
}

func scanHTMLCommentStep(data []byte, index int) (htmlSourceStep, error) {
	end, err := scanHTMLComment(data, index)
	if err != nil {
		return htmlSourceStep{}, err
	}
	annotation := sourceAnnotation{offset: index, text: string(data[index:end])}
	return htmlSourceStep{next: end, annotation: &annotation}, nil
}

func scanHTMLBogusCommentStep(data []byte, index int) (htmlSourceStep, error) {
	end, err := scanHTMLBogusComment(data, index)
	if err != nil {
		return htmlSourceStep{}, err
	}
	annotation := sourceAnnotation{offset: index, text: string(data[index:end])}
	return htmlSourceStep{next: end, annotation: &annotation}, nil
}

func scanHTMLTagStep(data []byte, index int) (htmlSourceStep, error) {
	end, name, closing, recognized, err := scanHTMLTag(data, index)
	if err != nil {
		return htmlSourceStep{}, err
	}
	if !recognized {
		return htmlSourceStep{next: index + 1}, nil
	}
	if closing || !htmlRawElement(name) {
		return htmlSourceStep{next: end}, nil
	}
	return scanHTMLRawTagStep(data, end, name)
}

func scanHTMLRawTagStep(data []byte, index int, name string) (htmlSourceStep, error) {
	bodyEnd, after, found, err := scanHTMLRawElement(data, index, name)
	if err != nil {
		return htmlSourceStep{}, err
	}
	if !found {
		if name == "plaintext" {
			return htmlSourceStep{done: true}, nil
		}
		return htmlSourceStep{}, fmt.Errorf("unterminated HTML %s element", name)
	}
	if htmlEmbeddedSource(data[index:bodyEnd], name) {
		return htmlSourceStep{}, fmt.Errorf("embedded HTML %s content requires its owning source scanner", name)
	}
	return htmlSourceStep{next: after}, nil
}

func htmlEmbeddedSource(data []byte, name string) bool {
	if name != "script" && name != "style" {
		return false
	}
	return strings.TrimSpace(string(data)) != ""
}

func htmlBogusCommentStart(data []byte, index int) bool {
	if index+1 >= len(data) || data[index] != '<' {
		return false
	}
	if data[index+1] == '?' {
		return true
	}
	return data[index+1] == '!' && !htmlDoctypeStart(data, index)
}

func htmlDoctypeStart(data []byte, index int) bool {
	const prefix = "<!doctype"
	if len(data)-index <= len(prefix) || !strings.EqualFold(string(data[index:index+len(prefix)]), prefix) {
		return false
	}
	return data[index+len(prefix)] == ' ' || data[index+len(prefix)] == '\t' || data[index+len(prefix)] == '\r' || data[index+len(prefix)] == '\n' || data[index+len(prefix)] == '\f'
}

func scanHTMLBogusComment(data []byte, index int) (int, error) {
	for index < len(data) && data[index] != '>' {
		index++
	}
	if index == len(data) {
		return 0, fmt.Errorf("unterminated HTML bogus comment")
	}
	return index + 1, nil
}

func htmlPrefix(data []byte, index int, prefix string) bool {
	return len(data)-index >= len(prefix) && string(data[index:index+len(prefix)]) == prefix
}

func scanHTMLComment(data []byte, index int) (int, error) {
	index += len("<!--")
	for index < len(data) {
		if index+2 < len(data) && data[index] == '-' && data[index+1] == '-' && data[index+2] == '>' {
			return index + 3, nil
		}
		if index+1 < len(data) && data[index] == '-' && data[index+1] == '-' {
			return 0, fmt.Errorf("malformed HTML comment")
		}
		index++
	}
	return 0, fmt.Errorf("unterminated HTML comment")
}

func scanHTMLTag(data []byte, index int) (int, string, bool, bool, error) {
	if index+1 >= len(data) {
		return index, "", false, false, nil
	}
	if data[index+1] == '!' && htmlDoctypeStart(data, index) {
		end, err := scanHTMLTagEnd(data, index+2)
		return end, "doctype", false, true, err
	}
	start := index + 1
	closing := false
	if data[start] == '/' {
		closing = true
		start++
	}
	if start >= len(data) || !htmlNameStart(data[start]) {
		return index, "", false, false, nil
	}
	endName := start + 1
	for endName < len(data) && htmlNameContinue(data[endName]) {
		endName++
	}
	end, err := scanHTMLTagEnd(data, endName)
	if err != nil {
		return 0, "", false, true, err
	}
	return end, strings.ToLower(string(data[start:endName])), closing, true, nil
}

func scanHTMLTagEnd(data []byte, index int) (int, error) {
	for index < len(data) {
		switch data[index] {
		case '\'', '"':
			quote := data[index]
			index++
			for index < len(data) && data[index] != quote {
				index++
			}
			if index == len(data) {
				return 0, fmt.Errorf("unterminated HTML quoted attribute")
			}
			index++
		case '>':
			return index + 1, nil
		default:
			index++
		}
	}
	return 0, fmt.Errorf("unterminated HTML tag")
}

func htmlNameStart(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func htmlNameContinue(value byte) bool {
	return htmlNameStart(value) || value >= '0' && value <= '9' || value == ':' || value == '-' || value == '_'
}

func htmlRawElement(name string) bool {
	switch name {
	case "script", "style", "textarea", "title", "xmp", "iframe", "noembed", "noframes", "plaintext":
		return true
	}
	return false
}

func scanHTMLRawElement(data []byte, index int, name string) (int, int, bool, error) {
	if name == "plaintext" {
		return len(data), len(data), false, nil
	}
	for candidate := index; candidate < len(data); candidate++ {
		if data[candidate] != '<' || candidate+2 >= len(data) || data[candidate+1] != '/' {
			continue
		}
		end, closingName, closing, recognized, err := scanHTMLTag(data, candidate)
		if err != nil {
			return 0, 0, false, err
		}
		if recognized && closing && closingName == name {
			return candidate, end, true, nil
		}
	}
	return len(data), len(data), false, nil
}

func scanPowerShellSource(data []byte) ([]sourceAnnotation, error) {
	annotations := []sourceAnnotation{}
	for index := 0; index < len(data); {
		step, err := scanPowerShellContent(data, index)
		if err != nil {
			return nil, err
		}
		if !step.handled {
			index++
			continue
		}
		annotations = append(annotations, step.annotations...)
		index = step.next
	}
	return annotations, nil
}

type powerShellContentStep struct {
	next        int
	annotations []sourceAnnotation
	handled     bool
}

func scanPowerShellContent(data []byte, index int) (powerShellContentStep, error) {
	if powerShellHereHeader(data, index) {
		return scanPowerShellHereStringStep(data, index)
	}
	if powerShellQuote(data[index]) {
		return scanPowerShellStringStep(data, index)
	}
	if data[index] == '<' {
		return scanPowerShellBlockCommentStep(data, index)
	}
	if data[index] == '#' {
		return scanPowerShellLineCommentStep(data, index), nil
	}
	if data[index] == '`' {
		return scanPowerShellEscapeStep(data, index)
	}
	return powerShellContentStep{}, nil
}

func scanPowerShellHereStringStep(data []byte, index int) (powerShellContentStep, error) {
	end, annotations, err := scanPowerShellHereString(data, index)
	if err != nil {
		return powerShellContentStep{}, err
	}
	return powerShellContentStep{next: end, annotations: annotations, handled: true}, nil
}

func powerShellQuote(value byte) bool {
	return value == '\'' || value == '"'
}

func scanPowerShellStringStep(data []byte, index int) (powerShellContentStep, error) {
	end, annotations, err := scanPowerShellString(data, index)
	if err != nil {
		return powerShellContentStep{}, err
	}
	return powerShellContentStep{next: end, annotations: annotations, handled: true}, nil
}

func scanPowerShellBlockCommentStep(data []byte, index int) (powerShellContentStep, error) {
	if index+1 >= len(data) || data[index+1] != '#' {
		return powerShellContentStep{}, nil
	}
	end, err := scanPowerShellBlockComment(data, index)
	if err != nil {
		return powerShellContentStep{}, err
	}
	annotation := sourceAnnotation{offset: index, text: string(data[index:end])}
	return powerShellContentStep{next: end, annotations: []sourceAnnotation{annotation}, handled: true}, nil
}

func scanPowerShellLineCommentStep(data []byte, index int) powerShellContentStep {
	end := lineEnd(data, index)
	annotation := sourceAnnotation{offset: index, text: string(data[index:end])}
	return powerShellContentStep{next: end, annotations: []sourceAnnotation{annotation}, handled: true}
}

func scanPowerShellEscapeStep(data []byte, index int) (powerShellContentStep, error) {
	if index+1 >= len(data) {
		return powerShellContentStep{}, fmt.Errorf("unterminated PowerShell escape")
	}
	return powerShellContentStep{next: index + 2, handled: true}, nil
}

func powerShellHereHeader(data []byte, index int) bool {
	if index+1 >= len(data) || data[index] != '@' || (data[index+1] != '\'' && data[index+1] != '"') {
		return false
	}
	return index == 0 || powerShellHereBoundary(data[index-1])
}

func powerShellHereBoundary(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n' || strings.ContainsRune("=([{,;|&:", rune(value))
}

func scanPowerShellHereString(data []byte, index int) (int, []sourceAnnotation, error) {
	quote, bodyStart, err := powerShellHereStringStart(data, index)
	if err != nil {
		return 0, nil, err
	}
	end, err := findPowerShellHereStringEnd(data, bodyStart, quote)
	if err != nil {
		return 0, nil, err
	}
	annotations, err := powerShellHereStringAnnotations(data, bodyStart, end, quote)
	if err != nil {
		return 0, nil, err
	}
	return nextLine(data, end+2), annotations, nil
}

func powerShellHereStringStart(data []byte, index int) (byte, int, error) {
	if index+2 >= len(data) || (data[index+2] != '\r' && data[index+2] != '\n') {
		return 0, 0, fmt.Errorf("malformed PowerShell here-string header")
	}
	return data[index+1], nextLine(data, index+2), nil
}

func findPowerShellHereStringEnd(data []byte, index int, quote byte) (int, error) {
	for index < len(data) {
		if powerShellHereStringEnd(data, index, quote) {
			return index, nil
		}
		index = nextLine(data, lineEnd(data, index))
	}
	return 0, fmt.Errorf("unterminated PowerShell here-string")
}

func powerShellHereStringEnd(data []byte, index int, quote byte) bool {
	if !sourceLineStart(data, index) || index+1 >= len(data) {
		return false
	}
	if data[index] != quote || data[index+1] != '@' {
		return false
	}
	end := index + 2
	return end == len(data) || data[end] == '\r' || data[end] == '\n'
}

func powerShellHereStringAnnotations(data []byte, start int, end int, quote byte) ([]sourceAnnotation, error) {
	if quote != '"' {
		return nil, nil
	}
	annotations, err := scanPowerShellInterpolations(data[start:end])
	if err != nil {
		return nil, err
	}
	return offsetSourceAnnotations(annotations, start), nil
}

func scanPowerShellString(data []byte, index int) (int, []sourceAnnotation, error) {
	quote := data[index]
	start := index + 1
	end, err := findPowerShellStringEnd(data, start, quote)
	if err != nil {
		return 0, nil, err
	}
	annotations, err := powerShellStringAnnotations(data, start, end, quote)
	if err != nil {
		return 0, nil, err
	}
	return end + 1, annotations, nil
}

func findPowerShellStringEnd(data []byte, index int, quote byte) (int, error) {
	for index < len(data) {
		if powerShellDoubledSingleQuote(data, index, quote) {
			index += 2
			continue
		}
		if powerShellStringEscape(data, index, quote) {
			if index+1 >= len(data) {
				return 0, fmt.Errorf("unterminated PowerShell string")
			}
			index += 2
			continue
		}
		if data[index] == quote {
			return index, nil
		}
		index++
	}
	return 0, fmt.Errorf("unterminated PowerShell string")
}

func powerShellDoubledSingleQuote(data []byte, index int, quote byte) bool {
	return quote == '\'' && index+1 < len(data) && data[index] == '\'' && data[index+1] == '\''
}

func powerShellStringEscape(data []byte, index int, quote byte) bool {
	return quote == '"' && data[index] == '`'
}

func powerShellStringAnnotations(data []byte, start int, end int, quote byte) ([]sourceAnnotation, error) {
	if quote != '"' {
		return nil, nil
	}
	annotations, err := scanPowerShellInterpolations(data[start:end])
	if err != nil {
		return nil, err
	}
	return offsetSourceAnnotations(annotations, start), nil
}

func scanPowerShellInterpolations(data []byte) ([]sourceAnnotation, error) {
	annotations := []sourceAnnotation{}
	for index := 0; index < len(data); {
		if data[index] == '`' && index+1 < len(data) {
			index += 2
			continue
		}
		if data[index] == '$' && index+1 < len(data) && data[index+1] == '(' {
			end, nested, err := scanPowerShellSubexpression(data, index+2)
			if err != nil {
				return nil, err
			}
			annotations = append(annotations, nested...)
			index = end
			continue
		}
		index++
	}
	return annotations, nil
}

func scanPowerShellSubexpression(data []byte, index int) (int, []sourceAnnotation, error) {
	annotations := []sourceAnnotation{}
	depth := 1
	for index < len(data) {
		step, err := scanPowerShellContent(data, index)
		if err != nil {
			return 0, nil, err
		}
		if step.handled {
			annotations = append(annotations, step.annotations...)
			index = step.next
			continue
		}
		switch data[index] {
		case '(':
			depth++
			index++
		case ')':
			depth--
			index++
			if depth == 0 {
				return index, annotations, nil
			}
		default:
			index++
		}
	}
	return 0, nil, fmt.Errorf("unterminated PowerShell subexpression")
}

func offsetSourceAnnotations(annotations []sourceAnnotation, offset int) []sourceAnnotation {
	for index := range annotations {
		annotations[index].offset += offset
	}
	return annotations
}

func scanPowerShellBlockComment(data []byte, index int) (int, error) {
	depth := 1
	index += 2
	for index+1 < len(data) {
		if data[index] == '<' && data[index+1] == '#' {
			depth++
			index += 2
			continue
		}
		if data[index] == '#' && data[index+1] == '>' {
			depth--
			index += 2
			if depth == 0 {
				return index, nil
			}
			continue
		}
		index++
	}
	return 0, fmt.Errorf("unterminated PowerShell block comment")
}
