package repository

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	DefaultTrendsRewriteDays = 14
	MaximumTrendsRewriteDays = 365
	DefaultTrendsWeeks       = 12
	MaximumTrendsWeeks       = 104
	maximumTrendsReverts     = 50
	maximumTrendsCopies      = 20
	maximumTrendsLockChanges = 50
	maximumTrendsRewrites    = 20
	maximumTrendsSubject     = 200
	trendsLockPath           = ".code-polishy.lock.json"
	trendsWeekDuration       = 7 * 24 * time.Hour
)

var (
	trendsRevertPattern  = regexp.MustCompile(`This reverts commit ([0-9a-fA-F]{40}(?:[0-9a-fA-F]{24})?)\b`)
	trendsVersionPattern = regexp.MustCompile(`"codePolishyVersion"\s*:\s*"([^"]{1,128})"`)
)

type TrendsRequest struct {
	Branch      string
	Since       time.Time
	AsOf        time.Time
	RewriteDays int
}

type TrendsAnalysis struct {
	Branch         string             `json:"branch"`
	Head           string             `json:"head"`
	Since          string             `json:"since"`
	AsOf           string             `json:"asOf"`
	RewriteDays    int                `json:"rewriteDays"`
	Changes        int                `json:"changes"`
	SkippedChanges int                `json:"skippedChanges"`
	Weeks          []TrendsWeek       `json:"weeks"`
	Rewrites       []TrendsRewrite    `json:"rewrites"`
	Reverts        []TrendsRevert     `json:"reverts"`
	Copies         []TrendsCopy       `json:"copies"`
	LockChanges    []TrendsLockChange `json:"lockChanges"`
}

type TrendsWeek struct {
	Week               string `json:"week"`
	Changes            int    `json:"changes"`
	LinesLanded        int    `json:"linesLanded"`
	LinesRewritten     int    `json:"linesRewritten"`
	RewriteComplete    bool   `json:"rewriteComplete"`
	LinesCopied        int    `json:"linesCopied"`
	ModuleChanges      int    `json:"moduleChanges"`
	ModulesTouched     int    `json:"modulesTouched"`
	Reverts            int    `json:"reverts"`
	CodePolishyVersion string `json:"codePolishyVersion"`
}

type TrendsRewrite struct {
	Commit         string               `json:"commit"`
	Week           string               `json:"week"`
	Subject        string               `json:"subject"`
	LinesLanded    int                  `json:"linesLanded"`
	LinesRewritten int                  `json:"linesRewritten"`
	CodePolishy    *TrendsVersionChange `json:"codePolishy,omitempty"`
}

type TrendsVersionChange struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
}

type TrendsRevert struct {
	Commit   string `json:"commit"`
	Reverted string `json:"reverted"`
	Week     string `json:"week"`
	Subject  string `json:"subject"`
}

type TrendsCopy struct {
	Path  string `json:"path"`
	Week  string `json:"week"`
	Lines int    `json:"lines"`
}

type TrendsLockChange struct {
	Commit         string `json:"commit"`
	Date           string `json:"date"`
	Week           string `json:"week"`
	Kind           string `json:"kind"`
	Version        string `json:"version"`
	LinesLanded    int    `json:"linesLanded"`
	LinesRewritten int    `json:"linesRewritten"`
}

type trendsWindow struct {
	start       time.Time
	asOf        time.Time
	weeks       int
	rewriteDays int
}

func (repo Repository) AnalyzeTrends(request TrendsRequest) (TrendsAnalysis, error) {
	branch, head, err := repo.trendsHead(request.Branch)
	if err != nil {
		return TrendsAnalysis{}, err
	}
	window, err := newTrendsWindow(request)
	if err != nil {
		return TrendsAnalysis{}, err
	}
	collector := newTrendsCollector(repo, window)
	collector.shallow = repo.shallowHistory()
	if err := repo.streamTrendsHistory(head, window.start, collector.record); err != nil {
		return TrendsAnalysis{}, err
	}
	return collector.analysis(branch, head), nil
}

func (analysis TrendsAnalysis) WeekIndex(moment time.Time) (int, bool) {
	start := trendsWeekStart(moment).Format(time.DateOnly)
	for index, week := range analysis.Weeks {
		if week.Week == start {
			return index, true
		}
	}
	return 0, false
}

