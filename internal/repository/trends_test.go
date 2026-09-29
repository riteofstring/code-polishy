package repository

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestAnalyzeTrendsCountsLinesRewrittenWithinTheWatchPeriod(t *testing.T) {
	t.Parallel()
	repo := newRewriteTrendsRepository(t)

	analysis := analyzeTrendsAt(t, repo, "", trendsDate(1, 0), trendsDate(29, 12))

	weeks := trendsWeeksByStart(analysis)
	if first := weeks["2026-06-01"]; first.LinesLanded != 12 || first.LinesRewritten != 2 || !first.RewriteComplete || first.Changes != 2 {
		t.Fatalf("first week = %+v", first)
	}
	if rename := weeks["2026-06-08"]; rename.LinesLanded != 0 || rename.LinesRewritten != 0 || rename.Changes != 1 {
		t.Fatalf("rename week = %+v", rename)
	}
	if late := weeks["2026-06-22"]; late.LinesLanded != 1 || late.LinesRewritten != 0 || late.RewriteComplete {
		t.Fatalf("late week = %+v", late)
	}
	if analysis.Changes != 4 || analysis.SkippedChanges != 0 || analysis.Since != "2026-06-01" || len(analysis.Weeks) != 5 || analysis.RewriteDays != 14 {
		t.Fatalf("analysis = %+v", analysis)
	}
}

func TestAnalyzeTrendsCountsLaterRewritesWithALongerWatchPeriod(t *testing.T) {
	t.Parallel()
	repo := newRewriteTrendsRepository(t)

	analysis, err := repo.AnalyzeTrends(TrendsRequest{Since: trendsDate(1, 0), AsOf: trendsDate(29, 12), RewriteDays: 30})
	if err != nil {
		t.Fatal(err)
	}

	if first := trendsWeeksByStart(analysis)["2026-06-01"]; first.LinesRewritten != 3 || first.RewriteComplete || analysis.RewriteDays != 30 {
		t.Fatalf("first week = %+v, rewrite days = %d", first, analysis.RewriteDays)
	}
}

func TestAnalyzeTrendsNamesTheChangesWhoseLinesWereRewritten(t *testing.T) {
	t.Parallel()
	repo := newRewriteTrendsRepository(t)
	first := strings.Fields(gitOutput(t, repo.Root, "rev-list", "--reverse", "HEAD"))[0]

	analysis := analyzeTrendsAt(t, repo, "", trendsDate(1, 0), trendsDate(29, 12))

	want := TrendsRewrite{Commit: first, Week: "2026-06-01", Subject: "add application", LinesLanded: 10, LinesRewritten: 2}
	if len(analysis.Rewrites) != 1 || analysis.Rewrites[0] != want {
		t.Fatalf("rewrites = %+v", analysis.Rewrites)
	}
}

func TestAnalyzeTrendsFollowsFilesMovedIntoTrackedSource(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	repo.Config = policy.Config{Scope: policy.Scope{Generated: []string{"generated/**"}}}
	lines := trendsSourceLines("moved", 5)
	writeFile(t, repo.Root, "generated/app.go", strings.Join(lines, "\n")+"\n")
	commitTrendsAt(t, repo.Root, "generate application", trendsDate(1, 9))
	lines[1] = "adopted := 1"
	writeFile(t, repo.Root, "src/app.go", strings.Join(lines, "\n")+"\n")
	if err := os.Remove(filepath.Join(repo.Root, "generated", "app.go")); err != nil {
		t.Fatal(err)
	}
	commitTrendsAt(t, repo.Root, "adopt generated code", trendsDate(2, 9))
	lines[1] = "adopted := 2"
	writeFile(t, repo.Root, "src/app.go", strings.Join(lines, "\n")+"\n")
	commitTrendsAt(t, repo.Root, "revise adopted code", trendsDate(3, 9))

	analysis := analyzeTrendsAt(t, repo, "", trendsDate(2, 0), trendsDate(29, 12))

	week := trendsWeeksByStart(analysis)["2026-06-01"]
	if week.LinesLanded != 2 || week.LinesRewritten != 1 || analysis.SkippedChanges != 0 {
		t.Fatalf("week = %+v, skipped = %d", week, analysis.SkippedChanges)
	}
}

