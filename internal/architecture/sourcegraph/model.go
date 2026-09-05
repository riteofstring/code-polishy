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
	Protocol string                `json:"protocol"`
	Identity string                `json:"identity"`
	Nodes    []Node                `json:"nodes"`
	Edges    []Edge                `json:"edges"`
	Inputs   []FactInput           `json:"inputs"`
	External []ExternalComposition `json:"externalCompositions"`
}

type ExternalComposition struct {
	Source           string             `json:"source"`
	SourceResolution string             `json:"sourceResolutionUnit"`
	Line             int                `json:"line"`
	Column           int                `json:"column"`
	Dependency       ExternalDependency `json:"dependency"`
	Contract         ExternalContract   `json:"contract"`
}

type ExternalDependency struct {
	Project        string `json:"project"`
	Lock           string `json:"lock"`
	ManifestSHA256 string `json:"manifestSha256"`
	LockSHA256     string `json:"lockSha256"`
	Distribution   string `json:"distribution"`
	Version        string `json:"version"`
	Kind           string `json:"kind"`
	Source         string `json:"source"`
	Namespace      string `json:"namespace"`
}

type ExternalContract struct {
	InputGrammar  string `json:"inputGrammar"`
	CheckKind     string `json:"checkKind"`
	Protocol      string `json:"protocol"`
	RuntimeType   string `json:"runtimeType"`
	CheckLine     int    `json:"checkLine"`
	CheckColumn   int    `json:"checkColumn"`
	SourceSHA256  string `json:"sourceSha256"`
	RuntimeSHA256 string `json:"runtimeSha256"`
	InputSHA256   string `json:"inputSha256"`
}

type FactInput struct {
	Analyzer         string   `json:"analyzer"`
	Protocol         string   `json:"protocol"`
	Project          string   `json:"project"`
	Root             string   `json:"root"`
	Paths            []string `json:"paths"`
	FactsSHA256      string   `json:"factsSha256"`
	PartitionsSHA256 string   `json:"partitionsSha256"`
	ResolutionSHA256 string   `json:"resolutionSha256"`
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
