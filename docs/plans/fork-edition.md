# Fork Edition: Governing a Fork's Own Changes

Status: proposed implementation plan, not yet decided; written 2026-10-15
against `main` at `da398234fad74b9417ff93de2fa939d1ec496b9c` with locked
release `v0.28.1`. The design was reviewed to agreement with a second model
(Codex, `gpt-6-astra`, maximum reasoning effort) over eleven rounds on
2026-10-14; the review transcripts were ephemeral and their conclusions are
recorded here.

## Problem

A repository that forks a separately owned upstream, adds its own code, and
keeps merging upstream cannot adopt Code Polishy as it stands. The target that
motivated this plan is `riteofstring/t3code-onirigiri`, a fork of
`pingdotgg/t3code` that adds an Onirigiri spatial workspace. Measured against
the upstream merge base on 2026-10-14:

| Fact                                              |  Value |
| ------------------------------------------------- | -----: |
| Tracked files                                     | 23,396 |
| Tracked files outside the `.repos` reference tree |  4,718 |
| Files added by the fork                           |    113 |
| Upstream files modified by the fork               |     96 |
| Upstream files deleted by the fork                |      1 |
| Modified upstream files above 2,500 lines         |      7 |
| Largest modified upstream file, `ChatView.tsx`    | 10,189 |

The added files cluster in `apps/spatial`, `packages/onirigiri-t3`, and
`apps/desktop/src/panePictures`. The modified upstream files are scattered
through `apps/web/src` and `apps/desktop/src`. "Ours" is therefore a Git fact,
not a directory, and it changes with every upstream merge.

Standard Code Polishy cannot express this, and the reasons are deliberate
policy rather than gaps:

- `scope.exclude` is glob-only with no negation, and doctor's hidden-input
  check rejects excluding handwritten source, manifests, lockfiles, or
  workflows.
- Excluded files are invisible to analysis. A relative import from a modified
  upstream file into an untouched sibling becomes an
  `architecture.importCoverage` finding and marks the graph incomplete.
- Exceptions are exact-path and expire within 366 days; seven files over the
  failure budget times several checks is not maintainable.
- The sealed Prettier configuration would rewrite 79 of the 87 modified
  upstream TypeScript files as they stand at the merge base, and upstream's
  formatter, oxfmt, rewrites Prettier output on 99 of 106 added files. Every
  upstream sync would conflict on style.
- The merge gate runs supply chain over the whole candidate, and
  `merge-gate --base upstream/main` is always first adoption because upstream
  has no configuration, forcing a full gate forever.
- The managed `AGENTS.md` collides with upstream's own `AGENTS.md`.

## Decision: a fork edition built from mainline

The fork profile ships as a separately identified **fork edition** of Code
Polishy, built from the mainline source tree behind a Go build tag. Standard
releases are built without the tag and contain no fork code path. The fork
edition declares `edition: fork` and a `fork-profile` feature in its release
manifest and publishes under its own archive name and publication index.

The requirement that drove this placement: the fork edition must not change how
standard Code Polishy behaves for a repository that adopted it normally, and an
agent must have no local route to demote code to upstream responsibility. The
review established that every activation input available locally is forgeable
by an agent that controls files and Git: the lock is hand-editable, `--base` is
caller-chosen, `update-ref` fabricates an upstream ref offline, `git replace`
and legacy grafts rewrite ancestry, and `GIT_CONFIG_COUNT` injects URL
rewrites. If the code is present in the standard binary, an agent can reach it.
If the code is absent, it cannot.

Edition transitions are identified from authenticated release identity, not
from the editable lock alone. `Satisfies` requires edition equality against the
launcher-verified installed manifest, so a lock whose recorded edition disagrees
with the installed release fails before any command runs. CI with the trusted
base SHA refuses a standard-to-fork edition transition; fork-to-standard is a
strengthening and is allowed; fork-to-fork with a different upstream repository
is refused.

## Why not configuration, a branch, or a separate repository

- **In-tree profile gated by configuration or lock.** Rejected. All local
  activation inputs are forgeable, and `Satisfies` accepts feature subsets, so
  a lock feature alone cannot carry the decision.
- **Separate repository or long-lived branch pulling from mainline.** Equal to
  the fork edition for isolation, worse for maintenance. The profile touches
  `repository`, `engine`, `quality`, `supplychain`, `format`, `agents`,
  `gaterun`, `testing`, and `release`, so frequent merge conflicts are likely,
  and a second complete release pipeline must be re-run for every mainline
  release. Go's import rule would admit a separate module under the
  `github.com/riteofstring/code-polishy/` path prefix, so a full source fork is
  not strictly required, but the maintenance argument stands.
