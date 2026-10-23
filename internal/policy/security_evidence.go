package policy

import (
	"slices"
	"time"
)

const SecurityEvidenceLifetime = 24 * time.Hour

type SecurityVerification struct {
	Status       string         `json:"status"`
	CheckedAt    time.Time      `json:"checkedAt,omitzero"`
	RefreshAfter time.Time      `json:"refreshAfter,omitzero"`
	ReportPath   string         `json:"reportPath,omitempty"`
	Scans        []SecurityScan `json:"scans,omitempty"`
}

type SecurityScan struct {
	Name            string    `json:"name"`
	Status          string    `json:"status"`
	ScannerVersion  string    `json:"scannerVersion,omitempty"`
	LastSucceededAt time.Time `json:"lastSucceededAt,omitzero"`
	RefreshAfter    time.Time `json:"refreshAfter,omitzero"`
	ReportPath      string    `json:"reportPath"`
}

func (verification *SecurityVerification) Clone() *SecurityVerification {
	if verification == nil {
		return nil
	}
	cloned := *verification
	cloned.Scans = slices.Clone(verification.Scans)
	return &cloned
}
