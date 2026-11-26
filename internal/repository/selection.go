package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type RequestedSelection struct {
	Mode     string   `json:"mode"`
	Operands []string `json:"operands"`
	Modules  []string `json:"modules,omitempty"`
	Expanded []string `json:"expanded"`
	Planned  []string `json:"planned,omitempty"`
}

type selectionOperand struct {
	path      string
	directory bool
}

func (repo Repository) Select(mode string, explicit []string) (Selection, error) {
	var selection Selection
	var err error
	switch mode {
	case "all":
		selection, err = repo.allSelection()
	case "files":
		selection, err = repo.explicitSelection(explicit)
	case "modules":
		selection, err = repo.moduleSelection(explicit)
	case "staged":
		selection, err = repo.stagedSelection()
		selection.Requested = RequestedSelection{Mode: "staged", Expanded: selection.Candidate.Paths()}
	case "changes", "":
		selection, err = repo.changedSelection()
		selection.Requested = RequestedSelection{Mode: "git-changes", Expanded: selection.Candidate.Paths()}
	default:
		return Selection{}, fmt.Errorf("unknown selection mode %q", mode)
	}
	return repo.validatedSelection(selection, err)
}

func (repo Repository) allSelection() (Selection, error) {
	files, err := repo.AllFiles()
	requested := RequestedSelection{Mode: "all", Expanded: append([]string{}, files...)}
	return Selection{Candidate: CandidateDelta{AddedOrModified: append([]string{}, files...)}, Files: files, Requested: requested, All: true}, err
}

func (repo Repository) explicitSelection(explicit []string) (Selection, error) {
	if len(explicit) == 0 {
		return Selection{}, errors.New("--files needs at least one file or directory")
	}
	allFiles, err := repo.AllFiles()
	if err != nil {
		return Selection{}, err
	}
	operands := make([]selectionOperand, 0, len(explicit))
	seen := map[string]bool{}
	for _, raw := range explicit {
		operand, err := repo.inspectSelectionOperand(raw)
		if err != nil {
			return Selection{}, err
		}
		if seen[operand.path] {
			return Selection{}, fmt.Errorf("selection operand is repeated: %s", operand.path)
		}
		seen[operand.path] = true
		operands = append(operands, operand)
	}
	if err := rejectOverlappingOperands(operands); err != nil {
		return Selection{}, err
	}
	expanded := []string{}
	requestedOperands := make([]string, 0, len(operands))
	for _, operand := range operands {
		requestedOperands = append(requestedOperands, operand.path)
		if !operand.directory {
			expanded = append(expanded, operand.path)
			continue
		}
		matched := directorySelectionFiles(repo, operand.path, allFiles)
		if len(matched) == 0 {
			return Selection{}, fmt.Errorf("selection directory has no governed regular files: %s", operand.path)
		}
		expanded = append(expanded, matched...)
	}
	expanded = uniqueSorted(expanded)
	requested := RequestedSelection{Mode: "files", Operands: requestedOperands, Expanded: append([]string{}, expanded...)}
	return Selection{Candidate: CandidateDelta{AddedOrModified: append([]string{}, expanded...)}, Files: expanded, Requested: requested}, nil
}

func (repo Repository) inspectSelectionOperand(raw string) (selectionOperand, error) {
	normalized, err := repo.normalizeSelectionOperand(raw)
	if err != nil {
		return selectionOperand{}, err
	}
	absolute := filepath.Join(repo.Root, filepath.FromSlash(normalized))
	info, err := os.Lstat(absolute)
	if err != nil {
		if os.IsNotExist(err) {
			return selectionOperand{}, fmt.Errorf("selection path is missing: %s; for intentional creation use design-context or task-start with --new-path %s", normalized, normalized)
		}
		return selectionOperand{}, fmt.Errorf("inspect selection path %s: %w", normalized, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return selectionOperand{}, fmt.Errorf("selection path is a symbolic link: %s", normalized)
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return selectionOperand{}, fmt.Errorf("selection path is not a regular file or directory: %s", normalized)
	}
	if repo.IsExcluded(selectionScopeProbe(normalized, info.IsDir())) {
		return selectionOperand{}, fmt.Errorf("selection path is outside governed scope: %s", normalized)
	}
	return selectionOperand{path: normalized, directory: info.IsDir()}, nil
}

func (repo Repository) ContextPathSelection(files, planned []string) (Selection, error) {
	selection := Selection{Requested: RequestedSelection{Mode: "files", Operands: []string{}, Expanded: []string{}}}
	if len(files) > 0 {
		var err error
		selection, err = repo.Select("files", files)
		if err != nil {
			return Selection{}, err
		}
	}
	for _, raw := range planned {
		path, err := repo.plannedSelectionPath(raw)
		if err != nil {
			return Selection{}, err
		}
		if slices.Contains(selection.Requested.Planned, path) {
			return Selection{}, fmt.Errorf("planned path is repeated: %s", path)
		}
		selection.Requested.Planned = append(selection.Requested.Planned, path)
		selection.Files = append(selection.Files, path)
		selection.Candidate.AddedOrModified = append(selection.Candidate.AddedOrModified, path)
	}
	return selection, nil
}

func (repo Repository) plannedSelectionPath(raw string) (string, error) {
	path, err := repo.normalizeSelectionOperand(raw)
	if err != nil {
		return "", err
	}
	if path == "." || strings.ContainsAny(path, "\\\x00:") || repo.IsExcluded(path) {
		return "", fmt.Errorf("planned path must name a contained governed file: %s", path)
	}
	return repo.inspectPlannedPath(path)
}

func (repo Repository) inspectPlannedPath(path string) (string, error) {
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return "", err
	}
	defer root.Close()
	parts := strings.Split(path, "/")
	for index := range parts {
		info, err := root.Lstat(filepath.FromSlash(strings.Join(parts[:index+1], "/")))
		if os.IsNotExist(err) {
			return path, nil
		}
		if err != nil {
			return "", fmt.Errorf("inspect planned path %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || index < len(parts)-1 && !info.IsDir() {
			return "", fmt.Errorf("planned path cannot contain a symbolic link or non-directory parent: %s", path)
		}
	}
	return "", fmt.Errorf("planned path already exists: %s; use --files", path)
}

func (repo Repository) normalizeSelectionOperand(raw string) (string, error) {
	if strings.TrimSpace(raw) != raw || raw == "" {
		return "", fmt.Errorf("selection path must be non-empty and contain no surrounding whitespace: %q", raw)
	}
	if filepath.IsAbs(raw) {
		relative, err := filepath.Rel(repo.Root, raw)
		if err != nil {
			return "", err
		}
		raw = relative
	}
	normalized := filepath.ToSlash(filepath.Clean(raw))
	normalized = strings.TrimPrefix(normalized, "./")
	if normalized == "" {
		normalized = "."
	}
	if normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", fmt.Errorf("selection path must stay inside the repository: %s", normalized)
	}
	return normalized, nil
}

