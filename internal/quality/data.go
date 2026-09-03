package quality

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const structuredDataMaximumBytes int64 = 4 * 1024 * 1024

const javascriptParseBudget = 2 * time.Minute

func DataSyntaxFindings(ctx context.Context, repo repository.Repository, files []string) []policy.Finding {
	selected := dataFiles(repo, files)
	findings := []policy.Finding{}
	readable := []string{}
	for _, path := range selected {
		if finding, valid := readableDataFile(repo, path); valid {
			readable = append(readable, path)
		} else {
			findings = append(findings, finding)
		}
	}
	if len(readable) == 0 {
		return findings
	}
	parseContext, cancel := context.WithTimeout(ctx, javascriptParseBudget)
	defer cancel()
	result, err := (javascript.Bundle{PolicyRoot: repo.PolicyRoot}).Parse(parseContext, repo.Root, readable)
	if err != nil {
		return append(findings, toolFinding("javascript-bundle", err.Error()))
	}
	for _, entry := range result.Unsupported {
		findings = append(findings, policy.Finding{
			Check: "quality.dataSyntax", Path: entry.Path, Subject: dataFormat(entry.Path),
			Message: "the parse-only data validator could not parse this file: " + entry.Reason,
		})
	}
	return findings
}

func dataFiles(repo repository.Repository, files []string) []string {
	selected := []string{}
	for _, path := range files {
		if repo.IsData(path) {
			selected = append(selected, path)
		}
	}
	sort.Strings(selected)
	return selected
}

func readableDataFile(repo repository.Repository, path string) (policy.Finding, bool) {
	resolved, err := repo.Resolve(path)
	if err != nil {
		return policy.Finding{
			Check: "quality.dataPath", Path: path, Subject: "contained path",
			Message: "declared data must resolve inside the repository and be readable",
		}, false
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return policy.Finding{
			Check: "quality.dataPath", Path: path, Subject: "regular file",
			Message: "declared data must be a contained regular file",
		}, false
	}
	if info.Size() > structuredDataMaximumBytes {
		return policy.Finding{
			Check: "quality.dataSize", Path: path, Subject: strconv.FormatInt(info.Size(), 10),
			Message: fmt.Sprintf("declared data exceeds the %d byte limit", structuredDataMaximumBytes),
		}, false
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return policy.Finding{
			Check: "quality.dataPath", Path: path, Subject: "contained path",
			Message: "declared data must resolve inside the repository and be readable",
		}, false
	}
	if !utf8.Valid(data) {
		return policy.Finding{
			Check: "quality.dataText", Path: path, Subject: "UTF-8",
			Message: "declared data must be valid UTF-8 text",
		}, false
	}
	return policy.Finding{}, true
}

func dataFormat(path string) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
}
