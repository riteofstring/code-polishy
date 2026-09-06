package architecture

import (
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

type pythonModuleIndex struct {
	files     map[string][]string
	ambiguous map[string]bool
}

func newPythonModuleIndex(project repository.PythonProject) pythonModuleIndex {
	index := pythonModuleIndex{files: map[string][]string{}, ambiguous: map[string]bool{}}
	for _, source := range project.Files {
		module, _ := repository.PythonModuleName(project, source)
		if module == "" {
			continue
		}
		index.files[module] = append(index.files[module], source)
	}
	for module := range index.files {
		sort.Strings(index.files[module])
		if pythonConflictingModulePaths(index.files[module]) {
			index.ambiguous[module] = true
		}
	}
	return index
}

func pythonConflictingModulePaths(paths []string) bool {
	locations := map[string]bool{}
	for _, path := range paths {
		location := strings.TrimSuffix(strings.TrimSuffix(path, ".pyi"), ".py")
		locations[location] = true
	}
	return len(locations) > 1
}
