package testreceipt

import (
	"errors"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/testartifact"
)

const (
	Version             = 1
	IdentityVersion     = 1
	Retention           = 30 * 24 * time.Hour
	maximumReceiptBytes = 2 << 20
	rootDirectory       = ".code-polishy-reports"
	storeDirectory      = "test-receipts"
)

var (
	ErrMissing = errors.New("test receipt is missing")
	ErrInvalid = errors.New("test receipt is invalid")
	ErrExpired = errors.New("test receipt is expired")
)

type Release struct {
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type Environment struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
	SHA256  string `json:"sha256,omitempty"`
}

type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Input struct {
	Path   string `json:"path"`
	Mode   uint32 `json:"mode"`
	SHA256 string `json:"sha256"`
}

type Identity struct {
	Version             int              `json:"version"`
	Release             Release          `json:"release"`
	PolicySchema        int              `json:"policy_schema"`
	ConfigurationSHA256 string           `json:"configuration_sha256"`
	Platform            Platform         `json:"platform"`
	Suite               policy.TestSuite `json:"suite"`
	Environment         []Environment    `json:"environment"`
	Tools               []Tool           `json:"tools"`
	Inputs              []Input          `json:"inputs"`
}

type Source struct {
	Kind         string `json:"kind"`
	BundleSHA256 string `json:"bundle_sha256,omitempty"`
}

type Receipt struct {
	Version        int                   `json:"version"`
	Identity       Identity              `json:"identity"`
	IdentitySHA256 string                `json:"identity_sha256"`
	Suite          string                `json:"suite"`
	DurationMillis int64                 `json:"duration_milliseconds"`
	Artifacts      []testartifact.Record `json:"artifacts"`
	CreatedAt      time.Time             `json:"created_at"`
	Source         Source                `json:"source"`
	SHA256         string                `json:"sha256"`
}

type Reusable struct {
	Receipt Receipt
	Path    string
}
