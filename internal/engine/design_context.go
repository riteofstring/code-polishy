package engine

import (
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
	Protocol        string                                 `json:"protocol"`
	Situations      []string                               `json:"situations"`
	DesignDocuments []repository.ContextDocument           `json:"designDocuments"`
	Handoffs        []repository.OperationalHandoffContext `json:"handoffs"`
}

func (engine *Engine) DesignContext(request ContextRequest) (Report, error) {
	situations, err := policy.HandoffContextSituations(request.Situations, request.Workflow)
	if err != nil {
		return Report{}, err
	}
	if findings := engine.Repository.DesignDocumentFindings(); len(findings) > 0 {
		return engine.finish(findings, nil), nil
	}
	selection, documents, err := engine.contextSelection(request)
	if err != nil {
		return Report{}, err
	}
	handoffs, findings, err := engine.Repository.OperationalHandoffs(repository.HandoffSelection{
		Files: selection.Files, Modules: request.Modules, Situations: request.Situations, Workflow: request.Workflow,
	})
	if err != nil || len(findings) > 0 {
		return engine.finish(findings, nil), err
	}
	context, findings := engine.loadRepositoryContext(documents, handoffs, situations)
	report := engine.finish(findings, nil)
	report.RepositoryContext = context
	if selection.Requested.Mode != "" {
		report.RequestedSelection = &selection.Requested
	}
	return engine.normalizeReport(report), nil
}

func (engine *Engine) contextSelection(request ContextRequest) (repository.Selection, []string, error) {
	if request.Mode == "none" && len(request.Modules) == 0 && len(request.Files) == 0 {
		return repository.Selection{}, nil, nil
	}
	mode, operands := request.Mode, request.Files
	if len(request.Modules) > 0 {
		if len(request.Files) > 0 {
			return repository.Selection{}, nil, fmt.Errorf("choose only one of file selection or --module")
		}
		mode, operands = "modules", request.Modules
	}
	selection, err := engine.Select(mode, operands)
	if err != nil {
		return repository.Selection{}, nil, err
	}
	if len(request.Modules) > 0 {
		documents, err := engine.Repository.DesignDocumentsForModules(request.Modules)
		return selection, documents, err
	}
	return selection, engine.Repository.DesignDocumentsForFiles(selection.Files), nil
}

func (engine *Engine) loadRepositoryContext(paths []string, handoffs []repository.OperationalHandoffContext, situations []string) (*RepositoryContext, []policy.Finding) {
	context := &RepositoryContext{
		Protocol: "repository-context/v1", Situations: reportArray(situations),
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