- **Fork edition from the same tree.** One codebase, shared tests, no merges,
  and a standard binary that provably lacks the code. Chosen.

## Fork profile design

### Ownership

The complete analysis inventory stays exactly as today. A derived **ownership
set** decides obligations, with three states: `added`, `modified`, and
`inherited`. Ownership derives from `upstream.commit` (U), the pinned upstream
merge base, by content comparison of the analyzed snapshot against U, including
the dirty working tree, so a reversion drops out. Renames count as delete plus
fork-owned add. Deletions are recorded. Ownership is a new derivation, not a
reuse of `selectionSince`, which expands to all files on deletions or
policy-sensitive changes.

Two refs stay separate. U derives ownership. `merge-gate --base`, checkpoints,
behavior review, and test selection stay on the fork's merge target.

No configuration field can mark a path inherited. Modules may be declared over
inherited paths with `"upstream": true`; they are graph targets only, carry no
coverage or style obligations, and cannot override derived ownership of a
modified file.

### Obligations by ownership

| Check                | Added                     | Modified upstream                                          | Inherited                                  |
| -------------------- | ------------------------- | ---------------------------------------------------------- | ------------------------------------------ |
| Lint, complexity     | Full                      | Baseline at U; new or worsened only; non-increasing excess | None                                       |
| File length          | Full                      | Above 2,500 at U: cumulative net growth cap, ceiling 100   | None                                       |
| Format               | Repository formatter owns | Read-only check, no writes from any lane                   | None                                       |
| Hygiene              | Sealed                    | Sealed                                                     | None                                       |
| Typecheck            | Integration lane          | Integration lane                                           | Integration errors block anywhere          |
| Dead code            | Full, complete consumers  | Baseline for pre-existing unused exports                   | Exports made unused by the fork reportable |
| Architecture         | `dependsOn` on new edges  | Same; edges present at U unchecked                         | Graph targets via `upstream: true` modules |
| Integration findings | Block                     | Block                                                      | Block                                      |

Context files can and must produce integration findings: a type error in an
untouched upstream caller of a fork API, an unresolved import, incomplete
analysis, or a configuration error blocks regardless of location. Only style
and budget obligations follow ownership.

The growth allowance is a cumulative 100 net lines measured against the fixed U
for files above 2,500 lines at U. 100 is the policy ceiling, configurable only
downward. It never resets per gate and resets only at an explicit anchor
advance. Measured today the fork's seams into oversized files range from -232
net lines (`ChatView.tsx`) to +38, so the rule admits every current seam while
bounding future growth. Complexity follows non-increasing excess: a previously
compliant function may grow while under the threshold; an over-budget function
may not worsen. Under the sealed parser `Sidebar` rises from 27 to 32 and
`SidebarSectionHeader` from 10 to 12 today; both are legitimate findings.

### Baseline analysis

Modifications are judged against the same sealed analyzer run on the file as it
exists at U, in a detached managed worktree created with the existing
`internal/repository/revision.go` worktree helper. The cache key is the tuple of
U, the release identity, the effective analysis configuration digest (compiled
rule activation and limits), and the baseline dependency environment identity.
Comparison matches rule plus symbol or function identity and compares
multiplicity and metric values, so a count rising from 1 to 2 or a complexity
rising from 27 to 32 is reported as worsened. Unmatchable diagnostics are new.

### Supply chain

Whole-tree integrity stays blocking in every state: lock consistency, frozen
install, protocol containment, lifecycle policy, incomplete coverage, and
unknown severity, because the fork ships the entire tree.

Attribution has three states per package and per use:

- **Inherited.** The lock entry is unchanged from U and the fork introduces no
  new use. Not age-checked. Low and moderate vulnerabilities are advisory; high,
  critical, and KEV need an exact, expiring `inherited-upstream` assessment;
  disallowed or unknown licenses need an exact, expiring exception naming
  package, version, scope, and upstream reference.
- **Fork-used.** The entry is unchanged but the fork adds a new importer edge,
  manifest declaration, or scope promotion. Ordinary release-age admission with
  publication evidence; reuse of prior evidence only when valid and unexpired;
  OSV and license findings follow standard rules.
- **Fork-changed.** New package, version or resolution change, patch, lifecycle
  or `allowBuilds` change, override. Full standard admission.

### Integration lane and formatter ownership

The fork declares upstream's own CI commands as repository checks and suites:
`vp check`, `vpr typecheck`, `knip:check`, builds, and tests. Merge acceptance
combines sealed fork-owned results with this lane. Where the sealed compiler
cannot honor the repository's compiler contract (the target pins TypeScript
7.0.2 with a patched compiler and `@effect/language-service` diagnostics
against the sealed 6.0.3), sealed typecheck reports `not-applicable` with the
exact reason and the integration lane owns typecheck.

