package supplychain

import (
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

const gitEvidencePayloadType = "application/vnd.code-polishy.git-evidence.v1+json"
const maximumGitEvidenceBytes = 4 << 20
const maximumGitEvidenceAge = 24 * time.Hour

type gitEvidenceEnvelope struct {
	PayloadType string                 `json:"payloadType"`
	Payload     string                 `json:"payload"`
	Signatures  []gitEvidenceSignature `json:"signatures"`
}

type gitEvidenceSignature struct {
	KeyID string `json:"keyid"`
	Sig   string `json:"sig"`
}

type gitEvidenceStatement struct {
	Protocol        string               `json:"protocol"`
	Issuer          string               `json:"issuer"`
	PolicySHA256    string               `json:"policySha256"`
	Scope           string               `json:"scope"`
	LockSHA256      string               `json:"lockSha256"`
	InventorySHA256 string               `json:"inventorySha256"`
	IssuedAt        time.Time            `json:"issuedAt"`
	ExpiresAt       time.Time            `json:"expiresAt"`
	Subjects        []gitEvidenceSubject `json:"subjects"`
	Scan            gitEvidenceScan      `json:"scan"`
}

type gitEvidenceSubject struct {
	Ecosystem    string                 `json:"ecosystem"`
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Repository   string                 `json:"repository"`
	Commit       string                 `json:"commit"`
	Subdirectory string                 `json:"subdirectory"`
	TreeSHA256   string                 `json:"treeSha256"`
	Observation  gitEvidenceObservation `json:"observation"`
}

type gitEvidenceObservation struct {
	Kind      string    `json:"kind"`
	Record    string    `json:"record"`
	Timestamp time.Time `json:"timestamp"`
}

type gitEvidenceScan struct {
	Coverage        string                     `json:"coverage"`
	Target          string                     `json:"target"`
	Scanner         policy.GitEvidenceScanner  `json:"scanner"`
	CompletedAt     time.Time                  `json:"completedAt"`
	AdvisoryVersion string                     `json:"advisoryVersion"`
	AdvisoryUpdated time.Time                  `json:"advisoryUpdated"`
	Vulnerabilities []gitEvidenceVulnerability `json:"vulnerabilities"`
	Licenses        []gitEvidenceLicense       `json:"licenses"`
}

type gitEvidenceVulnerability struct {
	Ecosystem      string `json:"ecosystem"`
	Name           string `json:"name"`
	Version        string `json:"version"`
	Source         string `json:"source"`
	ID             string `json:"id"`
	Severity       string `json:"severity"`
	KnownExploited *bool  `json:"knownExploited"`
}

type gitEvidenceLicense struct {
	Ecosystem  string `json:"ecosystem"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Source     string `json:"source"`
	Expression string `json:"expression"`
}

type GitEvidenceReceipt struct {
	Scope           string    `json:"scope"`
	Provider        string    `json:"provider"`
	Path            string    `json:"path"`
	SHA256          string    `json:"sha256"`
	PolicySHA256    string    `json:"policySha256"`
	LockSHA256      string    `json:"lockSha256"`
	InventorySHA256 string    `json:"inventorySha256"`
	RetrievedAt     time.Time `json:"retrievedAt"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

type gitEvidenceError struct {
	category string
	message  string
}

func (err *gitEvidenceError) Error() string {
	return err.message
}

func gitEvidenceFailure(category, message string) error {
	return &gitEvidenceError{category: category, message: message}
}
