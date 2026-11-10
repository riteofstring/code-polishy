package main

import (
	"fmt"
	"io"
	"reflect"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type reportGuidanceGroup[T any] struct {
	value   T
	failure bool
	count   int
}

func addReportGuidance[T any](groups *[]reportGuidanceGroup[T], value T, failure bool) int {
	for index := range *groups {
		group := &(*groups)[index]
		if group.failure == failure && reflect.DeepEqual(group.value, value) {
			group.count++
			return index + 1
		}
	}
	*groups = append(*groups, reportGuidanceGroup[T]{value: value, failure: failure, count: 1})
	return len(*groups)
}

func (group reportGuidanceGroup[T]) output(stdout, stderr io.Writer) io.Writer {
	if group.failure {
		return stderr
	}
	return stdout
}

type findingGuidance struct {
	remedies    []reportGuidanceGroup[string]
	commands    []reportGuidanceGroup[policy.FindingCommand]
	generations []reportGuidanceGroup[policy.GenerationRemediation]
}

func (guidance *findingGuidance) add(remediation policy.FindingRemediation, failure bool) string {
	references := ""
	if remediation.Summary != "" {
		references += fmt.Sprintf(" remedy=R%d", addReportGuidance(&guidance.remedies, remediation.Summary, failure))
	}
	if remediation.NextCommand != nil {
		references += fmt.Sprintf(" next=C%d", addReportGuidance(&guidance.commands, *remediation.NextCommand, failure))
	}
	if remediation.Generation != nil {
		references += fmt.Sprintf(" generation=G%d", addReportGuidance(&guidance.generations, *remediation.Generation, failure))
	}
	return references
}

func (guidance findingGuidance) print(stdout, stderr io.Writer) {
	for index, group := range guidance.remedies {
		output := group.output(stdout, stderr)
		fmt.Fprintf(output, "REMEDY R%d (%s):\n     remedy: %s\n", index+1, findingCount(group.count, "finding"), group.value)
	}
	for index, group := range guidance.commands {
		output := group.output(stdout, stderr)
		fmt.Fprintf(output, "COMMAND C%d (%s, cwd %s):\n     next: %s\n", index+1, findingCount(group.count, "finding"), group.value.Cwd, displayRemediationCommand(group.value.Argv))
	}
	for index, group := range guidance.generations {
		output := group.output(stdout, stderr)
		fmt.Fprintf(output, "GENERATION G%d (%s):\n", index+1, findingCount(group.count, "finding"))
		printGenerationRemediation(output, &group.value)
	}
}
