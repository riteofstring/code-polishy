package behaviorreview

import (
	"errors"

	"github.com/riteofstring/code-polishy/internal/architecture"
	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
)

const (
	architectureReviewProtocol = "architecture-review/v1"
	architectureDirectory      = ".code-polishy-reports/architecture-review"
	maximumArchitecturePacket  = 128 << 20
	maximumArchitecturePatch   = 16 << 20
	maximumArchitectureSource  = 8 << 20
	maximumArchitectureSources = 64 << 20
)

var (
	ErrArchitectureReview   = errors.New("invalid architecture review evidence")
	ErrArchitectureRequired = errors.New("architecture review is required")
)

type ArchitectureReviewInput struct {
	Graph    sourcegraph.Graph
	Findings []policy.Finding
}

type ArchitectureReviewStatus struct {
	State         string                      `json:"state"`
	Base          string                      `json:"base"`
	Candidate     string                      `json:"candidate"`
	Topology      string                      `json:"topology"`
	Signals       []architecture.ReviewSignal `json:"signals"`
	ReviewID      string                      `json:"reviewId,omitempty"`
	ReceiptSHA256 string                      `json:"receiptSha256,omitempty"`
	Reason        string                      `json:"reason,omitempty"`
}

type ArchitecturePrepareResult struct {
	ReviewID   string `json:"reviewId"`
	Base       string `json:"base"`
	Candidate  string `json:"candidate"`
	Topology   string `json:"topology"`
	PacketPath string `json:"packetPath"`
	ResultPath string `json:"resultPath"`
}

type ArchitectureReviewResult struct {
	Protocol  string                      `json:"protocol"`
	ReviewID  string                      `json:"reviewId"`
	Base      string                      `json:"base"`
	Candidate string                      `json:"candidate"`
	Topology  string                      `json:"topology"`
	Decision  string                      `json:"decision"`
	Rationale string                      `json:"rationale"`
	Evidence  []ArchitectureCitation      `json:"evidence"`
	Findings  []ArchitectureReviewFinding `json:"findings"`
}

type ArchitectureCitation struct {
	Pointer   string `json:"pointer"`
	Quote     string `json:"quote"`
	Rationale string `json:"rationale"`
}

type ArchitectureReviewFinding struct {
	Summary    string                 `json:"summary"`
	Evidence   []ArchitectureCitation `json:"evidence"`
	Correction ArchitectureCorrection `json:"correction"`
}

type ArchitectureCorrection struct {
	Rationale string                 `json:"rationale"`
	Modules   []policy.Module        `json:"modules"`
	Ownership []policy.TestOwnership `json:"ownership"`
}

type architecturePacket struct {
	Protocol          string                          `json:"protocol"`
	ReviewID          string                          `json:"reviewId"`
	Base              string                          `json:"base"`
	Candidate         string                          `json:"candidate"`
	Graph             sourcegraph.Graph               `json:"graph"`
	Topology          architecture.ReviewTopology     `json:"topology"`
	Signals           []architecture.ReviewSignal     `json:"signals"`
	Cycles            []sourcegraph.Component         `json:"cycles"`
	Summary           []architecture.ModuleSummary    `json:"summary"`
	PreviousCandidate string                          `json:"previousCandidate"`
	TopologyDiff      architecture.ReviewTopologyDiff `json:"topologyDiff"`
	Patch             string                          `json:"patch"`
	Sources           []architectureSource            `json:"sources"`
	DesignDocuments   []packetDesignDocument          `json:"designDocuments"`
	Instructions      string                          `json:"instructions"`
	ResultPath        string                          `json:"resultPath"`
}

type architectureSource struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

type architectureBinding struct {
	Protocol      string `json:"protocol"`
	ReviewID      string `json:"reviewId"`
	Base          string `json:"base"`
	Candidate     string `json:"candidate"`
	Graph         string `json:"graph"`
	Topology      string `json:"topology"`
	SignalsSHA256 string `json:"signalsSha256"`
	PacketSHA256  string `json:"packetSha256"`
}

type architectureReceipt struct {
	architectureBinding
	ResultSHA256 string `json:"resultSha256"`
}

type architectureAccepted struct {
	packet  architecturePacket
	receipt architectureReceipt
	digest  string
}
