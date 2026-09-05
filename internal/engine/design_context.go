package engine

import (
	"encoding/json"
	"fmt"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type ContextRequest struct {
	Mode       string
	Files      []string
	Modules    []string
	Situations []string
	Workflow   string
}

type RepositoryContext struct {
	DesignResolution repository.DesignResolution            `json:"designResolution"`
	Protocol         string                                 `json:"protocol"`
	Situations       []string                               `json:"situations"`
	DesignDocuments  []repository.ContextDocument           `json:"designDocuments"`
	Handoffs         []repository.OperationalHandoffContext `json:"handoffs"`
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
	if findings := engine.Repository.SelectedDesignDocumentFindings(resolution.DocumentPaths()); len(findings) > 0 {
		return engine.finish(findings, nil), nil
	}
	handoffs, findings, err := engine.Repository.OperationalHandoffs(repository.HandoffSelection{
		Files: selection.Files, Modules: request.Modules, Situations: request.Situations, Workflow: request.Workflow,
	})
	if err != nil || len(findings) > 0 {
		return engine.finish(findings, nil), err
	}
	context, findings := engine.loadRepositoryContext(resolution, handoffs, situations)
	report := engine.finish(findings, nil)
	report.RepositoryContext = context
	if selection.Requested.Mode != "" {
		report.RequestedSelection = &selection.Requested
	}
	return engine.normalizeReport(report), nil
}

func (engine *Engine) contextSelection(request ContextRequest) (repository.Selection, repository.DesignResolution, error) {
	if request.Mode == "none" && len(request.Modules) == 0 && len(request.Files) == 0 {
		resolution, err := engine.Repository.ResolveDesignContext(nil, nil)
		return repository.Selection{}, resolution, err
	}
	mode, operands := request.Mode, request.Files
	if len(request.Modules) > 0 {
		if len(request.Files) > 0 {
			return repository.Selection{}, repository.DesignResolution{}, fmt.Errorf("choose only one of file selection or --module")
		}
		mode, operands = "modules", request.Modules
	}
	var selection repository.Selection
	var err error
	if mode == "modules" {
		selection, err = engine.Repository.DesignModuleSelection(operands)
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