One formatter owns each language. In this target the repository formatter,
oxfmt through `vp fmt --check`, owns JavaScript, TypeScript, JSON, CSS, YAML,
Markdown, and HTML. The sealed formatter is `not-applicable` for those, with the
owning command named in every report; sealed hygiene checks still run. Every
integration formatting invocation is restricted to its assigned paths, including
formatting reached through `vp check`, and doctor rejects overlap or gaps.

### Tests, evidence, guidance

Every modified upstream test, 17 today, gets an explicit `tests.ownership`
entry and a suite that executes its complete file by path with a runner that
rejects empty runs; `--passWithNoTests`, which the target's `apps/web` and
`apps/desktop` scripts use today, is not allowed in a governed suite command.
Unmodified upstream tests run as integration evidence.

Profile and U enter the gate identity through the configuration digest and are
written as structured report fields. Fork-edition receipts, gate results, and CI
status carry a distinct kind that standard acceptance rejects, carried through
behavior-review evidence and stored graphs. Governed counts are reported against
the post-exclusion inventory, not tracked files.

`guidance.path` is first-class across install, sync, check, doctor, and
upgrade, with a doctor-verified discovery pointer. Upstream's `AGENTS.md` is
otherwise untouched.

### Anchor advance

Advancing U after an upstream merge is an explicit operation. It lists files
leaving fork ownership because upstream absorbed them, files entering it, stale
ownership entries, and findings that disappear, then requires the full
fork-profile gate. The new U must be an ancestor of the upstream tip.

## Provenance rules

