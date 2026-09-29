package repository

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"unicode"
)

const (
	trendsCopyLines         = 6
	trendsMinimumCopyLength = 10
)

type trendsCollector struct {
	repo         Repository
	window       trendsWindow
	files        map[string]*trendsFile
	copies       trendsCopyIndex
	weeks        []TrendsWeek
	reverts      []TrendsRevert
	copied       map[trendsCopyKey]int
	lockChanges  []TrendsLockChange
	landings     []trendsLanding
	changes      int
	skipped      int
	started      bool
	shallow      bool
	startVersion string
	version      string
}

type trendsCopyKey struct {
	path string
	week int
}

type trendsFile struct {
	lines   []string
	origins []int32
}

type trendsLanding struct {
	commit      string
	subject     string
	time        int64
	week        int
	lines       int
	rewritten   int
	codePolishy *TrendsVersionChange
}

type trendsChange struct {
	parent  string
	landing int32
	time    int64
	week    int
	rewrote int
	modules map[string]bool
}

type trendsWindowKey [trendsCopyLines]uint32

type trendsCopyIndex struct {
	lines   map[string]uint32
	windows map[trendsWindowKey]int32
}

type trendsSignificantLine struct {
	id  uint32
	raw int
}

func newTrendsCollector(repo Repository, window trendsWindow) *trendsCollector {
	weeks := make([]TrendsWeek, window.weeks)
	for index := range weeks {
		weeks[index] = window.week(index)
	}
	return &trendsCollector{
		repo: repo, window: window, files: map[string]*trendsFile{},
		copies: trendsCopyIndex{lines: map[string]uint32{}, windows: map[trendsWindowKey]int32{}},
		weeks:  weeks, reverts: []TrendsRevert{}, copied: map[trendsCopyKey]int{},
		lockChanges: []TrendsLockChange{},
	}
}

func (collector *trendsCollector) record(commit trendsCommit) error {
	if !collector.started {
		collector.started = true
		if commit.parent == "" && collector.shallow {
			return errors.New("trends needs history from before the requested weeks, but this clone is shallow; run git fetch --unshallow or choose a later --since")
		}
		if err := collector.loadSnapshot(commit.parent); err != nil {
			return err
		}
	}
	week := collector.window.index(commit.time)
	collector.changes++
	collector.weeks[week].Changes++
	collector.recordRevert(commit, week)
	lockChange := collector.recordLockChange(commit, week)
	collector.weeks[week].CodePolishyVersion = collector.version
	collector.recordCopies(commit, week)
	collector.landings = append(collector.landings, trendsLanding{
		commit: commit.id, subject: trendsSubject(commit.message), time: commit.time.Unix(), week: week,
	})
	change := &trendsChange{
		parent: commit.parent, landing: int32(len(collector.landings)), time: commit.time.Unix(), week: week, modules: map[string]bool{},
	}
	for _, file := range commit.files {
		collector.recordFileLines(change, file)
	}
	collector.finishChange(change, lockChange)
	return nil
}

func (collector *trendsCollector) finishChange(change *trendsChange, lockChange int) {
	if len(change.modules) > 0 {
		collector.weeks[change.week].ModuleChanges++
		collector.weeks[change.week].ModulesTouched += len(change.modules)
	}
	if lockChange < 0 {
		return
	}
	landing := &collector.landings[change.landing-1]
	recorded := &collector.lockChanges[lockChange]
	recorded.LinesLanded, recorded.LinesRewritten = landing.lines, change.rewrote
	landing.codePolishy = &TrendsVersionChange{Kind: recorded.Kind, Version: recorded.Version}
}

func (collector *trendsCollector) loadSnapshot(revision string) error {
	if revision == "" {
		return nil
	}
	collector.version = collector.repo.trendsLockVersion(revision)
	collector.startVersion = collector.version
	entries, err := collector.repo.gitLines("ls-tree", "-r", "-z", "--full-tree", revision)
	if err != nil {
		return err
	}
	objects := map[string]string{}
	for _, entry := range entries {
		if path, objectID, accepted := collector.trackedTreeEntry(entry); accepted {
			objects[path] = objectID
		}
	}
	objectIDs := make([]string, 0, len(objects))
	for _, objectID := range objects {
		objectIDs = append(objectIDs, objectID)
	}
	contents, err := collector.repo.gitBlobContents(objectIDs)
	if err != nil {
		return err
	}
	for path, objectID := range objects {
		if content := contents[objectID]; bytes.IndexByte(content, 0) < 0 {
			collector.track(path, newTrendsFile(content))
		}
	}
	return nil
}

