package supplychain

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

type resolvedPackage struct {
	Ecosystem string
	Name      string
	Version   string
	Scope     string
	Locations []string
	Source    resolvedSource
}

type resolvedSource struct {
	Kind      string
	Registry  string
	LocalPath string
	Reason    string
	Git       repository.PythonGitSource
}

func resolvedNodePackages(ctx context.Context, repo repository.Repository, manifest nodeManifest) ([]resolvedPackage, error) {
	path, found := nodeLockPath(repo, manifest.Root, manifest.Manager)
	if !found {
		return nil, fmt.Errorf("exact %s lockfile is unavailable", manifest.Manager)
	}
	if manifest.Manager == "pnpm" {
		result, err := pnpmFacts(ctx, repo, manifest.Root)
		if err != nil {
			return nil, err
		}
		if err := unreadableLock(result, path); err != nil {
			return nil, err
		}
		return resolvedPNPMPackages(result, path), nil
	}
	data, err := repo.Read(path)
	if err != nil {
		return nil, err
	}
	if manifest.Manager != "npm" {
		return nil, fmt.Errorf("resolved release-age inspection does not support %s", manifest.Manager)
	}
	return parsePackageLock(data, path, manifest.Manager)
}

func nodeLockPath(repo repository.Repository, root, manager string) (string, bool) {
	names := map[string][]string{
		"npm":  {"npm-shrinkwrap.json", "package-lock.json"},
		"pnpm": {pnpmLockName},
	}
	for _, name := range names[manager] {
		path := name
		if root != "." {
			path = root + "/" + name
		}
		if regularFile(filepath.Join(repo.Root, filepath.FromSlash(path))) {
			return path, true
		}
	}
	return "", false
}

type packageLockDocument struct {
	LockfileVersion int                         `json:"lockfileVersion"`
	Packages        map[string]packageLockEntry `json:"packages"`
	Dependencies    map[string]packageLockEntry `json:"dependencies"`
}

type packageLockEntry struct {
	Name         string                      `json:"name"`
	Version      string                      `json:"version"`
	Resolved     string                      `json:"resolved"`
	Link         bool                        `json:"link"`
	Dependencies map[string]packageLockEntry `json:"dependencies"`
}

func parsePackageLock(data []byte, scope, ecosystem string) ([]resolvedPackage, error) {
	var document packageLockDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse %s: %w", scope, err)
	}
	packages := []resolvedPackage{}
	if document.Packages != nil {
		for path, entry := range document.Packages {
			name := packageLockName(path, entry.Name)
			if name == "" || entry.Link || !exactVersion.MatchString(entry.Version) {
				continue
			}
			packages = append(packages, resolvedPackage{
				Ecosystem: ecosystem, Name: name, Version: entry.Version, Scope: scope,
				Locations: []string{filepath.ToSlash(path)},
			})
		}
	} else {
		walkPackageLockDependencies(document.Dependencies, ecosystem, scope, &packages)
	}
	return uniqueResolvedPackages(packages), nil
}

func packageLockName(path, declared string) string {
	_ = declared
	marker := "node_modules/"
	index := strings.LastIndex(filepath.ToSlash(path), marker)
	if index >= 0 {
		return strings.TrimPrefix(filepath.ToSlash(path)[index+len(marker):], "/")
	}
	return ""
}

func walkPackageLockDependencies(entries map[string]packageLockEntry, ecosystem, scope string, result *[]resolvedPackage) {
	for name, entry := range entries {
		if !entry.Link && exactVersion.MatchString(entry.Version) {
			*result = append(*result, resolvedPackage{Ecosystem: ecosystem, Name: name, Version: entry.Version, Scope: scope})
		}
		walkPackageLockDependencies(entry.Dependencies, ecosystem, scope, result)
	}
}

