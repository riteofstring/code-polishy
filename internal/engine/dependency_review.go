package engine

import "github.com/riteofstring/code-polishy/internal/supplychain"

func dependencyReviewTables(changes []supplychain.DependencyChange) []Table {
	removed := []supplychain.DependencyChange{}
	remaining := []supplychain.DependencyChange{}
	for _, change := range changes {
		if change.Change == "removed" {
			removed = append(removed, change)
		} else {
			remaining = append(remaining, change)
		}
	}
	tables := []Table{}
	if len(removed) > 0 {
		table := dependencyReviewTable(removed)
		table.Title = "DEPENDENCIES REMOVED"
		tables = append(tables, table)
	}
	if len(remaining) > 0 || len(removed) == 0 {
		tables = append(tables, dependencyReviewTable(remaining))
	}
	return tables
}

func dependencyReviewTable(changes []supplychain.DependencyChange) Table {
	rows := make([][]string, 0, len(changes))
	for _, change := range changes {
		usage := change.Usage
		if change.PackageAPI {
			usage += " (package API)"
		}
		rows = append(rows, []string{
			change.Ecosystem + "/" + change.Directness, usage, change.Change,
			change.Package, change.From, change.To, change.Scope,
		})
	}
	return Table{
		Title: "DEPENDENCY REVIEW", Columns: []string{"KIND", "USE", "CHANGE", "PACKAGE", "FROM", "TO", "SCOPE"}, Rows: rows,
	}
}