func (collector *trendsCollector) trackedTreeEntry(entry string) (string, string, bool) {
	metadata, path, found := strings.Cut(entry, "\t")
	fields := strings.Fields(metadata)
	if !found || len(fields) != 3 || fields[1] != "blob" || fields[0] != "100644" && fields[0] != "100755" {
		return "", "", false
	}
	if !exactRevision(fields[2]) || !collector.repo.trendsTracked(path) {
		return "", "", false
	}
	return path, strings.ToLower(fields[2]), true
}

func (repo Repository) gitBlobContents(objectIDs []string) (map[string][]byte, error) {
	objectIDs = uniqueSorted(objectIDs)
	result := make(map[string][]byte, len(objectIDs))
	if len(objectIDs) == 0 {
		return result, nil
	}
	command := exec.Command("git", "-C", repo.Root, "cat-file", "--batch")
	command.Stdin = strings.NewReader(strings.Join(objectIDs, "\n") + "\n")
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("read Git history blobs: %w", err)
	}
	for _, objectID := range objectIDs {
		content, rest, err := parseGitBatchBlob(output, objectID)
		if err != nil {
			return nil, err
		}
		result[objectID] = content
		output = rest
	}
	return result, nil
}

func parseGitBatchBlob(output []byte, objectID string) ([]byte, []byte, error) {
	header, rest, found := bytes.Cut(output, []byte{'\n'})
	fields := strings.Fields(string(header))
	if !found || len(fields) != 3 || !strings.EqualFold(fields[0], objectID) || fields[1] != "blob" {
		return nil, nil, errors.New("read Git history blobs: unexpected object response")
	}
	size, err := strconv.Atoi(fields[2])
	if err != nil || size < 0 || size >= len(rest) || rest[size] != '\n' {
		return nil, nil, errors.New("read Git history blobs: invalid object size")
	}
	return rest[:size], rest[size+1:], nil
}

func newTrendsFile(content []byte) *trendsFile {
	text := strings.TrimSuffix(string(content), "\n")
	lines := []string{}
	if len(content) > 0 {
		lines = strings.Split(text, "\n")
	}
	return &trendsFile{lines: lines, origins: make([]int32, len(lines))}
}

func (collector *trendsCollector) track(path string, file *trendsFile) {
	collector.files[path] = file
	collector.copies.add(file.lines, 1)
}

func (collector *trendsCollector) untrack(path string) *trendsFile {
	file := collector.files[path]
	if file != nil {
		collector.copies.add(file.lines, -1)
		delete(collector.files, path)
	}
	return file
}

func (collector *trendsCollector) recordFileLines(change *trendsChange, file trendsFileDiff) {
	tracked := collector.files[file.oldPath] != nil
	newTracked := !file.special && collector.repo.trendsTracked(file.newPath)
	if !tracked && !newTracked {
		return
	}
	if file.changed() {
		collector.addModules(file, tracked, newTracked, change.modules)
	}
	state := collector.fileState(change, file, newTracked)
	if state == nil || file.special {
		return
	}
	removed, applied := state.apply(file.hunks, change.landing)
	if !applied {
		collector.skipped++
		return
	}
	collector.recordRewrites(change, removed)
	if newTracked {
		added := trendsAddedLines(file.hunks)
		collector.weeks[change.week].LinesLanded += added
		collector.landings[change.landing-1].lines += added
		collector.track(file.newPath, state)
	}
}

func (collector *trendsCollector) fileState(change *trendsChange, file trendsFileDiff, newTracked bool) *trendsFile {
	if state := collector.untrack(file.oldPath); state != nil {
		return state
	}
	if file.oldPath == "" {
		return &trendsFile{}
	}
	if !newTracked || change.parent == "" {
		return nil
	}
	content, present, err := collector.repo.ReadRegularFileAt(change.parent, file.oldPath)
	if err != nil || !present || bytes.IndexByte(content, 0) >= 0 {
		return nil
	}
	return newTrendsFile(content)
}

func (collector *trendsCollector) addModules(file trendsFileDiff, tracked, newTracked bool, modules map[string]bool) {
	paths := []string{}
	if tracked {
		paths = append(paths, file.oldPath)
	}
	if newTracked {
		paths = append(paths, file.newPath)
	}
	for _, path := range paths {
		for _, module := range collector.repo.sizeOwnerModuleNames(path) {
			modules[module] = true
		}
	}
}

func (collector *trendsCollector) recordRewrites(change *trendsChange, origins []int32) {
	window := int64(collector.window.rewriteDays) * 24 * 60 * 60
	for _, origin := range origins {
		if origin == 0 {
			continue
		}
		landing := &collector.landings[origin-1]
		if change.time-landing.time <= window {
			collector.weeks[landing.week].LinesRewritten++
			landing.rewritten++
			change.rewrote++
		}
	}
}

