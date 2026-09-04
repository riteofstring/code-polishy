package testartifact

import "time"

const (
	RootName             = ".code-polishy-artifacts"
	EnvironmentID        = "CODE_POLISHY_EXECUTION_ID"
	EnvironmentDirectory = "CODE_POLISHY_ARTIFACT_DIR"
	DirectoryToken       = "{artifactDir}"
	ExecutionIDToken     = "{executionId}"
	MaximumArtifactBytes = 32 << 20
	manifestFilename     = ".code-polishy-owner.json"
	leaseFilename        = ".active"
	manifestVersion      = 1
	retainedExecutions   = 3
)

type Record struct {
	Suite    string `json:"suite"`
	Path     string `json:"path"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

type manifest struct {
	Version     int       `json:"version"`
	ExecutionID string    `json:"execution_id"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	SHA256      string    `json:"sha256"`
}

type Execution struct {
	repositoryRoot string
	root           string
	directory      string
	id             string
	createdAt      time.Time
	completed      bool
}
