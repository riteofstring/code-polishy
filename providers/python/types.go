package main

import "encoding/json"

type request struct {
	Provider        string            `json:"provider"`
	Inventory       []inventoryEntry  `json:"inventory"`
	Selection       selectionInput    `json:"selection"`
	Scopes          []analysisScope   `json:"scopes"`
	DiagnosticFiles []string          `json:"diagnosticFiles"`
	WriteFiles      []string          `json:"writeFiles"`
	ProtocolVersion int               `json:"protocolVersion"`
	Operation       string            `json:"operation"`
	Capability      string            `json:"capability"`
	ProjectRoot     string            `json:"projectRoot"`
	Files           []string          `json:"files"`
	Modules         []json.RawMessage `json:"modules"`
	Mode            string            `json:"mode"`
	Profile         string            `json:"profile"`
	OutputDirectory string            `json:"outputDirectory,omitempty"`
	Context         []inputFile       `json:"context"`
	Policy          json.RawMessage   `json:"policy"`
	Tools           []toolIdentity    `json:"tools"`
	Complete        bool              `json:"complete"`
	Pack            packSelection     `json:"pack"`
}

type selectionInput struct {
	Paths    []string `json:"paths"`
	Deleted  []string `json:"deleted"`
	Complete bool     `json:"complete"`
}

type inventoryEntry struct {
	Path        string   `json:"path"`
	Language    string   `json:"language,omitempty"`
	Context     string   `json:"context,omitempty"`
	Owner       string   `json:"owner,omitempty"`
	Modules     []string `json:"modules"`
	Source      bool     `json:"source"`
	Metadata    bool     `json:"metadata"`
	Dependency  bool     `json:"dependency"`
	Asset       bool     `json:"asset"`
	Test        bool     `json:"test"`
	Generated   bool     `json:"generated"`
	Data        bool     `json:"data"`
	Development bool     `json:"development"`
	Control     bool     `json:"control"`
}

type analysisScope struct {
	Handle     string          `json:"handle"`
	Language   string          `json:"language"`
	Root       string          `json:"root"`
	Members    []string        `json:"members"`
	EntryFiles []string        `json:"entryFiles"`
	Context    []string        `json:"context"`
	Data       json.RawMessage `json:"data"`
}

type packSelection struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

type toolIdentity struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type response struct {
	ProtocolVersion int               `json:"protocolVersion"`
	Status          string            `json:"status"`
	ScopeHandles    []string          `json:"scopeHandles,omitempty"`
	Discovery       *discoveryResult  `json:"discovery,omitempty"`
	Evidence        []string          `json:"evidence,omitempty"`
	Findings        []responseFinding `json:"findings,omitempty"`
	Notes           []string          `json:"notes,omitempty"`
	Failure         string            `json:"failure,omitempty"`
	Coverage        *coverage         `json:"coverage,omitempty"`
	Facts           *sourceFacts      `json:"facts,omitempty"`
	Inputs          []inputFile       `json:"inputs,omitempty"`
	Edits           []edit            `json:"edits,omitempty"`
}

type discoveryResult struct {
	Scopes []discoveredScope `json:"scopes"`
}

type discoveredScope struct {
	ID         string          `json:"id"`
	Language   string          `json:"language"`
	Root       string          `json:"root"`
	Members    []string        `json:"members"`
	EntryFiles []string        `json:"entryFiles"`
	Context    []string        `json:"context"`
	Selected   []string        `json:"selected"`
	Data       json.RawMessage `json:"data"`
}

type inputFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type responseFinding struct {
	Capability string `json:"capability"`
	Path       string `json:"path"`
	Line       int    `json:"line,omitempty"`
	Column     int    `json:"column,omitempty"`
	Subject    string `json:"subject"`
	Message    string `json:"message"`
	Rule       string `json:"rule"`
}

type coverage struct {
	Analyzed    []string      `json:"analyzed"`
	Unsupported []unsupported `json:"unsupported"`
}

type unsupported struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type sourceFacts struct {
	Imports   *[]importFact   `json:"imports,omitempty"`
	Comments  *[]commentFact  `json:"comments,omitempty"`
	Functions *[]functionFact `json:"functions,omitempty"`
}

type importFact struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Specifier string `json:"specifier"`
	Resolved  string `json:"resolved"`
	Package   string `json:"package"`
	Kind      string `json:"kind"`
}

type authoredImport struct {
	Path   string   `json:"path"`
	Module string   `json:"module"`
	Names  []string `json:"names"`
	Line   int      `json:"line"`
	Column int      `json:"column"`
	Kind   string   `json:"kind"`
}

type dynamicImport struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Callee string `json:"callee"`
}

type commentFact struct {
	Path             string `json:"path"`
	Line             int    `json:"line"`
	Column           int    `json:"column"`
	Kind             string `json:"kind"`
	Raw              string `json:"raw"`
	Complete         bool   `json:"complete"`
	BeforeCode       bool   `json:"beforeCode"`
	Preamble         bool   `json:"preamble"`
	ByteZero         bool   `json:"byteZero"`
	MachineDirective bool   `json:"machineDirective,omitempty"`
}

type functionFact struct {
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
	Name       string `json:"name"`
	Complexity int    `json:"complexity"`
	Depth      int    `json:"depth"`
	Parameters int    `json:"parameters"`
}

type edit struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}
