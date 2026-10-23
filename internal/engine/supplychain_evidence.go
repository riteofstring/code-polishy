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

func (engine *Engine) onlineSupplyChain(ctx context.Context, files []string, findings []policy.Finding, mode supplyChainMode) ([]policy.Finding, []supplychain.GitEvidenceReceipt, []string, *policy.SecurityVerification) {
	before, err := supplychain.ReadGitEvidenceState(engine.Repository, time.Now().UTC())
	if err != nil {
		return append(findings, gitEvidenceStateFinding(err)), nil, nil, nil
	}
	commands, err := supplychain.OnlineCommands(engine.Repository, files)
	if err != nil {
		return append(findings, securityEvidenceFinding(err)), nil, nil, nil
	}
	scans := engine.securityScans
	commandRunner := engine.Runner
	if scans == nil {
		scans = newSecurityScanRunner(engine, commands)
		commandRunner = scans
	}
	findings = append(findings, supplychain.Online(ctx, engine.Repository, files, commandRunner)...)
	selection := repository.Selection{Files: files, All: true}
	var notes []string
	if mode == dependencyReviewMode {
		notes = engine.deferredSupplyChainChecks(selection)
	} else {
		findings = append(findings, quality.RunCommandsForProfiles(ctx, engine.Repository, selection, engine.Runner, "supply-chain", "supply-chain-online", "security")...)
		artifacts := artifactsecurity.Run(ctx, engine.Repository, engine.Runner)
		findings = append(findings, artifacts.Findings...)
		notes = artifacts.Notes
	}
	findings = supplychain.ClassifyKnownExploited(ctx, engine.Repository, findings)
	after, err := engine.currentGitEvidence(before.IdentitySHA256)
	if err != nil {
		return append(findings, gitEvidenceStateFinding(err)), nil, notes, scans.verification()
	}
	verification := scans.verification()
	if verification.Status == "unavailable" {
		findings = append(findings, securityEvidenceFinding(fmt.Errorf("a package scan failed or its evidence expired during verification")))
	}
	return findings, after, notes, verification
}

func (engine *Engine) deferredSupplyChainChecks(selection repository.Selection) []string {
	notes := []string{}
	for _, command := range quality.CommandsForProfiles(engine.Repository, selection, "supply-chain", "supply-chain-online", "security") {
		notes = append(notes, fmt.Sprintf("deferred repository provider %q: pre-install review does not execute repository commands", command.Name))
	}
	for _, target := range engine.Repository.Config.SupplyChain.ArtifactSecurity.Targets {
		notes = append(notes, fmt.Sprintf("deferred artifact scan %q and scanner self-check: artifact preparation may install or build dependencies", target.Name))
	}
	command := "code-polishy supply-chain --offline"
	if len(notes) > 0 {
		command = "code-polishy supply-chain without --offline"
	}
	notes = append(notes, "installed dependency license evidence is deferred until the candidate tree is installed")
	notes = append(notes, fmt.Sprintf("after frozen script-disabled installation, run %s to complete the deferred checks; pre-install review does not establish their results", command))
	return notes
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