func selectionScopeProbe(path string, directory bool) string {
	if directory {
		if path == "." {
			return "selection-placeholder"
		}
		return path + "/selection-placeholder"
	}
	return path
}

func rejectOverlappingOperands(operands []selectionOperand) error {
	for left := range operands {
		for right := left + 1; right < len(operands); right++ {
			if operands[left].directory && pathWithinDirectory(operands[right].path, operands[left].path) {
				return fmt.Errorf("selection operands overlap: %s contains %s", operands[left].path, operands[right].path)
			}
			if operands[right].directory && pathWithinDirectory(operands[left].path, operands[right].path) {
				return fmt.Errorf("selection operands overlap: %s contains %s", operands[right].path, operands[left].path)
			}
		}
	}
	return nil
}

func pathWithinDirectory(path, directory string) bool {
	return directory == "." || strings.HasPrefix(path, strings.TrimSuffix(directory, "/")+"/")
}

func directorySelectionFiles(repo Repository, directory string, files []string) []string {
	selected := []string{}
	for _, path := range files {
		if !pathWithinDirectory(path, directory) {
			continue
		}
		info, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(path)))
		if err == nil && info.Mode().IsRegular() {
			selected = append(selected, path)
		}
	}
	return uniqueSorted(selected)
}

func (repo Repository) moduleSelection(names []string) (Selection, error) {
	return repo.moduleSelectionWithFiles(names, true)
}

func (repo Repository) DesignModuleSelection(names []string) (Selection, error) {
	selection, err := repo.moduleSelectionWithFiles(names, false)
	return repo.validatedSelection(selection, err)
}

func (repo Repository) moduleSelectionWithFiles(names []string, requireFiles bool) (Selection, error) {
	if len(names) == 0 {
		return Selection{}, errors.New("--module needs at least one declared module name")
	}
	modules := append([]string{}, names...)
	seen := map[string]bool{}
	for _, name := range modules {
		if seen[name] {
			return Selection{}, fmt.Errorf("module selection is repeated: %s", name)
		}
		seen[name] = true
		if !repo.hasModule(name) {
			return Selection{}, fmt.Errorf("module selection names an unknown module: %s\n%s", name, policy.DeclaredModules(repo.Config))
		}
	}
	allFiles, err := repo.AllFiles()
	if err != nil {
		return Selection{}, err
	}
	expanded := []string{}
	for _, name := range modules {
		matched := []string{}
		for _, path := range allFiles {
			if slices.Contains(repo.OwnerModuleNames(path), name) {
				matched = append(matched, path)
			}
		}
		if requireFiles && len(matched) == 0 {
			return Selection{}, fmt.Errorf("module selection has no governed files: %s", name)
		}
		expanded = append(expanded, matched...)
	}
	expanded = uniqueSorted(expanded)
	requested := RequestedSelection{Mode: "module", Operands: append([]string{}, modules...), Modules: uniqueSorted(modules), Expanded: append([]string{}, expanded...)}
	return Selection{Candidate: CandidateDelta{AddedOrModified: append([]string{}, expanded...)}, Files: expanded, Requested: requested}, nil
}

func (repo Repository) validatedSelection(selection Selection, err error) (Selection, error) {
	if err != nil {
		return selection, err
	}
	if err := repo.validateDataFiles(selection.Files); err != nil {
		return Selection{}, err
	}
	return selection, nil
}
