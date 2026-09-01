package versioneddocs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	CatalogPath      = "docs/catalog.json"
	CatalogVersion   = 1
	MaxCatalogBytes  = 256 * 1024
	MaxDocumentBytes = 1024 * 1024
)

var identifierPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Topic struct {
	ID      string   `json:"id"`
	Path    string   `json:"path"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Aliases []string `json:"aliases"`
	Public  bool     `json:"public"`
}

type catalog struct {
	Version int     `json:"version"`
	Topics  []Topic `json:"topics"`
}

type Library struct {
	root   string
	topics []Topic
	byName map[string][]int
	byPath map[string]int
}

type Document struct {
	Topic   Topic
	Content []byte
}

func Open(root string) (*Library, error) {
	resolvedRoot, err := resolveRoot(root)
	if err != nil {
		return nil, failure(ErrorUnavailable, root, err)
	}
	data, err := readRegularFile(resolvedRoot, CatalogPath, MaxCatalogBytes)
	if err != nil {
		return nil, failure(ErrorUnavailable, CatalogPath, err)
	}
	parsed, err := decodeCatalog(data)
	if err != nil {
		return nil, failure(ErrorInvalidCatalog, CatalogPath, err)
	}
	library, err := buildLibrary(resolvedRoot, parsed)
	if err != nil {
		return nil, failure(ErrorInvalidCatalog, CatalogPath, err)
	}
	return library, nil
}

func (library *Library) List() []Topic {
	result := make([]Topic, 0, len(library.topics))
	for _, topic := range library.topics {
		if topic.Public {
			result = append(result, cloneTopic(topic))
		}
	}
	return result
}

func (library *Library) Read(name string) (Document, error) {
	index, err := library.resolve(name)
	if err != nil {
		return Document{}, err
	}
	topic := library.topics[index]
	data, err := readRegularFile(library.root, topic.Path, MaxDocumentBytes)
	if err != nil {
		return Document{}, failure(ErrorUnavailable, topic.ID, err)
	}
	if !utf8.Valid(data) {
		return Document{}, failure(ErrorUnavailable, topic.ID, errors.New("document is not valid UTF-8"))
	}
	return Document{Topic: cloneTopic(topic), Content: data}, nil
}

func (library *Library) resolve(name string) (int, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if !identifierPattern.MatchString(normalized) {
		return 0, failure(ErrorUnknown, name, errors.New("use a topic identifier from `code-polishy docs list`"))
	}
	matches := library.byName[normalized]
	switch len(matches) {
	case 0:
		return 0, &Error{Kind: ErrorUnknown, Subject: name, Suggestions: library.suggestions(normalized)}
	case 1:
		return matches[0], nil
	default:
		identifiers := make([]string, 0, len(matches))
		for _, index := range matches {
			identifiers = append(identifiers, library.topics[index].ID)
		}
		sort.Strings(identifiers)
		return 0, &Error{Kind: ErrorAmbiguous, Subject: name, Suggestions: identifiers}
	}
}

func decodeCatalog(data []byte) (catalog, error) {
	if !utf8.Valid(data) {
		return catalog{}, errors.New("catalog is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var parsed catalog
	if err := decoder.Decode(&parsed); err != nil {
		return catalog{}, fmt.Errorf("parse catalog: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("catalog contains more than one JSON value")
		}
		return catalog{}, fmt.Errorf("parse catalog: %w", err)
	}
	return parsed, nil
}

func buildLibrary(root string, parsed catalog) (*Library, error) {
	if parsed.Version != CatalogVersion {
		return nil, fmt.Errorf("catalog version is %d; expected %d", parsed.Version, CatalogVersion)
	}
	if len(parsed.Topics) == 0 {
		return nil, errors.New("catalog contains no topics")
	}
	topics := make([]Topic, len(parsed.Topics))
	for index, topic := range parsed.Topics {
		topics[index] = cloneTopic(topic)
	}
	sort.Slice(topics, func(left, right int) bool { return topics[left].ID < topics[right].ID })
	library := &Library{
		root: root, topics: topics, byName: map[string][]int{}, byPath: map[string]int{},
	}
	for index, topic := range topics {
		if err := library.addTopic(index, topic); err != nil {
			return nil, err
		}
	}
	if len(library.List()) == 0 {
		return nil, errors.New("catalog contains no public topics")
	}
	return library, nil
}

func (library *Library) addTopic(index int, topic Topic) error {
	if err := validateTopicDefinition(index, topic); err != nil {
		return err
	}
	if err := library.addTopicPath(index, topic); err != nil {
		return err
	}
	return library.addTopicNames(index, topic)
}

func validateTopicDefinition(index int, topic Topic) error {
	label := fmt.Sprintf("topic %d", index+1)
	if !identifierPattern.MatchString(topic.ID) {
		return fmt.Errorf("%s has invalid identifier %q", label, topic.ID)
	}
	if err := validateDocumentPath(topic.Path); err != nil {
		return fmt.Errorf("topic %q: %w", topic.ID, err)
	}
	if err := validateDisplayText("title", topic.Title); err != nil {
		return fmt.Errorf("topic %q: %w", topic.ID, err)
	}
	if err := validateDisplayText("summary", topic.Summary); err != nil {
		return fmt.Errorf("topic %q: %w", topic.ID, err)
	}
	if !slices.IsSorted(topic.Aliases) {
		return fmt.Errorf("topic %q aliases are not sorted", topic.ID)
	}
	for _, alias := range topic.Aliases {
		if !identifierPattern.MatchString(alias) {
			return fmt.Errorf("topic %q has invalid alias %q", topic.ID, alias)
		}
	}
	return nil
}

func (library *Library) addTopicPath(index int, topic Topic) error {
	if _, exists := library.byPath[topic.Path]; exists {
		return fmt.Errorf("topic %q repeats document path %q", topic.ID, topic.Path)
	}
	if _, err := regularFilePath(library.root, topic.Path); err != nil {
		return fmt.Errorf("topic %q document: %w", topic.ID, err)
	}
	library.byPath[topic.Path] = index
	return nil
}

func (library *Library) addTopicNames(index int, topic Topic) error {
	if err := library.addName(topic.ID, index); err != nil {
		return err
	}
	for _, alias := range topic.Aliases {
		if err := library.addName(alias, index); err != nil {
			return err
		}
	}
	return nil
}

func (library *Library) addName(name string, index int) error {
	if existing := library.byName[name]; len(existing) != 0 {
		return fmt.Errorf("documentation name %q is assigned more than once", name)
	}
	library.byName[name] = []int{index}
	return nil
}

func validateDocumentPath(value string) error {
	if value == "" || strings.Contains(value, `\`) || path.IsAbs(value) || path.Clean(value) != value {
		return fmt.Errorf("document path %q is not a clean repository-relative slash path", value)
	}
	if value == "docs" || !strings.HasPrefix(value, "docs/") || path.Ext(value) != ".md" {
		return fmt.Errorf("document path %q is not Markdown below docs", value)
	}
	if strings.HasPrefix(value, "docs/plans/") {
		return fmt.Errorf("document path %q selects a temporary plan", value)
	}
	return nil
}

func validateDisplayText(label, value string) error {
	if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) || strings.ContainsAny(value, "\r\n\t") {
		return fmt.Errorf("%s must be one non-empty trimmed line", label)
	}
	if utf8.RuneCountInString(value) > 240 {
		return fmt.Errorf("%s exceeds 240 characters", label)
	}
	return nil
}

func resolveRoot(root string) (string, error) {
	if root == "" {
		return "", errors.New("release root is empty")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve release root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve release root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect release root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("release root is not a directory: %s", resolved)
	}
	return filepath.Clean(resolved), nil
}

func regularFilePath(root, relative string) (string, error) {
	target := filepath.Join(root, filepath.FromSlash(relative))
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", relative, err)
	}
	if !samePath(target, resolved) {
		return "", fmt.Errorf("%s must not use a symlink", relative)
	}
	contained, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", fmt.Errorf("contain %s: %w", relative, err)
	}
	if contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) || filepath.IsAbs(contained) {
		return "", fmt.Errorf("%s escapes the release root", relative)
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", relative, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", relative)
	}
	return resolved, nil
}

func readRegularFile(root, relative string, maximum int64) ([]byte, error) {
	resolved, err := regularFilePath(root, relative)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", relative, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect open %s: %w", relative, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", relative)
	}
	if info.Size() > maximum {
		return nil, fmt.Errorf("%s exceeds %d bytes", relative, maximum)
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", relative, err)
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("%s exceeds %d bytes", relative, maximum)
	}
	return data, nil
}

func samePath(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func cloneTopic(topic Topic) Topic {
	topic.Aliases = slices.Clone(topic.Aliases)
	return topic
}

func (library *Library) suggestions(name string) []string {
	bestDistance := -1
	best := []string{}
	for _, topic := range library.topics {
		distance := editDistance(name, topic.ID)
		if bestDistance == -1 || distance < bestDistance {
			bestDistance = distance
			best = []string{topic.ID}
		} else if distance == bestDistance {
			best = append(best, topic.ID)
		}
	}
	threshold := 2
	if utf8.RuneCountInString(name) > 8 {
		threshold = 3
	}
	if bestDistance > threshold || len(best) != 1 {
		return nil
	}
	return best
}

func editDistance(left, right string) int {
	a, b := []rune(left), []rune(right)
	previous := make([]int, len(b)+1)
	for index := range previous {
		previous[index] = index
	}
	for leftIndex, leftRune := range a {
		current := make([]int, len(b)+1)
		current[0] = leftIndex + 1
		for rightIndex, rightRune := range b {
			cost := 0
			if leftRune != rightRune {
				cost = 1
			}
			current[rightIndex+1] = min(
				current[rightIndex]+1,
				previous[rightIndex+1]+1,
				previous[rightIndex]+cost,
			)
		}
		previous = current
	}
	return previous[len(b)]
}
