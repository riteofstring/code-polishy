package engine

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func generationReceiptInputs(repo repository.Repository, suite policy.TestSuite, files []string, selected map[string]bool) string {
	inventory := repo.InspectGeneration(files)
	if len(inventory.Findings) != 0 {
		return "generated-source ownership is incomplete; reproducibility inputs cannot be bound"
	}
	pending := append([]repository.ResolvedGenerationProducer{}, inventory.Producers...)
	for len(pending) > 0 {
		remaining, changed, reason := expandGenerationReceiptInputs(repo, suite, pending, files, selected)
		if reason != "" || !changed {
			return reason
		}
		pending = remaining
	}
	return ""
}

func expandGenerationReceiptInputs(repo repository.Repository, suite policy.TestSuite, producers []repository.ResolvedGenerationProducer, files []string, selected map[string]bool) ([]repository.ResolvedGenerationProducer, bool, string) {
	remaining := []repository.ResolvedGenerationProducer{}
	changed := false
	for _, producer := range producers {
		if !generationReceiptRelevant(suite, producer, selected) {
			remaining = append(remaining, producer)
			continue
		}
		for _, path := range append(slices.Clone(producer.Inputs), producer.Outputs...) {
			selected[path] = true
		}
		for _, command := range []policy.GenerationCommand{producer.Declaration.Generate, producer.Declaration.Verify} {
			if reason := generationCommandReceiptInputs(repo, command, files, selected); reason != "" {
				return nil, false, fmt.Sprintf("generation producer %q: %s", producer.Declaration.Name, reason)
			}
		}
		changed = true
	}
	return remaining, changed, ""
}

func generationReceiptRelevant(suite policy.TestSuite, producer repository.ResolvedGenerationProducer, selected map[string]bool) bool {
	if slices.Equal(suite.Argv, producer.Declaration.Verify.Argv) && suite.Cwd == producer.Declaration.Verify.Cwd {
		return true
	}
	for _, output := range producer.Outputs {
		if selected[output] {
			return true
		}
	}
	return false
}

func generationCommandReceiptInputs(repo repository.Repository, command policy.GenerationCommand, files []string, selected map[string]bool) string {
	if err := validateReusableArguments(command.Argv); err != nil {
		return err.Error()
	}
	for index, argument := range command.Argv {
		path := argument
		if index > 0 {
			path = filepath.ToSlash(filepath.Join(command.Cwd, filepath.FromSlash(argument)))
		}
		normalized, err := repo.NormalizePath(path)
		if err == nil && slices.Contains(files, normalized) {
			selected[normalized] = true
		}
	}
	if executable := command.Argv[0]; strings.ContainsAny(executable, "/\\") {
		path, reason := localSuiteExecutable(repo, command.Argv)
		if reason != "" {
			return reason
		}
		selected[path] = true
	}
	paths := make([]string, 0, len(selected))
	for path := range selected {
		paths = append(paths, path)
	}
	return unboundedSuiteTool(command.Argv, suiteReceiptTools(repo), paths)
}

func generationReceiptEnvironment(repo repository.Repository, suite policy.TestSuite, selected []string) []string {
	names := slices.Clone(suite.Environment)
	paths := make(map[string]bool, len(selected))
	for _, path := range selected {
		paths[path] = true
	}
	files, err := repo.AllFiles()
	if err != nil {
		return sortedUniqueStrings(names)
	}
	for _, producer := range repo.InspectGeneration(files).Producers {
		if generationReceiptRelevant(suite, producer, paths) {
			names = append(names, producer.Declaration.Generate.Environment...)
			names = append(names, producer.Declaration.Verify.Environment...)
		}
	}
	return sortedUniqueStrings(names)
}
