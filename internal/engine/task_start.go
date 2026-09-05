package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const MaximumTaskStartBytes = 16 << 20

type TaskStartRequest struct {
	IntentPath string
	Features   []string
	Context    ContextRequest
}

type TaskStartPacket struct {
	Protocol           string                        `json:"protocol"`
	LockedRelease      release.Lock                  `json:"lockedRelease"`
	CatalogSHA256      string                        `json:"catalogSha256"`
	Capture            BehaviorReviewIntentCapture   `json:"capture"`
	RequestedSelection repository.RequestedSelection `json:"requestedSelection"`
	RepositoryContext  *RepositoryContext            `json:"repositoryContext"`
	WorkflowDocuments  []string                      `json:"workflowDocuments"`
	ConfiguredGuards   []CapabilityEntry             `json:"configuredGuards"`
	Verification       policy.Verification           `json:"verification"`
	FinalGateOwner     string                        `json:"finalGateOwner"`
	NextActions        []TaskStartAction             `json:"nextActions"`
}

type TaskStartAction struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Argv        []string `json:"argv,omitempty"`
}

func (engine *Engine) TaskStart(ctx context.Context, request TaskStartRequest) ([]byte, error) {
	if err := validateTaskStartSelection(request.Context); err != nil {
		return nil, err
	}
	prepared, err := behaviorreview.PrepareIntentCapture(ctx, engine.Repository, behaviorreview.CaptureIntentOptions{
		IntentPath: request.IntentPath, Features: request.Features,
	})
	if err != nil {
		return nil, err
	}
	packet, err := engine.taskStartPacket(request.Context, prepared.Result())
	if err != nil {
		return nil, err
	}
	data, err := TaskStartPacketJSON(packet)
	if err != nil {
		return nil, err
	}
	if _, err := prepared.Commit(ctx); err != nil {
		return nil, err
	}
	return data, nil
}

func validateTaskStartSelection(request ContextRequest) error {
	if request.Workflow != "" && request.Workflow != "task-start" {
		return fmt.Errorf("task-start uses its own exact workflow context")
	}
	files := request.Mode == "files" && len(request.Files) == 1 && len(request.Modules) == 0
	module := request.Mode == "modules" && len(request.Modules) == 1 && len(request.Files) == 0
	if !files && !module {
		return fmt.Errorf("task-start requires one exact --files file/directory or --module name")
	}
	return nil
}

func (engine *Engine) taskStartPacket(request ContextRequest, capture BehaviorReviewIntentCapture) (TaskStartPacket, error) {
	request.Workflow = "task-start"
	contextReport, err := engine.DesignContext(request)
	if err != nil {
		return TaskStartPacket{}, err
	}
	if HasFindings(contextReport) || contextReport.RepositoryContext == nil || contextReport.RequestedSelection == nil {
		return TaskStartPacket{}, fmt.Errorf("task-start cannot compose invalid context; use design-context with the same selection and situations for findings")
	}
	inventory, err := engine.Capabilities("")
	if err != nil {
		return TaskStartPacket{}, err
	}
	if inventory.LockedRelease == nil || inventory.ReleaseCatalog.Availability != "available" {
		return TaskStartPacket{}, fmt.Errorf("task-start requires an authenticated locked capability catalog: %s", inventory.ReleaseCatalog.Reason)
	}
	packet := TaskStartPacket{
		Protocol: "task-start/v1", LockedRelease: *inventory.LockedRelease, CatalogSHA256: inventory.ReleaseCatalog.SHA256,
		Capture: capture, RequestedSelection: *contextReport.RequestedSelection, RepositoryContext: contextReport.RepositoryContext,
		WorkflowDocuments: []string{"docs/agent-workflows.md"}, ConfiguredGuards: []CapabilityEntry{},
		Verification:   engine.Repository.Config.Verification,
		FinalGateOwner: engine.Repository.Config.Verification.EffectiveFinalGateOwner(),
	}
	packet.collectGuards(inventory.Capabilities)
	packet.NextActions = taskStartActions(packet)
	return packet, nil
}

func (packet *TaskStartPacket) collectGuards(entries []CapabilityEntry) {
	for _, entry := range entries {
		if entry.Kind == "check" || entry.Kind == "pack-capability" || entry.Kind == "behavior-feature" {
			packet.ConfiguredGuards = append(packet.ConfiguredGuards, entry)
		}
		if entry.Name == "task-start" && entry.Kind == "command" || entry.Kind == "behavior-feature" && slices.Contains(packet.Capture.Features, entry.Name) {
			packet.WorkflowDocuments = append(packet.WorkflowDocuments, entry.Workflows...)
		}
	}
	slices.Sort(packet.WorkflowDocuments)
	packet.WorkflowDocuments = slices.Compact(packet.WorkflowDocuments)
}

func TaskStartPacketJSON(packet TaskStartPacket) ([]byte, error) {
	data, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(data)+1 > MaximumTaskStartBytes {
		return nil, fmt.Errorf("task-start packet exceeds %d bytes", MaximumTaskStartBytes)
	}
	return append(data, '\n'), nil
}