func (repo Repository) trendsHead(requested string) (string, string, error) {
	branch := requested
	if branch == "" {
		branch = "HEAD"
	}
	if !repo.hasGit() || repo.git("rev-parse", "--verify", "HEAD") != nil {
		return "", "", errors.New("trends requires a Git repository with at least one commit")
	}
	if strings.TrimSpace(branch) != branch || strings.HasPrefix(branch, "-") || strings.ContainsAny(branch, " \t\r\n\x00") {
		return "", "", fmt.Errorf("invalid trends branch %q", branch)
	}
	heads, err := repo.gitLines("rev-parse", "--verify", branch+"^{commit}")
	if err != nil {
		return "", "", fmt.Errorf("resolve trends branch %q: %w", branch, err)
	}
	if len(heads) != 1 || !exactRevision(heads[0]) {
		return "", "", fmt.Errorf("resolve trends branch %q: expected one exact commit", branch)
	}
	return branch, strings.ToLower(heads[0]), nil
}

func (repo Repository) shallowHistory() bool {
	lines, err := repo.gitLines("rev-parse", "--is-shallow-repository")
	return err == nil && len(lines) == 1 && lines[0] == "true"
}

func newTrendsWindow(request TrendsRequest) (trendsWindow, error) {
	if request.AsOf.IsZero() {
		return trendsWindow{}, errors.New("trends requires an analysis time")
	}
	rewriteDays := request.RewriteDays
	if rewriteDays == 0 {
		rewriteDays = DefaultTrendsRewriteDays
	}
	if rewriteDays < 1 || rewriteDays > MaximumTrendsRewriteDays {
		return trendsWindow{}, fmt.Errorf("trends --rewrite-days must be between 1 and %d", MaximumTrendsRewriteDays)
	}
	asOf, since := request.AsOf.UTC(), request.Since
	if since.IsZero() {
		since = asOf.AddDate(0, 0, -7*(DefaultTrendsWeeks-1))
	}
	start := trendsWeekStart(since)
	if start.After(asOf) {
		return trendsWindow{}, errors.New("trends --since must not be later than the current week")
	}
	weeks := int(trendsWeekStart(asOf).Sub(start)/trendsWeekDuration) + 1
	if weeks > MaximumTrendsWeeks {
		return trendsWindow{}, fmt.Errorf("trends --since may cover at most %d weeks", MaximumTrendsWeeks)
	}
	return trendsWindow{start: start, asOf: asOf, weeks: weeks, rewriteDays: rewriteDays}, nil
}

func trendsWeekStart(moment time.Time) time.Time {
	moment = moment.UTC()
	day := time.Date(moment.Year(), moment.Month(), moment.Day(), 0, 0, 0, 0, time.UTC)
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}

func (window trendsWindow) index(moment time.Time) int {
	offset := int(trendsWeekStart(moment).Sub(window.start) / trendsWeekDuration)
	return min(max(offset, 0), window.weeks-1)
}

func (window trendsWindow) week(index int) TrendsWeek {
	start := window.start.AddDate(0, 0, 7*index)
	return TrendsWeek{
		Week: start.Format(time.DateOnly), RewriteComplete: !start.AddDate(0, 0, 7+window.rewriteDays).After(window.asOf),
	}
}

func (repo Repository) trendsTracked(path string) bool {
	return path != "" && !repo.IsExcluded(path) && !repo.IsControlInput(path) && !repo.sizeGenerated(path) &&
		!repo.sizeData(path) && repo.sizeExecutableSource(path)
}

func (collector *trendsCollector) analysis(branch, head string) TrendsAnalysis {
	if !collector.started {
		collector.version = collector.repo.trendsLockVersion(head)
		collector.startVersion = collector.version
	}
	version := collector.startVersion
	for index := range collector.weeks {
		if collector.weeks[index].Changes > 0 {
			version = collector.weeks[index].CodePolishyVersion
		}
		collector.weeks[index].CodePolishyVersion = version
	}
	return TrendsAnalysis{
		Branch: branch, Head: head, Since: collector.window.start.Format(time.DateOnly),
		AsOf: collector.window.asOf.Format(time.RFC3339), RewriteDays: collector.window.rewriteDays,
		Changes: collector.changes, SkippedChanges: collector.skipped, Weeks: collector.weeks,
		Rewrites:    collector.mostRewritten(),
		Reverts:     lastTrendsItems(collector.reverts, maximumTrendsReverts),
		Copies:      collector.largestCopies(),
		LockChanges: lastTrendsItems(collector.lockChanges, maximumTrendsLockChanges),
	}
}