func TestAnalyzeTrendsSeparatesReworkFromCodePolishyChanges(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	lines := trendsSourceLines("app", 6)
	writeFile(t, repo.Root, trendsLockPath, `{"codePolishyVersion": "0.1.0"}`+"\n")
	writeFile(t, repo.Root, "src/app.go", strings.Join(lines, "\n")+"\n")
	commitTrendsAt(t, repo.Root, "adopt Code Polishy", trendsDate(1, 9))
	lines[0], lines[1] = "upgraded := 0", "upgraded := 1"
	writeFile(t, repo.Root, trendsLockPath, `{"codePolishyVersion": "0.2.0"}`+"\n")
	writeFile(t, repo.Root, "src/app.go", strings.Join(lines, "\n")+"\n")
	commitTrendsAt(t, repo.Root, "upgrade Code Polishy", trendsDate(3, 9))
	lines[0] = "edited := 0"
	writeFile(t, repo.Root, "src/app.go", strings.Join(lines, "\n")+"\n")
	commitTrendsAt(t, repo.Root, "edit application", trendsDate(4, 9))

	analysis := analyzeTrendsAt(t, repo, "", trendsDate(1, 0), trendsDate(29, 12))

	if len(analysis.LockChanges) != 2 || analysis.LockChanges[1].LinesLanded != 2 || analysis.LockChanges[1].LinesRewritten != 2 {
		t.Fatalf("lock changes = %+v", analysis.LockChanges)
	}
	upgrade := TrendsVersionChange{Kind: "upgraded", Version: "0.2.0"}
	if len(analysis.Rewrites) != 2 || analysis.Rewrites[1].CodePolishy == nil || *analysis.Rewrites[1].CodePolishy != upgrade {
		t.Fatalf("rewrites = %+v", analysis.Rewrites)
	}
}

func newRewriteTrendsRepository(t *testing.T) Repository {
	t.Helper()
	repo := newGitRepository(t)
	lines := trendsSourceLines("value", 10)
	writeFile(t, repo.Root, "src/my app.go", strings.Join(lines, "\n")+"\n")
	writeFile(t, repo.Root, "docs/guide.md", "# Guide\n")
	commitTrendsAt(t, repo.Root, "add application", trendsDate(1, 9))
	edited := append([]string{}, lines...)
	edited[1], edited[2], edited[5] = "changed := 1", "changed := 2", "    "+lines[5]
	writeFile(t, repo.Root, "src/my app.go", strings.Join(edited, "\n")+"\n")
	writeFile(t, repo.Root, "docs/guide.md", "# Guide\n\nMore prose.\n")
	commitTrendsAt(t, repo.Root, "revise application", trendsDate(5, 9))
	git(t, repo.Root, "mv", "src/my app.go", "src/main.go")
	commitTrendsAt(t, repo.Root, "rename application", trendsDate(10, 9))
	edited[8] = "late := 8"
	writeFile(t, repo.Root, "src/main.go", strings.Join(edited, "\n")+"\n")
	commitTrendsAt(t, repo.Root, "late edit", trendsDate(24, 9))
	return repo
}

func TestAnalyzeTrendsCountsCopiedBlocksButNotMovedCode(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	copiedBlock := trendsSourceLines("copied", 8)
	movedBlock := trendsSourceLines("moved", 8)
	writeFile(t, repo.Root, "src/a.go", strings.Join(append(append([]string{}, copiedBlock...), movedBlock...), "\n")+"\n")
	commitTrendsAt(t, repo.Root, "add blocks", trendsDate(1, 9))
	writeFile(t, repo.Root, "src/b.go", strings.Join(append([]string{"fresh := 1", "}"}, copiedBlock...), "\n")+"\n")
	commitTrendsAt(t, repo.Root, "copy a block", trendsDate(2, 9))
	writeFile(t, repo.Root, "src/a.go", strings.Join(copiedBlock, "\n")+"\n")
	writeFile(t, repo.Root, "src/c.go", strings.Join(movedBlock, "\n")+"\n")
	commitTrendsAt(t, repo.Root, "move a block", trendsDate(3, 9))

	analysis := analyzeTrendsAt(t, repo, "", trendsDate(1, 0), trendsDate(29, 12))

	week := trendsWeeksByStart(analysis)["2026-06-01"]
	if week.LinesCopied != len(copiedBlock) {
		t.Fatalf("copied lines = %d, week = %+v", week.LinesCopied, week)
	}
	if len(analysis.Copies) != 1 || analysis.Copies[0].Path != "src/b.go" || analysis.Copies[0].Lines != len(copiedBlock) {
		t.Fatalf("copies = %+v", analysis.Copies)
	}
}