func parseUVLock(data []byte, scope string) ([]resolvedPackage, error) {
	parser := newUVLockParser(data, scope)
	if err := parser.parse(); err != nil {
		return nil, err
	}
	return uniqueResolvedPackages(parser.packages), nil
}

type uvLockParser struct {
	scanner       *bufio.Scanner
	scope         string
	packages      []resolvedPackage
	current       uvLockPackage
	inPackage     bool
	packageFields bool
	lineNumber    int
}

func newUVLockParser(data []byte, scope string) *uvLockParser {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	return &uvLockParser{scanner: scanner, scope: scope, current: newUVLockPackage()}
}

func (parser *uvLockParser) parse() error {
	for parser.scanner.Scan() {
		parser.lineNumber++
		line := strings.TrimSpace(stripTOMLComment(parser.scanner.Text()))
		if line == "[[package]]" {
			if err := parser.startPackage(); err != nil {
				return err
			}
			continue
		}
		if !parser.inPackage {
			continue
		}
		if strings.HasPrefix(line, "[") {
			parser.packageFields = false
			continue
		}
		if parser.packageFields {
			if err := parser.parsePackageLine(line); err != nil {
				return err
			}
		}
	}
	return parser.finish()
}

func (parser *uvLockParser) startPackage() error {
	if err := appendUVPackage(&parser.packages, parser.current, parser.inPackage, parser.scope); err != nil {
		return fmt.Errorf("parse %s before line %d: %w", parser.scope, parser.lineNumber, err)
	}
	parser.current = newUVLockPackage()
	parser.inPackage = true
	parser.packageFields = true
	return nil
}

func (parser *uvLockParser) parsePackageLine(line string) error {
	completeLine, err := parser.completeSourceLine(line)
	if err != nil {
		return err
	}
	if err := parseUVPackageField(completeLine, &parser.current); err != nil {
		return fmt.Errorf("parse %s at line %d: %w", parser.scope, parser.lineNumber, err)
	}
	return nil
}

func (parser *uvLockParser) completeSourceLine(line string) (string, error) {
	name, value, found := strings.Cut(line, "=")
	if !found || strings.TrimSpace(name) != "source" {
		return line, nil
	}
	for !uvInlineTableComplete(value) {
		if !parser.scanner.Scan() {
			return "", parser.unterminatedSourceError()
		}
		parser.lineNumber++
		value += "\n" + strings.TrimSpace(stripTOMLComment(parser.scanner.Text()))
	}
	return "source = " + value, nil
}

func (parser *uvLockParser) unterminatedSourceError() error {
	if err := parser.scanner.Err(); err != nil {
		return fmt.Errorf("parse %s: %w", parser.scope, err)
	}
	return fmt.Errorf("parse %s at line %d: package source inline table is unterminated", parser.scope, parser.lineNumber)
}

func (parser *uvLockParser) finish() error {
	if err := parser.scanner.Err(); err != nil {
		return fmt.Errorf("parse %s: %w", parser.scope, err)
	}
	if err := appendUVPackage(&parser.packages, parser.current, parser.inPackage, parser.scope); err != nil {
		return fmt.Errorf("parse %s: %w", parser.scope, err)
	}
	return nil
}

func uvInlineTableComplete(value string) bool {
	state := uvInlineTableState{}
	for index := 0; index < len(value); index++ {
		state.consume(value[index])
	}
	return state.seenOpen && !state.invalid && state.quote == 0 && state.depth == 0
}

type uvInlineTableState struct {
	depth    int
	quote    byte
	escaped  bool
	seenOpen bool
	invalid  bool
}

func (state *uvInlineTableState) consume(character byte) {
	if state.quote != 0 {
		state.consumeQuoted(character)
		return
	}
	state.consumeUnquoted(character)
}

func (state *uvInlineTableState) consumeQuoted(character byte) {
	if state.quote == '"' && character == '\\' && !state.escaped {
		state.escaped = true
		return
	}
	if character == state.quote && !state.escaped {
		state.quote = 0
	}
	state.escaped = false
}

