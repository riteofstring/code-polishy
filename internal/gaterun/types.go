package gaterun

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
)

const (
	Version            = 5
	DefaultStreamLimit = 1 << 20
	MaximumStreamLimit = 8 << 20
)

var (
	ErrInvalidInput    = errors.New("invalid gate run input")
	ErrMissingArtifact = errors.New("missing gate run artifact")
	ErrStaleArtifact   = errors.New("stale gate run artifact")
	ErrInvalidArtifact = errors.New("invalid gate run artifact")
	ErrIneligible      = errors.New("ineligible gate run receipt")
	ErrOperational     = errors.New("gate run operational failure")
)

type OperationalError struct {
	Operation string
	Err       error
}

func (err *OperationalError) Error() string {
	return fmt.Sprintf("%s: %v", err.Operation, err.Err)
}

func (err *OperationalError) Unwrap() error {
	return errors.Join(ErrOperational, err.Err)
}

func operational(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &OperationalError{Operation: operation, Err: err}
}

type GateKind string

const (
	MergeGate      GateKind = "merge-gate"
	CheckpointGate GateKind = "checkpoint-gate"
)

type CommandCategory string

const (
	OrdinaryTest     CommandCategory = "ordinary-test"
	Check            CommandCategory = "check"
	Build            CommandCategory = "build"
	SupplyChain      CommandCategory = "supply-chain"
	ArtifactSecurity CommandCategory = "artifact-security"
	BehaviorProof    CommandCategory = "behavior-proof"
)

type CommandStatus string

const (
	Passed CommandStatus = "passed"
	Failed CommandStatus = "failed"
)

type RunStatus string

const (
	RunPassed      RunStatus = "passed"
	RunFailed      RunStatus = "failed"
	RunOperational RunStatus = "operational"
)

type BehaviorReviewState string

const (
	BehaviorReviewNotRun   BehaviorReviewState = "not-run"
	BehaviorReviewRequired BehaviorReviewState = "required"
	BehaviorReviewPassed   BehaviorReviewState = "passed"
	BehaviorReviewFailed   BehaviorReviewState = "failed"
)

type BehaviorReviewBoundary string

const (
	BehaviorReviewOnRequest  BehaviorReviewBoundary = "on-request"
	BehaviorReviewMerge      BehaviorReviewBoundary = "merge"
	BehaviorReviewCheckpoint BehaviorReviewBoundary = "checkpoint"
)

type FailureCategory string

const (
	CommandExit FailureCategory = "command-exit"
	Timeout     FailureCategory = "timeout"
	Canceled    FailureCategory = "canceled"
	Environment FailureCategory = "environment"
	Resource    FailureCategory = "resource"
	Operational FailureCategory = "operational"
)

