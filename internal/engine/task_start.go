package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const MaximumTaskStartBytes = 16 << 20

type TaskStartRequest struct {
	IntentPath string
	Intent     io.Reader
	Features   []string
	Context    ContextRequest
}

type TaskStartPacket struct {
	Protocol           string                        `json:"protocol"`
	TaskBase           string                        `json:"taskBase"`
	LockedRelease      release.Lock                  `json:"lockedRelease"`
	CatalogSHA256      string                        `json:"catalogSha256"`
	Intent             TaskStartIntent               `json:"intent"`
	RequestedSelection repository.RequestedSelection `json:"requestedSelection"`
	RepositoryContext  *RepositoryContext            `json:"repositoryContext"`
	WorkflowDocuments  []string                      `json:"workflowDocuments"`
	ConfiguredGuards   []CapabilityEntry             `json:"configuredGuards"`
	Verification       policy.Verification           `json:"verification"`
	FinalGateOwner     string                        `json:"finalGateOwner"`
	NextActions        []TaskStartAction             `json:"nextActions"`
}

type TaskStartIntent struct {
	Captured         bool                         `json:"captured"`
	WillBeUsed       bool                         `json:"willBeUsed"`
	Reason           string                       `json:"reason"`
	SelectedFeatures []string                     `json:"selectedFeatures"`
	Capture          *BehaviorReviewIntentCapture `json:"capture,omitempty"`
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
	snapshot, err := engine.taskStartSnapshot()
	if err != nil {
		return nil, err
	}
	packet, features, err := engine.taskStartPacket(request.Context, request.Features, snapshot.Head)
	if err != nil {
		return nil, err
	}
	if packet.Intent.WillBeUsed {
		return engine.finishTaskStartWithCapture(ctx, packet, request, features, snapshot)
	}
	return engine.finishTaskStartWithoutCapture(packet, request, snapshot)
}

func (engine *Engine) taskStartSnapshot() (repository.CandidateStateSnapshot, error) {
	snapshot, err := engine.Repository.CandidateState()
	if err != nil {
		return repository.CandidateStateSnapshot{}, err
	}
	if snapshot.Dirty {
		return repository.CandidateStateSnapshot{}, fmt.Errorf("%w: task-start requires a clean task base; capture an artifact-affecting correction with behavior-review capture-intent", repository.ErrDirtyCandidate)
	}
	return snapshot, nil
}

func (engine *Engine) finishTaskStartWithCapture(ctx context.Context, packet TaskStartPacket, request TaskStartRequest, features []string, snapshot repository.CandidateStateSnapshot) ([]byte, error) {
	if !taskStartHasIntent(request) {
		return nil, fmt.Errorf("task-start requires intent because behavior review is selected; use --intent-file PATH or --intent-file - for standard input")
	}
	prepared, err := behaviorreview.PrepareIntentCapture(ctx, engine.Repository, behaviorreview.CaptureIntentOptions{
		IntentPath: request.IntentPath, Intent: request.Intent, Features: features,
	})
	if err != nil {
		return nil, err
	}
	capture := prepared.Result()
	if capture.Commit != snapshot.Head || capture.CandidateSHA256 != snapshot.SHA256 {
		return nil, fmt.Errorf("%w: candidate changed while task-start was prepared", behaviorreview.ErrCandidateChanged)
	}
	packet.Intent.Captured = true
	packet.Intent.Capture = &capture
	packet.NextActions = taskStartActions(packet)
	data, err := TaskStartPacketJSON(packet)
	if err != nil {
		return nil, err
	}
	if _, err := prepared.Commit(ctx); err != nil {
		return nil, err
	}
	return data, nil
}

func taskStartHasIntent(request TaskStartRequest) bool {
	return request.IntentPath != "" || request.Intent != nil
}

