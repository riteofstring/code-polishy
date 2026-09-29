package engine

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const (
	maximumFlakySuites  = 20
	trendsTableLimit    = 10
	trendsSubjectLength = 72
)

type TrendsOptions struct {
	Branch      string
	Since       time.Time
	RewriteDays int
}

type RepositoryTrends struct {
	History    repository.TrendsAnalysis `json:"history"`
	FlakyTests FlakyTestTrends           `json:"flakyTests"`
}

type FlakyTestTrends struct {
	Worktrees          int              `json:"worktrees"`
	SuiteRuns          int              `json:"suiteRuns"`
	OtherFormatRecords int              `json:"otherFormatRecords"`
	InvalidRecords     int              `json:"invalidRecords"`
	Weeks              []FlakyTestWeek  `json:"weeks"`
	Suites             []FlakyTestSuite `json:"suites"`
}

type FlakyTestWeek struct {
	Week      string `json:"week"`
	SuiteRuns int    `json:"suiteRuns"`
	Flaky     int    `json:"flaky"`
}

type FlakyTestSuite struct {
	Name  string `json:"name"`
	Flaky int    `json:"flaky"`
}

func (engine *Engine) Trends(options TrendsOptions) (Report, error) {
	history, err := engine.Repository.AnalyzeTrends(repository.TrendsRequest{
		Branch: options.Branch, Since: options.Since, AsOf: time.Now().UTC(), RewriteDays: options.RewriteDays,
	})
	if err != nil {
		return Report{}, err
	}
	flaky, err := engine.flakyTestTrends(history)
	if err != nil {
		return Report{}, err
	}
	trends := RepositoryTrends{History: history, FlakyTests: flaky}
	report := engine.normalizeReport(Report{Notes: repositoryTrendsNotes(trends)})
	report.RepositoryTrends = &trends
	report.Tables = repositoryTrendsTables(trends)
	return engine.normalizeReport(report), nil
}

func (engine *Engine) flakyTestTrends(history repository.TrendsAnalysis) (FlakyTestTrends, error) {
	roots, err := engine.Repository.WorktreeRoots()
	if err != nil {
		return FlakyTestTrends{}, err
	}
	outcomes := []gaterun.StoredTestOutcome{}
	otherFormat, invalid := 0, 0
	for _, root := range roots {
		stored, err := gaterun.StoredTestOutcomes(root)
		if err != nil {
			return FlakyTestTrends{}, err
		}
		outcomes = append(outcomes, stored.Outcomes...)
		otherFormat += stored.OtherFormat
		invalid += stored.Invalid
	}
	trends := summarizeFlakyTests(outcomes, history)
	trends.Worktrees, trends.OtherFormatRecords, trends.InvalidRecords = len(roots), otherFormat, invalid
	return trends, nil
}

func summarizeFlakyTests(outcomes []gaterun.StoredTestOutcome, history repository.TrendsAnalysis) FlakyTestTrends {
	sort.SliceStable(outcomes, func(left, right int) bool { return outcomes[left].StartedAt.Before(outcomes[right].StartedAt) })
	trends := FlakyTestTrends{Weeks: make([]FlakyTestWeek, len(history.Weeks))}
	for index, week := range history.Weeks {
		trends.Weeks[index].Week = week.Week
	}
	suites := map[string]int{}
	pending := map[string]time.Time{}
	for _, outcome := range outcomes {
		if index, found := history.WeekIndex(outcome.StartedAt); found {
			trends.Weeks[index].SuiteRuns++
			trends.SuiteRuns++
		}
		failedAt, flaky := flakyTestEvent(outcome, pending)
		if !flaky {
			continue
		}
		if index, found := history.WeekIndex(failedAt); found {
			trends.Weeks[index].Flaky++
			suites[outcome.Command.Name]++
		}
	}
	trends.Suites = rankedFlakySuites(suites)
	return trends
}

func flakyTestEvent(outcome gaterun.StoredTestOutcome, pending map[string]time.Time) (time.Time, bool) {
	key := outcome.Candidate + "\x00" + outcome.Command.Name
	failed, recovered := testAttemptRecovery(outcome.Command.Attempts)
	if recovered {
		delete(pending, key)
		return outcome.StartedAt, true
	}
	if failed {
		if _, exists := pending[key]; !exists && outcome.Candidate != "" {
			pending[key] = outcome.StartedAt
		}
		return time.Time{}, false
	}
	failedAt, exists := pending[key]
	if !exists || outcome.Command.Status != gaterun.Passed {
		return time.Time{}, false
	}
	delete(pending, key)
	return failedAt, true
}