func (state *uvInlineTableState) consumeUnquoted(character byte) {
	if character == '"' || character == '\'' {
		state.quote = character
		return
	}
	if character == '{' {
		state.depth++
		state.seenOpen = true
	}
	if character == '}' {
		state.depth--
		state.invalid = state.invalid || state.depth < 0
	}
}

func appendUVPackage(packages *[]resolvedPackage, current uvLockPackage, inPackage bool, scope string) error {
	if !inPackage {
		return nil
	}
	name, err := repository.NormalizePythonPackageName(current.name)
	if err != nil {
		return errors.New("package omitted a valid name")
	}
	if current.source.Kind == "registry" && current.version == "" {
		return errors.New("registry package omitted a version")
	}
	ecosystem := "pypi"
	switch current.source.Kind {
	case "git":
		ecosystem = "git"
	case "local":
		ecosystem = "local"
	case "unsupported":
		ecosystem = "python"
	}
	*packages = append(*packages, resolvedPackage{
		Ecosystem: ecosystem, Name: name, Version: current.version, Scope: scope, Source: current.source,
	})
	return nil
}

func parseUVPackageField(line string, current *uvLockPackage) error {
	name, value, found := strings.Cut(line, "=")
	if !found {
		return nil
	}
	switch strings.TrimSpace(name) {
	case "name":
		current.name = tomlString(value)
	case "version":
		current.version = tomlString(value)
	case "source":
		source, err := parseUVSource(value)
		if err != nil {
			return err
		}
		current.source = source
	}
	return nil
}

type uvLockPackage struct {
	name, version string
	source        resolvedSource
}

func newUVLockPackage() uvLockPackage {
	return uvLockPackage{source: resolvedSource{Kind: "unsupported", Reason: "uv lock package has no source"}}
}

func parseUVSource(value string) (resolvedSource, error) {
	fields, err := parseUVInlineTable(value)
	if err != nil {
		return resolvedSource{}, err
	}
	if registry, found := fields["registry"]; found && registry != "" {
		return resolvedSource{Kind: "registry", Registry: registry}, nil
	}
	if gitURL, found := fields["git"]; found {
		commit := fields["commit"]
		if commit == "" {
			commit = fields["rev"]
		}
		source, sourceErr := repository.ParsePythonGitSource(gitURL, commit, fields["subdirectory"])
		if sourceErr != nil {
			return resolvedSource{Kind: "unsupported", Reason: sourceErr.Error()}, nil
		}
		return resolvedSource{Kind: "git", Git: source}, nil
	}
	for _, key := range []string{"editable", "directory", "virtual"} {
		if local, found := fields[key]; found {
			return resolvedSource{Kind: "local", LocalPath: local}, nil
		}
	}
	return resolvedSource{Kind: "unsupported", Reason: "uv lock package source is unsupported"}, nil
}

func parseUVInlineTable(value string) (map[string]string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != '{' || value[len(value)-1] != '}' {
		return nil, errors.New("package source must be one inline TOML table")
	}
	parser := uvInlineTableParser{value: value, index: 1, end: len(value) - 1}
	fields := map[string]string{}
	for parser.nextField() {
		key, decoded, err := parser.parseField()
		if err != nil {
			return nil, err
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, errors.New("package source declares one key more than once")
		}
		fields[key] = decoded
	}
	if len(fields) == 0 {
		return nil, errors.New("package source is empty")
	}
	return fields, nil
}

type uvInlineTableParser struct {
	value string
	index int
	end   int
}

func (parser *uvInlineTableParser) nextField() bool {
	for parser.index < parser.end && isUVInlineSeparator(parser.value[parser.index]) {
		parser.index++
	}
	return parser.index < parser.end
}

func isUVInlineSeparator(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n' || character == ','
}