func (engine *Engine) finishTaskStartWithoutCapture(packet TaskStartPacket, request TaskStartRequest, snapshot repository.CandidateStateSnapshot) ([]byte, error) {
	if taskStartHasIntent(request) {
		return nil, fmt.Errorf("task-start will not use intent because behavior review is optional for the selected scope; omit --intent-file")
	}
	packet.NextActions = taskStartActions(packet)
	data, err := TaskStartPacketJSON(packet)
	if err != nil {
		return nil, err
	}
	current, err := engine.Repository.CandidateState()
	if err != nil {
		return nil, err
	}
	if current != snapshot {
		return nil, fmt.Errorf("%w: candidate changed while task-start was prepared", behaviorreview.ErrCandidateChanged)
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

func (engine *Engine) taskStartPacket(request ContextRequest, requestedFeatures []string, taskBase string) (TaskStartPacket, []string, error) {
	request.Workflow = "task-start"
	contextReport, err := engine.DesignContext(request)
	if err != nil {
		return TaskStartPacket{}, nil, err
	}
	if HasFindings(contextReport) || contextReport.RepositoryContext == nil || contextReport.RequestedSelection == nil {
		return TaskStartPacket{}, nil, fmt.Errorf("task-start cannot compose invalid context; use design-context with the same selection and situations for findings")
	}
	selection, err := engine.taskStartSelection(request)
	if err != nil {
		return TaskStartPacket{}, nil, err
	}
	features, err := policy.ResolveBehaviorReviewFeatures(engine.Repository.Config, requestedFeatures)
	if err != nil {
		return TaskStartPacket{}, nil, err
	}
	inventory, err := engine.Capabilities("")
	if err != nil {
		return TaskStartPacket{}, nil, err
	}
	if inventory.LockedRelease == nil || inventory.ReleaseCatalog.Availability != "available" {
		return TaskStartPacket{}, nil, fmt.Errorf("task-start requires an authenticated locked capability catalog: %s", inventory.ReleaseCatalog.Reason)
	}
	packet := TaskStartPacket{
		Protocol: "task-start/v2", TaskBase: taskBase, LockedRelease: *inventory.LockedRelease, CatalogSHA256: inventory.ReleaseCatalog.SHA256,
		Intent: engine.taskStartIntent(selection, features), RequestedSelection: *contextReport.RequestedSelection, RepositoryContext: contextReport.RepositoryContext,
		WorkflowDocuments: []string{"docs/agent-workflows.md"}, ConfiguredGuards: []CapabilityEntry{},
		Verification:   engine.Repository.Config.Verification,
		FinalGateOwner: engine.Repository.Config.Verification.EffectiveFinalGateOwner(),
	}
	packet.collectGuards(inventory.Capabilities)
	return packet, features, nil
}

func (engine *Engine) taskStartSelection(request ContextRequest) (repository.Selection, error) {
	if request.Mode == "files" {
		return engine.Repository.Select("files", request.Files)
	}
	return engine.Repository.Select("modules", request.Modules)
}

func (engine *Engine) taskStartIntent(selection repository.Selection, explicit []string) TaskStartIntent {
	configured := engine.taskStartRequiredFeatures(selection)
	selected := sortedUniqueStrings(append(slices.Clone(explicit), configured...))
	explicitlySelected := len(explicit) > 0
	policySelected := len(configured) > 0 || engine.taskStartRequiresFullCandidate(selection)
	reason := "behavior review is optional for the selected scope"
	if explicitlySelected && policySelected {
		reason = "behavior review is explicitly requested and required by policy for the selected scope"
	} else if explicitlySelected {
		reason = "behavior review is explicitly requested"
	} else if policySelected {
		reason = "behavior review is required by policy for the selected scope"
	}
	return TaskStartIntent{WillBeUsed: explicitlySelected || policySelected, Reason: reason, SelectedFeatures: selected}
}

func (engine *Engine) taskStartRequiredFeatures(selection repository.Selection) []string {
	impact := engine.Repository.CandidateImpact(selection.Candidate)
	result := []string{}
	for _, feature := range behaviorReviewFeatures(engine.Repository.Config) {
		if behaviorReviewFeatureRequired(engine.Repository.Config, feature, impact, BehaviorReviewMerge) {
			result = append(result, feature.Name)
		}
	}
	return sortedUniqueStrings(result)
}

func (engine *Engine) taskStartRequiresFullCandidate(selection repository.Selection) bool {
	return !engine.Repository.ClassifyDocumentationCandidate(selection).Ordinary &&
		behaviorReviewPersistentRequirement(engine.Repository.Config, BehaviorReviewMerge, true)
}

func (packet *TaskStartPacket) collectGuards(entries []CapabilityEntry) {
	for _, entry := range entries {
		if taskStartGuard(entry) {
			packet.ConfiguredGuards = append(packet.ConfiguredGuards, entry)
		}
		if packet.selectsTaskStartWorkflow(entry) {
			packet.WorkflowDocuments = append(packet.WorkflowDocuments, entry.Workflows...)
		}
	}
	slices.Sort(packet.WorkflowDocuments)
	packet.WorkflowDocuments = slices.Compact(packet.WorkflowDocuments)
}

func taskStartGuard(entry CapabilityEntry) bool {
	return entry.Kind == "check" || entry.Kind == "pack-capability" || entry.Kind == "behavior-feature"
}

func (packet TaskStartPacket) selectsTaskStartWorkflow(entry CapabilityEntry) bool {
	taskStart := entry.Name == "task-start" && entry.Kind == "command"
	selectedFeature := entry.Kind == "behavior-feature" && slices.Contains(packet.Intent.SelectedFeatures, entry.Name)
	selectedReview := entry.Name == "behavior-review" && entry.Kind == "command" && packet.Intent.WillBeUsed
	return taskStart || selectedFeature || selectedReview
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