func testAttemptRecovery(attempts []gaterun.Attempt) (bool, bool) {
	failed := false
	for _, attempt := range attempts {
		if attempt.Diagnostic {
			continue
		}
		if attempt.Status == gaterun.Failed {
			failed = failed || attempt.FailureCategory == gaterun.CommandExit || attempt.FailureCategory == gaterun.Timeout
			continue
		}
		if failed {
			return true, true
		}
	}
	return failed, false
}

func rankedFlakySuites(counts map[string]int) []FlakyTestSuite {
	suites := make([]FlakyTestSuite, 0, len(counts))
	for name, flaky := range counts {
		suites = append(suites, FlakyTestSuite{Name: name, Flaky: flaky})
	}
	sort.Slice(suites, func(left, right int) bool {
		if suites[left].Flaky != suites[right].Flaky {
			return suites[left].Flaky > suites[right].Flaky
		}
		return suites[left].Name < suites[right].Name
	})
	return suites[:min(len(suites), maximumFlakySuites)]
}

func repositoryTrendsNotes(trends RepositoryTrends) []string {
	history := trends.History
	copies := "working copies"
	if trends.FlakyTests.Worktrees == 1 {
		copies = "working copy"
	}
	notes := []string{
		fmt.Sprintf("trends follow the first-parent history of %s (%s) from the week of %s", history.Branch, history.Head[:12], history.Since),
		fmt.Sprintf("rewritten: share of new lines changed or deleted within %d days of landing; \"so far\" marks weeks still inside that period", history.RewriteDays),
		"copied: share of new lines inside blocks of 6 or more lines that already existed elsewhere; moved code is not counted",
		fmt.Sprintf("flaky tests cover gate runs from every branch in %d %s of this repository on this machine, not only the analyzed branch", trends.FlakyTests.Worktrees, copies),
		"trends describe how the repository changed; they do not show what caused a change",
	}
	if slices.ContainsFunc(history.LockChanges, func(change repository.TrendsLockChange) bool { return change.LinesRewritten > 0 }) {
		notes = append(notes, "version-change commits changed the Code Polishy version and also changed code; rewrote counts recent lines they changed or deleted, which can include unrelated work in the same commit")
	}
	if trendsIncludeWeeksBeforeAdoption(history) {
		notes = append(notes, "weeks before Code Polishy was adopted classify files and modules with today's configuration, so their rewrite rate, copied share, and module spread are less reliable; check whether their most rewritten and copied files are generated code")
	}
	if trends.FlakyTests.OtherFormatRecords > 0 {
		notes = append(notes, fmt.Sprintf("%d gate records use another Code Polishy report format and were skipped", trends.FlakyTests.OtherFormatRecords))
	}
	if trends.FlakyTests.InvalidRecords > 0 {
		notes = append(notes, fmt.Sprintf("%d gate records were incomplete or failed validation and were skipped", trends.FlakyTests.InvalidRecords))
	}
	if history.SkippedChanges > 0 {
		notes = append(notes, fmt.Sprintf("%d file changes did not match the tracked history and were left out", history.SkippedChanges))
	}
	return notes
}

func trendsIncludeWeeksBeforeAdoption(history repository.TrendsAnalysis) bool {
	for _, week := range history.Weeks {
		if week.Changes > 0 && week.CodePolishyVersion == "" {
			return true
		}
	}
	return false
}

func repositoryTrendsTables(trends RepositoryTrends) []Table {
	tables := []Table{repositoryTrendsWeekTable(trends)}
	tables = appendTrendsRewriteTable(tables, trends.History.Rewrites)
	tables = appendCodePolishyChangeTable(tables, trends.History.LockChanges)
	tables = appendTrendsCopyTable(tables, trends.History.Copies)
	tables = appendTrendsRevertTable(tables, trends.History.Reverts)
	return appendFlakySuiteTable(tables, trends.FlakyTests.Suites)
}

func repositoryTrendsWeekTable(trends RepositoryTrends) Table {
	rows := make([][]string, 0, len(trends.History.Weeks))
	for index, week := range trends.History.Weeks {
		flaky := FlakyTestWeek{}
		if index < len(trends.FlakyTests.Weeks) {
			flaky = trends.FlakyTests.Weeks[index]
		}
		rows = append(rows, []string{
			week.Week, strconv.Itoa(week.Changes), strconv.Itoa(week.LinesLanded), trendsRewriteCell(week),
			trendsShare(week.LinesCopied, week.LinesLanded), trendsSpreadCell(week), strconv.Itoa(week.Reverts), trendsFlakyCell(flaky),
			trendsVersionCell(week.CodePolishyVersion),
		})
	}
	return Table{
		Title:   "REPOSITORY TRENDS",
		Columns: []string{"WEEK", "CHANGES", "NEW LINES", "REWRITTEN", "COPIED", "MODULES/CHANGE", "REVERTS", "FLAKY TESTS", "CODE POLISHY"},
		Rows:    rows,
	}
}

