package repository

import (
	"fmt"
	"os"
	"slices"
	"sort"

	"github.com/riteofstring/code-polishy/internal/policy"
)

const OperationalHandoffCheck = "policy.operationalHandoff"

type HandoffSelection struct {
	Files      []string
	Modules    []string
	Situations []string
	Workflow   string
}

type HandoffReason struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type OperationalHandoffContext struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Document    ContextDocument `json:"document"`
	Reasons     []HandoffReason `json:"reasons"`
}

func (repo Repository) OperationalHandoffs(selection HandoffSelection) ([]OperationalHandoffContext, []policy.Finding, error) {
	situations, err := policy.HandoffContextSituations(selection.Situations, selection.Workflow)
	if err != nil {
		return nil, nil, err
	}
	modules := map[string]bool{}
	for _, module := range selection.Modules {
		if !repo.hasModule(module) || modules[module] {
			return nil, nil, fmt.Errorf("handoff context requires distinct declared modules: %q", module)
		}
		modules[module] = true
	}
	files := map[string]bool{}
	for _, path := range selection.Files {
		normalized, err := repo.NormalizePath(path)
		if err != nil {
			return nil, nil, err
		}
		files[normalized] = true
		for _, module := range repo.OwnerModuleNames(normalized) {
			modules[module] = true
		}
	}
	return repo.selectedOperationalHandoffs(files, modules, situations)
}

func (repo Repository) selectedOperationalHandoffs(files, modules map[string]bool, situations []string) ([]OperationalHandoffContext, []policy.Finding, error) {
	if len(repo.Config.Documentation.Handoffs) > policy.MaximumOperationalHandoffs {
		return nil, nil, fmt.Errorf("operational handoff inventory exceeds %d declarations", policy.MaximumOperationalHandoffs)
	}
	contexts := []OperationalHandoffContext{}
	findings := []policy.Finding{}
	bytes := 0
	for _, handoff := range repo.Config.Documentation.Handoffs {
		reasons := operationalHandoffReasons(handoff, files, modules, situations)
		if len(reasons) == 0 {
			continue
		}
		document, problems := repo.readOperationalHandoff(handoff)
		findings = append(findings, problems...)
		bytes += len(document.Content)
		if bytes > MaximumContextDocumentSetBytes {
			findings = append(findings, operationalHandoffFinding(handoff, handoff.Path, fmt.Sprintf("selected handoff documents exceed %d bytes", MaximumContextDocumentSetBytes)))
			break
		}
		contexts = append(contexts, OperationalHandoffContext{Name: handoff.Name, Description: handoff.Description, Document: document, Reasons: reasons})
	}
	if len(findings) > 0 {
		return nil, findings, nil
	}
	sort.Slice(contexts, func(left, right int) bool { return contexts[left].Name < contexts[right].Name })
	return contexts, nil, nil
}

func operationalHandoffReasons(handoff policy.OperationalHandoff, files, modules map[string]bool, situations []string) []HandoffReason {
	reasons := []HandoffReason{}
	for _, situation := range handoff.Situations {
		if slices.Contains(situations, situation) {
			reasons = append(reasons, HandoffReason{Kind: "situation", Value: situation})
		}
	}
	for _, module := range handoff.Modules {
		if modules[module] {
			reasons = append(reasons, HandoffReason{Kind: "module", Value: module})
		}
	}
	for _, path := range handoff.SourcePaths {
		if files[path] {
			reasons = append(reasons, HandoffReason{Kind: "source", Value: path})
		}
	}
	sort.Slice(reasons, func(left, right int) bool {
		return reasons[left].Kind+"\x00"+reasons[left].Value < reasons[right].Kind+"\x00"+reasons[right].Value
	})
	return slices.Compact(reasons)
}

func (repo Repository) OperationalHandoffFindings() []policy.Finding {
	findings := []policy.Finding{}
	for _, handoff := range repo.Config.Documentation.Handoffs {
		_, problems := repo.readOperationalHandoff(handoff)
		findings = append(findings, problems...)
	}
	return findings
}

func (repo Repository) readOperationalHandoff(handoff policy.OperationalHandoff) (ContextDocument, []policy.Finding) {
	findings := []policy.Finding{}
	for _, module := range handoff.Modules {
		if !repo.hasModule(module) {
			findings = append(findings, operationalHandoffFinding(handoff, handoff.Path, "handoff references an unknown module: "+module))
		}
	}
	for _, source := range handoff.SourcePaths {
		if message := repo.handoffSourcePathMessage(source); message != "" {
			findings = append(findings, operationalHandoffFinding(handoff, source, message))
		}
	}
	document, err := repo.ReadContextDocument(handoff.Path)
	if err != nil {
		findings = append(findings, operationalHandoffFinding(handoff, handoff.Path, err.Error()))
	}
	return document, findings
}

func (repo Repository) handoffSourcePathMessage(path string) string {
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return err.Error()
	}
	defer root.Close()
	if _, err := repo.containedRegularFileInfo(root, path); err != nil {
		return err.Error()
	}
	if repo.IsExcluded(path) {
		return "handoff source path is excluded from governed scope"
	}
	if len(repo.OwnerModuleNames(path)) != 1 {
		return "handoff source path must belong to exactly one module"
	}
	return ""
}

func operationalHandoffFinding(handoff policy.OperationalHandoff, path, message string) policy.Finding {
	return policy.Finding{
		Check: OperationalHandoffCheck, Path: policy.ConfigFilename, Subject: handoff.Name + ":" + path,
		Related: []policy.FindingLocation{{Path: path, Message: "referenced operational handoff input"}},
		Fields:  map[string]string{"handoff": handoff.Name, "document": handoff.Path},
		Message: "Operational handoff " + handoff.Name + ": " + message,
		Remediation: policy.FindingRemediation{
			Summary:     "Repair the handoff declaration or its repository-owned document and source references.",
			NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "doctor"}, Cwd: "."},
		},
	}
}