func trendsAddedLines(hunks []trendsHunk) int {
	added := 0
	for _, hunk := range hunks {
		added += len(hunk.added)
	}
	return added
}

func (file *trendsFile) apply(hunks []trendsHunk, landing int32) ([]int32, bool) {
	lines := make([]string, 0, len(file.lines)+trendsAddedLines(hunks))
	origins := make([]int32, 0, cap(lines))
	removed := []int32{}
	cursor := 0
	for _, hunk := range hunks {
		start := hunk.oldStart - 1
		if len(hunk.removed) == 0 {
			start = hunk.oldStart
		}
		if start < cursor || start+len(hunk.removed) > len(file.lines) || !file.matches(start, hunk.removed) {
			return nil, false
		}
		lines = append(append(lines, file.lines[cursor:start]...), hunk.added...)
		origins = append(origins, file.origins[cursor:start]...)
		removed = append(removed, file.origins[start:start+len(hunk.removed)]...)
		for range hunk.added {
			origins = append(origins, landing)
		}
		cursor = start + len(hunk.removed)
	}
	file.lines = append(lines, file.lines[cursor:]...)
	file.origins = append(origins, file.origins[cursor:]...)
	return removed, true
}

func (file *trendsFile) matches(start int, removed []string) bool {
	for offset, line := range removed {
		if compactTrendsLine(file.lines[start+offset]) != compactTrendsLine(line) {
			return false
		}
	}
	return true
}

func compactTrendsLine(line string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) {
			return -1
		}
		return character
	}, line)
}

func (collector *trendsCollector) recordCopies(commit trendsCommit, week int) {
	moved := map[trendsWindowKey]bool{}
	for _, file := range commit.files {
		if collector.files[file.oldPath] == nil {
			continue
		}
		for _, hunk := range file.hunks {
			for _, key := range collector.copies.windowKeys(collector.copies.significant(hunk.removed, false)) {
				moved[key] = true
			}
		}
	}
	for _, file := range commit.files {
		if file.special || !collector.repo.trendsTracked(file.newPath) {
			continue
		}
		copied := 0
		for _, hunk := range file.hunks {
			copied += collector.copies.copiedLines(hunk.added, moved)
		}
		if copied > 0 {
			collector.weeks[week].LinesCopied += copied
			collector.copied[trendsCopyKey{path: file.newPath, week: week}] += copied
		}
	}
}

func (index trendsCopyIndex) significant(lines []string, intern bool) []trendsSignificantLine {
	significant := []trendsSignificantLine{}
	for raw, line := range lines {
		compact := compactTrendsLine(line)
		if len(compact) < trendsMinimumCopyLength || !strings.ContainsFunc(compact, isTrendsWordCharacter) {
			continue
		}
		id, known := index.lines[compact]
		if !known && intern {
			id = uint32(len(index.lines) + 1)
			index.lines[compact] = id
		}
		significant = append(significant, trendsSignificantLine{id: id, raw: raw})
	}
	return significant
}

func isTrendsWordCharacter(character rune) bool {
	return unicode.IsLetter(character) || unicode.IsDigit(character)
}

func (index trendsCopyIndex) windowKeys(lines []trendsSignificantLine) []trendsWindowKey {
	keys := []trendsWindowKey{}
	for start := 0; start+trendsCopyLines <= len(lines); start++ {
		if key, known := trendsWindowKeyFor(lines[start : start+trendsCopyLines]); known {
			keys = append(keys, key)
		}
	}
	return keys
}

func trendsWindowKeyFor(lines []trendsSignificantLine) (trendsWindowKey, bool) {
	key := trendsWindowKey{}
	for offset, line := range lines {
		if line.id == 0 {
			return trendsWindowKey{}, false
		}
		key[offset] = line.id
	}
	return key, true
}

func (index trendsCopyIndex) add(lines []string, delta int32) {
	for _, key := range index.windowKeys(index.significant(lines, true)) {
		index.windows[key] += delta
		if index.windows[key] <= 0 {
			delete(index.windows, key)
		}
	}
}

func (index trendsCopyIndex) copiedLines(lines []string, moved map[trendsWindowKey]bool) int {
	significant := index.significant(lines, false)
	copied := make([]bool, len(lines))
	for start := 0; start+trendsCopyLines <= len(significant); start++ {
		window := significant[start : start+trendsCopyLines]
		key, known := trendsWindowKeyFor(window)
		if !known || index.windows[key] == 0 || moved[key] {
			continue
		}
		for raw := window[0].raw; raw <= window[trendsCopyLines-1].raw; raw++ {
			copied[raw] = true
		}
	}
	count := 0
	for _, line := range copied {
		if line {
			count++
		}
	}
	return count
}
