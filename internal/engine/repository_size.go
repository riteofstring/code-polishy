package engine

import (
	"fmt"
	"math"
	"strconv"

	"github.com/riteofstring/code-polishy/internal/repository"
)

const repositorySizeTableLimit = 10

func (engine *Engine) Size(base string) (Report, error) {
	analysis, err := engine.Repository.AnalyzeSize(base)
	if err != nil {
		return Report{}, err
	}
	notes := []string{
		"repository size is descriptive evidence; project context determines whether it is reasonable",
		"source contents were not included in the size report",
	}
	if analysis.Comparison != nil {
		notes = append(notes, "base comparison uses canonical Git blob sizes for both revisions")
	}
	report := engine.finish(nil, notes)
	report.RepositorySize = &analysis
	report.Tables = repositorySizeTables(analysis)
	return engine.normalizeReport(report), nil
}

func repositorySizeTables(analysis repository.SizeAnalysis) []Table {
	tables := []Table{repositorySizeSummaryTable(analysis)}
	tables = appendSizeGroupTable(tables, "WORKSPACE CATEGORIES", "CATEGORY", analysis.Workspace.Categories, analysis.Workspace.Bytes)
	tables = appendSizeGroupTable(tables, "WORKSPACE TOP LEVEL", "PATH", analysis.Workspace.TopLevel, analysis.Workspace.Bytes)
	tables = appendSizeGroupTable(tables, "GOVERNED CONTENT", "CATEGORY", analysis.Governed.Categories, analysis.Governed.Bytes)
	tables = appendSizeGroupTable(tables, "GOVERNED MODULES", "MODULE", analysis.Governed.Modules, analysis.Governed.Bytes)
	tables = appendSizeGroupTable(tables, "GOVERNED LANGUAGES", "LANGUAGE", analysis.Governed.Languages, analysis.Governed.Bytes)
	tables = appendSizeFileTable(tables, "LARGEST WORKSPACE FILES", analysis.Workspace.LargestFiles)
	tables = appendSizeFileTable(tables, "LARGEST GOVERNED FILES", analysis.Governed.LargestFiles)
	if analysis.Comparison != nil {
		tables = appendSizeDeltaTable(tables, "SIZE CHANGE BY CATEGORY", "CATEGORY", analysis.Comparison.Categories)
		tables = appendSizeDeltaTable(tables, "SIZE CHANGE BY MODULE", "MODULE", analysis.Comparison.Modules)
		tables = appendSizeChangeTable(tables, analysis.Comparison.LargestChanges)
	}
	return tables
}

func repositorySizeSummaryTable(analysis repository.SizeAnalysis) Table {
	rows := [][]string{
		{"workspace", strconv.Itoa(analysis.Workspace.Files), formatSizeBytes(analysis.Workspace.Bytes)},
		{"governed working tree", strconv.Itoa(analysis.Governed.Files), formatSizeBytes(analysis.Governed.Bytes)},
	}
	if comparison := analysis.Comparison; comparison != nil {
		rows = append(rows,
			[]string{"base Git blobs", strconv.Itoa(comparison.Base.Files), formatSizeBytes(comparison.Base.Bytes)},
			[]string{"current Git blobs", strconv.Itoa(comparison.Current.Files), formatSizeBytes(comparison.Current.Bytes)},
			[]string{"change from base", signedSizeCount(comparison.DeltaFiles), formatSignedSizeBytes(comparison.DeltaBytes)},
		)
	}
	return Table{Title: "REPOSITORY SIZE", Columns: []string{"SCOPE", "FILES", "SIZE"}, Rows: rows}
}

func appendSizeGroupTable(tables []Table, title, label string, groups []repository.SizeGroup, total int64) []Table {
	groups = boundedSizeGroups(groups, repositorySizeTableLimit)
	if len(groups) == 0 {
		return tables
	}
	rows := make([][]string, 0, len(groups))
	for _, group := range groups {
		rows = append(rows, []string{group.Name, strconv.Itoa(group.Files), formatSizeBytes(group.Bytes), formatSizeShare(group.Bytes, total)})
	}
	return append(tables, Table{Title: title, Columns: []string{label, "FILES", "SIZE", "SHARE"}, Rows: rows})
}

