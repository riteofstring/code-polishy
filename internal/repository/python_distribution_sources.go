package repository

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/pythonfacts"
)

type PythonDistributionSources struct {
	Root     string
	Metadata pythonfacts.Input
	Record   pythonfacts.Input
	Origin   *pythonfacts.Input
	Sources  []pythonfacts.Input
	Identity string
}

func (repo Repository) ReadPythonDistributionSources(project PythonProject, dependency PythonPluginDependency) (PythonDistributionSources, error) {
	if project.Venv == "" || dependency.Error != "" || dependency.Distribution == "" {
		return PythonDistributionSources{}, fmt.Errorf("external contract requires a project environment and admitted dependency")
	}
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return PythonDistributionSources{}, err
	}
	defer root.Close()
	directories, err := pythonDistributionDirectories(root, project.Venv)
	if err != nil {
		return PythonDistributionSources{}, err
	}
	metadata, err := repo.pythonDistributionMetadata(root, directories, dependency)
	if err != nil {
		return PythonDistributionSources{}, err
	}
	return repo.pythonDistributionRecord(root, metadata)
}

func (repo Repository) pythonDistributionMetadata(root *os.Root, directories []string, dependency PythonPluginDependency) (pythonfacts.Input, error) {
	inputs := []pythonfacts.Input{}
	for _, directory := range directories {
		stem := strings.ReplaceAll(strings.ToLower(path.Base(directory)), "_", "-")
		if !strings.HasPrefix(stem, dependency.Distribution+"-") {
			continue
		}
		data, err := repo.pythonDistributionInput(root, path.Join(directory, "METADATA"), 1<<20)
		if err != nil {
			return pythonfacts.Input{}, err
		}
		inputs = append(inputs, data)
	}
	python, err := pythonfacts.DefaultInterpreter()
	if err != nil {
		return pythonfacts.Input{}, err
	}
	parsed, err := pythonfacts.Analyze(python, pythonfacts.Request{Metadata: inputs})
	if err != nil {
		return pythonfacts.Input{}, err
	}
	return selectPythonDistributionMetadata(inputs, parsed.Metadata, dependency)
}

func selectPythonDistributionMetadata(inputs []pythonfacts.Input, parsed []pythonfacts.Distribution, dependency PythonPluginDependency) (pythonfacts.Input, error) {
	selected := -1
	for index, metadata := range parsed {
		if metadata.Name != dependency.Distribution {
			continue
		}
		if selected >= 0 || metadata.Error != "" || metadata.Version != dependency.Version {
			return pythonfacts.Input{}, fmt.Errorf("installed distribution is ambiguous or does not match its admitted version")
		}
		selected = index
	}
	if selected < 0 {
		return pythonfacts.Input{}, fmt.Errorf("admitted distribution has no installed metadata")
	}
	return inputs[selected], nil
}

func pythonDistributionSiteRoots(root *os.Root, environment string) ([]string, error) {
	roots := []string{path.Join(environment, "Lib/site-packages")}
	entries, err := pythonDistributionEntries(root, path.Join(environment, "lib"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "python") {
			roots = append(roots, path.Join(environment, "lib", entry.Name(), "site-packages"))
		}
	}
	return roots, nil
}

func pythonDistributionDirectories(root *os.Root, environment string) ([]string, error) {
	roots, err := pythonDistributionSiteRoots(root, environment)
	if err != nil {
		return nil, err
	}
	directories := []string{}
	for _, directory := range roots {
		entries, err := pythonDistributionEntries(root, directory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".dist-info") {
				if !entry.IsDir() {
					return nil, fmt.Errorf("distribution metadata is not a physical directory")
				}
				directories = append(directories, path.Join(directory, entry.Name()))
			}
		}
	}
	slices.Sort(directories)
	return directories, nil
}

func pythonDistributionEntries(root *os.Root, directory string) ([]os.DirEntry, error) {
	file, err := root.Open(directory)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries, err := file.ReadDir(4097)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > 4096 {
		return nil, fmt.Errorf("distribution directory exceeds its entry boundary")
	}
	return entries, nil
}

