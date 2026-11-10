package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type reportGuidanceValue interface {
	string | policy.FindingCommand | policy.GenerationRemediation | policy.VulnerabilityAssessment
}

type reportGuidanceKey struct {
	value   string
	failure bool
}

type reportGuidanceGroups[T reportGuidanceValue] struct {
	groups []reportGuidanceGroup[T]
	index  map[reportGuidanceKey]int
}

type reportGuidanceGroup[T reportGuidanceValue] struct {
	value   T
	failure bool
	count   int
}

func (groups *reportGuidanceGroups[T]) add(value T, failure bool) int {
	encoded, _ := json.Marshal(value)
	key := reportGuidanceKey{value: string(encoded), failure: failure}
	if index, exists := groups.index[key]; exists {
		groups.groups[index].count++
		return index + 1
	}
	if groups.index == nil {
		groups.index = map[reportGuidanceKey]int{}
	}
	groups.index[key] = len(groups.groups)
	groups.groups = append(groups.groups, reportGuidanceGroup[T]{value: value, failure: failure, count: 1})
	return len(groups.groups)
}

func (group reportGuidanceGroup[T]) output(stdout, stderr io.Writer) io.Writer {
	if group.failure {
		return stderr
	}
	return stdout
}

type findingGuidance struct {
	remedies    reportGuidanceGroups[string]
	commands    reportGuidanceGroups[policy.FindingCommand]
	generations reportGuidanceGroups[policy.GenerationRemediation]
}

func (guidance *findingGuidance) add(remediation policy.FindingRemediation, failure bool) string {
	references := ""
	if remediation.Summary != "" {
		references += fmt.Sprintf(" remedy=R%d", guidance.remedies.add(remediation.Summary, failure))
	}
	if remediation.NextCommand != nil {
		references += fmt.Sprintf(" next=C%d", guidance.commands.add(*remediation.NextCommand, failure))
	}
	if remediation.Generation != nil {
		references += fmt.Sprintf(" generation=G%d", guidance.generations.add(*remediation.Generation, failure))
	}
	return references
}

func (guidance findingGuidance) print(stdout, stderr io.Writer) {
	for index, group := range guidance.remedies.groups {
		output := group.output(stdout, stderr)
		fmt.Fprintf(output, "REMEDY R%d (%s):\n     remedy: %s\n", index+1, findingCount(group.count, "finding"), group.value)
	}
	for index, group := range guidance.commands.groups {
		output := group.output(stdout, stderr)
		fmt.Fprintf(output, "COMMAND C%d (%s, cwd %s):\n     next: %s\n", index+1, findingCount(group.count, "finding"), group.value.Cwd, displayRemediationCommand(group.value.Argv))
	}
	for index, group := range guidance.generations.groups {
		output := group.output(stdout, stderr)
		fmt.Fprintf(output, "GENERATION G%d (%s):\n", index+1, findingCount(group.count, "finding"))
		printGenerationRemediation(output, &group.value)
	}
}