func appendSizeFileTable(tables []Table, title string, files []repository.SizeFile) []Table {
	if len(files) == 0 {
		return tables
	}
	if len(files) > repositorySizeTableLimit {
		files = files[:repositorySizeTableLimit]
	}
	rows := make([][]string, 0, len(files))
	for _, file := range files {
		rows = append(rows, []string{file.Path, file.Category, formatSizeBytes(file.Bytes)})
	}
	return append(tables, Table{Title: title, Columns: []string{"PATH", "CATEGORY", "SIZE"}, Rows: rows})
}

func appendSizeDeltaTable(tables []Table, title, label string, groups []repository.SizeGroupDelta) []Table {
	if len(groups) == 0 {
		return tables
	}
	if len(groups) > repositorySizeTableLimit {
		groups = groups[:repositorySizeTableLimit]
	}
	rows := make([][]string, 0, len(groups))
	for _, group := range groups {
		rows = append(rows, []string{
			group.Name, formatSizeBytes(group.BaseBytes), formatSizeBytes(group.CurrentBytes),
			formatSignedSizeBytes(group.DeltaBytes), signedSizeCount(group.DeltaFiles),
		})
	}
	return append(tables, Table{Title: title, Columns: []string{label, "BASE", "CURRENT", "CHANGE", "FILES"}, Rows: rows})
}

func appendSizeChangeTable(tables []Table, changes []repository.SizeChange) []Table {
	if len(changes) == 0 {
		return tables
	}
	if len(changes) > repositorySizeTableLimit {
		changes = changes[:repositorySizeTableLimit]
	}
	rows := make([][]string, 0, len(changes))
	for _, change := range changes {
		rows = append(rows, []string{
			change.Path, change.State, formatSizeBytes(change.BaseBytes),
			formatSizeBytes(change.CurrentBytes), formatSignedSizeBytes(change.DeltaBytes),
		})
	}
	return append(tables, Table{Title: "LARGEST SIZE CHANGES", Columns: []string{"PATH", "STATE", "BASE", "CURRENT", "CHANGE"}, Rows: rows})
}

func boundedSizeGroups(groups []repository.SizeGroup, limit int) []repository.SizeGroup {
	if len(groups) <= limit {
		return groups
	}
	bounded := append([]repository.SizeGroup{}, groups[:limit-1]...)
	overflow := repository.SizeGroup{Name: "remaining"}
	for _, group := range groups[limit-1:] {
		overflow.Files += group.Files
		overflow.Bytes += group.Bytes
	}
	return append(bounded, overflow)
}

func formatSizeShare(bytes, total int64) string {
	if total <= 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(bytes)*100/float64(total))
}

func signedSizeCount(count int) string {
	if count > 0 {
		return "+" + strconv.Itoa(count)
	}
	return strconv.Itoa(count)
}

func formatSignedSizeBytes(bytes int64) string {
	if bytes > 0 {
		return "+" + formatSizeBytes(bytes)
	}
	return formatSizeBytes(bytes)
}

func formatSizeBytes(bytes int64) string {
	if bytes == 0 {
		return "0 B"
	}
	sign := ""
	value := bytes
	if value < 0 {
		sign = "-"
		value = -value
	}
	if value < 1024 {
		return sign + strconv.FormatInt(value, 10) + " B"
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	amount := float64(value)
	unit := units[0]
	for index := 0; index < len(units); index++ {
		amount = float64(value) / math.Pow(1024, float64(index+1))
		unit = units[index]
		if amount < 1024 || index == len(units)-1 {
			break
		}
	}
	return fmt.Sprintf("%s%.1f %s", sign, amount, unit)
}
