package policy

import (
	"regexp"
	"strings"
)

func MatchesAny(path string, patterns []string) bool {
	path = strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "./")
	for _, pattern := range patterns {
		if Match(path, pattern) {
			return true
		}
	}
	return false
}

func Match(path, pattern string) bool {
	path = strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "./")
	pattern = strings.TrimPrefix(strings.ReplaceAll(pattern, "\\", "/"), "./")
	var expression strings.Builder
	expression.WriteString("^")
	for index := 0; index < len(pattern); {
		character := pattern[index]
		switch character {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				index += 2
				if index < len(pattern) && pattern[index] == '/' {
					expression.WriteString("(?:.*/)?")
					index++
				} else {
					expression.WriteString(".*")
				}
			} else {
				expression.WriteString("[^/]*")
				index++
			}
		case '?':
			expression.WriteString("[^/]")
			index++
		default:
			expression.WriteString(regexp.QuoteMeta(string(character)))
			index++
		}
	}
	expression.WriteString("$")
	matched, err := regexp.MatchString(expression.String(), path)
	return err == nil && matched
}

func IsTestPath(path string) bool {
	return MatchesAny(path, DefaultTestPatterns)
}

func PatternsOverlap(left, right string) bool {
	leftTokens := globPatternTokens(left)
	rightTokens := globPatternTokens(right)
	type state struct{ left, right int }
	queued := []state{{}}
	seen := map[state]bool{}
	for len(queued) > 0 {
		current := queued[0]
		queued = queued[1:]
		if seen[current] {
			continue
		}
		seen[current] = true
		leftStates := globEpsilonClosure(leftTokens, current.left)
		rightStates := globEpsilonClosure(rightTokens, current.right)
		for _, leftState := range leftStates {
			for _, rightState := range rightStates {
				if leftState == len(leftTokens) && rightState == len(rightTokens) {
					return true
				}
				for _, leftTransition := range globTransitions(leftTokens, leftState) {
					for _, rightTransition := range globTransitions(rightTokens, rightState) {
						if !globTransitionOverlap(leftTransition, rightTransition) {
							continue
						}
						queued = append(queued, state{leftTransition.next, rightTransition.next})
					}
				}
			}
		}
	}
	return false
}

type globTokenKind int

const (
	globLiteral globTokenKind = iota
	globSingleSegment
	globSegmentStar
	globAnyStar
	globDirectoryStar
	globSlash
)

type globToken struct {
	kind    globTokenKind
	literal byte
}

type globTransition struct {
	kind    globTokenKind
	literal byte
	next    int
}

func globPatternTokens(pattern string) []globToken {
	pattern = strings.TrimPrefix(strings.ReplaceAll(pattern, "\\", "/"), "./")
	tokens := []globToken{}
	for index := 0; index < len(pattern); {
		switch pattern[index] {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				index += 2
				if index < len(pattern) && pattern[index] == '/' {
					tokens = append(tokens, globToken{kind: globDirectoryStar})
					index++
					continue
				}
				tokens = append(tokens, globToken{kind: globAnyStar})
				continue
			}
			tokens = append(tokens, globToken{kind: globSegmentStar})
			index++
		case '?':
			tokens = append(tokens, globToken{kind: globSingleSegment})
			index++
		default:
			tokens = append(tokens, globToken{kind: globLiteral, literal: pattern[index]})
			index++
		}
	}
	return tokens
}

func globEpsilonClosure(tokens []globToken, start int) []int {
	states := []int{start}
	for index := start; index < len(tokens); index++ {
		switch tokens[index].kind {
		case globSegmentStar, globAnyStar, globDirectoryStar:
			states = append(states, index+1)
		default:
			return states
		}
	}
	return states
}

func globTransitions(tokens []globToken, state int) []globTransition {
	if state >= len(tokens) {
		return nil
	}
	token := tokens[state]
	switch token.kind {
	case globLiteral:
		if token.literal == '/' {
			return []globTransition{{kind: globSlash, next: state + 1}}
		}
		return []globTransition{{kind: globLiteral, literal: token.literal, next: state + 1}}
	case globSingleSegment:
		return []globTransition{{kind: globSingleSegment, next: state + 1}}
	case globSegmentStar:
		return []globTransition{{kind: globSingleSegment, next: state}}
	case globAnyStar:
		return []globTransition{{kind: globAnyStar, next: state}}
	case globDirectoryStar:
		return []globTransition{{kind: globAnyStar, next: state}, {kind: globSlash, next: state + 1}}
	default:
		return nil
	}
}

func globTransitionOverlap(left, right globTransition) bool {
	if left.kind == globAnyStar || right.kind == globAnyStar {
		return true
	}
	if left.kind == globSlash || right.kind == globSlash {
		return left.kind == globSlash && right.kind == globSlash
	}
	if left.kind == globLiteral && right.kind == globLiteral {
		return left.literal == right.literal
	}
	return true
}
