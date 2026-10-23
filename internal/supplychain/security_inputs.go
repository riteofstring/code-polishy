package supplychain

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func SecurityScanInputs(repo repository.Repository) ([]string, error) {
	files, err := repo.RecursiveScanFiles()
	if err != nil {
		return nil, err
	}
	inputs, err := securityScanIgnoreFiles(repo, files)
	if err != nil {
		return nil, err
	}
	for _, path := range files {
		if isOSVPackageSourceInput(repo, path) || repo.IsControlInput(path) {
			inputs = append(inputs, path)
		}
	}
	slices.Sort(inputs)
	return slices.Compact(inputs), nil
}

func securityScanIgnoreFiles(repo repository.Repository, files []string) ([]string, error) {
	directories := map[string]bool{".": true}
	for _, path := range files {
		for directory := filepath.Dir(filepath.FromSlash(path)); !directories[directory]; directory = filepath.Dir(directory) {
			directories[directory] = true
		}
	}
	inputs := []string{}
	for directory := range directories {
		path := filepath.Join(directory, ".gitignore")
		if _, err := os.Lstat(filepath.Join(repo.Root, path)); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		inputs = append(inputs, filepath.ToSlash(path))
	}
	return inputs, nil
}
