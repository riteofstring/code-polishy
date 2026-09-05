package engine

import (
	"context"
	"fmt"

	"github.com/riteofstring/code-polishy/internal/architecture"
	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

func (engine *Engine) ArchitectureReview(ctx context.Context, base, action string) (Report, error) {
	input, report, err := engine.architectureReviewInput(ctx)
	if err != nil || HasFindings(report) {
		return report, err
	}
	switch action {
	case "prepare":
		prepared, err := behaviorreview.PrepareArchitectureReview(ctx, engine.Repository, base, input)
		if err != nil {
			return report, err
		}
		report.ArchitecturePreparation = &prepared
		return report, nil
	case "finalize":
		status, err := behaviorreview.FinalizeArchitectureReview(ctx, engine.Repository, base, input)
		if err != nil {
			return report, err
		}
		return engine.withArchitectureReviewStatus(report, status, base), nil
	case "status":
		status, err := behaviorreview.ArchitectureReviewStatusFor(ctx, engine.Repository, base, input)
		if err != nil {
			return report, err
		}
		return engine.withArchitectureReviewStatus(report, status, base), nil
	default:
		return report, fmt.Errorf("unknown architecture-review action %q", action)
	}
}

func (engine *Engine) architectureReviewInput(ctx context.Context) (behaviorreview.ArchitectureReviewInput, Report, error) {
	candidate, err := engine.Repository.CleanHead()
	if err != nil {
		return behaviorreview.ArchitectureReviewInput{}, Report{}, err
	}
	selection, err := engine.Repository.Select("all", nil)
	if err != nil {
		return behaviorreview.ArchitectureReviewInput{}, Report{}, err
	}
	analysis := architecture.AnalyzeWithRunner(ctx, engine.Repository, selection.Files, engine.Runner)
	findings := append(analysis.Findings, architecture.CoverageFindings(engine.Repository, selection.Files)...)
	findings = append(findings, testpolicy.OwnershipFindings(engine.Repository, selection.Files)...)
	findings = append(findings, engine.Repository.DesignDocumentFindings()...)
	findings = append(findings, engine.PolicyModuleFindings...)
	findings = testpolicy.EnrichOwnershipFindings(engine.Repository, selection.Files, findings, analysis.TestImports)
	report := engine.withSelection(engine.finish(findings, nil), selection)
	report.SourceDependencyGraph = analysis.Graph
	current, err := engine.Repository.CleanHead()
	if err != nil || current != candidate {
		return behaviorreview.ArchitectureReviewInput{}, report, fmt.Errorf("architecture review candidate changed during analysis: %v", err)
	}
	if analysis.Graph == nil {
		if !HasFindings(report) {
			return behaviorreview.ArchitectureReviewInput{}, report, fmt.Errorf("architecture review requires complete source dependency coverage")
		}
		return behaviorreview.ArchitectureReviewInput{}, report, nil
	}
	return behaviorreview.ArchitectureReviewInput{Graph: *analysis.Graph, Findings: report.Findings}, report, nil
}

func (engine *Engine) withArchitectureReviewStatus(report Report, status behaviorreview.ArchitectureReviewStatus, base string) Report {
	findings := architecture.ReviewSignalFindings(status.Signals, status.State == "passed", base)
	if status.State == "required" {
		findings = append(findings, policy.Finding{Check: "policy.architectureReview", Path: policy.ConfigFilename, Subject: "required", Message: status.Reason,
			Remediation: policy.FindingRemediation{Summary: "Prepare a clean architecture review, resolve its findings, and finalize the accepted result.", NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "architecture-review", "prepare", "--base", base}, Cwd: "."}},
		})
	}
	report = engine.combine(report, engine.finish(findings, nil))
	report.ArchitectureReview = &status
	return report
}

func combineArchitectureReview(left, right *behaviorreview.ArchitectureReviewStatus) *behaviorreview.ArchitectureReviewStatus {
	if right != nil {
		return right
	}
	return left
}

func (engine *Engine) Architecture(ctx context.Context, selection repository.Selection) Report {
	analysis := architecture.AnalyzeWithRunner(ctx, engine.Repository, selection.Files, engine.Runner)
	files, err := engine.Repository.AllFiles()
	findings := analysis.Findings
	if analysis.Graph != nil && selection.All {
		topology, topologyErr := architecture.BuildReviewTopology(engine.Repository, *analysis.Graph)
		if topologyErr != nil {
			findings = append(findings, policy.Finding{Check: "architecture.sourceGraphCoverage", Path: "repository", Subject: "review-topology", Message: topologyErr.Error()})
		} else {
			findings = append(findings, architecture.ReviewSignalFindings(architecture.ReviewSignals(topology), false, selection.Base)...)
		}
	}
	if err == nil {
		findings = append(findings, testpolicy.OwnershipFindings(engine.Repository, files)...)
		findings = testpolicy.EnrichOwnershipFindings(engine.Repository, files, findings, analysis.TestImports)
	}
	report := engine.finish(findings, []string{fmt.Sprintf("checked architecture for %d files", len(selection.Files))})
	report.SourceDependencyGraph = analysis.Graph
	if err == nil {
		report.Tables = append(report.Tables, architectureSummaryTable(architecture.Summary(engine.Repository, files, analysis.Graph)))
	}
	return engine.withSelection(report, selection)
}

func architectureSummaryTable(summaries []architecture.ModuleSummary) Table {
	rows := make([][]string, 0, len(summaries))
	for _, summary := range summaries {
		rows = append(rows, []string{
			summary.Name, fmt.Sprint(summary.Production), fmt.Sprint(summary.Tests), fmt.Sprint(summary.Incoming),
			fmt.Sprint(summary.Outgoing), fmt.Sprint(summary.External), fmt.Sprint(summary.FocusedSuites),
		})
	}
	return Table{
		Title:   "ARCHITECTURE SUMMARY",
		Columns: []string{"MODULE", "PRODUCTION", "TESTS", "IN", "OUT", "EXTERNAL", "FOCUSED"},
		Rows:    rows,
	}
}
