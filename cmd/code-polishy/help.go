package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
)

type commandHelpPage struct {
	name        string
	summary     string
	syntax      []string
	selectors   []string
	sideEffects []string
	exits       []string
	examples    []string
}

type commandArgumentError struct{ cause error }

func (err commandArgumentError) Error() string { return err.cause.Error() }
func (err commandArgumentError) Unwrap() error { return err.cause }

func commandInputError(err error) error {
	return commandArgumentError{cause: err}
}

func isCommandInputError(err error) bool {
	var inputError commandArgumentError
	return errors.As(err, &inputError)
}

var commandHelpPages = []commandHelpPage{
	{
		name:        "capabilities",
		summary:     "Discover capabilities from the exact release, installed packs, and repository declarations.",
		syntax:      []string{"code-polishy capabilities [--query TEXT] [--format human|json]"},
		selectors:   []string{"Without a query, lists the bounded capability inventory. --query returns at most 20 deterministic candidates from at most 16 terms and 1024 UTF-8 bytes.", "Discovery never selects a behavior-review feature. Supply an exact configured name or alias only after the user's request identifies it.", "Missing or invalid authenticated release catalogs, unavailable packs, and inapplicable scopes remain explicit."},
		sideEffects: []string{"Reads the locked release catalog, repository declarations, governed inventory, and selected pack receipts. Runs no checks, tests, package operations, or repository commands, and writes no reports or review artifacts."},
		exits:       []string{"0 discovery completed, including explicit unavailable evidence", "2 invalid usage, configuration, bounded input, or operational failure"},
		examples:    []string{"code-polishy capabilities", "code-polishy capabilities --query 'purchase behavior' --format json"},
	},
	{
		name:    "task-start",
		summary: "Validate task context and atomically capture the exact request in one bounded JSON packet.",
		syntax:  []string{"code-polishy task-start --intent-file PATH (--files PATH | --module NAME) [--feature NAME...] [--situation NAME...] [--format json]"},
		selectors: []string{
			"Choose exactly one contained file, directory, or declared module; change-aware and repository-wide selectors are not accepted.",
			"Repeat --feature only for exact canonical names or aliases explicitly requested by the caller; repeat --situation for exact operational contexts.",
		},
		sideEffects: []string{"Validates the intent, selection, features, context, catalog, and 16 MiB packet bound before atomically appending the intent journal. Emits one task-start/v1 JSON document. Runs no tests, reviews, package operations, or repository commands."},
		exits:       []string{"0 intent captured and packet produced", "2 invalid usage, unavailable context, or operational failure"},
		examples:    []string{"code-polishy task-start --intent-file /tmp/request.txt --module application --feature checkout", "code-polishy task-start --intent-file /tmp/request.txt --files frontend --situation deployment"},
	},
	{
		name:    "version",
		summary: "Print the running Code Polishy release version.",
		syntax:  []string{"code-polishy version"},
		selectors: []string{
			"No command options or positional arguments.",
		},
		sideEffects: []string{"Reads VERSION from the policy root."},
		exits:       []string{"0 version printed", "2 invalid usage or version read failure"},
		examples:    []string{"code-polishy version"},
	},
	{
		name:    "docs",
		summary: "List, search, or read the documentation shipped with this exact Code Polishy release.",
		syntax: []string{
			"code-polishy docs list",
			"code-polishy docs find QUERY...",
			"code-polishy docs read TOPIC",
		},
		selectors: []string{
			"list accepts no arguments and prints public topic identifiers in stable order.",
			"find requires one or more query terms and returns a bounded deterministic result list.",
			"read requires one exact topic identifier or documented alias.",
		},
		sideEffects: []string{"After the launcher selects the repository's locked release, reads only its documentation; no project files or network access."},
		exits:       []string{"0 requested documentation printed or no search results", "2 invalid usage or unavailable documentation"},
		examples:    []string{"code-polishy docs list", "code-polishy docs find behavior review", "code-polishy docs read agent-workflows"},
	},
	{
		name:    "pack",
		summary: "Install, verify, or inspect an exact local language pack.",
		syntax: []string{
			"code-polishy pack install --source PATH",
			"code-polishy pack verify --source PATH",
			"code-polishy pack root",
		},
		selectors: []string{
			"install validates and atomically installs one local source directory.",
			"verify runs the source directory's declared conformance fixtures without installing it.",
			"root prints the effective local pack storage root.",
		},
		sideEffects: []string{"install writes an immutable content-addressed user-data directory; verify executes declared local adapters; root only reads platform configuration."},
		exits:       []string{"0 requested action completed", "2 invalid usage, unsafe pack, unavailable integration, or operational failure"},
		examples:    []string{"code-polishy pack verify --source ./code-polishy-rust", "code-polishy pack install --source ./code-polishy-rust", "code-polishy pack root"},
	},
	{
		name:    "agents",
		summary: "Install, synchronize, or verify generated AI-agent guidance and report-artifact hygiene.",
		syntax:  []string{"code-polishy agents <install|sync|check>"},
		selectors: []string{
			"install creates guidance files and the report-artifact ignore rule.",
			"sync refreshes generated guidance and repairs the ignore rule.",
			"check verifies that all managed adoption surfaces are current.",
		},
		sideEffects: []string{"install and sync transactionally write guidance files and the root .gitignore report-artifact rule; check only reads them."},
		exits:       []string{"0 adoption surfaces installed, synchronized, or current", "1 check found a stale adoption surface", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy agents sync", "code-polishy agents check"},
	},
	{
		name:    "lock",
		summary: "Record the installed Code Polishy release required by this repository.",
		syntax:  []string{"code-polishy lock"},
		selectors: []string{
			"No command options or positional arguments.",
		},
		sideEffects: []string{"Writes .code-polishy.lock.json at the repository root."},
		exits:       []string{"0 lock written", "2 invalid usage or release validation failure"},
		examples:    []string{"code-polishy lock"},
	},
	{
		name:    "install-bundle",
		summary: "Install a verified native release archive.",
		syntax:  []string{"code-polishy install-bundle --source PATH --sha256 DIGEST --prefix PATH"},
		selectors: []string{
			"--source PATH is an already acquired local release ZIP.",
			"--sha256 DIGEST is the expected release zip SHA-256.",
			"--prefix PATH is the installation prefix.",
		},
		sideEffects: []string{"Writes an installed release tree below --prefix."},
		exits:       []string{"0 release installed", "2 invalid usage or installation failure"},
		examples:    []string{"code-polishy install-bundle --source ./release.zip --sha256 DIGEST --prefix /opt/code-polishy"},
	},
	{
		name:    "release-manifest",
		summary: "Verify, package, and publish one native release tree.",
		syntax: []string{
			"code-polishy release-manifest write --root PATH --source-revision COMMIT",
			"code-polishy release-manifest verify --root PATH",
			"code-polishy release-manifest materialize --source PATH --destination PATH",
			"code-polishy release-manifest archive --root PATH --output PATH",
			"code-polishy release-manifest publish --archive PATH --destination PATH",
			"code-polishy release-manifest index --artifact-descriptor PATH... --output PATH",
			"code-polishy release-manifest oci-context --descriptor PATH --template PATH --destination PATH",
		},
		selectors: []string{
			"write records a manifest for a staged or installed release root.",
			"verify validates the existing manifest at --root.",
			"materialize copies a closed source tree to a new dereferenced destination.",
			"archive creates one checksum-stable ZIP from a verified release root.",
			"publish creates the archive checksum, manifest, SBOM, deterministic provenance metadata, and descriptor atomically.",
			"index combines every supported host descriptor into one verified release index.",
			"oci-context installs one Linux archive through the bundle verifier into a new image context.",
			"This command accepts no global --config option.",
		},
		sideEffects: []string{"write, materialize, archive, publish, index, and oci-context create their named outputs; verify only reads."},
		exits:       []string{"0 requested release operation completed", "2 invalid usage or release validation failure"},
		examples:    []string{"code-polishy release-manifest verify --root ./dist/code-polishy"},
	},
	{
		name:    "change-boundary",
		summary: "Verify that current changes stay inside a declared task boundary.",
		syntax:  []string{"code-polishy change-boundary --base COMMIT --module NAME... [--allow-path PATH...] [--allow-new-path PATH...]"},
		selectors: []string{
			"--base COMMIT and at least one --module NAME are required.",
			"--allow-path PATH permits an exact tracked non-control path.",
			"--allow-new-path PATH permits an exact new regular non-control path.",
		},
		sideEffects: []string{"Reads the repository and configuration; does not write project files."},
		exits:       []string{"0 boundary satisfied", "1 changed paths fall outside the boundary", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy change-boundary --base HEAD~1 --module cli --allow-path docs/guide.md"},
	},
	{
		name:    "task-session",
		summary: "Run one task command in a disposable Git worktree with a frozen authority boundary.",
		syntax:  []string{"code-polishy task-session --module NAME... [--repo-root PATH] [--config PATH] [--allow-path PATH...] [--allow-new-path PATH...] [--output-dir PATH] [--promote] -- COMMAND [ARG...]"},
		selectors: []string{
			"At least one --module NAME and a command after -- are required.",
			"--allow-path and --allow-new-path extend the declared boundary with exact paths.",
			"--promote fast-forwards the original branch after successful validation.",
			"Place task-session options after task-session; its --repo-root and --config are not global options.",
		},
		sideEffects: []string{"Creates and removes a disposable worktree, writes external session artifacts, and may promote the original branch."},
		exits:       []string{"0 session completed", "worker or policy failure uses its reported nonzero status", "2 invalid usage"},
		examples:    []string{"code-polishy task-session --module cli -- ./scripts/test.sh ./cmd/code-polishy/..."},
	},
	{
		name:    "task-session-artifacts",
		summary: "Freeze or validate the private artifacts used by a task-session workflow.",
		syntax: []string{
			"code-polishy task-session-artifacts freeze --output-dir PATH [--config PATH] --module NAME... [--allow-path PATH...] [--allow-new-path PATH...] -- COMMAND [ARG...]",
			"code-polishy task-session-artifacts validate --output-dir PATH --scope-digest SHA256 --command-digest SHA256",
		},
		selectors: []string{
			"freeze requires scope options and a command after --.",
			"validate accepts only the exact expected digests.",
		},
		sideEffects: []string{"freeze writes private scope and command artifacts; validate only reads them."},
		exits:       []string{"0 artifacts frozen or validated", "2 invalid usage or artifact failure"},
		examples:    []string{"code-polishy task-session-artifacts validate --output-dir /tmp/session --scope-digest SHA256 --command-digest SHA256"},
	},
	{
		name:    "task-session-receipt",
		summary: "Write the private receipt for a completed task-session workflow.",
		syntax:  []string{"code-polishy task-session-receipt --output-dir PATH --status VALUE --source-root PATH --source-branch NAME --trusted-base COMMIT --candidate-head COMMIT --config PATH --promote VALUE --policy-binary PATH --policy-digest SHA256 --environment-receipt PATH --environment-digest SHA256 --environment-paths PATH --environment-paths-digest SHA256 --scope-digest SHA256 --workspace PATH --command-digest SHA256 --module NAME... [--exact-path PATH...] [--new-path PATH...]"},
		selectors: []string{
			"Every listed single-value option is required exactly once.",
			"At least one --module NAME is required; --exact-path and --new-path may repeat.",
		},
		sideEffects: []string{"Writes the private session receipt in --output-dir."},
		exits:       []string{"0 receipt written", "2 invalid usage or receipt write failure"},
		examples:    []string{"code-polishy task-session-receipt --help"},
	},
	{
		name:    "governed-environment",
		summary: "Freeze the governed command environment into a private receipt.",
		syntax:  []string{"code-polishy governed-environment --output PATH"},
		selectors: []string{
			"--output PATH is required and no positional arguments are accepted.",
		},
		sideEffects: []string{"Writes a private environment receipt at --output."},
		exits:       []string{"0 environment receipt written", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy governed-environment --output /tmp/environment.json"},
	},
	{
		name:    "check",
		summary: "Run configured policy checks over one evaluation selection or named checks.",
		syntax: []string{
			"code-polishy check [--git-changes|--staged|--all|--files PATH...|--module NAME...]",
			"code-polishy check --name NAME...",
		},
		selectors: []string{
			"Evaluation selectors are mutually exclusive; --git-changes is the default.",
			"--files accepts contained regular files and directories; directories expand to governed descendants without following symbolic links.",
			"--module selects one or more declared modules and cannot be combined with file, change-aware, staged, or all-repository selectors.",
			"--name NAME may repeat and cannot be combined with evaluation selectors.",
			"Select --all or --name for configured repository-wide checks with no path or module triggers; focused checks retain global configuration validation and required context for applicable analyzers.",
		},
		sideEffects: []string{"Reads the configuration and selected files; runs configured checks."},
		exits:       []string{"0 no findings", "1 policy findings", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy check --git-changes", "code-polishy check --name content-check"},
	},
	{
		name:    "gate",
		summary: "Run the configured standard policy gate.",
		syntax:  []string{"code-polishy gate"},
		selectors: []string{
			"No command options or positional arguments.",
		},
		sideEffects: []string{"Runs the configured gate checks and reads their declared project inputs."},
		exits:       []string{"0 no findings", "1 policy findings", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy gate"},
	},
	{
		name:    "checkpoint-gate",
		summary: "Run the checkpoint gate against a declared task boundary.",
		syntax:  []string{"code-polishy checkpoint-gate --base REF"},
		selectors: []string{
			"Exactly one --base REF is required.",
		},
		sideEffects: []string{"Runs checkpoint policy, writes managed logs and a JSON run report below .code-polishy-reports/checkpoint-gate/, and records checkpoint evidence on success."},
		exits:       []string{"0 gate passed", "1 policy findings or failed gate command", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy checkpoint-gate --base PREVIOUS_CHECKPOINT"},
	},
	{
		name:    "merge-gate",
		summary: "Run the final merge gate against a declared merge target.",
		syntax:  []string{"code-polishy merge-gate --base REF [--resume]"},
		selectors: []string{
			"Exactly one --base REF is required.",
			"An exact already-passed identity executes no commands; new identities may reuse only complete matching suite receipts.",
			"--resume explicitly reuses only eligible successful ordinary test suites from a content-matching failed merge gate; it does not reduce gate scope.",
		},
		sideEffects: []string{"Reads exact prior evidence; when work is required, runs merge policy and writes managed logs and a JSON run report below .code-polishy-reports/merge-gate/."},
		exits:       []string{"0 gate passed", "1 policy findings or failed gate command", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy merge-gate --base origin/main", "code-polishy merge-gate --base origin/main --resume"},
	},
	{
		name:        "architecture-review",
		summary:     "Optionally review concept ownership and dependency topology when explicitly requested.",
		syntax:      []string{"code-polishy architecture-review <status|prepare|finalize> --base REF"},
		selectors:   []string{"Exactly one --base REF and a clean committed candidate are required.", "status reports current signals and validates accepted evidence; prepare writes a bounded packet for a separate clean-context reviewer; finalize validates its strict result.", "Ordinary gates do not require architecture review. An accepted review does not waive deterministic architecture or ownership failures."},
		sideEffects: []string{"Reads the full source graph with the locked analyzers. prepare writes managed packet and binding files; finalize writes an acceptance receipt. The calling harness supplies the reviewer; no AI provider or tests are invoked."},
		exits:       []string{"0 prepared, accepted, or no review required", "1 deterministic findings or required review", "2 invalid usage, invalid review result, changed candidate, or operational failure"},
		examples:    []string{"code-polishy architecture-review status --base origin/main", "code-polishy architecture-review prepare --base origin/main", "code-polishy architecture-review finalize --base origin/main"},
	},
	{
		name:    "behavior-review",
		summary: "Capture intent and corrections, inspect review status, prepare evidence, or finalize a behavior and final-state review.",
		syntax: []string{
			"code-polishy behavior-review capture-intent --intent-file PATH [--feature NAME...] [--format human|json]",
			"code-polishy behavior-review require --base REF --feature NAME...",
			"code-polishy behavior-review status --base REF [--format human|json]",
			"code-polishy behavior-review prepare --base REF",
			"code-polishy behavior-review finalize --base REF",
		},
		selectors: []string{
			"capture-intent requires exactly one --intent-file PATH and a clean committed base for the original request; later corrections may bind a dirty candidate.",
			"Repeat --feature only for explicitly requested configured names or exact normalized aliases. Stored requirements always use canonical names; intent keywords never activate features.",
			"capture-intent and status always confirm canonical features and state on stdout, including in pipes; --format json emits one behavior-review/v1 document.",
			"require requires exactly one --base REF, at least one configured --feature NAME, and a clean committed candidate with pre-code intent.",
			"status requires exactly one --base REF and reads the behavior-review decision without running tests or writing artifacts.",
			"prepare requires exactly one --base REF and uses intent captured before implementation.",
			"finalize requires exactly one --base REF.",
		},
		sideEffects: []string{"capture-intent and require append to the managed intent journal; status only reads review state; prepare writes a review packet; finalize writes the corresponding review receipt."},
		exits:       []string{"0 requested review operation completed or readable status reported, including an incomplete review", "2 invalid usage or operational failure"},
		examples: []string{
			"code-polishy behavior-review capture-intent --intent-file request.md --feature checkout --feature search",
			"code-polishy behavior-review require --base TASK_BASE --feature authentication",
			"code-polishy behavior-review status --base TASK_BASE",
			"code-polishy behavior-review prepare --base TASK_BASE",
		},
	},
	{
		name:    "regression-proof",
		summary: "Record an executable regression proof for a behavior review.",
		syntax:  []string{"code-polishy regression-proof --base REF --suite NAME --evidence PATH... --id ID [--red-exit STATUS]"},
		selectors: []string{
			"--base, --suite, and --id are each required exactly once.",
			"At least one unique --evidence PATH is required.",
			"--red-exit STATUS is an optional integer from 1 through 255.",
		},
		sideEffects: []string{"Runs the named proof suite and writes a regression-proof record on success."},
		exits:       []string{"0 proof recorded", "1 proof did not establish the requested behavior", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy regression-proof --base TASK_BASE --suite cli-contract --evidence cmd/code-polishy/help_test.go --id contextual-help"},
	},
	{
		name:    "test",
		summary: "Run one configured test selection.",
		syntax: []string{
			"code-polishy test [--changed [--base REF]|--recommended [--base REF]|--all|--supplemental [--resume]|--module NAME...|--suite NAME...]",
		},
		selectors: []string{
			"Choose at most one of --changed, --recommended, --all, --supplemental, --module, or --suite.",
			"Without a selector, tests affected by working-tree changes are selected.",
			"--base REF requires --changed or --recommended and compares against merge-base(REF, HEAD) plus working-tree changes.",
			"--supplemental is an explicit hardening selection; declared supplemental suites and tests.requiredSupplementalKinds do not invoke it.",
			"--resume with --supplemental reuses only unexpired receipts whose complete suite inputs still match.",
		},
		sideEffects: []string{"Runs configured test commands with normal streaming output and managed test artifacts; successful eligible suites may write reusable receipts, but no gate report."},
		exits:       []string{"0 selected suites passed", "1 selected suite failed", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy test --changed --base TASK_BASE", "code-polishy test --supplemental --resume"},
	},
	{
		name:    "test-plan",
		summary: "Show the impact-based test plan for a working tree or task base.",
		syntax:  []string{"code-polishy test-plan [--base REF]"},
		selectors: []string{
			"--base REF is optional; without it the working tree is compared with HEAD.",
		},
		sideEffects: []string{"Reads the repository and configuration; does not run suites or write project files. Its supplemental row is informational and never selects supplemental execution."},
		exits:       []string{"0 plan produced", "1 policy findings", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy test-plan --base TASK_BASE"},
	},
	{
		name:    "test-receipts",
		summary: "Export or import one authenticated reusable-test receipt bundle.",
		syntax: []string{
			"code-polishy test-receipts export --output PATH",
			"code-polishy test-receipts import --source PATH --sha256 DIGEST",
		},
		selectors: []string{
			"export writes current unexpired local receipts to one new bundle and prints its SHA-256 digest.",
			"import accepts one regular bundle only when its exact caller-supplied SHA-256 digest matches.",
		},
		sideEffects: []string{"export creates the requested bundle; import atomically replaces the repository's one authenticated CI receipt bundle."},
		exits:       []string{"0 bundle exported or imported", "2 invalid usage, missing receipts, unsafe paths, or invalid evidence"},
		examples:    []string{"code-polishy test-receipts export --output /tmp/receipts.json", "code-polishy test-receipts import --source /tmp/receipts.json --sha256 DIGEST"},
	},
	{
		name:    "test-levels",
		summary: "Show the configured test levels for a working tree or task base.",
		syntax:  []string{"code-polishy test-levels [--base REF]"},
		selectors: []string{
			"--base REF is optional; without it the working tree is compared with HEAD.",
		},
		sideEffects: []string{"Reads the repository and configuration; does not run suites or write project files. Its supplemental row is informational and never selects supplemental execution."},
		exits:       []string{"0 levels produced", "1 policy findings", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy test-levels --base TASK_BASE"},
	},
	{
		name:    "verify",
		summary: "Run the configured verification workflow.",
		syntax:  []string{"code-polishy verify [--tests-only]"},
		selectors: []string{
			"--tests-only limits verification to configured test commands.",
		},
		sideEffects: []string{"Runs configured verification commands and reads their declared project inputs."},
		exits:       []string{"0 verification passed", "1 policy findings or verification failure", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy verify --tests-only"},
	},
	{
		name:    "architecture",
		summary: "Check architecture policy over one evaluation selection.",
		syntax:  []string{"code-polishy architecture [--git-changes|--staged|--all|--files PATH...|--module NAME...]"},
		selectors: []string{
			"Evaluation selectors are mutually exclusive; --git-changes is the default.",
			"--files accepts contained regular files and directories; --module accepts declared module names.",
		},
		sideEffects: []string{"Reads the configuration and selected files; does not write project files."},
		exits:       []string{"0 no findings", "1 architecture findings", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy architecture --files internal/engine/engine.go"},
	},
	{
		name:    "supply-chain",
		summary: "Run configured supply-chain checks.",
		syntax:  []string{"code-polishy supply-chain [--offline]"},
		selectors: []string{
			"--offline disables online dependency and vulnerability lookups.",
		},
		sideEffects: []string{"Runs supply-chain checks; the default mode may contact configured package and vulnerability sources."},
		exits:       []string{"0 no findings", "1 supply-chain findings", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy supply-chain --offline"},
	},
	{
		name:    "dependency-review",
		summary: "Review dependency-input changes against a declared base.",
		syntax:  []string{"code-polishy dependency-review --base REF"},
		selectors: []string{
			"Exactly one --base REF is required.",
		},
		sideEffects: []string{"Reads dependency inputs and runs the configured dependency review; does not write project files."},
		exits:       []string{"0 review passed", "1 dependency findings", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy dependency-review --base origin/main"},
	},
	{
		name:    "artifact-security",
		summary: "Check declared build artifacts for security-policy findings.",
		syntax:  []string{"code-polishy artifact-security"},
		selectors: []string{
			"No command options or positional arguments.",
		},
		sideEffects: []string{"Reads declared artifact targets and their inputs; does not write project files."},
		exits:       []string{"0 no findings", "1 artifact-security findings", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy artifact-security"},
	},
	{
		name:    "doctor",
		summary: "Inspect the configured policy environment and command discovery.",
		syntax:  []string{"code-polishy doctor [--strict]"},
		selectors: []string{
			"--strict enables the stricter environment checks.",
		},
		sideEffects: []string{"Reads configuration and environment state; does not write project files."},
		exits:       []string{"0 no findings", "1 environment findings", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy doctor --strict"},
	},
	{
		name:    "design-context",
		summary: "Discover current design documents and relevant repository operational handoffs.",
		syntax: []string{
			"code-polishy design-context --module NAME... [--situation NAME...]",
			"code-polishy design-context [--git-changes|--staged|--all|--files PATH...] [--situation NAME...]",
		},
		selectors: []string{
			"Choose either one or more --module NAME selectors or one file selector.",
			"File selectors are mutually exclusive; --git-changes is the default.",
			"Use --files PATH... for explicit paths; positional paths are not accepted.",
			"Module and exact-source design mappings are additive. Output explains each match and reports uncovered selected modules and paths; missing mappings alone do not fail the command.",
			"Repeat --situation NAME for exact operational situations such as authentication, release, or deployment. Situations alone select no files; a file or module selector may be added explicitly.",
			"Handoffs match a selected situation, exact source, or module; the actual design-context workflow situation also applies. Only selected documents are loaded, with their SHA-256 identities and selection reasons.",
		},
		sideEffects: []string{"Reads selected documents and writes a bounded managed context report. Human output identifies documents; JSON includes their exact contents. It executes no procedure commands, retrieves no credentials, and changes no managed guidance."},
		exits:       []string{"0 context composed", "1 invalid current document or selected handoff", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy design-context --files cmd/code-polishy/main.go", "code-polishy design-context --module cli --situation release", "code-polishy design-context --situation authentication --format json"},
	},
	{
		name:    "format",
		summary: "Check or apply configured formatting over one evaluation selection.",
		syntax:  []string{"code-polishy format [--git-changes|--staged|--all|--files PATH...|--module NAME...]"},
		selectors: []string{
			"Evaluation selectors are mutually exclusive; --git-changes is the default.",
			"--files accepts contained regular files and directories; --module accepts declared module names.",
		},
		sideEffects: []string{"Runs configured formatters and may rewrite selected files."},
		exits:       []string{"0 formatting completed without findings", "1 formatting findings", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy format --git-changes", "code-polishy format --files cmd/code-polishy/main.go"},
	},
	{
		name:    "fix",
		summary: "Apply configured formatting fixes over one evaluation selection.",
		syntax:  []string{"code-polishy fix [--git-changes|--staged|--all|--files PATH...|--module NAME...]"},
		selectors: []string{
			"Evaluation selectors are mutually exclusive; --git-changes is the default.",
			"--files accepts contained regular files and directories; --module accepts declared module names.",
		},
		sideEffects: []string{"Runs configured formatters and may rewrite selected files."},
		exits:       []string{"0 fixes completed without findings", "1 remaining findings", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy fix --git-changes"},
	},
	{
		name:    "list-files",
		summary: "List the files in one resolved evaluation selection.",
		syntax:  []string{"code-polishy list-files [--git-changes|--staged|--all|--files PATH...|--module NAME...]"},
		selectors: []string{
			"Evaluation selectors are mutually exclusive; --git-changes is the default.",
			"--files accepts contained regular files and directories; --module accepts declared module names.",
		},
		sideEffects: []string{"Reads repository state and prints selected paths; does not write project files."},
		exits:       []string{"0 paths listed", "2 invalid usage or operational failure"},
		examples:    []string{"code-polishy list-files --all"},
	},
}

func commandHelpFor(command string) (commandHelpPage, bool) {
	if command == "--version" {
		command = "version"
	}
	for _, page := range commandHelpPages {
		if page.name == command {
			return page, true
		}
	}
	return commandHelpPage{}, false
}

func printCommandHelp(output io.Writer, command string) bool {
	page, found := commandHelpFor(command)
	if !found {
		return false
	}
	page.writeTo(output)
	return true
}

func (page commandHelpPage) writeTo(output io.Writer) {
	if reportOutputCommand(page.name) {
		page.selectors = append(append([]string{}, page.selectors...), reportOutputHelp()...)
		page.sideEffects = append(append([]string{}, page.sideEffects...), "Writes a complete versioned JSON report below .code-polishy-reports even when the displayed view is filtered or truncated.")
	}
	writeHelpSection(output, "Usage", page.syntax)
	fmt.Fprintf(output, "\n%s\n", page.summary)
	writeHelpSection(output, "Selectors and arguments", page.selectors)
	writeHelpSection(output, "Side effects", page.sideEffects)
	writeHelpSection(output, "Exit status", page.exits)
	writeHelpSection(output, "Examples", page.examples)
}

func reportOutputHelp() []string {
	return []string{
		"Output options are separate from evaluation selectors and never reduce evaluated scope or exit status.",
		"--format human|json|sarif selects bounded human output or one machine document; the default is human.",
		"--output PATH atomically writes an explicit contained regular file instead of stdout.",
		"--filter-rule, --filter-module, --filter-path, and --filter-relation are repeatable display-only filters.",
		"--group-by rule|module|path|relation controls view ordering without changing the complete report.",
		fmt.Sprintf("--display-limit N bounds human findings from 1 through %d; the default is %d.", engine.MaximumFindingDisplayLimit, engine.DefaultFindingDisplayLimit),
	}
}

func writeHelpSection(output io.Writer, heading string, lines []string) {
	fmt.Fprintf(output, "\n%s:\n", heading)
	for _, line := range lines {
		fmt.Fprintf(output, "  %s\n", line)
	}
}

func handleContextualHelp(invocation invocation) (int, bool) {
	switch invocation.command {
	case "--help", "-h":
		fmt.Print(usage)
		return 0, true
	case "help":
		return handleNamedHelp(invocation.arguments), true
	}
	if !helpRequested(invocation.arguments) {
		return 0, false
	}
	if printCommandHelp(os.Stdout, invocation.command) {
		return 0, true
	}
	return commandUsageError("", "unknown command "+invocation.command), true
}

func handleNamedHelp(arguments []string) int {
	if len(arguments) == 0 || len(arguments) == 1 && (arguments[0] == "--help" || arguments[0] == "-h") {
		fmt.Print(usage)
		return 0
	}
	if len(arguments) != 1 {
		return commandUsageError("", "help requires exactly one command")
	}
	if printCommandHelp(os.Stdout, arguments[0]) {
		return 0
	}
	return commandUsageError("", "unknown command "+arguments[0])
}

func helpRequested(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "--" {
			return false
		}
		if argument == "--help" || argument == "-h" {
			return true
		}
	}
	return false
}

func commandUsageError(command, message string) int {
	return commandUsageErrorWithCorrection(command, message, "")
}

func commandUsageErrorForInvocation(invocation invocation, message string) int {
	return commandUsageErrorWithCorrection(invocation.command, message, commandCorrection(invocation))
}

func commandUsageErrorWithCorrection(command, message, correction string) int {
	fmt.Fprintln(os.Stderr, "usage error:", message)
	if correction != "" {
		fmt.Fprintln(os.Stderr, correction)
	}
	if !printCommandHelp(os.Stderr, command) {
		fmt.Fprint(os.Stderr, usage)
	}
	return 2
}

func commandCorrection(invocation invocation) string {
	if invocation.command == "design-context" && len(invocation.arguments) > 0 && !strings.HasPrefix(invocation.arguments[0], "-") {
		paths := []string{}
		for _, argument := range invocation.arguments {
			if strings.HasPrefix(argument, "-") {
				break
			}
			paths = append(paths, argument)
		}
		return "Did you mean: code-polishy design-context --files " + strings.Join(paths, " ") + "?"
	}
	if invocation.command == "test" && containsOption(invocation.arguments, "--base") && !containsOption(invocation.arguments, "--changed") && !containsOption(invocation.arguments, "--recommended") {
		return "Use --changed or --recommended with --base; for changed tests: code-polishy test --changed --base REF"
	}
	return ""
}

func containsOption(arguments []string, option string) bool {
	for _, argument := range arguments {
		if argument == option || strings.HasPrefix(argument, option+"=") {
			return true
		}
	}
	return false
}
