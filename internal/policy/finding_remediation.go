package policy

import "strings"

var packRemediationRules = map[string]string{
	"pack.format": "quality.format", "pack.lint": "quality.lint", "pack.typecheck": "quality.typecheck",
	"pack.complexity": "quality.complexity", "pack.dead-code": "quality.deadCode", "pack.architecture": "architecture.moduleDependency",
	"pack.dependency-policy": "supplyChain.dependencySource", "pack.lock-sync": "supplyChain.lockConsistency",
	"pack.release-age": "supplyChain.releaseAge", "pack.security": "policy.securityProvider",
}

var findingRemediationRules = []struct {
	Rules   string
	Summary string
}{
	{"quality.format quality.gofmt quality.finalNewline quality.trailingWhitespace quality.documentationFinalNewline quality.documentationWhitespace", "Apply the locked formatter to the selected handwritten source, then check the same scope. Repair generated content through its declared producer."},
	{"quality.fileLength", "Separate distinct responsibilities into substantive modules or helpers and remove redundant code; do not hide excess length with forwarding-only files or generated markers."},
	{"quality.complexity quality.goComplexity", "Simplify the reported function's branching and extract cohesive operations while preserving its observable behavior."},
	{"quality.deadCode", "Remove the reported unused declaration or connect it to a real supported caller. For external consumers, provide exact current consumer evidence instead of suppressing the finding."},
	{"quality.typecheck", "Correct the reported expression, declaration, or caller so the actual values satisfy the declared type contract; do not replace the contract with an unchecked type."},
	{"quality.goSyntax quality.shellSyntax quality.dataSyntax", "Repair the syntax at the reported location using the file's declared language or data format, then rerun its selected check."},
	{"quality.lint quality.goVet quality.shellcheck", "Correct the reported analyzer diagnostic at its source while preserving the intended behavior; keep the analyzer enabled."},
	{"quality.goModule", "Place this Go source under its actual contained go.mod and correct module ownership; do not fabricate an unrelated root module."},
	{"quality.path quality.dataPath quality.documentationPath", "Restore the selected path as a contained readable regular file and correct any missing, escaping, or unsafe path component."},
	{"quality.dataSize quality.documentationSize", "Reduce the document to the supported byte limit or divide it into independently meaningful contained documents before validation."},
	{"quality.dataText quality.documentationText", "Encode the document as valid UTF-8 text without NUL bytes, preserving its data or documentation meaning."},
	{"quality.documentationLink", "Update the link to a current contained document or restore its intended target; do not leave a reference to missing or escaping content."},
	{"quality.documentationAnchor", "Point the fragment to an existing heading or explicit anchor in the target document, or restore the intended anchor."},
	{"quality.documentationCorpus", "Repair the reported documentation inventory or read failure so the complete selected documentation corpus can be validated."},
	{"quality.formatConfiguration quality.lintConfiguration quality.deadCodeConfiguration", "Correct the named analyzer configuration using the locked provider's supported options and the actual owning project's paths."},
	{"quality.formatCoverage quality.lintCoverage quality.deadCodeCoverage quality.typecheckCoverage quality.complexityCoverage", "Restore the named locked analyzer and its owning project inputs so every selected source receives the required analysis; do not treat unavailable coverage as a pass."},
	{"quality.go", "Repair the reported Go module or source inventory failure before relying on Go analysis results."},
	{"quality.command", "Repair the declared validation command or its prerequisites using the recorded failure, then rerun the affected selected check."},
	{"quality.testEvidence", "Make the declared test execute meaningful observable assertions and produce the required evidence; do not substitute a successful no-op command."},
	{"architecture.fileCycle testing.fileCycle", "Break a dependency edge in the reported cyclic component by moving the shared responsibility to its proper owner or removing the reverse dependency; preserve meaningful boundary tests."},
	{"architecture.moduleDependency architecture.packageDependency", "Move the dependency or responsibility to its correct owner so imports follow the declared direction. Change the module contract only when the intended architecture changes and receives review."},
	{"architecture.importCoverage architecture.pythonFactsCoverage architecture.sourceGraphCoverage", "Restore exact import resolution and complete bounded analyzer evidence for the selected projects before evaluating dependency direction or cycles."},
	{"architecture.inventory", "Repair the reported contained source inventory failure so the dependency graph includes every applicable governed source."},
	{"architecture.reviewSignal", "Inspect ownership and implementation depth in the architecture-review packet and resolve any substantive boundary findings before finalizing the review."},
	{"portability.machinePath", "Replace the machine-specific path with an explicit configurable input or a contained repository-relative path and test unavailable-input behavior."},
	{"portability.siblingReference", "Replace the implicit sibling-directory dependency with an exact declared external input and its contract tests, or remove the dependency."},
	{"policy.agentGuidance", "Synchronize managed agent guidance from the exact locked release; keep repository-specific procedures in declared operational handoffs."},
	{"policy.architectureReview", "Prepare the selected architecture review against the trusted merge base, resolve findings, and finalize the accepted result for the candidate topology."},
	{"policy.behaviorReview", "Inspect behavior-review status against the trusted review base and complete its selected review and regression-proof requirements before the gate."},
	{"policy.testOwnership", "Declare exactly one production owner and primary quick focused suite for the reported test, using actual boundary ownership and explicit suite paths."},
	{"policy.testCoverage policy.testStrength", "Provide an executable quick boundary suite covering the reported module or test behavior, with meaningful assertions and exact declared ownership."},
	{"policy.moduleCoverage policy.conditionalModule", "Correct module paths so each governed production file has one substantive owner and every declared module matches its intended source."},
	{"policy.languageCoverage", "Declare one supported language owner for each selected executable file and remove overlapping or stale language mappings."},
	{"policy.checkCoverage policy.builtInCapability", "Declare an applicable supported provider for each required module capability and correct its source coverage without disabling the locked baseline."},
	{"policy.tool policy.command", "Restore the exact locked tool or declared command prerequisites identified by the diagnostic, then rerun the owning validation."},
	{"policy.packCoverage policy.packManifest policy.packOperation policy.packProvider policy.packUnavailable", "Restore the exact authenticated pack declared by this repository and correct its manifest or capability coverage before retrying the affected operation."},
	{"policy.designDocumentation", "Correct the selected design mapping and restore a readable current rationale document describing the actual ownership and invariants."},
	{"policy.operationalHandoff", "Correct the selected handoff declaration and restore its current contained repository-owned procedure before the associated operation."},
	{"policy.generatedJavaScriptOwnership", "Map each generated JavaScript output to its actual contained source package and remove missing, stale, or overlapping owner declarations."},
	{"policy.generatedStyleCoverage", "Separate cosmetic style from semantic and security analysis so generated bytes are protected while non-style coverage remains complete."},
	{"policy.generatedWriteProtection policy.generationOwnership", "Repair the exact producer declaration, contained inputs, output ownership, and generation and verification commands. Regenerate through that producer instead of editing generated bytes."},
	{"policy.pythonDynamicReference policy.pythonExternalAttribute policy.pythonReachability", "Bind the exact Python target or write to a current independently resolvable consumer, receiver, and source location; remove stale or unproven declarations instead of preserving unused symbols."},
	{"policy.pythonExternalPluginImport", "Bind the external plug-in to its exact direct dependency, namespace, loader grammar, and rejecting runtime protocol check without granting local dead-code reachability."},
	{"policy.pythonProject policy.pythonBuildBackend policy.pythonRuffConfiguration", "Correct the owning Python project's manifest, supported pinned build backend, and locked analyzer configuration without borrowing another project's environment."},
	{"policy.portabilityCoverage", "Declare the reported external input's exact owner, resolution, unavailable behavior, and executable contract and behavior suites."},
	{"policy.sourceComment", "Remove prose comments or docstrings forbidden by the repository policy; preserve non-local rationale in current mapped design documents."},
	{"policy.sourceCommentCoverage", "Restore supported source parsing so comment policy can be checked for every selected handwritten file."},
	{"policy.exceptionExpired policy.dependencyOverridePolicyExpired policy.releaseAgeAssessmentExpired policy.vulnerabilityAssessmentExpired", "Remove the expired exception or assessment and resolve the underlying policy failure. Any new exception must have exact scope, current evidence, an owner, and explicit expiry."},
	{"policy.dependencyOverridePolicyUnused policy.releaseAgeAssessmentUnused policy.vulnerabilityAssessmentUnused", "Remove the stale assessment or override declaration that no longer matches a current dependency finding."},
	{"policy.releaseAgeAssessmentWindow", "Correct the assessment dates to the allowed evidence window; do not extend an exception merely to bypass release-age policy."},
	{"policy.vulnerabilityAssessmentKnownExploited policy.vulnerabilityAssessmentSeverity", "Remove the inadmissible vulnerability assessment and replace or remediate the affected dependency under the locked severity and exploitation policy."},
	{"policy.securityMonitoring policy.securityProvider policy.securityScanner policy.supplyChain policy.supplyChainCoverage", "Restore the required locked security provider, current authenticated evidence, and complete dependency coverage before relying on security acceptance."},
	{"policy.artifactSecurityCoverage artifactSecurity.operation", "Restore the declared artifact, exact producer prerequisites, and complete supported security analysis for the intended artifact identity."},
	{"policy.finalGateOwner", "Run the final gate through its configured local or checked-in CI owner and retain evidence for the exact candidate."},
	{"policy.formatEvidence", "Restore trustworthy formatter execution evidence for the selected files and verify which handwritten bytes changed and which generated bytes remained protected."},
	{"policy.reportArtifacts", "Restore contained writable managed report storage and resolve the publication error before relying on a successful command result."},
	{"policy.inventory policy.scope", "Correct the requested contained selection or repository inventory so every intended governed input is present, readable, and unambiguous."},
	{"supplyChain.exactVersion supplyChain.pythonExactVersion", "Pin the direct declaration to its unique current importer-scoped locked version when available. If resolution is missing or ambiguous, repair the lock before choosing a replacement and run dependency review."},
	{"supplyChain.lockConsistency supplyChain.lockCoverage supplyChain.lockfile supplyChain.goLock", "Regenerate the candidate lock from the exact intended manifests without lifecycle scripts, resolve missing or ambiguous entries, and complete dependency review before installation."},
	{"supplyChain.nodeManifest supplyChain.pythonManifest supplyChain.goModule supplyChain.goWorkspace", "Correct the owning manifest or workspace using its supported syntax and contained project boundaries; preserve exact dependency declarations and regenerate matching lock evidence."},
	{"supplyChain.dependencySource supplyChain.pythonSource supplyChain.localDependency supplyChain.goReplace", "Use an admissible exact source identity or contained declared local dependency, preserving repository, commit, and subdirectory identity through the lock and dependency review."},
	{"supplyChain.packageManager supplyChain.goVersion", "Pin the package manager or Go toolchain to one exact supported version and update its authenticated lock and release evidence."},
	{"supplyChain.containerPin supplyChain.gitLabImagePin supplyChain.workflowPin supplyChain.gitLabIncludePin", "Replace the mutable image, action, or include reference with its verified immutable digest or full commit identity and review that exact dependency."},
	{"supplyChain.workflow supplyChain.gitLabCoverage", "Correct the workflow's supported structure and contained include graph so every security-relevant image, action, and command is resolved and checked."},
	{"supplyChain.dependencyLicense", "Replace the dependency with an admissibly licensed exact version or remove it; retain complete license evidence for the resolved graph."},
	{"supplyChain.licenseCoverage", "Provide current verifiable license evidence for every resolved dependency; unknown or incomplete licensing cannot count as approval."},
	{"supplyChain.nodeVulnerability supplyChain.goVulnerability supplyChain.osvVulnerability supplyChain.gitVulnerability", "Remove or update the exact affected dependency to a verified fixed candidate and run dependency review; retain advisory identity and complete transitive vulnerability coverage."},
	{"supplyChain.gitEvidence", "Obtain current signed security, license, and trusted age evidence from the declared authorized provider for the exact Git contents and complete lock inventory; keep private identities out of public providers."},
	{"supplyChain.releaseAge supplyChain.newDependencyAge", "Prefer an admissible exact dependency with sufficient authoritative release-age evidence; any necessary exception must satisfy the exact evidence, ownership, and expiry policy."},
	{"supplyChain.releaseAgeCoverage", "Restore authoritative release or trusted commit-observation timestamps for the exact dependency; self-reported Git dates do not establish age."},
	{"supplyChain.auditIgnore supplyChain.pnpmSecurity supplyChain.lifecycleScripts", "Remove undeclared audit bypasses or lifecycle execution and restore frozen, script-free dependency admission under the locked security policy."},
	{"supplyChain.dependencyOverride", "Remove the undeclared override or supply its exact current digest-bound policy assessment, then review the resulting complete dependency graph."},
	{"supplyChain.peerDependency", "Declare and lock a compatible exact peer dependency in the correct importer scope rather than borrowing an unrelated workspace resolution."},
	{"artifactSecurity.vulnerability", "Update or remove the affected component in the authoritative artifact inputs, rebuild the declared target, and rescan the exact resulting artifact for the reported advisory."},
	{"artifactSecurity.secret", "Remove the exposed secret from the authoritative artifact inputs and arrange credential revocation through the authorized owner; rebuild and rescan without copying the secret into reports."},
	{"artifactSecurity.misconfiguration", "Correct the reported security setting in the authoritative image or artifact configuration, then rebuild and rescan the declared target."},
	{"artifactSecurity.end-of-life", "Replace the unsupported operating-system base with an exact supported image identity, rebuild the target, and rerun its security scan."},
	{"repository.changeBoundary", "Move task edits back inside the caller-authorized boundary or obtain an explicit scope correction; preserve unrelated work and do not broaden the task silently."},
}

func findingRemediationSummary(finding Finding) string {
	if rule, exists := packRemediationRules[finding.Check]; exists {
		finding.Check = rule
	}
	if finding.Check == "pack.build" {
		return "Repair the reported build failure in the authoritative source or declared build prerequisites, then rerun verification through the locked build provider."
	}
	if strings.HasPrefix(finding.Check, "test.") {
		return "Inspect the recorded failure and required artifacts for suite " + finding.Subject + ", correct the tested behavior or execution prerequisites, and rerun that exact suite."
	}
	for _, entry := range findingRemediationRules {
		for _, rule := range strings.Fields(entry.Rules) {
			if finding.Check == rule {
				return entry.Summary
			}
		}
	}
	return "Resolve " + finding.Check + " for " + finding.Subject + " and rerun its owning policy command."
}
