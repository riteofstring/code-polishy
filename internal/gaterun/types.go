package gaterun

import (
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	Version            = 1
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
	Category           CommandCategory `json:"category"`
	Scope              string          `json:"scope"`
	Cost               string          `json:"cost"`
	Name               string          `json:"name"`
	Provides           []string        `json:"provides"`
	Argv               []string        `json:"argv"`
	Cwd                string          `json:"cwd"`
	Paths              []string        `json:"paths"`
	Modules            []string        `json:"modules"`
	RunOn              []string        `json:"run_on"`
	Environment        []string        `json:"environment"`
	ExclusiveResources []string        `json:"exclusive_resources"`
	TimeoutSeconds     int             `json:"timeout_seconds"`
	Managed            bool            `json:"managed"`
	PassFiles          bool            `json:"pass_files"`
	PassFilePaths      []string        `json:"pass_file_paths"`
	SealedEnvironment  bool            `json:"sealed_environment"`
}

type IdentityInput struct {
	Gate                GateKind
	RequestedBase       string
	ExactBase           string
	Candidate           string
	PolicyLevel         string
	Release             ReleaseIdentity
	ConfigurationSHA256 string
	Platform            Platform
	Commands            []CommandSpec
	Environment         []EnvironmentInput
}

type Identity struct {
	Version             int                      `json:"version"`
	Gate                GateKind                 `json:"gate"`
	RequestedBase       string                   `json:"requested_base"`
	ExactBase           string                   `json:"exact_base"`
	Candidate           string                   `json:"candidate"`
	PolicyLevel         string                   `json:"policy_level"`
	Release             ReleaseIdentity          `json:"release"`
	ConfigurationSHA256 string                   `json:"configuration_sha256"`
	Platform            Platform                 `json:"platform"`
	Commands            []CommandSpec            `json:"commands"`
	Environment         []EnvironmentFingerprint `json:"environment"`
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
}

type CommandOutcome struct {
	CommandIndex  int             `json:"command_index"`
	CommandSHA256 string          `json:"command_sha256"`
	Name          string          `json:"name"`
	Category      CommandCategory `json:"category"`
	Status        CommandStatus   `json:"status"`
	Reused        bool            `json:"reused"`
	Attempts      []Attempt       `json:"attempts"`
	ReceiptPath   string          `json:"receipt_path,omitempty"`
	ReceiptSHA256 string          `json:"receipt_sha256,omitempty"`
}

type Finding struct {
	Check   string `json:"check"`
	Path    string `json:"path"`
	Subject string `json:"subject"`
	Message string `json:"message"`
}

type Report struct {
	Version        int              `json:"version"`
	Identity       Identity         `json:"identity"`
	IdentitySHA256 string           `json:"identity_sha256"`
	Status         RunStatus        `json:"status"`
	StartedAt      time.Time        `json:"started_at"`
	CompletedAt    time.Time        `json:"completed_at"`
	Commands       []CommandOutcome `json:"commands"`
	Findings       []Finding        `json:"findings"`
	Notes          []string         `json:"notes"`
	SHA256         string           `json:"sha256"`
}

type FinalizeOptions struct {
	Status      RunStatus
	Findings    []Finding
	Notes       []string
	CompletedAt time.Time
}

type Receipt struct {
	Version       int             `json:"version"`
	Gate          GateKind        `json:"gate"`
	RunSHA256     string          `json:"run_sha256"`
	CommandSHA256 string          `json:"command_sha256"`
	Category      CommandCategory `json:"category"`
	Status        CommandStatus   `json:"status"`
	LogSHA256     string          `json:"log_sha256"`
	SHA256        string          `json:"sha256"`
}

type ReusableReceipt struct {
	Receipt Receipt
	Path    string
}

type LogOptions struct {
	StreamLimit int
}

type LogResult struct {
	Path            string
	SHA256          string
	Stdout          []byte
	Stderr          []byte
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
	directory      string
	reportPath     string
	identity       Identity
	runSHA256      string
	startedAt      time.Time
	commands       map[int]CommandOutcome
	openLogs       map[int]bool
	finalized      bool
}
