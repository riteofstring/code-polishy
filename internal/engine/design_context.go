package engine

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type ContextRequest struct {
	Mode       string
	Files      []string
	NewPaths   []string
	Modules    []string
	Features   []string
	Situations []string
	Workflow   string
}

type RepositoryContext struct {
	DesignResolution repository.DesignResolution            `json:"designResolution"`
	Protocol         string                                 `json:"protocol"`
	Situations       []string                               `json:"situations"`
	DesignDocuments  []repository.ContextDocument           `json:"designDocuments"`
	Handoffs         []repository.OperationalHandoffContext `json:"handoffs"`
	Review           ContextReviewRequirement               `json:"review"`
}

type ContextReviewRequirement struct {
	Required         bool     `json:"required"`
	Reason           string   `json:"reason"`
	SelectedFeatures []string `json:"selectedFeatures"`
}

func (engine *Engine) DesignContext(request ContextRequest) (Report, error) {
	situations, err := policy.HandoffContextSituations(request.Situations, request.Workflow)
	if err != nil {
		return Report{}, err
	}
	selection, resolution, err := engine.contextSelection(request)
	if err != nil {
		return Report{}, err
	}
	if findings := engine.Repository.SelectedDesignDocumentFindings(resolution.DocumentPaths(), selection.Requested.Planned...); len(findings) > 0 {
		return engine.designContextReport(findings), nil
	}
	handoffs, findings, err := engine.Repository.OperationalHandoffs(repository.HandoffSelection{
		Files: selection.Files, Modules: request.Modules, Situations: request.Situations, Workflow: request.Workflow,
	})
	if err != nil || len(findings) > 0 {
		return engine.designContextReport(findings), err
	}
	context, findings := engine.loadRepositoryContext(resolution, handoffs, situations)
	features, err := policy.ResolveBehaviorReviewFeatures(engine.Repository.Config, request.Features)
	if err != nil {
		return Report{}, err
	}
	if context != nil {
		context.Review = engine.contextReviewRequirement(selection, features)
	}
	report := engine.designContextReport(findings)
	report.RepositoryContext = context
	if selection.Requested.Mode != "" {
		report.RequestedSelection = &selection.Requested
	}
	return engine.normalizeReport(report), nil
}

func (engine *Engine) contextReviewRequirement(selection repository.Selection, explicit []string) ContextReviewRequirement {
	configured := engine.contextRequiredFeatures(selection)
	selected := sortedUniqueStrings(append(slices.Clone(explicit), configured...))
	explicitlySelected := len(explicit) > 0
	policySelected := len(configured) > 0 || engine.contextRequiresFullReview(selection)
	reason := "behavior review is optional for the selected scope"
	if explicitlySelected && policySelected {
		reason = "behavior review is explicitly requested and required by policy for the selected scope"
	} else if explicitlySelected {
		reason = "behavior review is explicitly requested"
	} else if policySelected {
		reason = "behavior review is required by policy for the selected scope"
	}
	return ContextReviewRequirement{Required: explicitlySelected || policySelected, Reason: reason, SelectedFeatures: selected}
}

func (engine *Engine) contextRequiredFeatures(selection repository.Selection) []string {
	impact := engine.Repository.CandidateImpact(selection.Candidate)
	result := []string{}
	for _, feature := range behaviorReviewFeatures(engine.Repository.Config) {
		if behaviorReviewFeatureRequired(engine.Repository.Config, feature, impact, BehaviorReviewMerge) {
			result = append(result, feature.Name)
		}
	}
	return sortedUniqueStrings(result)
}

func (engine *Engine) contextRequiresFullReview(selection repository.Selection) bool {
	return !engine.Repository.ClassifyDocumentationCandidate(selection).Ordinary &&
		behaviorReviewPersistentRequirement(engine.Repository.Config, BehaviorReviewMerge, true)
}

func (engine *Engine) designContextReport(findings []policy.Finding) Report {
	return engine.normalizeReport(Report{Findings: findings})
}

func (engine *Engine) contextSelection(request ContextRequest) (repository.Selection, repository.DesignResolution, error) {
	if request.Mode == "none" && len(request.Modules) == 0 && len(request.Files)+len(request.NewPaths) == 0 {
		resolution, err := engine.Repository.ResolveDesignContext(nil, nil)
		return repository.Selection{}, resolution, err
	}
	mode, operands := request.Mode, request.Files
	if len(request.Modules) > 0 {
		if len(request.Files)+len(request.NewPaths) > 0 {
			return repository.Selection{}, repository.DesignResolution{}, fmt.Errorf("choose only one of file selection or --module")
		}
		mode, operands = "modules", request.Modules
	}
	var selection repository.Selection
	var err error
	if mode == "modules" {
		selection, err = engine.Repository.DesignModuleSelection(operands)
	} else if len(request.NewPaths) > 0 {
		if mode != "files" && mode != "" {
			return repository.Selection{}, repository.DesignResolution{}, fmt.Errorf("--new-path can be combined only with --files")
		}
		selection, err = engine.Repository.ContextPathSelection(operands, request.NewPaths)
	} else {
		selection, err = engine.Select(mode, operands)
	}
	if err != nil {
		return repository.Selection{}, repository.DesignResolution{}, err
	}
	resolution, err := engine.Repository.ResolveDesignContext(selection.Files, request.Modules)
	return selection, resolution, err
}

func (engine *Engine) loadRepositoryContext(resolution repository.DesignResolution, handoffs []repository.OperationalHandoffContext, situations []string) (*RepositoryContext, []policy.Finding) {
	paths := resolution.DocumentPaths()
	context := &RepositoryContext{
		DesignResolution: resolution,
		Protocol:         "repository-context/v1", Situations: reportArray(situations),
		DesignDocuments: []repository.ContextDocument{}, Handoffs: reportArray(handoffs),
	}
	if len(paths)+len(handoffs) > 128 {
		return nil, []policy.Finding{contextDocumentFinding("context", "selected context exceeds 128 documents")}
	}
	bytes := 0
	for _, handoff := range handoffs {
		bytes += len(handoff.Document.Content)
	}
	for _, path := range paths {
		document, err := engine.Repository.ReadContextDocument(path)
		if err != nil {
			return nil, []policy.Finding{contextDocumentFinding(path, err.Error())}
		}
		bytes += len(document.Content)
		if bytes > repository.MaximumContextDocumentSetBytes {
			return nil, []policy.Finding{contextDocumentFinding("context", fmt.Sprintf("selected context exceeds %d bytes", repository.MaximumContextDocumentSetBytes))}
		}
		context.DesignDocuments = append(context.DesignDocuments, document)
	}
	data, err := json.Marshal(resolution)
	if err != nil || len(data)+bytes > repository.MaximumContextDocumentSetBytes {
		return nil, []policy.Finding{contextDocumentFinding("context", "design context metadata exceeds the remaining byte boundary or cannot be serialized")}
	}
	return context, nil
}

func contextDocumentFinding(path, message string) policy.Finding {
	return policy.Finding{
		Check: repository.DesignDocumentationCheck, Path: policy.ConfigFilename, Subject: path,
		Message: "Current design context cannot be composed: " + message,
		Remediation: policy.FindingRemediation{
			Summary:     "Repair the selected current design documents and their mappings.",
			NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "doctor"}, Cwd: "."},
		},
	}
}
