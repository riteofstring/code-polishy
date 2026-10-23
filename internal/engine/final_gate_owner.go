package engine

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/supplychain"
	workflowfacts "github.com/riteofstring/code-polishy/internal/workflow"
)

func finalGateOwnerFindings(repo repository.Repository, files []string, gitLab supplychain.GitLabInspection) []policy.Finding {
	if repo.Config.Verification.EffectiveFinalGateOwner() != policy.FinalGateOwnerCI {
		return nil
	}
	for _, script := range gitLab.UnconditionalScripts {
		if workflowfacts.HasMergeGateCommand(script, "sh") {
			return nil
		}
	}
	findings := append([]policy.Finding{}, gitLab.GateRejections...)
	for _, path := range files {
		if !finalGateWorkflowPath(path) {
			continue
		}
		accepted, rejected := finalGateWorkflowFindings(repo, path)
		if accepted {
			return nil
		}
		findings = append(findings, rejected...)
	}
	if len(findings) > 0 {
		return findings
	}
	return []policy.Finding{{
		Check: "policy.finalGateOwner", Path: receiptConfigurationPath(repo), Subject: "ci",
		Message: "verification.finalGateOwner is ci, but no active merge workflow declares a supported dedicated gate step; use one code-polishy or repository-wrapper merge-gate --base REF command with a standard shell, and move setup to earlier steps or jobs",
	}}
}

func finalGateWorkflowFindings(repo repository.Repository, path string) (bool, []policy.Finding) {
	facts, err := workflowfacts.Read(path, repo.Read)
	if err != nil {
		return false, []policy.Finding{gateDeclarationFinding(path, "workflow", "cannot read workflow facts: "+err.Error(), 0)}
	}
	findings := []policy.Finding{}
	mergeEvent := false
	for _, trigger := range facts.Triggers {
		if !slices.Contains([]string{"push", "pull_request", "merge_group"}, trigger.Name) {
			continue
		}
		mergeEvent = true
		accepted, rejected := workflowEventGateFindings(facts, trigger.Name)
		if accepted {
			return true, nil
		}
		findings = append(findings, rejected...)
	}
	if len(findings) == 0 {
		reason := "workflow contains no dedicated merge-gate command"
		if !mergeEvent {
			reason = "workflow has no push, pull_request, or merge_group trigger"
		}
		findings = append(findings, gateDeclarationFinding(path, "workflow", reason, 0))
	}
	return false, findings
}

func workflowEventGateFindings(facts workflowfacts.Facts, event string) (bool, []policy.Finding) {
	findings := []policy.Finding{}
	for _, job := range facts.Jobs {
		for index, step := range job.Steps {
			if !strings.Contains(step.Run, "merge-gate") {
				continue
			}
			reason := gateStepIssue(facts.Jobs, job, step, event)
			if reason == "" {
				return true, nil
			}
			subject := fmt.Sprintf("job %s, step %d", job.ID, index+1)
			if step.ID != "" {
				subject = fmt.Sprintf("job %s, step %s", job.ID, step.ID)
			}
			findings = append(findings, gateDeclarationFinding(facts.Path, subject, reason, step.Line))
		}
	}
	return false, findings
}

func gateStepIssue(jobs []workflowfacts.Job, job workflowfacts.Job, step workflowfacts.Step, event string) string {
	switch {
	case job.AllowsFailure:
		return "job permits failure through continue-on-error"
	case !workflowfacts.JobRunsOnEvent(jobs, job.ID, event):
		return "job or a prerequisite is not statically active for " + event
	case step.AllowsFailure:
		return "step permits failure through continue-on-error"
	case !workflowfacts.ConditionRunsOnEvent(step.Condition, event):
		return "step condition is not statically active for " + event
	case step.ShellStartupUnknown:
		return "effective BASH_ENV, SHELLOPTS, or PS4 environment contains unsupported shell startup settings"
	default:
		return workflowfacts.MergeGateCommandIssue(step.Run, step.Shell)
	}
}

func gateDeclarationFinding(path, subject, reason string, line int) policy.Finding {
	return policy.Finding{
		Check: "policy.finalGateOwner", Path: path, Subject: subject, Line: line,
		Message: "cannot establish CI gate ownership: " + reason,
	}
}

func finalGateWorkflowPath(path string) bool {
	normalized := filepath.ToSlash(path)
	extension := strings.ToLower(filepath.Ext(normalized))
	if extension != ".yml" && extension != ".yaml" {
		return false
	}
	return strings.HasPrefix(normalized, ".github/workflows/")
}
