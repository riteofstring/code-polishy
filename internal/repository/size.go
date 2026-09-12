package repository

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

const (
	maximumSizeGroups       = 64
	maximumSizeTopLevel     = 32
	maximumSizeLargestFiles = 20
)

var sizeAnalysisExcludedPaths = []string{
	".code-polishy-artifacts/**",
	".code-polishy-reports/**",
}

var sizeGitTransformAttributes = []string{
	"filter",
	"ident",
	"text",
	"eol",
	"working-tree-encoding",
	"crlf",
}

type SizeAnalysis struct {
	Workspace     SizeWorkspace   `json:"workspace"`
	Governed      SizeGoverned    `json:"governed"`
	ExcludedPaths []string        `json:"excludedPaths"`
	Comparison    *SizeComparison `json:"comparison,omitempty"`
}

type SizeWorkspace struct {
	Files        int         `json:"files"`
	Bytes        int64       `json:"bytes"`
	Categories   []SizeGroup `json:"categories"`
	TopLevel     []SizeGroup `json:"topLevel"`
	LargestFiles []SizeFile  `json:"largestFiles"`
}

type SizeGoverned struct {
	Files        int         `json:"files"`
	Bytes        int64       `json:"bytes"`
	Categories   []SizeGroup `json:"categories"`
	Modules      []SizeGroup `json:"modules"`
	Languages    []SizeGroup `json:"languages"`
	LargestFiles []SizeFile  `json:"largestFiles"`
}

type SizeGroup struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

type SizeFile struct {
	Path     string `json:"path"`
	Category string `json:"category"`
	Bytes    int64  `json:"bytes"`
}

type SizeTotal struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

type SizeComparison struct {
	RequestedBase  string           `json:"requestedBase"`
	MergeBase      string           `json:"mergeBase"`
	Base           SizeTotal        `json:"base"`
	Current        SizeTotal        `json:"current"`
	DeltaFiles     int              `json:"deltaFiles"`
	DeltaBytes     int64            `json:"deltaBytes"`
	AddedFiles     int              `json:"addedFiles"`
	RemovedFiles   int              `json:"removedFiles"`
	ResizedFiles   int              `json:"resizedFiles"`
	AddedBytes     int64            `json:"addedBytes"`
	RemovedBytes   int64            `json:"removedBytes"`
	ResizedBytes   int64            `json:"resizedBytes"`
	Categories     []SizeGroupDelta `json:"categories"`
	Modules        []SizeGroupDelta `json:"modules"`
	Languages      []SizeGroupDelta `json:"languages"`
	LargestChanges []SizeChange     `json:"largestChanges"`
}

type SizeGroupDelta struct {
	Name         string `json:"name"`
	BaseFiles    int    `json:"baseFiles"`
	CurrentFiles int    `json:"currentFiles"`
	DeltaFiles   int    `json:"deltaFiles"`
	BaseBytes    int64  `json:"baseBytes"`
	CurrentBytes int64  `json:"currentBytes"`
	DeltaBytes   int64  `json:"deltaBytes"`
}

type SizeChange struct {
	Path         string `json:"path"`
	State        string `json:"state"`
	BaseBytes    int64  `json:"baseBytes"`
	CurrentBytes int64  `json:"currentBytes"`
	DeltaBytes   int64  `json:"deltaBytes"`
}

type sizeRecord struct {
	path  string
	bytes int64
}

type workspaceSizeCollector struct {
	repo          Repository
	governedPaths map[string]bool
	categories    map[string]SizeGroup
	topLevel      map[string]SizeGroup
	largest       []SizeFile
	total         SizeTotal
}

