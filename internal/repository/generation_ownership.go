package repository

import (
	"fmt"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func (inspector *generationInspector) outputOwners() map[string][]int {
	owners := map[string][]int{}
	for index, producer := range inspector.inventory.Producers {
		for _, output := range producer.Outputs {
			owners[output] = append(owners[output], index)
		}
	}
	return owners
}

func (inspector *generationInspector) validateOwnership() {
	owners := inspector.outputOwners()
	for _, path := range inspector.files {
		if !inspector.repo.IsGenerated(path) || !inspector.repo.IsExecutableSource(path) {
			continue
		}
		if len(owners[path]) == 0 {
			inspector.inventory.Findings = append(inspector.inventory.Findings, generationFinding(path, "producer", "generated executable output requires exactly one current generation.producers declaration"))
		}
	}
	for path, indices := range owners {
		if len(indices) <= 1 {
			continue
		}
		names := make([]string, 0, len(indices))
		for _, index := range indices {
			names = append(names, inspector.inventory.Producers[index].Declaration.Name)
		}
		slices.Sort(names)
		message := fmt.Sprintf("output matches multiple producers: %s", strings.Join(names, ", "))
		inspector.inventory.Findings = append(inspector.inventory.Findings, generationFinding(path, "producer", message))
	}
}

func (inspector *generationInspector) validateCycles() {
	owners := inspector.outputOwners()
	edges := make([][]int, len(inspector.inventory.Producers))
	for index, producer := range inspector.inventory.Producers {
		for _, input := range producer.Inputs {
			edges[index] = append(edges[index], owners[input]...)
		}
		slices.Sort(edges[index])
		edges[index] = slices.Compact(edges[index])
	}
	if cycle := generationCycle(edges); len(cycle) != 0 {
		names := make([]string, 0, len(cycle))
		for _, index := range cycle {
			names = append(names, inspector.inventory.Producers[index].Declaration.Name)
		}
		message := "producer input/output cycle: " + strings.Join(names, " -> ")
		inspector.inventory.Findings = append(inspector.inventory.Findings, generationFinding(policy.ConfigFilename, "producer-cycle", message))
	}
}

func generationCycle(edges [][]int) []int {
	state := make([]int, len(edges))
	for start := range edges {
		if cycle := walkGenerationCycle(edges, start, state, nil); len(cycle) != 0 {
			return cycle
		}
	}
	return nil
}

func walkGenerationCycle(edges [][]int, current int, state []int, stack []int) []int {
	if state[current] == 2 {
		return nil
	}
	if state[current] == 1 {
		start := slices.Index(stack, current)
		return append(slices.Clone(stack[start:]), current)
	}
	state[current] = 1
	stack = append(stack, current)
	for _, target := range edges[current] {
		if cycle := walkGenerationCycle(edges, target, state, stack); len(cycle) != 0 {
			return cycle
		}
	}
	state[current] = 2
	return nil
}
