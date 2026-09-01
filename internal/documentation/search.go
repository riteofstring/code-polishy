package versioneddocs

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	MaxSearchResults = 20
	MaxExcerptRunes  = 180
)

type SearchResult struct {
	Topic   Topic
	Excerpt string
}

type rankedSearchResult struct {
	result SearchResult
	rank   int
}

func (library *Library) Find(query string) ([]SearchResult, error) {
	terms := normalizedTerms(query)
	if len(terms) == 0 {
		return nil, failure(ErrorInvalidQuery, query, errors.New("provide at least one search term"))
	}
	normalizedQuery := strings.Join(terms, " ")
	ranked := make([]rankedSearchResult, 0, len(library.topics))
	for _, topic := range library.topics {
		if !topic.Public {
			continue
		}
		document, err := library.Read(topic.ID)
		if err != nil {
			return nil, err
		}
		match, found := matchDocument(document, terms, normalizedQuery)
		if found {
			ranked = append(ranked, match)
		}
	}
	sort.Slice(ranked, func(left, right int) bool {
		if ranked[left].rank != ranked[right].rank {
			return ranked[left].rank < ranked[right].rank
		}
		return ranked[left].result.Topic.ID < ranked[right].result.Topic.ID
	})
	if len(ranked) > MaxSearchResults {
		ranked = ranked[:MaxSearchResults]
	}
	result := make([]SearchResult, len(ranked))
	for index, match := range ranked {
		result[index] = match.result
	}
	return result, nil
}

func matchDocument(document Document, terms []string, normalizedQuery string) (rankedSearchResult, bool) {
	topic := document.Topic
	metadata := strings.ToLower(strings.Join(append([]string{topic.ID, topic.Title, topic.Summary}, topic.Aliases...), "\n"))
	headings, bodyLines := documentParts(string(document.Content))
	headingText := strings.ToLower(strings.Join(headings, "\n"))
	bodyText := strings.ToLower(strings.Join(bodyLines, "\n"))
	if !containsEvery(strings.Join([]string{metadata, headingText, bodyText}, "\n"), terms) {
		return rankedSearchResult{}, false
	}
	rank := 2
	excerpt := bestLine(bodyLines, terms)
	if strings.EqualFold(normalizedQuery, topic.ID) || strings.EqualFold(normalizedQuery, normalizedSpace(topic.Title)) {
		rank = 0
		excerpt = topic.Summary
	} else if containsEvery(metadata, terms) {
		rank = 1
		excerpt = topic.Summary
	} else if containsEvery(headingText, terms) {
		rank = 1
		excerpt = bestLine(headings, terms)
	}
	if excerpt == "" {
		excerpt = topic.Summary
	}
	return rankedSearchResult{
		result: SearchResult{Topic: cloneTopic(topic), Excerpt: boundedExcerpt(excerpt)}, rank: rank,
	}, true
}

func normalizedTerms(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	result := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		if seen[field] {
			continue
		}
		seen[field] = true
		result = append(result, field)
	}
	return result
}

func normalizedSpace(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}

func documentParts(document string) ([]string, []string) {
	headings := []string{}
	body := []string{}
	for _, line := range strings.Split(document, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			headings = append(headings, strings.TrimSpace(strings.TrimLeft(trimmed, "#")))
			continue
		}
		body = append(body, trimmed)
	}
	return headings, body
}

func containsEvery(value string, terms []string) bool {
	for _, term := range terms {
		if !strings.Contains(value, term) {
			return false
		}
	}
	return true
}

func bestLine(lines, terms []string) string {
	best, bestMatches := "", 0
	for _, line := range lines {
		lower := strings.ToLower(line)
		matches := 0
		for _, term := range terms {
			if strings.Contains(lower, term) {
				matches++
			}
		}
		if matches > bestMatches {
			best, bestMatches = line, matches
		}
	}
	return best
}

func boundedExcerpt(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) <= MaxExcerptRunes {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:MaxExcerptRunes-1])) + "…"
}