func TestAnalyzeTrendsFollowsTheRequestedBranch(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	repo.Config = policy.Config{Modules: []policy.Module{
		{Name: "app", Paths: []string{"src/app/**"}}, {Name: "lib", Paths: []string{"src/lib/**"}},
	}}
	writeFile(t, repo.Root, trendsLockPath, `{"codePolishyVersion": "0.1.0"}`+"\n")
	writeFile(t, repo.Root, "src/app/app.go", strings.Join(trendsSourceLines("app", 4), "\n")+"\n")
	commitTrendsAt(t, repo.Root, "adopt Code Polishy", trendsDate(1, 9))
	git(t, repo.Root, "checkout", "-b", "feature")
	writeFile(t, repo.Root, "src/app/app.go", strings.Join(trendsSourceLines("app", 5), "\n")+"\n")
	writeFile(t, repo.Root, "src/lib/lib.go", strings.Join(trendsSourceLines("lib", 3), "\n")+"\n")
	commitTrendsAt(t, repo.Root, "span both modules", trendsDate(9, 9))
	spanning := strings.TrimSpace(gitOutput(t, repo.Root, "rev-parse", "HEAD"))
	revertTrendsAt(t, repo.Root, spanning, trendsDate(10, 9))
	writeFile(t, repo.Root, trendsLockPath, `{"codePolishyVersion": "0.2.0"}`+"\n")
	commitTrendsAt(t, repo.Root, "upgrade Code Polishy", trendsDate(16, 9))
	writeFile(t, repo.Root, trendsLockPath, `{"codePolishyVersion": "0.1.5"}`+"\n")
	commitTrendsAt(t, repo.Root, "return to an earlier Code Polishy", trendsDate(23, 9))
	git(t, repo.Root, "checkout", "main")

	onMain := analyzeTrendsAt(t, repo, "", trendsDate(1, 0), trendsDate(29, 12))
	if onMain.Changes != 1 || len(onMain.Reverts) != 0 {
		t.Fatalf("main analysis = %+v", onMain)
	}
	analysis := analyzeTrendsAt(t, repo, "feature", trendsDate(1, 0), trendsDate(29, 12))

	weeks := trendsWeeksByStart(analysis)
	if spread := weeks["2026-06-08"]; spread.ModuleChanges != 2 || spread.ModulesTouched != 4 || spread.Reverts != 1 {
		t.Fatalf("spread week = %+v", spread)
	}
	if len(analysis.Reverts) != 1 || analysis.Reverts[0].Reverted != spanning || analysis.Reverts[0].Week != "2026-06-08" {
		t.Fatalf("reverts = %+v", analysis.Reverts)
	}
	versions := []string{}
	for _, week := range analysis.Weeks {
		versions = append(versions, week.CodePolishyVersion)
	}
	if strings.Join(versions, ",") != "0.1.0,0.1.0,0.2.0,0.1.5,0.1.5" {
		t.Fatalf("versions = %v, lock changes = %+v", versions, analysis.LockChanges)
	}
	kinds := []string{}
	for _, change := range analysis.LockChanges {
		kinds = append(kinds, change.Kind)
	}
	if strings.Join(kinds, ",") != "adopted,upgraded,downgraded" {
		t.Fatalf("lock changes = %+v", analysis.LockChanges)
	}
}

func TestAnalyzeTrendsDefaultsToTwelveWeeksFromTheCurrentBranch(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "src/app.go", "package app\n")
	commitTrendsAt(t, repo.Root, "start", trendsDate(1, 9))

	analysis := analyzeTrendsAt(t, repo, "", time.Time{}, time.Date(2026, 9, 2, 15, 0, 0, 0, time.UTC))

	if analysis.Branch != "HEAD" || analysis.Since != "2026-06-15" || len(analysis.Weeks) != DefaultTrendsWeeks || analysis.Changes != 0 {
		t.Fatalf("analysis = %+v", analysis)
	}
	if index, found := analysis.WeekIndex(time.Date(2026, 9, 6, 23, 0, 0, 0, time.UTC)); !found || index != DefaultTrendsWeeks-1 {
		t.Fatalf("week index = %d, %v", index, found)
	}
}

