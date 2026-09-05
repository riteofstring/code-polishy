package repository

import (
	"fmt"
	"os"
	"slices"
	"sort"

	"github.com/riteofstring/code-polishy/internal/policy"
)

const MaximumGenerationFiles = 8192
const maximumGenerationMatches = 1 << 20

type GenerationInventory struct {
	Producers []ResolvedGenerationProducer
	Findings  []policy.Finding
}

type ResolvedGenerationProducer struct {
	Declaration policy.GenerationProducer
	Inputs      []string
	Outputs     []string
}

type generationInspector struct {
	repo      Repository
	root      *os.Root
	files     []string
	matches   int
	inventory GenerationInventory
}

func (repo Repository) InspectGeneration(files []string) GenerationInventory {
	if err := policy.ValidateGeneration(repo.Config.Generation); err != nil {
		return GenerationInventory{Findings: []policy.Finding{generationFinding(policy.ConfigFilename, "configuration", err.Error())}}
	}
	if len(repo.Config.Generation.Producers) == 0 && !repo.HasGeneratedExecutable(files) {
		return GenerationInventory{}
	}
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return GenerationInventory{Findings: []policy.Finding{generationFinding(policy.ConfigFilename, "repository", err.Error())}}
	}
	defer root.Close()
	ordered := slices.Clone(files)
	sort.Strings(ordered)
	inspector := generationInspector{repo: repo, root: root, files: slices.Compact(ordered)}
	for _, producer := range repo.Config.Generation.Producers {
		resolved, err := inspector.resolveProducer(producer)
		if err != nil {
			inspector.inventory.Findings = append(inspector.inventory.Findings, generationFinding(policy.ConfigFilename, producer.Name, err.Error()))
			continue
		}
		inspector.inventory.Producers = append(inspector.inventory.Producers, resolved)
	}
	inspector.validateOwnership()
	inspector.validateCycles()
	sort.Slice(inspector.inventory.Producers, func(left, right int) bool {
		return inspector.inventory.Producers[left].Declaration.Name < inspector.inventory.Producers[right].Declaration.Name
	})
	sort.Slice(inspector.inventory.Findings, func(left, right int) bool {
		a, b := inspector.inventory.Findings[left], inspector.inventory.Findings[right]
		return a.Path+"\x00"+a.Subject+"\x00"+a.Message < b.Path+"\x00"+b.Subject+"\x00"+b.Message
	})
	return inspector.inventory
}

func (repo Repository) HasGeneratedExecutable(files []string) bool {
	for _, path := range files {
		if repo.IsGenerated(path) && repo.IsExecutableSource(path) {
			return true
		}
	}
	return false
}

func (inventory GenerationInventory) ProducerFor(path string) (ResolvedGenerationProducer, bool) {
	if len(inventory.Findings) != 0 {
		return ResolvedGenerationProducer{}, false
	}
	for _, producer := range inventory.Producers {
		if slices.Contains(producer.Outputs, path) {
			return producer, true
		}
	}
	return ResolvedGenerationProducer{}, false
}

func (inspector *generationInspector) resolveProducer(producer policy.GenerationProducer) (ResolvedGenerationProducer, error) {
	outputs, err := inspector.expand(producer.Outputs)
	if err != nil {
		return ResolvedGenerationProducer{}, fmt.Errorf("outputs: %w", err)
	}
	inputs, err := inspector.expand(producer.Inputs)
	if err != nil {
		return ResolvedGenerationProducer{}, fmt.Errorf("inputs: %w", err)
	}
	for _, input := range inputs {
		if slices.Contains(outputs, input) {
			return ResolvedGenerationProducer{}, fmt.Errorf("input %q is also an output of the same producer", input)
		}
	}
	for _, output := range outputs {
		if !inspector.repo.IsGenerated(output) {
			return ResolvedGenerationProducer{}, fmt.Errorf("output %q is outside generated source classification", output)
		}
	}
	return ResolvedGenerationProducer{Declaration: producer, Inputs: inputs, Outputs: outputs}, nil
}

func (inspector *generationInspector) expand(patterns []string) ([]string, error) {
	selected := map[string]bool{}
	for _, pattern := range patterns {
		matches, err := inspector.matchPattern(pattern)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("pattern %q matches no current governed regular file", pattern)
		}
		for _, path := range matches {
			if selected[path] {
				return nil, fmt.Errorf("patterns overlap at %q", path)
			}
			selected[path] = true
		}
		if len(selected) > MaximumGenerationFiles {
			return nil, fmt.Errorf("producer file set exceeds %d paths", MaximumGenerationFiles)
		}
	}
	return sortedSet(selected), nil
}

func (inspector *generationInspector) matchPattern(pattern string) ([]string, error) {
	matches := []string{}
	for _, path := range inspector.files {
		inspector.matches++
		if inspector.matches > maximumGenerationMatches {
			return nil, fmt.Errorf("generation inventory exceeded its bounded matching budget")
		}
		if inspector.repo.IsExcluded(path) || !policy.Match(path, pattern) {
			continue
		}
		if _, err := inspector.repo.containedRegularFileInfo(inspector.root, path); err != nil {
			return nil, fmt.Errorf("producer path %q is invalid: %w", path, err)
		}
		matches = append(matches, path)
	}
	return matches, nil
}

func generationFinding(path, subject, message string) policy.Finding {
	return policy.Finding{Check: "policy.generationOwnership", Path: path, Subject: subject, Message: message}
}
