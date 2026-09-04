package sourcegraph

const (
	Protocol              = "source-dependency-graph/v1"
	MaximumNodes          = 100000
	MaximumEdges          = 500000
	MaximumTraversalDepth = 100000
	MaximumEncodedBytes   = 64 << 20
	MaximumPathBytes      = 4096
)

type Graph struct {
	Protocol string `json:"protocol"`
	Identity string `json:"identity"`
	Nodes    []Node `json:"nodes"`
	Edges    []Edge `json:"edges"`
}

type Node struct {
	Path       string `json:"path"`
	Language   string `json:"language"`
	Generated  bool   `json:"generated"`
	Test       bool   `json:"test"`
	Root       string `json:"root"`
	Module     string `json:"module"`
	Resolution string `json:"resolutionUnit"`
}

type Edge struct {
	Source           string   `json:"source"`
	Target           string   `json:"target"`
	SourceResolution string   `json:"sourceResolutionUnit"`
	TargetResolution string   `json:"targetResolutionUnit"`
	Line             int      `json:"line"`
	Column           int      `json:"column"`
	Ecosystem        string   `json:"ecosystem"`
	Kind             EdgeKind `json:"kind"`
}

type EdgeKind string

const (
	EdgeRuntime       EdgeKind = "runtime"
	EdgeTypeOnly      EdgeKind = "type-only"
	EdgeReExport      EdgeKind = "re-export"
	EdgeProvenDynamic EdgeKind = "proven-dynamic"
)

type Component struct {
	Classification string `json:"classification"`
	Identity       string `json:"identity"`
	Members        []Node `json:"members"`
	Edges          []Edge `json:"edges"`
	Witness        []Edge `json:"witness"`
}