func (collector *trendsCollector) largestCopies() []TrendsCopy {
	copies := make([]TrendsCopy, 0, len(collector.copied))
	for key, lines := range collector.copied {
		copies = append(copies, TrendsCopy{Path: key.path, Week: collector.weeks[key.week].Week, Lines: lines})
	}
	sort.Slice(copies, func(left, right int) bool {
		if copies[left].Lines != copies[right].Lines {
			return copies[left].Lines > copies[right].Lines
		}
		if copies[left].Week != copies[right].Week {
			return copies[left].Week < copies[right].Week
		}
		return copies[left].Path < copies[right].Path
	})
	return copies[:min(len(copies), maximumTrendsCopies)]
}

func (collector *trendsCollector) mostRewritten() []TrendsRewrite {
	rewrites := []TrendsRewrite{}
	for _, landing := range collector.landings {
		if landing.rewritten > 0 {
			rewrites = append(rewrites, TrendsRewrite{
				Commit: landing.commit, Week: collector.weeks[landing.week].Week, Subject: landing.subject,
				LinesLanded: landing.lines, LinesRewritten: landing.rewritten, CodePolishy: landing.codePolishy,
			})
		}
	}
	sort.SliceStable(rewrites, func(left, right int) bool { return rewrites[left].LinesRewritten > rewrites[right].LinesRewritten })
	return rewrites[:min(len(rewrites), maximumTrendsRewrites)]
}

func lastTrendsItems[T any](items []T, limit int) []T {
	if len(items) <= limit {
		return items
	}
	return items[len(items)-limit:]
}

func (collector *trendsCollector) recordRevert(commit trendsCommit, week int) {
	match := trendsRevertPattern.FindStringSubmatch(commit.message)
	if match == nil {
		return
	}
	collector.weeks[week].Reverts++
	collector.reverts = append(collector.reverts, TrendsRevert{
		Commit: commit.id, Reverted: strings.ToLower(match[1]), Week: collector.weeks[week].Week,
		Subject: trendsSubject(commit.message),
	})
}

func trendsSubject(message string) string {
	subject, _, _ := strings.Cut(strings.TrimSpace(message), "\n")
	subject = strings.ToValidUTF8(strings.TrimSpace(subject), "�")
	if utf8.RuneCountInString(subject) <= maximumTrendsSubject {
		return subject
	}
	return string([]rune(subject)[:maximumTrendsSubject])
}

func (collector *trendsCollector) recordLockChange(commit trendsCommit, week int) int {
	for _, file := range commit.files {
		if file.oldPath != trendsLockPath && file.newPath != trendsLockPath {
			continue
		}
		change, recorded := trendsLockChangeFor(file)
		if !recorded {
			continue
		}
		change.Commit, change.Date, change.Week = commit.id, commit.time.UTC().Format(time.DateOnly), collector.weeks[week].Week
		collector.lockChanges = append(collector.lockChanges, change)
		collector.version = change.Version
		return len(collector.lockChanges) - 1
	}
	return -1
}

func (repo Repository) trendsLockVersion(revision string) string {
	data, present, err := repo.ReadRegularFileAt(revision, trendsLockPath)
	if err != nil || !present {
		return ""
	}
	if match := trendsVersionPattern.FindSubmatch(data); match != nil {
		return string(match[1])
	}
	return ""
}

func trendsLockChangeFor(file trendsFileDiff) (TrendsLockChange, bool) {
	if file.newPath != trendsLockPath {
		return TrendsLockChange{Kind: "removed"}, true
	}
	previous, version := "", ""
	for _, hunk := range file.hunks {
		previous = lastTrendsVersion(hunk.removed, previous)
		version = lastTrendsVersion(hunk.added, version)
	}
	if file.oldPath != trendsLockPath {
		return TrendsLockChange{Kind: "adopted", Version: version}, true
	}
	if version == "" || version == previous {
		return TrendsLockChange{}, false
	}
	return TrendsLockChange{Kind: trendsVersionDirection(previous, version), Version: version}, true
}

func lastTrendsVersion(lines []string, fallback string) string {
	for _, line := range lines {
		if match := trendsVersionPattern.FindStringSubmatch(line); match != nil {
			fallback = match[1]
		}
	}
	return fallback
}

func trendsVersionDirection(previous, version string) string {
	older, olderParsed := trendsVersionNumbers(previous)
	newer, newerParsed := trendsVersionNumbers(version)
	if !olderParsed || !newerParsed {
		return "changed"
	}
	if slices.Compare(newer, older) < 0 {
		return "downgraded"
	}
	return "upgraded"
}

func trendsVersionNumbers(version string) ([]int, bool) {
	core, _, _ := strings.Cut(version, "+")
	core, _, _ = strings.Cut(core, "-")
	numbers := []int{}
	for _, part := range strings.Split(core, ".") {
		number, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		numbers = append(numbers, number)
	}
	return numbers, true
}
