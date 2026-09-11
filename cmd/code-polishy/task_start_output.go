package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
)

const taskStartCollectionLimit = 12

func renderTaskStart(data []byte, format string) (string, error) {
	if format == "json" {
		return strings.TrimSuffix(string(data), "\n"), nil
	}
	var packet engine.TaskStartPacket
	if err := json.Unmarshal(data, &packet); err != nil {
		return "", fmt.Errorf("decode task-start packet for human output: %w", err)
	}
	return taskStartHuman(packet), nil
}

func taskStartHuman(packet engine.TaskStartPacket) string {
	var output strings.Builder
	fmt.Fprintf(&output, "TASK START: %s\n", packet.Protocol)
	fmt.Fprintf(&output, "TASK BASE: %s\n", packet.TaskBase)
	fmt.Fprintf(&output, "LOCKED RELEASE: %s %s\n", packet.LockedRelease.CodePolishyVersion, packet.LockedRelease.ReleaseDigest)
	fmt.Fprintf(&output, "SELECTION: mode=%s operands=%s expanded-paths=%d\n",
		packet.RequestedSelection.Mode, boundedTaskStartValues(packet.RequestedSelection.Operands), len(packet.RequestedSelection.Expanded))
	if len(packet.RequestedSelection.Modules) > 0 {
		fmt.Fprintln(&output, "MODULES:", boundedTaskStartValues(packet.RequestedSelection.Modules))
	}
	fmt.Fprintf(&output, "INTENT: captured=%t willBeUsed=%t\n", packet.Intent.Captured, packet.Intent.WillBeUsed)
	fmt.Fprintln(&output, "INTENT REASON:", packet.Intent.Reason)
	fmt.Fprintln(&output, "SELECTED FEATURES:", boundedTaskStartValues(packet.Intent.SelectedFeatures))
	printTaskStartCapture(&output, packet.Intent.Capture)
	printTaskStartContext(&output, packet.RepositoryContext)
	fmt.Fprintf(&output, "CONFIGURED GUARDS: %d; use `code-polishy capabilities --format json` for the complete catalog.\n", len(packet.ConfiguredGuards))
	fmt.Fprintln(&output, "WORKFLOW DOCUMENTS:", boundedTaskStartValues(packet.WorkflowDocuments))
	fmt.Fprintln(&output, "FINAL GATE OWNER:", packet.FinalGateOwner)
	for _, action := range packet.NextActions {
		fmt.Fprintf(&output, "NEXT %s: %s", action.Name, action.Description)
		if len(action.Argv) > 0 {
			fmt.Fprintf(&output, " [%s]", strings.Join(action.Argv, " "))
		}
		fmt.Fprintln(&output)
	}
	return strings.TrimSuffix(output.String(), "\n")
}

func printTaskStartCapture(output *strings.Builder, capture *engine.BehaviorReviewIntentCapture) {
	if capture == nil {
		return
	}
	fmt.Fprintf(output, "CAPTURE: id=%s commit=%s journal=%s", capture.ID, capture.Commit, capture.JournalPath)
	if capture.RequirementID != "" {
		fmt.Fprintf(output, " requirement=%s", capture.RequirementID)
	}
	fmt.Fprintln(output)
}

func printTaskStartContext(output *strings.Builder, context *engine.RepositoryContext) {
	if context == nil {
		fmt.Fprintln(output, "DESIGN CONTEXT: unavailable")
		return
	}
	resolution := context.DesignResolution
	fmt.Fprintf(output, "DESIGN CONTEXT: selected-paths=%d documents=%d handoffs=%d\n",
		resolution.SelectedPathCount, len(context.DesignDocuments), len(context.Handoffs))
	for _, document := range context.DesignDocuments[:min(taskStartCollectionLimit, len(context.DesignDocuments))] {
		fmt.Fprintf(output, "DESIGN DOCUMENT: %s sha256:%s\n", document.Path, document.SHA256)
	}
	printTaskStartOmitted(output, "design document", len(context.DesignDocuments)-min(taskStartCollectionLimit, len(context.DesignDocuments)))
	fmt.Fprintln(output, "UNMAPPED MODULES:", boundedTaskStartValues(resolution.UnmappedModules))
	fmt.Fprintf(output, "UNMAPPED PATHS: %d", len(resolution.UnmappedPaths))
	if len(resolution.UnmappedPaths) > 0 {
		fmt.Fprintf(output, " (%s)", boundedTaskStartValues(resolution.UnmappedPaths))
	}
	fmt.Fprintln(output)
	for _, handoff := range context.Handoffs[:min(taskStartCollectionLimit, len(context.Handoffs))] {
		fmt.Fprintf(output, "HANDOFF %s: %s [%s sha256:%s]\n", handoff.Name, handoff.Description, handoff.Document.Path, handoff.Document.SHA256)
	}
	printTaskStartOmitted(output, "handoff", len(context.Handoffs)-min(taskStartCollectionLimit, len(context.Handoffs)))
}

func boundedTaskStartValues(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	displayed := values[:min(taskStartCollectionLimit, len(values))]
	result := strings.Join(displayed, ", ")
	if len(displayed) < len(values) {
		result += fmt.Sprintf(", ... (%d more)", len(values)-len(displayed))
	}
	return result
}

func printTaskStartOmitted(output *strings.Builder, label string, count int) {
	if count > 0 {
		fmt.Fprintf(output, "OMITTED: %d additional %s(s); use --format json for complete machine output.\n", count, label)
	}
}
