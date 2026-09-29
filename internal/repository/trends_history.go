package repository

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

var trendsHunkPattern = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

type trendsCommit struct {
	id      string
	parent  string
	time    time.Time
	message string
	files   []trendsFileDiff
}

type trendsFileDiff struct {
	oldPath string
	newPath string
	special bool
	hunks   []trendsHunk
}

type trendsHunk struct {
	oldStart int
	removed  []string
	added    []string
}

type trendsPatchParser struct {
	files        []trendsFileDiff
	current      *trendsFileDiff
	header       bool
	remainingOld int
	remainingNew int
}

func (file trendsFileDiff) changed() bool {
	return len(file.hunks) > 0 || file.oldPath != file.newPath
}

func (repo Repository) streamTrendsHistory(head string, since time.Time, visit func(trendsCommit) error) error {
	command := exec.Command("git", repo.trendsLogArguments(head, since)...)
	command.Env = slices.DeleteFunc(os.Environ(), func(entry string) bool { return strings.HasPrefix(entry, "GIT_DIFF_OPTS=") })
	stderr := &bytes.Buffer{}
	command.Stderr = stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("read Git history: %w", err)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("read Git history: %w", err)
	}
	readErr := readTrendsCommits(bufio.NewReaderSize(stdout, 1<<20), visit)
	if readErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if readErr != nil {
		return readErr
	}
	if waitErr != nil {
		return fmt.Errorf("read Git history: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (repo Repository) trendsLogArguments(head string, since time.Time) []string {
	return []string{
		"-C", repo.Root, "-c", "core.quotePath=false", "log", "--first-parent", "--reverse", "--diff-merges=first-parent",
		"--root", "--patch", "--unified=0", "--inter-hunk-context=0", "--ignore-all-space", "--find-renames",
		"--diff-algorithm=myers", "--submodule=short", "--no-color", "--no-ext-diff", "--no-textconv", "--no-show-signature", "--no-relative",
		"--src-prefix=a/", "--dst-prefix=b/", "--format=%x00%H %P%x00%ct%x00%B%x00",
		"--since=" + since.UTC().Format(time.RFC3339), head, "--",
	}
}

func readTrendsCommits(reader *bufio.Reader, visit func(trendsCommit) error) error {
	if _, err := reader.ReadString(0); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("read Git history: %w", err)
	}
	for {
		fields, last, err := readTrendsRecord(reader)
		if err != nil {
			return err
		}
		commit, err := parseTrendsCommit(fields)
		if err != nil {
			return err
		}
		if err := visit(commit); err != nil {
			return err
		}
		if last {
			return nil
		}
	}
}

func readTrendsRecord(reader *bufio.Reader) ([4]string, bool, error) {
	var fields [4]string
	for index := range fields {
		value, err := reader.ReadString(0)
		if errors.Is(err, io.EOF) && index == len(fields)-1 {
			fields[index] = value
			return fields, true, nil
		}
		if err != nil {
			return fields, false, fmt.Errorf("read Git history: incomplete commit record: %w", err)
		}
		fields[index] = strings.TrimSuffix(value, "\x00")
	}
	return fields, false, nil
}