- **R1. Constructed Git environment.** Git subprocesses never inherit the
  caller's environment. The engine builds one: PATH, a managed TMPDIR,
  `LC_ALL=C`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`,
  `GIT_GRAFT_FILE=/dev/null`, `GIT_NO_REPLACE_OBJECTS=1`, and nothing else.
  Every invocation passes `-c core.commitGraph=false`. Doctor and gates fail
  closed on `info/grafts`, `shallow`, `objects/info/alternates`, or
  `refs/replace/*`.
- **R2. Repository configuration is untrusted for network resolution.** The
  upstream tip T is resolved with `ls-remote` against the locked URL, and T's
  history fetched, from a fresh per-run scratch Git directory with
  `transfer.fsckObjects=true`, so `insteadOf` rewrites, transports, and
  `core.sshCommand` in the repository's own configuration are never consulted.
- **R3. Provenance is computed only in the scratch store.** Ancestry of U under
  T and the U version of every governed path are evaluated in the scratch store
  populated from the network. The repository's own object store is never used
  for provenance.
- **R4. Historical reads are hash-authenticated from a trusted root.** For a
  trusted SHA the engine reads the commit object, recomputes its object ID,
  walks tree objects to the path verifying each hash, and verifies the blob
  against the OID its parent tree names. This replaces today's unverified
  `ls-tree` plus `cat-file blob` path for base configuration, base lock, and
  every other historical read.
- **R5. Authority.** The fork edition requires
  `verification.finalGateOwner: ci`. Only the CI run, on a fresh runner with
  the trusted base SHA and T resolved there, may write a merge-accepting fork
  receipt. Local fork gates perform the same computation, label every
  provenance-dependent classification "locally derived, not authoritative", and
  write no merge-accepting receipt.

## Assurance boundaries

**Local gates, both editions.** Trusted by assumption: the launcher-verified
installed release, the engine process, the evidence store, the installed
dependency tree, and repository-declared commands including their executed
code. Untrusted: repository files, the index, refs, Git configuration, the
environment, and the agent's edits before and after the run. Within this
boundary an authentic standard executable cannot activate the fork profile, and
an agent cannot forge upstream provenance, demote a fork-owned file, or have a
commit certified on bytes it did not commit. This narrows the guarantee; it does
not repair the trusted mechanisms.

**Authoritative acceptance.** A fresh isolated runner; base SHA and upstream
tip from the CI environment; dependencies installed on the runner from the
frozen lock with scripts disabled, trusted only while inputs, caches, and tree
stay untouched through analysis; checkout-supplied receipts and bundles
rejected, since an agent can force-track a forged receipt; policy-owned
analyzers on authenticated buffers. A repository-declared test that lies about
its own result is a code-review failure no gate can detect. CODEOWNERS plus
required review on policy files and the lock is a deployment prerequisite that
this repository does not currently have.

## Fork-side prerequisites, independent of the edition

- Contain `@riteofstring/onirigiri`: publish and pin exactly, or make it a
  governed in-repository workspace package. `link:../../../onirigiri-beta`
  leaves the repository and is refused.
- Replace caret and tilde ranges in `apps/spatial/package.json` with exact
  pins.
- Add pnpm 11 support to Code Polishy: `catalog:` specifiers and `allowBuilds`.
  Neither is handled today.
- Demonstrate sealed lint and complexity compatibility with the target's
  TypeScript 7.0.2 syntax by a spike before relying on them.

## Mainline hardening surfaced by the review

These hold in standard Code Polishy today, with or without the fork edition.
They are listed here because the review found them; they deserve their own
issues and do not depend on this plan being adopted.

1. Git helpers inherit the caller's environment and honor `git replace`,
   grafts, and the commit-graph cache. A graft supplied on standard input
   collapses `rev-list --count HEAD` from 4,772 to 1 on Git 2.43.0 even with
   `--no-replace-objects`; `GIT_GRAFT_FILE=/dev/null` restores it. Reproduced.
2. Base configuration and lock are read through `ls-tree` and `cat-file blob`
   with no object-hash verification, so an overwritten loose object is served
   as genuine and can falsify first adoption.
3. `assume-unchanged` and `skip-worktree` defeat `CleanHead`: `status`,
   `diff --quiet HEAD`, and `diff-index --quiet HEAD` all report clean while
   working-tree bytes differ from HEAD, so a gate can certify a commit on bytes
   it never analyzed. Reproduced.
4. `Repository.Read` reopens paths with `os.ReadFile` after any verification,
   so analyzed bytes are not the verified bytes.
5. The reusable-suite execution view is sealed with `chmod` only and verified
   after execution; a repository-declared command can unseal, substitute,
   restore, and exit successfully.
6. Local receipts are protected by an unkeyed digest, reuse returns success
   without execution, and a forged receipt can be force-tracked to survive a
   fresh checkout. Agent-writable storage cannot authenticate itself;
   authoritative runs must reject checkout-supplied receipts and bundles.
7. Results that depend on an unverified local dependency tree should be
   advisory; a `.d.ts` edited after installation is analyzed as truth today.

## Milestones

1. **Mainline hardening.** Items 1 through 4 above: constructed Git
   environment, hash-authenticated historical reads, analysis on authenticated
   buffers, and `CleanHead` by content. Each with a regression test that
   reproduces the mechanism. Ships in the standard edition.
2. **Edition plumbing.** Build tag, `edition` and `fork-profile` in the
   manifest and lock, edition equality in `Satisfies`, edition-aware release
   scripts and workflow matrix, distinct archive and index names, and a
   standard-edition regression corpus proving identical reports, selection,
   formatting, receipts, and guidance with and without the tag.
3. **Ownership derivation and provenance.** Scratch-store resolution of T,
   ownership set from U, anchor-advance report, and the fail-closed Git checks.
   Dry run with `list-files` and `doctor` against the target.
4. **Obligation routing.** Baseline analysis at U with the configuration-keyed
   cache, growth cap, non-increasing excess, formatter ownership, integration
   lane, and `not-applicable` typecheck routing.
5. **Supply chain and evidence.** Three-state attribution, inherited
   assessments and exceptions, distinct receipt and status kinds, CI-only
   authority.
6. **Target adoption.** pnpm 11 support, containment of the engine dependency,
   exact pins, test ownership for the 17 modified tests, guidance path, and the
   first fork-edition gate on the target in CI.

## Verification and delivery rules

Follow the locked `agent-workflows` guidance. Each milestone is a completed
source boundary verified with the narrowest exact tests, then
`test --changed --base TASK_BASE`. The standard-edition regression corpus runs
in every milestone from 2 onward. Milestone 1 lands independently and does not
wait for a decision on the rest. No public cutover is advertised until the
fork-edition gate passes on the target in CI.

## Completion criteria

- A standard-edition binary built from the same commit contains no fork code
  path and behaves identically to the previous release on the regression
  corpus.
- The target repository locks against the fork edition, passes doctor, and
  passes an authoritative CI fork gate whose report names the edition, U, and
  the governed count against the post-exclusion inventory.
- An anchor advance on the target produces the obligations report and a
  passing full fork gate.
- Every mechanism in the mainline hardening list has a regression test.

## Open decisions

- Whether to proceed at all. The design is agreed; the work is substantial, not
  subtraction.
- Whether milestone 1 should ship first regardless, as its own release.
- Whether the standard edition's local final-gate default should change. The
  review placed that outside this plan as a separate maintainer decision.
- Where the target's `@riteofstring/onirigiri` dependency should live.