func (repo Repository) AnalyzeSize(requestedBase string) (SizeAnalysis, error) {
	paths, err := repo.AllFiles()
	if err != nil {
		return SizeAnalysis{}, err
	}
	current, err := repo.currentSizeRecords(paths)
	if err != nil {
		return SizeAnalysis{}, err
	}
	governed := repo.summarizeGovernedSize(current)
	workspace, err := repo.summarizeWorkspaceSize(current)
	if err != nil {
		return SizeAnalysis{}, err
	}
	analysis := SizeAnalysis{
		Workspace: workspace, Governed: governed,
		ExcludedPaths: slices.Clone(sizeAnalysisExcludedPaths),
	}
	if requestedBase == "" {
		return analysis, nil
	}
	mergeBase, err := repo.MergeBase(requestedBase)
	if err != nil {
		return SizeAnalysis{}, err
	}
	base, err := repo.sizeRecordsAt(mergeBase)
	if err != nil {
		return SizeAnalysis{}, err
	}
	candidate, err := repo.gitCandidateSizeRecords()
	if err != nil {
		return SizeAnalysis{}, err
	}
	baseGoverned := repo.summarizeGovernedSize(base)
	candidateGoverned := repo.summarizeGovernedSize(candidate)
	comparison := compareSizeRecords(requestedBase, mergeBase, base, candidate, baseGoverned, candidateGoverned)
	analysis.Comparison = &comparison
	return analysis, nil
}

func (repo Repository) currentSizeRecords(paths []string) ([]sizeRecord, error) {
	records := make([]sizeRecord, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(path)))
		if err != nil {
			return nil, fmt.Errorf("measure %s: %w", path, err)
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return nil, fmt.Errorf("measure %s: path is not a regular file or symbolic link", path)
		}
		records = append(records, sizeRecord{path: path, bytes: info.Size()})
	}
	return records, nil
}

