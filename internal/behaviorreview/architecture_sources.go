package behaviorreview

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func readArchitectureSources(repo repository.Repository, candidate string, paths []string) ([]architectureSource, error) {
	sources := []architectureSource{}
	remaining := maximumArchitectureSources
	for _, path := range slices.Sorted(slices.Values(paths)) {
		if remaining == 0 {
			return nil, fmt.Errorf("%w: candidate source exceeds %d bytes", ErrArchitectureReview, maximumArchitectureSources)
		}
		data, present, err := repo.ReadRegularFileAtLimit(candidate, path, min(maximumArchitectureSource, remaining))
		if err != nil {
			return nil, fmt.Errorf("%w: candidate source %q: %v", ErrArchitectureReview, path, err)
		}
		if !present || !utf8.Valid(data) {
			return nil, fmt.Errorf("%w: candidate source %q must be a committed UTF-8 regular file", ErrArchitectureReview, path)
		}
		sources = append(sources, architectureSource{Path: path, Content: string(data), SHA256: sha256Hex(data)})
		remaining -= len(data)
	}
	return sources, nil
}