func (parser *uvInlineTableParser) parseField() (string, string, error) {
	key, err := parser.parseKey()
	if err != nil {
		return "", "", err
	}
	parser.skipHorizontalSpace()
	if parser.index >= parser.end || parser.value[parser.index] != '=' {
		return "", "", errors.New("package source key has no value")
	}
	parser.index++
	parser.skipHorizontalSpace()
	decoded, next, err := parseUVInlineString(parser.value, parser.index)
	if err != nil {
		return "", "", err
	}
	parser.index = next
	parser.skipHorizontalSpace()
	if parser.index < parser.end && parser.value[parser.index] != ',' {
		return "", "", errors.New("package source values must be comma-separated")
	}
	return key, decoded, nil
}

func (parser *uvInlineTableParser) parseKey() (string, error) {
	start := parser.index
	for parser.index < parser.end && isUVInlineKeyCharacter(parser.value[parser.index]) {
		parser.index++
	}
	if start == parser.index {
		return "", errors.New("package source has an invalid key")
	}
	return parser.value[start:parser.index], nil
}

func isUVInlineKeyCharacter(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-'
}

func (parser *uvInlineTableParser) skipHorizontalSpace() {
	for parser.index < parser.end && (parser.value[parser.index] == ' ' || parser.value[parser.index] == '\t') {
		parser.index++
	}
}

func parseUVInlineString(value string, index int) (string, int, error) {
	if index >= len(value) || value[index] != '"' && value[index] != '\'' {
		return "", 0, errors.New("package source value must be a string")
	}
	quote := value[index]
	start := index
	end, found := uvInlineStringEnd(value, index+1, quote)
	if !found {
		return "", 0, errors.New("package source string is unterminated")
	}
	decoded, err := decodeUVInlineString(value[start:end])
	if err != nil {
		return "", 0, err
	}
	return decoded, end, nil
}

func uvInlineStringEnd(value string, index int, quote byte) (int, bool) {
	escaped := false
	for index < len(value) {
		character := value[index]
		if quote == '"' && character == '\\' && !escaped {
			escaped = true
			index++
			continue
		}
		if character == quote && !escaped {
			return index + 1, true
		}
		escaped = false
		index++
	}
	return 0, false
}

func decodeUVInlineString(raw string) (string, error) {
	decoded := tomlString(raw)
	if decoded == "" && raw != `""` && raw != `''` {
		return "", errors.New("package source string is malformed")
	}
	return decoded, nil
}

func tomlString(value string) string {
	value = strings.TrimSpace(stripTOMLComment(value))
	if decoded, err := strconv.Unquote(value); err == nil {
		return decoded
	}
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1]
	}
	return ""
}

func uniqueResolvedPackages(packages []resolvedPackage) []resolvedPackage {
	sort.Slice(packages, func(left, right int) bool {
		return resolvedPackageKey(packages[left]) < resolvedPackageKey(packages[right])
	})
	result := make([]resolvedPackage, 0, len(packages))
	last := ""
	for _, item := range packages {
		key := resolvedPackageKey(item)
		sort.Strings(item.Locations)
		item.Locations = compactStrings(item.Locations)
		if len(result) > 0 && key == last {
			locations := append(result[len(result)-1].Locations, item.Locations...)
			sort.Strings(locations)
			result[len(result)-1].Locations = compactStrings(locations)
			continue
		}
		result = append(result, item)
		last = key
	}
	return result
}

func resolvedPackageKey(item resolvedPackage) string {
	return item.Ecosystem + "\x00" + item.Scope + "\x00" + item.Name + "\x00" + item.Version + "\x00" + resolvedSourceKey(item.Source)
}

func resolvedSourceKey(source resolvedSource) string {
	switch source.Kind {
	case "git":
		return source.Kind + "\x00" + source.Git.Identity()
	case "registry":
		return source.Kind + "\x00" + source.Registry
	case "local":
		return source.Kind + "\x00" + source.LocalPath
	default:
		return source.Kind + "\x00" + source.Reason
	}
}