type ReleaseIdentity struct {
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type EnvironmentInput struct {
	Name    string
	Value   string
	Present bool
}

type EnvironmentFingerprint struct {
	Name    string `json:"name"`
	SHA256  string `json:"sha256,omitempty"`
	Present bool   `json:"present"`
}

type CommandSpec struct {
	Category            CommandCategory `json:"category"`
	Scope               string          `json:"scope"`
	Cost                string          `json:"cost"`
	Name                string          `json:"name"`
	Provides            []string        `json:"provides"`
	Argv                []string        `json:"argv"`
	Cwd                 string          `json:"cwd"`
	Paths               []string        `json:"paths"`
	Modules             []string        `json:"modules"`
	RunOn               []string        `json:"run_on"`
	Environment         []string        `json:"environment"`
	ExclusiveResources  []string        `json:"exclusive_resources"`
	TimeoutSeconds      int             `json:"timeout_seconds"`
	Managed             bool            `json:"managed"`
	PassFiles           bool            `json:"pass_files"`
	PassFilePaths       []string        `json:"pass_file_paths"`
	SealedEnvironment   bool            `json:"sealed_environment"`
	Artifacts           []ArtifactSpec  `json:"artifacts"`
	SuiteIdentitySHA256 string          `json:"suite_identity_sha256,omitempty"`
}

type ArtifactSpec struct {
	Path     string `json:"path"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

type BehaviorReviewFeatureSelection struct {
	Name    string   `json:"name"`
	Reasons []string `json:"reasons"`
}

type BehaviorReview struct {
	State            BehaviorReviewState              `json:"state"`
	RequiredBoundary BehaviorReviewBoundary           `json:"required_boundary"`
	SelectedFeatures []BehaviorReviewFeatureSelection `json:"selected_features"`
	SelectionDigest  string                           `json:"selection_digest"`
	FullCandidate    bool                             `json:"full_candidate"`
	ReviewID         string                           `json:"review_id"`
	ReceiptPath      string                           `json:"receipt_path"`
}

type IdentityInput struct {
	Gate                     GateKind
	RequestedBase            string
	ExactBase                string
	Candidate                string
	PolicyLevel              string
	Release                  ReleaseIdentity
	ConfigurationSHA256      string
	ArchitectureReviewSHA256 string
	GitEvidenceSHA256        string
	PolicyValiditySHA256     string
	PythonReachabilitySHA256 string
	Platform                 Platform
	Commands                 []CommandSpec
	Environment              []EnvironmentInput
	AmbientEnvironment       []EnvironmentInput
	BehaviorReview           BehaviorReview
}

type Identity struct {
	Version                  int                      `json:"version"`
	Gate                     GateKind                 `json:"gate"`
	RequestedBase            string                   `json:"requested_base"`
	ExactBase                string                   `json:"exact_base"`
	Candidate                string                   `json:"candidate"`
	PolicyLevel              string                   `json:"policy_level"`
	Release                  ReleaseIdentity          `json:"release"`
	ConfigurationSHA256      string                   `json:"configuration_sha256"`
	ArchitectureReviewSHA256 string                   `json:"architecture_review_sha256"`
	GitEvidenceSHA256        string                   `json:"git_evidence_sha256"`
	PolicyValiditySHA256     string                   `json:"policy_validity_sha256"`
	PythonReachabilitySHA256 string                   `json:"python_reachability_sha256"`
	Platform                 Platform                 `json:"platform"`
	Commands                 []CommandSpec            `json:"commands"`
	Environment              []EnvironmentFingerprint `json:"environment"`
	AmbientEnvironment       []EnvironmentFingerprint `json:"ambient_environment"`
	BehaviorReview           BehaviorReview           `json:"behavior_review"`
}

type CommandRef struct {
	Index  int
	SHA256 string
	Spec   CommandSpec
}

type StartOptions struct {
	RepositoryRoot string
	Identity       Identity
	StartedAt      time.Time
}

type AttemptInput struct {
	Status          CommandStatus
	FailureCategory FailureCategory
	ExitStatus      int
	Duration        time.Duration
	ResourceWait    time.Duration
	Diagnostic      bool
}

type Attempt struct {
	Number                   int             `json:"number"`
	Status                   CommandStatus   `json:"status"`
	FailureCategory          FailureCategory `json:"failure_category,omitempty"`
	ExitStatus               int             `json:"exit_status"`
	DurationMilliseconds     int64           `json:"duration_milliseconds"`
	ResourceWaitMilliseconds int64           `json:"resource_wait_milliseconds"`
	LogPath                  string          `json:"log_path"`
	LogSHA256                string          `json:"log_sha256"`
	StdoutTruncated          bool            `json:"stdout_truncated"`
	StderrTruncated          bool            `json:"stderr_truncated"`
	Diagnostic               bool            `json:"diagnostic"`
}

type CommandOutcome struct {
	CommandIndex              int             `json:"command_index"`
	CommandSHA256             string          `json:"command_sha256"`
	Name                      string          `json:"name"`
	Category                  CommandCategory `json:"category"`
	Status                    CommandStatus   `json:"status"`
	Reused                    bool            `json:"reused"`
	Attempts                  []Attempt       `json:"attempts"`
	ReceiptPath               string          `json:"receipt_path,omitempty"`
	ReceiptSHA256             string          `json:"receipt_sha256,omitempty"`
	ReuseSourcePath           string          `json:"reuse_source_path,omitempty"`
	ReuseSourceSHA256         string          `json:"reuse_source_sha256,omitempty"`
	PriorDurationMilliseconds int64           `json:"prior_duration_milliseconds,omitempty"`
}

type Report struct {
	SourceDependencyGraph *sourcegraph.Graph             `json:"source_dependency_graph,omitempty"`
	Version               int                            `json:"version"`
	Identity              Identity                       `json:"identity"`
	IdentitySHA256        string                         `json:"identity_sha256"`
	ExecutionID           string                         `json:"execution_id"`
	Status                RunStatus                      `json:"status"`
	StartedAt             time.Time                      `json:"started_at"`
	CompletedAt           time.Time                      `json:"completed_at"`
	Commands              []CommandOutcome               `json:"commands"`
	Findings              []policy.Finding               `json:"findings"`
	Suppressed            []policy.Suppressed            `json:"suppressed"`
	Assessed              []policy.AssessedVulnerability `json:"vulnerability_assessments"`
	ReleaseAges           []policy.AssessedReleaseAge    `json:"release_age_assessments"`
	Notes                 []string                       `json:"notes"`
	TestEvidence          []TestEvidence                 `json:"test_evidence"`
	TestDiagnostics       []TestDiagnostic               `json:"test_diagnostics"`
	SuiteSatisfactions    []SuiteSatisfaction            `json:"suite_satisfactions"`
	BehaviorReview        BehaviorReview                 `json:"behavior_review"`
	SHA256                string                         `json:"sha256"`
}

type TestEvidence struct {
	Name                  string          `json:"name"`
	Kind                  string          `json:"kind"`
	Scope                 string          `json:"scope"`
	Cost                  string          `json:"cost"`
	Target                string          `json:"target"`
	SuiteModules          []string        `json:"suite_modules"`
	SuitePaths            []string        `json:"suite_paths"`
	ChangedModules        []string        `json:"changed_modules"`
	ImpactedModules       []string        `json:"impacted_modules"`
	ChangedModuleOverlap  []string        `json:"changed_module_overlap"`
	ImpactedModuleOverlap []string        `json:"impacted_module_overlap"`
	ChangedPathOverlap    []string        `json:"changed_path_overlap"`
	Status                CommandStatus   `json:"status"`
	FailureCategory       FailureCategory `json:"failure_category,omitempty"`
	FailureMessage        string          `json:"failure_message,omitempty"`
	Attempt               int             `json:"attempt"`
	LogPath               string          `json:"log_path,omitempty"`
	Diagnostic            bool            `json:"diagnostic"`
	Artifacts             []TestArtifact  `json:"artifacts"`
	Reused                bool            `json:"reused"`
	ReceiptSourcePath     string          `json:"receipt_source_path,omitempty"`
	ReceiptSourceSHA256   string          `json:"receipt_source_sha256,omitempty"`
}

type TestArtifact struct {
	Suite    string `json:"suite"`
	Path     string `json:"path"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

type TestDiagnostic struct {
	Suite          string        `json:"suite"`
	State          string        `json:"state"`
	CandidateRetry *TestEvidence `json:"candidate_retry,omitempty"`
	BaselineReplay *TestEvidence `json:"baseline_replay,omitempty"`
}

type SuiteSatisfactionInput struct {
	Suite      string
	ExecutedBy string
	Reason     string
}

type SuiteSatisfaction struct {
	Suite         string `json:"suite"`
	ExecutedBy    string `json:"executed_by"`
	Reason        string `json:"reason"`
	ReceiptPath   string `json:"receipt_path"`
	ReceiptSHA256 string `json:"receipt_sha256"`
}

type FinalizeOptions struct {
	SourceDependencyGraph *sourcegraph.Graph
	Status                RunStatus
	Findings              []policy.Finding
	Suppressed            []policy.Suppressed
	Assessed              []policy.AssessedVulnerability
	ReleaseAges           []policy.AssessedReleaseAge
	Notes                 []string
	TestEvidence          []TestEvidence
	TestDiagnostics       []TestDiagnostic
	SuiteSatisfactions    []SuiteSatisfactionInput
	BehaviorReview        BehaviorReview
	CompletedAt           time.Time
}

type ExecutionEvidence struct {
	Gate           GateKind `json:"gate"`
	IdentitySHA256 string   `json:"identity_sha256"`
	ExecutionID    string   `json:"execution_id"`
	ReportSHA256   string   `json:"report_sha256"`
}

type PreparedFinalization struct {
	run       *Run
	report    Report
	completed bool
}

type Receipt struct {
	Version                            int             `json:"version"`
	Gate                               GateKind        `json:"gate"`
	RunSHA256                          string          `json:"run_sha256"`
	ExecutionID                        string          `json:"execution_id"`
	CommandSHA256                      string          `json:"command_sha256"`
	Category                           CommandCategory `json:"category"`
	Status                             CommandStatus   `json:"status"`
	LogSHA256                          string          `json:"log_sha256"`
	SourceExecutionID                  string          `json:"source_execution_id,omitempty"`
	SourceReceiptSHA256                string          `json:"source_receipt_sha256,omitempty"`
	ExternalSourcePath                 string          `json:"external_source_path,omitempty"`
	ExternalSourceSHA256               string          `json:"external_source_sha256,omitempty"`
	ExternalSourceDurationMilliseconds int64           `json:"external_source_duration_milliseconds,omitempty"`
	SHA256                             string          `json:"sha256"`
}

type ReusableReceipt struct {
	Receipt Receipt
	Path    string
}

type SuiteReuse struct {
	IdentitySHA256 string
	ReceiptPath    string
	ReceiptSHA256  string
	DurationMillis int64
}

type LogOptions struct {
	StreamLimit int
}

type LogResult struct {
	Path            string
	SHA256          string
	Stdout          []byte
	Stderr          []byte
	StdoutTail      []byte
	StderrTail      []byte
	StdoutBytes     int64
	StderrBytes     int64
	StdoutTruncated bool
	StderrTruncated bool
	StreamLimit     int
}

type CommandLog struct {
	stdout *boundedBuffer
	stderr *boundedBuffer
	close  func(*CommandLog) (LogResult, error)
	closed bool
}

func (log *CommandLog) Stdout() io.Writer {
	return log.stdout
}

func (log *CommandLog) Stderr() io.Writer {
	return log.stderr
}

func (log *CommandLog) Close() (LogResult, error) {
	if log == nil || log.close == nil {
		return LogResult{}, fmt.Errorf("%w: command log is invalid", ErrInvalidInput)
	}
	if log.closed {
		return LogResult{}, fmt.Errorf("%w: command log is already closed", ErrInvalidInput)
	}
	log.closed = true
	return log.close(log)
}

type Run struct {
	repositoryRoot string
	runDirectory   artifactDirectory
	directory      artifactDirectory
	report         artifactFile
	reportPath     string
	executionID    string
	identity       Identity
	runSHA256      string
	startedAt      time.Time
	commands       map[int]CommandOutcome
	openLogs       map[int]bool
	preparation    *PreparedFinalization
	finalized      bool
}