func (repo Repository) gitCandidateSizeRecords() ([]sizeRecord, error) {
	records, err := repo.indexSizeRecords()
	if err != nil {
		return nil, err
	}
	byPath := sizeRecordsByPath(records)
	deleted, err := repo.gitLines("diff", "-z", "--name-only", "--no-renames", "--ignore-submodules=all", "--diff-filter=D", "--")
	if err != nil {
		return nil, err
	}
	for _, path := range deleted {
		if !repo.IsExcluded(path) {
			delete(byPath, path)
		}
	}
	modified, err := repo.gitLines("diff", "-z", "--name-only", "--no-renames", "--ignore-submodules=all", "--diff-filter=ACMRT", "--")
	if err != nil {
		return nil, err
	}
	untracked, err := repo.gitLines("ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	working, err := repo.comparableWorkingTreeSizeRecords(append(modified, untracked...))
	if err != nil {
		return nil, err
	}
	for _, record := range working {
		byPath[record.path] = record
	}
	return sortedSizeRecords(byPath), nil
}

func (repo Repository) indexSizeRecords() ([]sizeRecord, error) {
	entries, err := repo.gitLines("ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	indexEntries := []sizeIndexEntry{}
	objectIDs := []string{}
	for _, raw := range entries {
		entry, accepted, err := repo.parseIndexSizeEntry(raw)
		if err != nil {
			return nil, err
		}
		if accepted {
			indexEntries = append(indexEntries, entry)
			objectIDs = append(objectIDs, entry.objectID)
		}
	}
	sizes, err := repo.gitBlobSizes(objectIDs)
	if err != nil {
		return nil, err
	}
	records := make([]sizeRecord, 0, len(indexEntries))
	for _, entry := range indexEntries {
		records = append(records, sizeRecord{path: entry.path, bytes: sizes[entry.objectID]})
	}
	sort.Slice(records, func(left, right int) bool { return records[left].path < records[right].path })
	return records, nil
}

type sizeIndexEntry struct {
	path     string
	objectID string
}

func (repo Repository) parseIndexSizeEntry(raw string) (sizeIndexEntry, bool, error) {
	separator := strings.IndexByte(raw, '\t')
	if separator < 0 || separator == len(raw)-1 {
		return sizeIndexEntry{}, false, errorsForIndexSize("invalid index entry")
	}
	path := raw[separator+1:]
	if repo.IsExcluded(path) {
		return sizeIndexEntry{}, false, nil
	}
	fields := strings.Fields(raw[:separator])
	if len(fields) != 3 {
		return sizeIndexEntry{}, false, errorsForIndexSize("invalid index entry metadata")
	}
	if fields[2] != "0" {
		return sizeIndexEntry{}, false, fmt.Errorf("measure Git index: unmerged path %s", path)
	}
	if fields[0] != "100644" && fields[0] != "100755" && fields[0] != "120000" {
		return sizeIndexEntry{}, false, nil
	}
	if !exactRevision(fields[1]) {
		return sizeIndexEntry{}, false, errorsForIndexSize("invalid blob object ID")
	}
	return sizeIndexEntry{path: path, objectID: strings.ToLower(fields[1])}, true, nil
}

func (repo Repository) gitBlobSizes(objectIDs []string) (map[string]int64, error) {
	objectIDs = uniqueSorted(objectIDs)
	result := make(map[string]int64, len(objectIDs))
	if len(objectIDs) == 0 {
		return result, nil
	}
	command := exec.Command("git", "-C", repo.Root, "cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)")
	command.Stdin = strings.NewReader(strings.Join(objectIDs, "\n") + "\n")
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("measure Git index blobs: %w", err)
	}
	lines := bytes.Split(bytes.TrimSuffix(output, []byte{'\n'}), []byte{'\n'})
	if len(lines) != len(objectIDs) {
		return nil, errorsForIndexSize("unexpected blob-size response count")
	}
	requested := make(map[string]bool, len(objectIDs))
	for _, objectID := range objectIDs {
		requested[objectID] = true
	}
	for _, line := range lines {
		if err := parseGitBlobSize(line, requested, result); err != nil {
			return nil, err
		}
	}
	if len(result) != len(objectIDs) {
		return nil, errorsForIndexSize("missing blob size")
	}
	return result, nil
}

func parseGitBlobSize(line []byte, requested map[string]bool, result map[string]int64) error {
	fields := strings.Fields(string(line))
	if len(fields) != 3 || !exactRevision(fields[0]) || fields[1] != "blob" {
		return errorsForIndexSize("invalid blob-size response")
	}
	objectID := strings.ToLower(fields[0])
	if !requested[objectID] {
		return errorsForIndexSize("unexpected blob object ID")
	}
	if _, exists := result[objectID]; exists {
		return errorsForIndexSize("duplicate blob object ID")
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 {
		return errorsForIndexSize("invalid blob size")
	}
	result[objectID] = size
	return nil
}

func (repo Repository) comparableWorkingTreeSizeRecords(paths []string) ([]sizeRecord, error) {
	paths = uniqueSorted(paths)
	filtered := paths[:0]
	for _, path := range paths {
		if path != "" && !repo.IsExcluded(path) {
			filtered = append(filtered, path)
		}
	}
	paths = filtered
	if len(paths) == 0 {
		return []sizeRecord{}, nil
	}
	attributes, err := repo.gitSizeAttributes(paths)
	if err != nil {
		return nil, err
	}
	autoCRLF, err := repo.gitSizeConfig("core.autocrlf")
	if err != nil {
		return nil, err
	}
	records := make([]sizeRecord, 0, len(paths))
	for _, path := range paths {
		record, err := repo.comparableWorkingTreeSizeRecord(path, attributes[path], autoCRLF)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (repo Repository) comparableWorkingTreeSizeRecord(path string, attributes map[string]string, autoCRLF string) (sizeRecord, error) {
	absolute := filepath.Join(repo.Root, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if err != nil {
		return sizeRecord{}, fmt.Errorf("measure Git candidate %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(absolute)
		if err != nil {
			return sizeRecord{}, fmt.Errorf("measure Git candidate %s: %w", path, err)
		}
		return sizeRecord{path: path, bytes: int64(len([]byte(target)))}, nil
	}
	if !info.Mode().IsRegular() {
		return sizeRecord{}, fmt.Errorf("measure Git candidate %s: path is not a regular file or symbolic link", path)
	}
	if sizePathUsesGitTransform(attributes, autoCRLF) {
		return sizeRecord{}, fmt.Errorf("measure Git candidate %s: unstaged or untracked content uses Git transformations; stage or commit it before using --base", path)
	}
	return sizeRecord{path: path, bytes: info.Size()}, nil
}

func (repo Repository) gitSizeAttributes(paths []string) (map[string]map[string]string, error) {
	result := make(map[string]map[string]string, len(paths))
	if len(paths) == 0 {
		return result, nil
	}
	requested := make(map[string]bool, len(paths))
	input := &bytes.Buffer{}
	for _, path := range paths {
		requested[path] = true
		result[path] = map[string]string{}
		input.WriteString(path)
		input.WriteByte(0)
	}
	arguments := append([]string{"-C", repo.Root, "check-attr", "--stdin", "-z"}, sizeGitTransformAttributes...)
	command := exec.Command("git", arguments...)
	command.Stdin = input
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("inspect Git size transformations: %w", err)
	}
	parts := bytes.Split(output, []byte{0})
	if len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	if len(parts)%3 != 0 {
		return nil, errorsForIndexSize("invalid Git attribute response")
	}
	if err := parseGitSizeAttributes(parts, requested, result); err != nil {
		return nil, err
	}
	for _, path := range paths {
		if len(result[path]) != len(sizeGitTransformAttributes) {
			return nil, errorsForIndexSize("incomplete Git attribute response")
		}
	}
	return result, nil
}

func parseGitSizeAttributes(parts [][]byte, requested map[string]bool, result map[string]map[string]string) error {
	for index := 0; index < len(parts); index += 3 {
		path := filepath.ToSlash(string(parts[index]))
		attribute := string(parts[index+1])
		value := string(parts[index+2])
		if !requested[path] || !slices.Contains(sizeGitTransformAttributes, attribute) {
			return errorsForIndexSize("unexpected Git attribute response")
		}
		if _, exists := result[path][attribute]; exists {
			return errorsForIndexSize("duplicate Git attribute response")
		}
		result[path][attribute] = value
	}
	return nil
}

func (repo Repository) gitSizeConfig(name string) (string, error) {
	command := exec.Command("git", "-C", repo.Root, "config", "--get", name)
	output, err := command.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("inspect Git size configuration %s: %w", name, err)
	}
	value := strings.TrimSpace(string(output))
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("inspect Git size configuration %s: invalid value", name)
	}
	return strings.ToLower(value), nil
}

func sizePathUsesGitTransform(attributes map[string]string, autoCRLF string) bool {
	for _, name := range []string{"filter", "ident", "eol", "working-tree-encoding", "crlf"} {
		if activeGitAttribute(attributes[name]) {
			return true
		}
	}
	text := attributes["text"]
	if activeGitAttribute(text) {
		return true
	}
	return text != "unset" && autoCRLF != "" && !slices.Contains([]string{"false", "no", "off", "0"}, autoCRLF)
}

func activeGitAttribute(value string) bool {
	return value != "" && value != "unspecified" && value != "unset"
}

func sortedSizeRecords(byPath map[string]sizeRecord) []sizeRecord {
	records := make([]sizeRecord, 0, len(byPath))
	for _, record := range byPath {
		records = append(records, record)
	}
	sort.Slice(records, func(left, right int) bool { return records[left].path < records[right].path })
	return records
}

func errorsForIndexSize(message string) error {
	return fmt.Errorf("measure Git index: %s", message)
}

func (repo Repository) summarizeWorkspaceSize(governed []sizeRecord) (SizeWorkspace, error) {
	governedPaths := make(map[string]bool, len(governed))
	for _, record := range governed {
		governedPaths[record.path] = true
	}
	collector := workspaceSizeCollector{
		repo: repo, governedPaths: governedPaths,
		categories: map[string]SizeGroup{}, topLevel: map[string]SizeGroup{}, largest: []SizeFile{},
	}
	err := filepath.WalkDir(repo.Root, collector.visit)
	if err != nil {
		return SizeWorkspace{}, err
	}
	return SizeWorkspace{
		Files: collector.total.Files, Bytes: collector.total.Bytes,
		Categories:   orderedSizeGroups(collector.categories, 0, ""),
		TopLevel:     orderedSizeGroups(collector.topLevel, maximumSizeTopLevel, "remaining-top-level"),
		LargestFiles: collector.largest,
	}, nil
}

func (collector *workspaceSizeCollector) visit(path string, entry fs.DirEntry, walkErr error) error {
	relative, skipDirectory, omit, err := workspaceSizeWalkEntry(collector.repo.Root, path, entry, walkErr)
	if err != nil {
		return err
	}
	if skipDirectory {
		return filepath.SkipDir
	}
	if omit {
		return nil
	}
	info, accepted, err := measurableSizeEntry(relative, entry)
	if err != nil {
		return err
	}
	if !accepted {
		return nil
	}
	collector.add(relative, info.Size())
	return nil
}

func workspaceSizeWalkEntry(root, path string, entry fs.DirEntry, walkErr error) (string, bool, bool, error) {
	if walkErr != nil {
		return "", false, false, walkErr
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", false, false, err
	}
	relative = filepath.ToSlash(relative)
	if relative == "." {
		return relative, false, true, nil
	}
	if excludedSizeAnalysisPath(relative) {
		return relative, entry.IsDir(), true, nil
	}
	return relative, false, entry.IsDir(), nil
}

func measurableSizeEntry(path string, entry fs.DirEntry) (fs.FileInfo, bool, error) {
	info, err := entry.Info()
	if err != nil {
		return nil, false, fmt.Errorf("measure %s: %w", path, err)
	}
	accepted := info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0
	return info, accepted, nil
}

func (collector *workspaceSizeCollector) add(path string, bytes int64) {
	category := collector.repo.workspaceSizeCategory(path, collector.governedPaths[path])
	addSizeGroup(collector.categories, category, bytes)
	addSizeGroup(collector.topLevel, topLevelSizePath(path), bytes)
	collector.total.Files++
	collector.total.Bytes += bytes
	collector.largest = retainLargestSizeFile(collector.largest, SizeFile{Path: path, Category: category, Bytes: bytes})
}

func (repo Repository) summarizeGovernedSize(records []sizeRecord) SizeGoverned {
	categories := map[string]SizeGroup{}
	modules := map[string]SizeGroup{}
	languages := map[string]SizeGroup{}
	largest := []SizeFile{}
	total := SizeTotal{}
	for _, record := range records {
		category := repo.governedSizeCategory(record.path)
		addSizeGroup(categories, category, record.bytes)
		addSizeGroup(modules, repo.sizeModule(record.path), record.bytes)
		addSizeGroup(languages, repo.sizeLanguage(record.path), record.bytes)
		total.Files++
		total.Bytes += record.bytes
		largest = retainLargestSizeFile(largest, SizeFile{Path: record.path, Category: category, Bytes: record.bytes})
	}
	return SizeGoverned{
		Files: total.Files, Bytes: total.Bytes,
		Categories:   orderedSizeGroups(categories, 0, ""),
		Modules:      orderedSizeGroups(modules, maximumSizeGroups, "remaining-modules"),
		Languages:    orderedSizeGroups(languages, maximumSizeGroups, "remaining-languages"),
		LargestFiles: largest,
	}
}

func (repo Repository) sizeRecordsAt(revision string) ([]sizeRecord, error) {
	entries, err := repo.gitLines("ls-tree", "-r", "-l", "-z", "--full-tree", revision)
	if err != nil {
		return nil, err
	}
	records := make([]sizeRecord, 0, len(entries))
	for _, entry := range entries {
		record, accepted, err := repo.parseTreeSizeRecord(entry)
		if err != nil {
			return nil, err
		}
		if accepted {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(left, right int) bool { return records[left].path < records[right].path })
	return records, nil
}

func (repo Repository) parseTreeSizeRecord(entry string) (sizeRecord, bool, error) {
	separator := strings.IndexByte(entry, '\t')
	if separator < 0 || separator == len(entry)-1 {
		return sizeRecord{}, false, errorsForTreeSize("invalid tree entry")
	}
	fields := strings.Fields(entry[:separator])
	if len(fields) != 4 {
		return sizeRecord{}, false, errorsForTreeSize("invalid tree entry metadata")
	}
	if fields[1] != "blob" || fields[0] != "100644" && fields[0] != "100755" && fields[0] != "120000" {
		return sizeRecord{}, false, nil
	}
	bytes, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || bytes < 0 {
		return sizeRecord{}, false, errorsForTreeSize("invalid blob size")
	}
	path := entry[separator+1:]
	if repo.IsExcluded(path) {
		return sizeRecord{}, false, nil
	}
	return sizeRecord{path: path, bytes: bytes}, true, nil
}

func errorsForTreeSize(message string) error {
	return fmt.Errorf("measure Git tree: %s", message)
}

func (repo Repository) governedSizeCategory(path string) string {
	switch {
	case repo.sizeGenerated(path):
		return "generated"
	case repo.IsTest(path):
		return "tests"
	case documentationSizePath(path):
		return "documentation"
	case repo.sizeData(path):
		return "data"
	case assetSizePath(path):
		return "assets"
	case repo.sizeExecutableSource(path):
		return "source"
	case repo.IsControlInput(path):
		return "configuration"
	default:
		return "other"
	}
}

func (repo Repository) workspaceSizeCategory(path string, governed bool) string {
	segments := sizePathSegments(path)
	if len(segments) > 0 && segments[0] == ".git" {
		return "git-metadata"
	}
	if containsSizeSegment(segments, ".tools", "node_modules", ".venv", "venv", "vendor", "pods", "carthage") {
		return "dependencies-and-toolchains"
	}
	if containsSizeSegment(segments, "dist", "build", "target", "coverage", ".cache", ".gradle", ".next", ".nuxt", "out", "__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache", "playwright-report", "test-results") {
		return "build-cache-and-test-output"
	}
	if governed {
		return "governed-" + repo.governedSizeCategory(path)
	}
	return "other-local-files"
}

func (repo Repository) sizeModule(path string) string {
	owners := repo.sizeOwnerModuleNames(path)
	if len(owners) == 0 {
		return "unowned"
	}
	sort.Strings(owners)
	if len(owners) == 1 {
		return owners[0]
	}
	return "shared:" + strings.Join(owners, ",")
}

func (repo Repository) sizeLanguage(path string) string {
	languages := repo.sizeLanguages(path)
	if len(languages) == 0 {
		return "non-source"
	}
	sort.Strings(languages)
	if len(languages) == 1 {
		return languages[0]
	}
	return "multi:" + strings.Join(languages, ",")
}

func (repo Repository) sizeGenerated(path string) bool {
	if repo.sizeData(path) || repo.isDynamicControlInput(path) {
		return false
	}
	patterns := append(append([]string{}, policy.DefaultGenerated...), repo.Config.Scope.Generated...)
	return policy.MatchesAny(path, patterns)
}

func (repo Repository) sizeData(path string) bool {
	return !repo.IsControlInput(path) && !repo.sizeExecutableSource(path) && policy.MatchesAny(path, repo.Config.Scope.Data)
}

func (repo Repository) sizeExecutableSource(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return len(repo.sizeLanguages(path)) > 0 || extension == ".ps1" || extension == ".psm1"
}

func (repo Repository) sizeLanguages(path string) []string {
	if language := builtInExtensionLanguage(path); language != "" {
		return []string{language}
	}
	languages := []string{}
	for _, rule := range repo.Config.Scope.Languages {
		if policy.MatchesAny(path, rule.Paths) {
			languages = append(languages, rule.Name)
		}
	}
	return languages
}

func (repo Repository) sizeOwnerModuleNames(path string) []string {
	if repo.IsTest(path) && repo.sizeExecutableSource(path) {
		return repo.sizeTestOwnerModuleNames(path)
	}
	modulePath := path
	if repo.sizeGenerated(path) && slices.Contains(repo.sizeLanguages(path), "typescript") {
		modulePath = repo.JavaScriptContextPath(path)
	}
	return repo.sizeDeclaredModuleNames(modulePath)
}

func (repo Repository) sizeTestOwnerModuleNames(path string) []string {
	owners := []string{}
	for _, ownership := range repo.Config.Tests.Ownership {
		if policy.MatchesAny(path, ownership.Paths) {
			owners = append(owners, ownership.Module)
		}
	}
	return owners
}

func (repo Repository) sizeDeclaredModuleNames(path string) []string {
	owners := []string{}
	for _, module := range repo.Config.Modules {
		if policy.MatchesAny(path, module.Paths) {
			owners = append(owners, module.Name)
		}
	}
	return owners
}

func excludedSizeAnalysisPath(path string) bool {
	return path == ".code-polishy-artifacts" || strings.HasPrefix(path, ".code-polishy-artifacts/") ||
		path == ".code-polishy-reports" || strings.HasPrefix(path, ".code-polishy-reports/")
}

func documentationSizePath(path string) bool {
	lower := strings.ToLower(path)
	extension := strings.ToLower(filepath.Ext(path))
	return strings.HasPrefix(lower, "docs/") || strings.HasPrefix(lower, "doc/") ||
		slices.Contains([]string{".md", ".markdown", ".mdx", ".rst", ".adoc", ".txt"}, extension) ||
		slices.Contains([]string{"readme", "license", "copying", "contributing", "changelog"}, strings.ToLower(filepath.Base(path)))
}

func assetSizePath(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return slices.Contains([]string{
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".ico", ".svg", ".pdf",
		".mp3", ".wav", ".ogg", ".mp4", ".mov", ".webm", ".woff", ".woff2", ".ttf", ".otf",
		".zip", ".gz", ".tgz", ".bz2", ".xz", ".7z",
	}, extension)
}

func sizePathSegments(path string) []string {
	parts := strings.Split(strings.ToLower(filepath.ToSlash(path)), "/")
	return slices.DeleteFunc(parts, func(part string) bool { return part == "" || part == "." })
}

func containsSizeSegment(segments []string, candidates ...string) bool {
	for _, segment := range segments {
		if slices.Contains(candidates, segment) {
			return true
		}
	}
	return false
}

func topLevelSizePath(path string) string {
	if separator := strings.IndexByte(path, '/'); separator >= 0 {
		return path[:separator]
	}
	return path
}

func addSizeGroup(groups map[string]SizeGroup, name string, bytes int64) {
	group := groups[name]
	group.Name = name
	group.Files++
	group.Bytes += bytes
	groups[name] = group
}

func orderedSizeGroups(groups map[string]SizeGroup, limit int, overflowName string) []SizeGroup {
	ordered := make([]SizeGroup, 0, len(groups))
	for _, group := range groups {
		ordered = append(ordered, group)
	}
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].Bytes != ordered[right].Bytes {
			return ordered[left].Bytes > ordered[right].Bytes
		}
		if ordered[left].Files != ordered[right].Files {
			return ordered[left].Files > ordered[right].Files
		}
		return ordered[left].Name < ordered[right].Name
	})
	if limit <= 0 || len(ordered) <= limit {
		return ordered
	}
	overflow := SizeGroup{Name: overflowName}
	for _, group := range ordered[limit-1:] {
		overflow.Files += group.Files
		overflow.Bytes += group.Bytes
	}
	return append(ordered[:limit-1], overflow)
}

func retainLargestSizeFile(files []SizeFile, candidate SizeFile) []SizeFile {
	files = append(files, candidate)
	sort.Slice(files, func(left, right int) bool {
		if files[left].Bytes != files[right].Bytes {
			return files[left].Bytes > files[right].Bytes
		}
		return files[left].Path < files[right].Path
	})
	if len(files) > maximumSizeLargestFiles {
		files = files[:maximumSizeLargestFiles]
	}
	return files
}

func compareSizeRecords(requestedBase, mergeBase string, base, current []sizeRecord, baseSummary, currentSummary SizeGoverned) SizeComparison {
	comparison := SizeComparison{
		RequestedBase: requestedBase, MergeBase: mergeBase,
		Base:       SizeTotal{Files: baseSummary.Files, Bytes: baseSummary.Bytes},
		Current:    SizeTotal{Files: currentSummary.Files, Bytes: currentSummary.Bytes},
		Categories: sizeGroupDeltas(baseSummary.Categories, currentSummary.Categories),
		Modules:    sizeGroupDeltas(baseSummary.Modules, currentSummary.Modules),
		Languages:  sizeGroupDeltas(baseSummary.Languages, currentSummary.Languages),
	}
	comparison.DeltaFiles = comparison.Current.Files - comparison.Base.Files
	comparison.DeltaBytes = comparison.Current.Bytes - comparison.Base.Bytes
	baseByPath := sizeRecordsByPath(base)
	currentByPath := sizeRecordsByPath(current)
	paths := make(map[string]bool, len(baseByPath)+len(currentByPath))
	for path := range baseByPath {
		paths[path] = true
	}
	for path := range currentByPath {
		paths[path] = true
	}
	for path := range paths {
		baseRecord, inBase := baseByPath[path]
		currentRecord, inCurrent := currentByPath[path]
		change := SizeChange{Path: path, BaseBytes: baseRecord.bytes, CurrentBytes: currentRecord.bytes}
		switch {
		case !inBase:
			change.State = "added"
			comparison.AddedFiles++
			comparison.AddedBytes += currentRecord.bytes
		case !inCurrent:
			change.State = "removed"
			comparison.RemovedFiles++
			comparison.RemovedBytes += baseRecord.bytes
		case baseRecord.bytes != currentRecord.bytes:
			change.State = "resized"
			comparison.ResizedFiles++
			comparison.ResizedBytes += currentRecord.bytes - baseRecord.bytes
		default:
			continue
		}
		change.DeltaBytes = change.CurrentBytes - change.BaseBytes
		comparison.LargestChanges = append(comparison.LargestChanges, change)
	}
	sort.Slice(comparison.LargestChanges, func(left, right int) bool {
		leftDelta := absoluteSize(comparison.LargestChanges[left].DeltaBytes)
		rightDelta := absoluteSize(comparison.LargestChanges[right].DeltaBytes)
		if leftDelta != rightDelta {
			return leftDelta > rightDelta
		}
		return comparison.LargestChanges[left].Path < comparison.LargestChanges[right].Path
	})
	if len(comparison.LargestChanges) > maximumSizeLargestFiles {
		comparison.LargestChanges = comparison.LargestChanges[:maximumSizeLargestFiles]
	}
	if comparison.LargestChanges == nil {
		comparison.LargestChanges = []SizeChange{}
	}
	return comparison
}

func sizeRecordsByPath(records []sizeRecord) map[string]sizeRecord {
	byPath := make(map[string]sizeRecord, len(records))
	for _, record := range records {
		byPath[record.path] = record
	}
	return byPath
}

func sizeGroupDeltas(base, current []SizeGroup) []SizeGroupDelta {
	baseByName := sizeGroupsByName(base)
	currentByName := sizeGroupsByName(current)
	names := map[string]bool{}
	for name := range baseByName {
		names[name] = true
	}
	for name := range currentByName {
		names[name] = true
	}
	deltas := []SizeGroupDelta{}
	for name := range names {
		baseGroup := baseByName[name]
		currentGroup := currentByName[name]
		if baseGroup.Files == currentGroup.Files && baseGroup.Bytes == currentGroup.Bytes {
			continue
		}
		deltas = append(deltas, SizeGroupDelta{
			Name: name, BaseFiles: baseGroup.Files, CurrentFiles: currentGroup.Files,
			DeltaFiles: currentGroup.Files - baseGroup.Files,
			BaseBytes:  baseGroup.Bytes, CurrentBytes: currentGroup.Bytes,
			DeltaBytes: currentGroup.Bytes - baseGroup.Bytes,
		})
	}
	sort.Slice(deltas, func(left, right int) bool {
		leftDelta := absoluteSize(deltas[left].DeltaBytes)
		rightDelta := absoluteSize(deltas[right].DeltaBytes)
		if leftDelta != rightDelta {
			return leftDelta > rightDelta
		}
		return deltas[left].Name < deltas[right].Name
	})
	if len(deltas) > maximumSizeGroups {
		deltas = deltas[:maximumSizeGroups]
	}
	return deltas
}

func sizeGroupsByName(groups []SizeGroup) map[string]SizeGroup {
	byName := make(map[string]SizeGroup, len(groups))
	for _, group := range groups {
		byName[group.Name] = group
	}
	return byName
}

func absoluteSize(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
