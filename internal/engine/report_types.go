package engine

import (
	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/supplychain"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

type Report struct {
	Schema                  string                                    `json:"$schema"`
	Protocol                string                                    `json:"protocol"`
	Command                 string                                    `json:"command"`
	ReportPath              string                                    `json:"reportPath"`
	Execution               *ExecutionTelemetry                       `json:"execution,omitempty"`
	RequestedSelection      *repository.RequestedSelection            `json:"requestedSelection,omitempty"`
	AnalysisContext         []AnalysisContext                         `json:"analysisContext"`
	RepositoryContext       *RepositoryContext                        `json:"repositoryContext,omitempty"`
	SourceDependencyGraph   *sourcegraph.Graph                        `json:"sourceDependencyGraph,omitempty"`
	ArchitectureReview      *behaviorreview.ArchitectureReviewStatus  `json:"architectureReview,omitempty"`
	ArchitecturePreparation *behaviorreview.ArchitecturePrepareResult `json:"architecturePreparation,omitempty"`
	Summary                 ReportSummary                             `json:"summary"`
	Formatting              *FormatOutcome                            `json:"formatting,omitempty"`
	Display                 *ReportDisplay                            `json:"display,omitempty"`
	BehaviorReview          *BehaviorReviewStatus                     `json:"behaviorReview,omitempty"`
	MergePolicy             *MergePolicy                              `json:"mergePolicy,omitempty"`
	CheckpointPolicy        *CheckpointPolicy                         `json:"checkpointPolicy,omitempty"`
	GateRunPolicy           *GateRunPolicy                            `json:"gateRunPolicy,omitempty"`
	ChangeBoundary          *ChangeBoundary                           `json:"changeBoundary,omitempty"`
	ChangedTestScope        *ChangedTestScope                         `json:"changedTestScope,omitempty"`
	TestQualityReminder     *TestQualityReminder                      `json:"testQualityReminder,omitempty"`
	TestCommands            []TestCommandEvidence                     `json:"testCommands"`
	TestDiagnostics         []TestFailureDiagnostic                   `json:"testDiagnostics"`
	TestAggregations        []testpolicy.SuiteAggregation             `json:"testAggregations"`
	Findings                []policy.Finding                          `json:"findings"`
	Suppressed              []policy.Suppressed                       `json:"suppressed"`
	Assessed                []policy.AssessedVulnerability            `json:"vulnerabilityAssessments"`
	ReleaseAges             []policy.AssessedReleaseAge               `json:"releaseAgeAssessments"`
	GitEvidence             []supplychain.GitEvidenceReceipt          `json:"gitEvidence,omitempty"`
	Tables                  []Table                                   `json:"tables"`
	Notes                   []string                                  `json:"notes"`
}

type ExecutionTelemetry struct {
	EvaluationDurationMilliseconds int64              `json:"evaluationDurationMilliseconds"`
	Scope                          ExecutionScope     `json:"scope"`
	Phases                         []ExecutionPhase   `json:"phases"`
	Commands                       []ExecutionCommand `json:"commands"`
	Caches                         []ExecutionCache   `json:"caches"`
}

type ExecutionScope struct {
	RequestedOperands int `json:"requestedOperands"`
	SelectedPaths     int `json:"selectedPaths"`
	ContextPaths      int `json:"contextPaths"`
	GraphNodes        int `json:"graphNodes"`
	GraphEdges        int `json:"graphEdges"`
}

type ExecutionPhase struct {
	Name                 string `json:"name"`
	DurationMilliseconds int64  `json:"durationMilliseconds"`
}

type ExecutionCommand struct {
	Phase                string           `json:"phase"`
	Name                 string           `json:"name"`
	Cwd                  string           `json:"cwd"`
	Argv                 []string         `json:"argv"`
	DurationMilliseconds int64            `json:"durationMilliseconds"`
	ResourceWaitMillis   int64            `json:"resourceWaitMilliseconds"`
	ExitStatus           int              `json:"exitStatus"`
	FailureCategory      string           `json:"failureCategory,omitempty"`
	Phases               []ExecutionPhase `json:"phases,omitempty"`
}

type ExecutionCache struct {
	Name   string `json:"name"`
	Scope  string `json:"scope"`
	Hits   int64  `json:"hits"`
	Misses int64  `json:"misses"`
	Builds int64  `json:"builds"`
}

type AnalysisContext struct {
	Analyzer string   `json:"analyzer"`
	Root     string   `json:"root"`
	Reason   string   `json:"reason"`
	Paths    []string `json:"paths"`
}

type ReportSummary struct {
	Status      string                           `json:"status"`
	Errors      int                              `json:"errors"`
	Warnings    int                              `json:"warnings"`
	Information int                              `json:"information"`
	Suppressed  int                              `json:"suppressed"`
	Reviewed    int                              `json:"reviewed"`
	ByRelation  map[policy.SelectionRelation]int `json:"bySelectionRelation"`
	ByRule      map[string]int                   `json:"byRule"`
	ByModule    map[string]int                   `json:"byModule"`
}

type ReportFilters struct {
	Rules     []string                   `json:"rules,omitempty"`
	Modules   []string                   `json:"modules,omitempty"`
	Paths     []string                   `json:"paths,omitempty"`
	Relations []policy.SelectionRelation `json:"relations,omitempty"`
}

type ReportDisplay struct {
	Filters   ReportFilters `json:"filters"`
	GroupBy   string        `json:"groupBy,omitempty"`
	Limit     int           `json:"limit,omitempty"`
	Total     int           `json:"total"`
	Displayed int           `json:"displayed"`
	Omitted   int           `json:"omitted"`
}

type DisplayOptions struct {
	Filters ReportFilters
	GroupBy string
	Limit   int
}

type MergePolicy struct {
	Level   string   `json:"level"`
	Base    string   `json:"base"`
	Reasons []string `json:"reasons"`
}

type CheckpointPolicy struct {
	Scope       string `json:"scope"`
	Base        string `json:"base"`
	Candidate   string `json:"candidate"`
	ReceiptPath string `json:"receiptPath"`
}

type GateRunPolicy struct {
	Status       string   `json:"status"`
	ReportPath   string   `json:"reportPath"`
	ReusedPhases []string `json:"reusedPhases"`
}

type ChangeBoundary struct {
	Base     string   `json:"base"`
	Modules  []string `json:"modules"`
	Paths    []string `json:"paths"`
	NewPaths []string `json:"newPaths"`
}

type TestQualityReminder struct {
	ChangedTestPaths     []string `json:"changedTestPaths"`
	TaskBase             string   `json:"taskBase"`
	TaskChangedTestPaths []string `json:"taskChangedTestPaths"`
}

type Table struct {
	Title   string     `json:"title"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

func combineSourceDependencyGraph(left, right *sourcegraph.Graph) *sourcegraph.Graph {
	if right != nil {
		return right
	}
	return left
}

func combineRequestedSelection(left, right *repository.RequestedSelection) *repository.RequestedSelection {
	if right != nil {
		return right
	}
	return left
}
