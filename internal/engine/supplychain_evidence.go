package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/riteofstring/code-polishy/internal/artifactsecurity"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/quality"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/supplychain"
)

func (engine *Engine) onlineSupplyChain(ctx context.Context, files []string, findings []policy.Finding) ([]policy.Finding, []supplychain.GitEvidenceReceipt, []string) {
	before, err := supplychain.ReadGitEvidenceState(engine.Repository, time.Now().UTC())
	if err != nil {
		return append(findings, gitEvidenceStateFinding(err)), nil, nil
	}
	findings = append(findings, supplychain.Online(ctx, engine.Repository, files, engine.Runner)...)
	selection := repository.Selection{Files: files, All: true}
	findings = append(findings, quality.RunCommandsForProfiles(ctx, engine.Repository, selection, engine.Runner, "supply-chain", "supply-chain-online", "security")...)
	artifacts := artifactsecurity.Run(ctx, engine.Repository, engine.Runner)
	findings = append(findings, artifacts.Findings...)
	findings = supplychain.ClassifyKnownExploited(ctx, engine.Repository, findings)
	after, err := engine.currentGitEvidence(before.IdentitySHA256)
	if err != nil {
		return append(findings, gitEvidenceStateFinding(err)), nil, artifacts.Notes
	}
	return findings, after, artifacts.Notes
}

func (engine *Engine) currentGitEvidence(expected string) ([]supplychain.GitEvidenceReceipt, error) {
	state, err := supplychain.ReadGitEvidenceState(engine.Repository, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if state.IdentitySHA256 != expected {
		return nil, fmt.Errorf("git evidence changed or expired during verification")
	}
	return state.Receipts, nil
}

func (engine *Engine) gateEvidenceIdentities() (string, string, error) {
	evidence, err := supplychain.ReadGitEvidenceState(engine.Repository, time.Now().UTC())
	if err != nil {
		return "", "", err
	}
	pythonDigest, err := engine.Repository.PythonReachabilityStateSHA256()
	if err != nil {
		return "", "", err
	}
	return evidence.IdentitySHA256, pythonDigest, nil
}

func gitEvidenceStateFinding(err error) policy.Finding {
	return policy.Finding{Check: "supplyChain.gitEvidence", Path: "repository", Subject: "verification-state", Message: err.Error()}
}
