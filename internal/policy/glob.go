package policy

import (
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
)

const globPatternCacheCapacity = 4096

var sharedGlobPatterns = globPatternCache{entries: map[string]compiledGlob{}, order: make([]string, 0, globPatternCacheCapacity)}

type compiledGlob struct {
	matcher *regexp.Regexp
	tokens  []globToken
}

type globPatternCache struct {
	mutex   sync.RWMutex
	entries map[string]compiledGlob
	order   []string
	next    int
	hits    atomic.Int64
	misses  atomic.Int64
	builds  atomic.Int64
}

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
	return cachedGlobPattern(pattern).matcher.MatchString(path)
}

func IsTestPath(path string) bool {
	return MatchesAny(path, DefaultTestPatterns)
}

func PatternsOverlap(left, right string) bool {
	leftTokens := cachedGlobPattern(left).tokens
	rightTokens := cachedGlobPattern(right).tokens
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

func GlobCacheStats() (int64, int64, int64) {
	return sharedGlobPatterns.hits.Load(), sharedGlobPatterns.misses.Load(), sharedGlobPatterns.builds.Load()
}

func cachedGlobPattern(pattern string) compiledGlob {
	pattern = strings.TrimPrefix(strings.ReplaceAll(pattern, "\\", "/"), "./")
	sharedGlobPatterns.mutex.RLock()
	compiled, found := sharedGlobPatterns.entries[pattern]
	sharedGlobPatterns.mutex.RUnlock()
	if found {
		sharedGlobPatterns.hits.Add(1)
		return compiled
	}
	sharedGlobPatterns.misses.Add(1)
	compiled = compileGlob(pattern)
	sharedGlobPatterns.mutex.Lock()
	defer sharedGlobPatterns.mutex.Unlock()
	if existing, present := sharedGlobPatterns.entries[pattern]; present {
		sharedGlobPatterns.hits.Add(1)
		return existing
	}
	sharedGlobPatterns.builds.Add(1)
	if len(sharedGlobPatterns.order) < globPatternCacheCapacity {
		sharedGlobPatterns.order = append(sharedGlobPatterns.order, pattern)
	} else {
		delete(sharedGlobPatterns.entries, sharedGlobPatterns.order[sharedGlobPatterns.next])
		sharedGlobPatterns.order[sharedGlobPatterns.next] = pattern
		sharedGlobPatterns.next = (sharedGlobPatterns.next + 1) % globPatternCacheCapacity
	}
	sharedGlobPatterns.entries[pattern] = compiled
	return compiled
}

func compileGlob(pattern string) compiledGlob {
	var expression strings.Builder
	expression.WriteByte('^')
	characters := []rune(pattern)
	for index := 0; index < len(characters); {
		switch characters[index] {
		case '*':
			if index+1 < len(characters) && characters[index+1] == '*' {
				index += 2
				if index < len(characters) && characters[index] == '/' {
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
			expression.WriteString(regexp.QuoteMeta(string(characters[index])))
			index++
		}
	}
	expression.WriteByte('$')
	return compiledGlob{matcher: regexp.MustCompile(expression.String()), tokens: globPatternTokens(pattern)}
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
	literal rune
}

type globTransition struct {
	kind    globTokenKind
	literal rune
	next    int
}

func globPatternTokens(pattern string) []globToken {
	characters := []rune(pattern)
	tokens := []globToken{}
	for index := 0; index < len(characters); {
		switch characters[index] {
		case '*':
			if index+1 < len(characters) && characters[index+1] == '*' {
				index += 2
				if index < len(characters) && characters[index] == '/' {
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
			tokens = append(tokens, globToken{kind: globLiteral, literal: characters[index]})
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
