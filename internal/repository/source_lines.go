package repository

import (
	"bytes"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type SourceLines struct {
	Production int               `json:"production"`
	Test       int               `json:"test"`
	Languages  []SourceLineGroup `json:"languages"`
	Budget     []LineBudgetScope `json:"budget"`
}

type SourceLineGroup struct {
	Name       string `json:"name"`
	Production int    `json:"production"`
	Test       int    `json:"test"`
}

type LineBudgetScope struct {
	Scope  string `json:"scope"`
	Lines  int    `json:"lines"`
	Budget int    `json:"budget"`
}

func (scope LineBudgetScope) Exceeded() bool {
	return scope.Lines > scope.Budget
}

func (repo Repository) CountsSourceLines(path string) bool {
	return repo.countsSourcePath(path) && repo.IsExecutableSource(path)
}

func (repo Repository) countsSourcePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".json", ".jsonc", ".yaml", ".yml", ".toml", ".html", ".htm", ".css", ".sql", ".proto":
		return false
	}
	name := strings.ToLower(filepath.Base(path))
	if name == "go.sum" || name == "go.work.sum" || strings.HasSuffix(name, ".lock") || name == "license" {
		return false
	}
	return !repo.IsGenerated(path) && !repo.IsData(path) && !repo.IsSymbolicLink(path)
}

func (repo Repository) CountSourceLines() (SourceLines, error) {
	sparse, err := repo.sparseIndexSources()
	if err != nil {
		return SourceLines{}, err
	}
	paths, err := repo.AllFiles()
	if err != nil {
		return SourceLines{}, err
	}
	lines := SourceLines{}
	groups := map[string]*SourceLineGroup{}
	for _, path := range paths {
		if _, indexed := sparse[path]; indexed || !repo.CountsSourceLines(path) {
			continue
		}
		data, err := repo.Read(path)
		if err != nil {
			return SourceLines{}, err
		}
		repo.addSourceLines(&lines, groups, path, data)
	}
	for path, data := range sparse {
		repo.addSourceLines(&lines, groups, path, data)
	}
	lines.Languages = boundedSourceLineGroups(groups)
	lines.Budget = lineBudgetScopes(repo.Config.Quality.LineBudget, lines)
	return lines, nil
}

func (repo Repository) addSourceLines(lines *SourceLines, groups map[string]*SourceLineGroup, path string, data []byte) {
	if bytes.IndexByte(data, 0) >= 0 {
		return
	}
	count := nonBlankLines(data)
	name := repo.sourceLineLanguage(path, data)
	if groups[name] == nil {
		groups[name] = &SourceLineGroup{Name: name}
	}
	if repo.IsTest(path) {
		lines.Test += count
		groups[name].Test += count
	} else {
		lines.Production += count
		groups[name].Production += count
	}
}

func (repo Repository) sparseIndexSources() (map[string][]byte, error) {
	sources := map[string][]byte{}
	if !repo.hasGit() {
		return sources, nil
	}
	entries, err := repo.gitLines("ls-files", "-z", "-t", "--stage")
	if err != nil {
		return nil, err
	}
	objectIDs := map[string]string{}
	for _, entry := range entries {
		if path, objectID, sparse := sparseRegularIndexEntry(entry); sparse && !repo.IsExcluded(path) && repo.countsSourcePath(path) {
			objectIDs[path] = objectID
		}
	}
	contents, err := repo.gitBlobContents(slices.Collect(maps.Values(objectIDs)))
	if err != nil {
		return nil, err
	}
	for path, objectID := range objectIDs {
		if data := contents[objectID]; repo.IsExecutableSource(path) || shellShebang(data) {
			sources[path] = data
		}
	}
	return sources, nil
}

func boundedSourceLineGroups(groups map[string]*SourceLineGroup) []SourceLineGroup {
	ordered := make([]SourceLineGroup, 0, len(groups))
	for _, group := range groups {
		ordered = append(ordered, *group)
	}
	sort.Slice(ordered, func(left, right int) bool {
		leftLines := ordered[left].Production + ordered[left].Test
		rightLines := ordered[right].Production + ordered[right].Test
		if leftLines != rightLines {
			return leftLines > rightLines
		}
		return ordered[left].Name < ordered[right].Name
	})
	if len(ordered) <= maximumSizeGroups {
		return ordered
	}
	remaining := SourceLineGroup{Name: "remaining-languages"}
	for _, group := range ordered[maximumSizeGroups-1:] {
		remaining.Production += group.Production
		remaining.Test += group.Test
	}
	return append(ordered[:maximumSizeGroups-1], remaining)
}

func nonBlankLines(data []byte) int {
	count := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			count++
		}
	}
	return count
}

func lineBudgetScopes(budget *policy.LineBudget, lines SourceLines) []LineBudgetScope {
	scopes := []LineBudgetScope{}
	if budget == nil {
		return scopes
	}
	for _, scope := range []LineBudgetScope{
		{Scope: "production", Lines: lines.Production, Budget: budget.Production},
		{Scope: "test", Lines: lines.Test, Budget: budget.Test},
		{Scope: "total", Lines: lines.Production + lines.Test, Budget: budget.Total},
	} {
		if scope.Budget > 0 {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

func sparseRegularIndexEntry(entry string) (string, string, bool) {
	tag, staged, _ := strings.Cut(entry, " ")
	metadata, path, found := strings.Cut(staged, "\t")
	fields := strings.Fields(metadata)
	regular := len(fields) == 3 && (fields[0] == "100644" || fields[0] == "100755")
	if tag != "S" || !found || !regular {
		return "", "", false
	}
	return path, fields[1], true
}

func (repo Repository) sourceLineLanguage(path string, data []byte) string {
	languages := repo.Languages(path)
	switch {
	case len(languages) == 1:
		return languages[0]
	case len(languages) > 1:
		sorted := append([]string{}, languages...)
		sort.Strings(sorted)
		return "multi:" + strings.Join(sorted, ",")
	case shellShebang(data):
		return "shell"
	default:
		return strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}
}