func parseTrendsCommit(fields [4]string) (trendsCommit, error) {
	identity := strings.Fields(fields[0])
	if len(identity) == 0 || !exactRevision(identity[0]) {
		return trendsCommit{}, errors.New("read Git history: invalid commit identity")
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
	if err != nil {
		return trendsCommit{}, fmt.Errorf("read Git history: invalid commit time for %s", identity[0])
	}
	files, err := parseTrendsPatch(fields[3])
	if err != nil {
		return trendsCommit{}, fmt.Errorf("read Git history for %s: %w", identity[0], err)
	}
	commit := trendsCommit{id: strings.ToLower(identity[0]), time: time.Unix(seconds, 0).UTC(), message: fields[2], files: files}
	if len(identity) > 1 {
		commit.parent = strings.ToLower(identity[1])
	}
	return commit, nil
}

func parseTrendsPatch(patch string) ([]trendsFileDiff, error) {
	parser := trendsPatchParser{}
	for _, line := range strings.Split(patch, "\n") {
		if err := parser.line(line); err != nil {
			return nil, err
		}
	}
	if err := parser.finishFile(); err != nil {
		return nil, err
	}
	return parser.files, nil
}

func (parser *trendsPatchParser) line(line string) error {
	switch {
	case strings.HasPrefix(line, "diff --git "):
		return parser.startFile(line)
	case parser.current == nil:
		return nil
	case parser.header:
		return parser.headerLine(line)
	default:
		return parser.hunkLine(line)
	}
}

func (parser *trendsPatchParser) startFile(line string) error {
	if err := parser.finishFile(); err != nil {
		return err
	}
	path := symmetricTrendsPatchPath(strings.TrimPrefix(line, "diff --git "))
	parser.current = &trendsFileDiff{oldPath: path, newPath: path}
	parser.header = true
	return nil
}

func (parser *trendsPatchParser) finishFile() error {
	if parser.current == nil {
		return nil
	}
	if parser.remainingOld != 0 || parser.remainingNew != 0 {
		return errors.New("incomplete patch hunk")
	}
	parser.files = append(parser.files, *parser.current)
	parser.current = nil
	return nil
}

func (parser *trendsPatchParser) headerLine(line string) error {
	if strings.HasPrefix(line, "@@") {
		parser.header = false
		return parser.startHunk(line)
	}
	if strings.HasPrefix(line, "Binary files ") || strings.HasPrefix(line, "GIT binary patch") {
		parser.current.special = true
		return nil
	}
	if parser.modeLine(line) {
		return nil
	}
	return parser.pathLine(line)
}

func (parser *trendsPatchParser) modeLine(line string) bool {
	mode := ""
	switch {
	case strings.HasPrefix(line, "new file mode "):
		parser.current.oldPath = ""
		mode = strings.TrimPrefix(line, "new file mode ")
	case strings.HasPrefix(line, "deleted file mode "):
		parser.current.newPath = ""
		mode = strings.TrimPrefix(line, "deleted file mode ")
	case strings.HasPrefix(line, "old mode "), strings.HasPrefix(line, "new mode "):
		mode = line[len("old mode "):]
	case strings.HasPrefix(line, "index "):
		fields := strings.Fields(line)
		mode = fields[len(fields)-1]
	default:
		return false
	}
	if mode == "120000" || mode == "160000" {
		parser.current.special = true
	}
	return true
}

func (parser *trendsPatchParser) pathLine(line string) error {
	prefixes := []struct {
		marker string
		strip  string
		target *string
	}{
		{"rename from ", "", &parser.current.oldPath},
		{"rename to ", "", &parser.current.newPath},
		{"--- ", "a/", &parser.current.oldPath},
		{"+++ ", "b/", &parser.current.newPath},
	}
	for _, prefix := range prefixes {
		if value, found := strings.CutPrefix(line, prefix.marker); found {
			path, err := trendsPatchPath(value, prefix.strip)
			*prefix.target = path
			return err
		}
	}
	return nil
}

func trendsPatchPath(value, prefix string) (string, error) {
	value = strings.TrimSuffix(value, "\t")
	if value == "/dev/null" {
		return "", nil
	}
	if strings.HasPrefix(value, `"`) {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("invalid quoted patch path %s", value)
		}
		value = unquoted
	}
	if prefix != "" {
		path, found := strings.CutPrefix(value, prefix)
		if !found {
			return "", fmt.Errorf("unexpected patch path %q", value)
		}
		return path, nil
	}
	return value, nil
}

func symmetricTrendsPatchPath(value string) string {
	if len(value) < 5 || (len(value)-5)%2 != 0 || !strings.HasPrefix(value, "a/") {
		return ""
	}
	path := value[2 : 2+(len(value)-5)/2]
	if value != "a/"+path+" b/"+path {
		return ""
	}
	return path
}

func (parser *trendsPatchParser) startHunk(line string) error {
	if parser.remainingOld != 0 || parser.remainingNew != 0 {
		return errors.New("incomplete patch hunk")
	}
	match := trendsHunkPattern.FindStringSubmatch(line)
	if match == nil {
		return fmt.Errorf("invalid patch hunk header %q", line)
	}
	oldStart, _ := strconv.Atoi(match[1])
	parser.remainingOld = trendsHunkCount(match[2])
	parser.remainingNew = trendsHunkCount(match[4])
	parser.current.hunks = append(parser.current.hunks, trendsHunk{oldStart: oldStart})
	return nil
}

func trendsHunkCount(value string) int {
	if value == "" {
		return 1
	}
	count, _ := strconv.Atoi(value)
	return count
}

func (parser *trendsPatchParser) hunkLine(line string) error {
	hunk := &parser.current.hunks[len(parser.current.hunks)-1]
	switch {
	case strings.HasPrefix(line, "@@"):
		return parser.startHunk(line)
	case strings.HasPrefix(line, "-") && parser.remainingOld > 0:
		hunk.removed = append(hunk.removed, line[1:])
		parser.remainingOld--
	case strings.HasPrefix(line, "+") && parser.remainingNew > 0:
		hunk.added = append(hunk.added, line[1:])
		parser.remainingNew--
	case strings.HasPrefix(line, `\`):
	case line == "" && parser.remainingOld == 0 && parser.remainingNew == 0:
	default:
		return fmt.Errorf("unexpected patch line %q", line)
	}
	return nil
}
