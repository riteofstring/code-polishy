package supplychain

import (
	"github.com/riteofstring/code-polishy/internal/repository"
)

func SecurityScanInputs(repo repository.Repository) ([]string, error) {
	files, err := repo.RecursiveScanFiles()
	if err != nil {
		return nil, err
	}
	inputs := []string{}
	for _, path := range files {
		if isOSVPackageSourceInput(repo, path) || repo.IsControlInput(path) {
			inputs = append(inputs, path)
		}
	}
	return inputs, nil
}