func (repo Repository) pythonDistributionInput(root *os.Root, name string, limit int) (pythonfacts.Input, error) {
	info, err := repo.containedRegularFileInfo(root, name)
	if err != nil {
		return pythonfacts.Input{}, err
	}
	data, err := readContainedFile(root, name, info, limit)
	if err != nil {
		return pythonfacts.Input{}, err
	}
	if !utf8.Valid(data) {
		return pythonfacts.Input{}, fmt.Errorf("distribution source is not UTF-8")
	}
	return pythonfacts.Input{Path: name, Source: string(data)}, nil
}

func (repo Repository) pythonDistributionRecord(root *os.Root, metadata pythonfacts.Input) (PythonDistributionSources, error) {
	directory := path.Dir(metadata.Path)
	record, err := repo.pythonDistributionInput(root, path.Join(directory, "RECORD"), 4<<20)
	if err != nil {
		return PythonDistributionSources{}, err
	}
	capture := pythonDistributionCapture{repo: repo, root: root, result: PythonDistributionSources{Root: path.Dir(directory), Metadata: metadata, Record: record, Sources: []pythonfacts.Input{}}}
	rows, err := parsePythonDistributionRecord(record.Source)
	if err != nil {
		return PythonDistributionSources{}, err
	}
	for _, row := range rows {
		if err := capture.add(row); err != nil {
			return PythonDistributionSources{}, err
		}
	}
	return capture.finish()
}

type pythonDistributionCapture struct {
	repo         Repository
	root         *os.Root
	result       PythonDistributionSources
	metadataSeen bool
	total        int
}

func parsePythonDistributionRecord(source string) ([][]string, error) {
	reader := csv.NewReader(bytes.NewBufferString(source))
	reader.FieldsPerRecord = 3
	seen := map[string]bool{}
	rows := [][]string{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		if len(rows) >= 65536 || seen[row[0]] {
			return nil, fmt.Errorf("distribution record is duplicated or exceeds its item boundary")
		}
		seen[row[0]] = true
		rows = append(rows, row)
	}
}

func (capture *pythonDistributionCapture) add(row []string) error {
	name := path.Join(capture.result.Root, row[0])
	origin := path.Join(path.Dir(capture.result.Metadata.Path), "direct_url.json")
	selected := name == capture.result.Metadata.Path || name == origin || strings.HasSuffix(name, ".py") || strings.HasSuffix(name, ".pyi")
	if !selected {
		return nil
	}
	if path.Clean(row[0]) != row[0] || strings.HasPrefix(row[0], "../") || path.IsAbs(row[0]) {
		return fmt.Errorf("distribution source record escapes its installation")
	}
	input, err := capture.repo.pythonDistributionInput(capture.root, name, 1<<20)
	if err != nil {
		return err
	}
	if err := verifyPythonDistributionRecord(row, input.Source); err != nil {
		return err
	}
	return capture.retain(input, origin)
}

func (capture *pythonDistributionCapture) retain(input pythonfacts.Input, origin string) error {
	capture.total += len(input.Source)
	if capture.total > 64<<20 {
		return fmt.Errorf("distribution sources exceed their aggregate byte boundary")
	}
	switch input.Path {
	case capture.result.Metadata.Path:
		if input.Source != capture.result.Metadata.Source {
			return fmt.Errorf("distribution metadata changed during capture")
		}
		capture.metadataSeen = true
	case origin:
		capture.result.Origin = &input
	default:
		capture.result.Sources = append(capture.result.Sources, input)
	}
	return nil
}

func (capture pythonDistributionCapture) finish() (PythonDistributionSources, error) {
	if err := capture.validateOriginRecord(); err != nil {
		return PythonDistributionSources{}, err
	}
	result := capture.result
	if !capture.metadataSeen || len(result.Sources) == 0 {
		return PythonDistributionSources{}, fmt.Errorf("distribution record omits metadata or Python source")
	}
	slices.SortFunc(result.Sources, func(left, right pythonfacts.Input) int { return strings.Compare(left.Path, right.Path) })
	encoded, err := json.Marshal(result)
	if err != nil {
		return PythonDistributionSources{}, err
	}
	digest := sha256.Sum256(encoded)
	result.Identity = hex.EncodeToString(digest[:])
	return result, nil
}

func verifyPythonDistributionRecord(row []string, source string) error {
	digest := sha256.Sum256([]byte(source))
	if row[1] != "sha256="+base64.RawURLEncoding.EncodeToString(digest[:]) || row[2] != strconv.Itoa(len(source)) {
		return fmt.Errorf("distribution source does not match its RECORD hash and size")
	}
	return nil
}