func trendsRewriteCell(week repository.TrendsWeek) string {
	share := trendsShare(week.LinesRewritten, week.LinesLanded)
	if share != "-" && !week.RewriteComplete {
		return share + " so far"
	}
	return share
}

func trendsShare(part, total int) string {
	if total <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", float64(part)*100/float64(total))
}

func trendsSpreadCell(week repository.TrendsWeek) string {
	if week.ModuleChanges == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", float64(week.ModulesTouched)/float64(week.ModuleChanges))
}

func trendsVersionCell(version string) string {
	if version == "" {
		return "-"
	}
	return version
}

func trendsFlakyCell(week FlakyTestWeek) string {
	if week.SuiteRuns == 0 {
		return "-"
	}
	return fmt.Sprintf("%d of %d", week.Flaky, week.SuiteRuns)
}

func appendTrendsRewriteTable(tables []Table, rewrites []repository.TrendsRewrite) []Table {
	if len(rewrites) == 0 {
		return tables
	}
	rows := [][]string{}
	for _, rewrite := range rewrites[:min(len(rewrites), trendsTableLimit)] {
		rows = append(rows, []string{
			rewrite.Week, rewrite.Commit[:12], trendsTableSubject(rewrite.Subject),
			strconv.Itoa(rewrite.LinesLanded), strconv.Itoa(rewrite.LinesRewritten), trendsVersionChangeCell(rewrite.CodePolishy),
		})
	}
	return append(tables, Table{
		Title: "MOST REWRITTEN CHANGES", Columns: []string{"WEEK", "COMMIT", "SUBJECT", "NEW LINES", "REWRITTEN", "CODE POLISHY"}, Rows: rows,
	})
}

func trendsVersionChangeCell(change *repository.TrendsVersionChange) string {
	if change == nil {
		return "-"
	}
	return change.Kind + " " + change.Version
}

func appendCodePolishyChangeTable(tables []Table, changes []repository.TrendsLockChange) []Table {
	changed := []repository.TrendsLockChange{}
	for _, change := range changes {
		if change.LinesLanded > 0 || change.LinesRewritten > 0 {
			changed = append(changed, change)
		}
	}
	if len(changed) == 0 {
		return tables
	}
	sort.SliceStable(changed, func(left, right int) bool { return changed[left].LinesRewritten > changed[right].LinesRewritten })
	rows := [][]string{}
	for _, change := range changed[:min(len(changed), trendsTableLimit)] {
		rows = append(rows, []string{
			change.Date, change.Commit[:12], change.Kind, change.Version,
			strconv.Itoa(change.LinesLanded), strconv.Itoa(change.LinesRewritten),
		})
	}
	return append(tables, Table{
		Title: "CODE POLISHY VERSION-CHANGE COMMITS", Columns: []string{"DATE", "COMMIT", "CHANGE", "VERSION", "NEW LINES", "REWROTE"}, Rows: rows,
	})
}

func trendsTableSubject(subject string) string {
	characters := []rune(subject)
	if len(characters) <= trendsSubjectLength {
		return subject
	}
	return string(characters[:trendsSubjectLength-3]) + "..."
}

func appendTrendsCopyTable(tables []Table, copies []repository.TrendsCopy) []Table {
	if len(copies) == 0 {
		return tables
	}
	rows := [][]string{}
	for _, copied := range copies[:min(len(copies), trendsTableLimit)] {
		rows = append(rows, []string{copied.Week, copied.Path, strconv.Itoa(copied.Lines)})
	}
	return append(tables, Table{Title: "LARGEST COPIES", Columns: []string{"WEEK", "PATH", "COPIED LINES"}, Rows: rows})
}

func appendTrendsRevertTable(tables []Table, reverts []repository.TrendsRevert) []Table {
	if len(reverts) == 0 {
		return tables
	}
	rows := [][]string{}
	for _, revert := range reverts[max(len(reverts)-trendsTableLimit, 0):] {
		rows = append(rows, []string{revert.Week, revert.Commit[:12], trendsTableSubject(revert.Subject)})
	}
	return append(tables, Table{Title: "REVERTS", Columns: []string{"WEEK", "COMMIT", "SUBJECT"}, Rows: rows})
}

func appendFlakySuiteTable(tables []Table, suites []FlakyTestSuite) []Table {
	if len(suites) == 0 {
		return tables
	}
	rows := [][]string{}
	for _, suite := range suites[:min(len(suites), trendsTableLimit)] {
		rows = append(rows, []string{suite.Name, strconv.Itoa(suite.Flaky)})
	}
	return append(tables, Table{Title: "FLAKY TEST SUITES", Columns: []string{"SUITE", "FLAKY FAILURES"}, Rows: rows})
}