func TestAnalyzeTrendsRequiresHistoryBeforeTheWindowInShallowClones(t *testing.T) {
	t.Parallel()
	source := newGitRepository(t)
	for day := range 3 {
		writeFile(t, source.Root, "src/app.go", strings.Join(trendsSourceLines("app", day+1), "\n")+"\n")
		commitTrendsAt(t, source.Root, fmt.Sprintf("change %d", day), trendsDate(1+7*day, 9))
	}
	clone := Repository{Root: t.TempDir()}
	git(t, clone.Root, "clone", "-q", "--depth", "2", "file://"+source.Root, ".")

	if _, err := clone.AnalyzeTrends(TrendsRequest{Since: trendsDate(1, 0), AsOf: trendsDate(29, 12)}); err == nil || !strings.Contains(err.Error(), "shallow") {
		t.Fatalf("shallow window error = %v", err)
	}
	analysis := analyzeTrendsAt(t, clone, "", trendsDate(15, 0), trendsDate(29, 12))
	if analysis.Changes != 1 || trendsWeeksByStart(analysis)["2026-06-15"].LinesLanded != 1 {
		t.Fatalf("analysis after the shallow boundary = %+v", analysis)
	}
}

func TestAnalyzeTrendsIgnoresUserDiffSettingsForSubmodulesAndContext(t *testing.T) {
	t.Setenv("GIT_DIFF_OPTS", "--unified=3")
	repo := newGitRepository(t)
	git(t, repo.Root, "config", "diff.submodule", "log")
	lines := trendsSourceLines("app", 3)
	writeFile(t, repo.Root, "src/app.go", strings.Join(lines, "\n")+"\n")
	git(t, repo.Root, "add", "src/app.go")
	git(t, repo.Root, "update-index", "--add", "--cacheinfo", "160000,"+strings.Repeat("a", 40)+",vendor/library")
	runTrendsGit(t, repo.Root, trendsDate(1, 9), "commit", "-q", "-m", "add application and library")
	lines[1] = "changed := 1"
	writeFile(t, repo.Root, "src/app.go", strings.Join(lines, "\n")+"\n")
	git(t, repo.Root, "add", "src/app.go")
	git(t, repo.Root, "update-index", "--add", "--cacheinfo", "160000,"+strings.Repeat("b", 40)+",vendor/library")
	runTrendsGit(t, repo.Root, trendsDate(2, 9), "commit", "-q", "-m", "update application and library")

	analysis := analyzeTrendsAt(t, repo, "", trendsDate(1, 0), trendsDate(29, 12))

	week := trendsWeeksByStart(analysis)["2026-06-01"]
	if week.LinesLanded != 4 || week.LinesRewritten != 1 || analysis.SkippedChanges != 0 {
		t.Fatalf("week = %+v, skipped = %d", week, analysis.SkippedChanges)
	}
}

func TestAnalyzeTrendsRejectsInvalidRequests(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "src/app.go", "package app\n")
	commitTrendsAt(t, repo.Root, "start", trendsDate(1, 9))
	asOf := trendsDate(29, 12)
	requests := []TrendsRequest{
		{Branch: "-p", AsOf: asOf},
		{Branch: "missing", AsOf: asOf},
		{Since: asOf.AddDate(0, 0, 14), AsOf: asOf},
		{Since: asOf.AddDate(-3, 0, 0), AsOf: asOf},
		{AsOf: asOf, RewriteDays: -1},
		{AsOf: asOf, RewriteDays: MaximumTrendsRewriteDays + 1},
		{},
	}
	for _, request := range requests {
		if _, err := repo.AnalyzeTrends(request); err == nil {
			t.Errorf("request %+v was accepted", request)
		}
	}
}

func analyzeTrendsAt(t *testing.T, repo Repository, branch string, since, asOf time.Time) TrendsAnalysis {
	t.Helper()
	analysis, err := repo.AnalyzeTrends(TrendsRequest{Branch: branch, Since: since, AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	return analysis
}

func trendsWeeksByStart(analysis TrendsAnalysis) map[string]TrendsWeek {
	weeks := map[string]TrendsWeek{}
	for _, week := range analysis.Weeks {
		weeks[week.Week] = week
	}
	return weeks
}

func trendsSourceLines(prefix string, count int) []string {
	lines := make([]string, 0, count)
	for index := range count {
		lines = append(lines, fmt.Sprintf("%sValue%d := computeResult(%d, \"%s\")", prefix, index, index, prefix))
	}
	return lines
}

func trendsDate(day, hour int) time.Time {
	return time.Date(2026, 6, day, hour, 0, 0, 0, time.UTC)
}

func commitTrendsAt(t *testing.T, root, message string, at time.Time) {
	t.Helper()
	git(t, root, "add", "-A")
	runTrendsGit(t, root, at, "commit", "-q", "-m", message)
}

func revertTrendsAt(t *testing.T, root, commit string, at time.Time) {
	t.Helper()
	runTrendsGit(t, root, at, "revert", "--no-edit", commit)
}

func runTrendsGit(t *testing.T, root string, at time.Time, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	stamp := at.Format(time.RFC3339)
	command.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
