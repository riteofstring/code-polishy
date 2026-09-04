# Verification and Testing Policy

Verification proves observable behavior at the narrowest stable boundary that
can detect the risk, while retaining one complete whole-application profile.

## Required suite model

Every test suite declares:

- a unique lowercase `name`;
- an open `kind`, such as `unit`, `integration`, `contract`, `component`,
  `browser`, `visual`, `e2e`, `content`, `performance`, `mutation`, or `live`;
- `scope: module` with exactly one module, or `scope: repository` for
  cross-module evidence;
- `cost: quick`, `standard`, or `expensive`;
- an argument-array command, contained working directory, and timeout;
- one or more execution profiles: the nested `focused`, `recommended`, and
  `full` profiles, or the isolated `supplemental` profile.

Module suites default to quick and all three profiles. Repository suites
default to standard and full only. Browser, visual, E2E, performance, and live
suites default to expensive. Mutation, acceptance-mutation, Gherkin-mutation,
CRAP, and risk-analysis suites default to expensive and supplemental only.
Explicit values may refine ordinary defaults, but:

- focused suites must be quick and must also run in recommended and full;
- recommended suites must be quick or standard and must also run in full;
- supplemental suites must be expensive and cannot also belong to focused,
  recommended, or full;
- mutation and related test-strength kinds must remain supplemental.

Every module must have at least one module-scoped focused suite. Every
repository must have at least one repository-scoped full suite. A project may
also list `tests.requiredKinds` to make specific layers mandatory.

Built-in project capabilities imply these repository-scoped full-profile
requirements:

| Capability | Required full evidence        |
| ---------- | ----------------------------- |
| `backend`  | integration, contract, or E2E |
| `frontend` | component, browser, or E2E    |
| `ui`       | browser or E2E                |
| `visual`   | visual                        |
| `content`  | content or contract           |

Conditional stack detection adds requirements without making targets repeat
capability declarations:

| Detected policy module | Required full evidence     |
| ---------------------- | -------------------------- |
| `react`                | component, browser, or E2E |
| `electron`             | Electron, browser, or E2E  |

Mutation testing is optional. Projects may add supplemental mutation suites and
use `tests.requiredSupplementalKinds` to make mutation, CRAP, fuzz hardening,
or another deliberate test-strength layer mandatory for a selected hardening
event. The declarations do not schedule supplemental execution: ordinary
development, changed tests, checkpoint and merge gates, guidance
synchronization, and lock upgrades report it as `NOT RUN`.

Kinds and tools are otherwise open. A Playwright browser workflow and an
Electron CDP harness can both be `browser`; a local screenshot comparator and a
hosted service can both be `visual`. The target owns how evidence is produced.
That command also owns framework-specific skip semantics: configure the test
runner or a repository wrapper to return nonzero when no tests execute or an
unexpected test skips. Code Polishy treats an exit-zero suite command as
successful and does not infer executed or skipped counts from human-readable
output.

## Verification checkpoints

Verification follows changed risk, not the number of edits, commits, or chat
turns:

| Event                                                   | Required verification                             |
| ------------------------------------------------------- | ------------------------------------------------- |
| Read-only inspection or status report                   | None                                              |
| Ordinary prose-only Markdown change                     | Format and documentation checks only              |
| Clean checkout, fetch, merge, rebase, tag, or push prep | None                                              |
| Manually resolved source conflict                       | One narrow affected test                          |
| Coherent runnable source change                         | One narrow affected test                          |
| Completed source task with no final gate next           | `test --changed`                                  |
| Final candidate                                         | One base-aware merge gate, owned locally or by CI |
| Stable release candidate                                | Only explicitly selected supplemental suites      |

Use the first applicable row. A clean merge or rebase means Git applied it
without manual file edits. A resolved source conflict is a source change; a
resolved ordinary Markdown conflict remains documentation-only. Committing,
branch synchronization, tag creation, installation, lock updates, and push
preparation do not invalidate unchanged evidence or authorize a new run.

An exact test is the smallest named suite or module suite that can observe the
changed behavior. Run it after a coherent runnable slice, not after every edit.
`test --changed` is broader task-boundary feedback. Skip it when a final merge
gate immediately follows over the same candidate because that gate already
selects the required changed-impact tests. One delivery event has one final-gate
owner; local and CI gates are both required only when independent evidence was
explicitly requested.

A stable release candidate is the exact committed tree intended for tagging,
after ordinary verification passes and planned source and policy changes stop.
Supplemental selection happens only then. A failing supplemental run is a
diagnostic boundary: fix and rerun its exact failed or invalidated suites before
considering another broad run.

## Execution profiles

```sh
code-polishy test --module MODULE
code-polishy test --changed [--base TASK_BASE]
code-polishy test --suite SUITE
code-polishy test-levels [--base TASK_BASE]
code-polishy test-plan [--base TASK_BASE]
code-polishy test --recommended [--base TASK_BASE]
code-polishy test --all
code-polishy test --supplemental
code-polishy verify [--tests-only]
code-polishy gate
code-polishy checkpoint-gate --base PREVIOUS_CHECKPOINT
code-polishy merge-gate --base MERGE_TARGET
```

- `--module` runs only focused suites attached to the named modules. This is
  the narrowest explicit iteration mode.
- `--changed` maps changed and deleted paths to modules, computes reverse
  dependents from the module DAG, and runs their focused suites. A change to a
  foundation therefore tests its consumers. An ordinary Markdown-only delta
  selects zero application suites. Without `--base`, it compares the working
  tree with `HEAD`. With `--base TASK_BASE`, it compares
  `merge-base(TASK_BASE, HEAD)` plus the current working tree. The report
  records the requested base, resolved exact base, candidate, and governed path
  count.
- `--suite` runs exact named suites for diagnosis or an authorized special
  workflow.
- `test-levels` and its compatibility alias `test-plan` execute no tests. They
  show a terminal-safe ASCII table of all four scopes, changed and impacted
  modules, and their cost mix. Its supplemental row is availability information,
  not a recommendation or execution trigger. For an ordinary Markdown-only
  delta, they add a first-class documentation row with zero application suites.
  With a trusted base, they report the exact policy-selected level, reasons,
  and the one `merge-gate` execution path; without a base, they report
  diagnostic advice only.
- `--recommended` runs focused suites for impacted modules plus repository
  suites marked recommended whose `paths` match the change. Without `--base`,
  it uses the working tree compared with `HEAD`. With `--base TASK_BASE`, it
  uses `merge-base(TASK_BASE, HEAD)`, all committed branch changes, current
  worktree changes, and untracked files.
- `--all` runs every suite marked `full`; it does not include supplemental
  test-strength work.
- `--supplemental` runs every separately declared supplemental suite only when
  the caller explicitly requests it, a checked-in workflow explicitly invokes
  it for that event, or the release checklist selects one run after a stable
  release candidate has stopped changing. The first stable release candidate
  runs the full set once. After failure, use exact `--suite` runs for failed
  suites and passed suites invalidated by changes to their tested production
  files or tests, or their own commands or configuration. That evidence
  composes with still-valid passed suites. Repeat the full set only when shared
  mutation infrastructure, toolchain, or selection changes, or impact cannot
  be bounded.
- `verify` runs the full test profile and then build providers.
- `gate` adds strict coverage, repository-wide code health, and online
  supply-chain enforcement. Neither `verify` nor `gate` silently runs
  supplemental suites, even when they are declared or their kinds are required.
- `checkpoint-gate` accepts one clean committed task on a long-lived branch.
  It no-ops when the supplied base yields no governed candidate paths, runs the
  documentation contract for ordinary Markdown, and runs affected checks and
  focused changed-scope tests. It reports behavior review as optional, required,
  passed, or failed; only selected task or checkpoint features require evidence.
  Only a complete pass records the accepted HEAD and checkpoint receipt.
- `merge-gate` is the executable ordinary merge policy. Given a trusted base,
  it selects the documentation contract, an impact-scoped recommended profile,
  or the complete full gate. Its independent behavior decision uses the base
  and candidate policy plus additive task requirements. Gate output shows one
  concise behavior status, phase progress, and a final result. On failure it
  prints a bounded failure tail and the path to the corresponding managed log;
  direct `test` commands keep their normal streaming output.

## Test-quality reminder

When a candidate contains an added or modified test file, Code Polishy prints
one prominent, non-blocking reminder once per invocation at high-signal change
checkpoints:

- the default `check`, `check --git-changes`, and `check --staged`;
- the default changed-scope `test` command and `test --changed`;
- `test-levels` and its `test-plan` compatibility alias; and
- `checkpoint-gate` and `merge-gate`.

The reminder says: “Make sure none of the tests (new or old) are tautological
or change-detector tests.” A tautological test derives its expected result from
the same production implementation being tested. A change-detector test mirrors
private source spelling, extracted helper text, or collaborator choreography and
therefore breaks under a behavior-preserving implementation change.

The reminder does not alter selection or exit status, run additional tests or
checks, request authorization, or need configuration. It stays quiet for
deleted-only test paths, production-only or other unrelated changes,
`check --all`, `check --files`, or `check --name`, and explicit suite, module,
full, recommended, or supplemental test runs.

For a checkpoint gate, the changed-test count is the slice since its explicit
previous checkpoint. A merge gate always keeps its merge-target-wide changed
test count. When a valid checkpoint receipt is bound to the current candidate,
the merge reminder also identifies the latest task slice and its base. Missing,
stale, or malformed checkpoint evidence is ignored for this advisory display;
it never changes merge selection or gate scope.

Without a base, the diagnostic planner advises full when an application
selection expands to the whole repository, when at least 20 governed paths
change, when at least three modules change directly, or when dependency impact
reaches at least two thirds of a graph with three or more modules. It advises
recommended for narrower application work. A non-documentation deletion or a
policy, dependency, workflow, container, ESLint, Knip, Ruff, Vulture, carried
CPython, `ty`, TypeScript, OSV, or `scope.pythonDynamicReferences` input expands
analysis to the whole repository.

## Default documentation merge gate

Code Polishy classifies the exact Git candidate before module ownership,
adaptive configuration, or repository-wide impact expansion. The
`documentation` level is selected when every added, modified, deleted, or
renamed path is ordinary Markdown:

- `.md` and `.markdown` match case-insensitively at any path;
- `AGENTS.md`, `CLAUDE.md`, `SKILL.md`, and Markdown below any `skills` or
  `templates` directory are control inputs and follow ordinary application
  verification;
- exact paths listed in `documentation.productInputs` follow ordinary
  application verification; and
- any mixed or non-Markdown candidate follows the existing recommended or full
  decision.

`documentation.productInputs` accepts unique, exact, contained,
repository-relative Markdown paths. It can only make selection more
conservative. Use it for Markdown consumed by a build, generator, test fixture,
prompt, or product runtime.

Documentation additions, deletions, renames, and changes spanning at least 20
documents stay in this level. Analysis may still expand for a deletion or
non-regular file, but the exact candidate delta remains separate and
authoritative for merge classification.

The documentation level runs one built-in cross-platform contract. It requires
contained regular UTF-8 Markdown within the size bound, final newlines, no
trailing whitespace, sealed formatting, and valid local link, image, and
fragment targets across the complete remaining Markdown corpus. External URLs
are never fetched. Missing, escaped, non-regular, ambiguous, or malformed local
targets are deterministic findings.

The merge gate is read-only. An agent runs
`code-polishy format --git-changes`, fixes any remaining documentation finding,
and repeats the final gate without asking the user for authorization. The level
runs no strict doctor, application test, build, configured target command,
supply-chain provider, artifact target, supplemental suite, or live probe.

Default output is:

```text
MERGE GATE: DOCUMENTATION against origin/main
```

## Long-lived branch checkpoint gate

`checkpoint-gate --base REF` is the task boundary for a branch that will keep
receiving AI changes before merge. `REF` is the previous accepted commit, and
the candidate must be one clean committed HEAD. The command accepts no file,
module, suite, or profile downscope.

An unchanged candidate prints `CHECKPOINT GATE: UNCHANGED` and performs no
review, checks, tests, or receipt write. A changed candidate first computes one
behavior decision. Missing configuration and no task request reports `NOT RUN`
and reads no review artifacts. A task-requested or checkpoint-required feature
must satisfy its exact receipt and proof replay; a merge-only feature remains
optional at this boundary. The gate then runs the normal change-aware policy
check for the selected files and focused suites for changed modules plus reverse
dependents, forcing and deduplicating any selected feature suites. It stops
after a failed phase and never runs merge-only builds, supply-chain work, full
suites, or supplemental suites, even when they are declared or their kinds are
required.

After a complete pass, Code Polishy verifies that HEAD stayed unchanged and
records checkpoint evidence for the accepted candidate. A checkpoint that runs
gate work also writes a versioned JSON run report and one bounded log for each
executed command below `.code-polishy-reports/checkpoint-gate/`. The report
binds the requested and exact base, candidate, selected policy level, release
and configuration identity, command outcomes, failure evidence, log paths, and
final status. The checkpoint receipt records the exact merge base, accepted
candidate, scope, full behavior-review status and selection digest, and the
exact passed run identity, execution, and report digest. Readers require that
report to remain the current validated passed checkpoint report. It is an audit
record, not an implicit base: the next invocation still supplies the accepted
commit explicitly.

The terminal shows `RUN`, `PASS`, or `FAIL` phase progress. A failure prints a
bounded tail and the managed log path; the report and log remain the durable
machine-readable evidence. An artifact-write failure is operational failure,
so the gate cannot report success.

Default successful output is:

```text
CHECKPOINT GATE: CHANGED against PREVIOUS_CHECKPOINT
CHECKPOINT ACCEPTED: 0123456789abcdef0123456789abcdef01234567
```

This command does not hook an AI harness or run after every chat turn. Checked-in
agent guidance invokes it after a completed code-changing task. A generic
wrapper may invoke it more broadly if it supplies the last accepted commit;
the unchanged path makes conversational and read-only turns harmless.

## Optional adaptive application merge gate

Mixed content-and-code repositories can opt in without changing the default
for other targets:

```json
{
  "verification": {
    "trustedMergeTarget": "origin/main",
    "mergeGate": {
      "recommendedModules": ["content"]
    }
  }
}
```

The names must be existing modules. For candidates outside the documentation
level, an omitted `verification.mergeGate` retains full-gate behavior. The list
is an allowlist only: target configuration cannot remove shared escalation
rules. `verification.trustedMergeTarget` gives agents checked-in merge-target
guidance. Resolve an explicit task target first, then this field,
`origin/HEAD`, `origin/main`, and `origin/master`. Record the selected source in
the task handoff and fail on invalid configured guidance.

`merge-gate --base REF` derives the change from `merge-base(REF, HEAD)` plus
the current staged, unstaged, and untracked work. It exposes no `--files`,
`--module`, `--suite`, or caller-selected policy level. CI should supply a
trusted PR base SHA or push-before SHA; a missing, option-like, or invalid base
fails instead of guessing.

Each merge gate writes a versioned JSON run report and bounded per-command logs
below `.code-polishy-reports/merge-gate/`. The report is the machine-readable
record of the selected command plan, attempts, durations, reuse decisions,
structured findings, and final status. It records command failure categories
from runner facts only: `command-exit`, `timeout`, `canceled`, `environment`,
`resource`, or `operational`. Test evidence also identifies suite ownership,
changed and impacted overlap, exit status, attempt count, and log path.
Before an exact-base replay, Code Polishy loads the base release and
configuration and requires the named base suite to match the candidate suite
definition. An unavailable or changed suite reports `baseline-unavailable`
instead of guessing. Candidate retries and exact-base replays may add observed
diagnostic states; they never turn the original gate failure into a pass.

After documentation classification, recommended is selected only when every
changed path maps to exactly one allowlisted module and the existing impact
planner recommends it. Full is forced when:

- adaptive merge verification is not configured;
- a non-documentation deletion or a shared policy-sensitive input expands
  selection to the whole repository;
- a path is unowned, ambiguously owned, or belongs to a non-allowlisted module;
- at least 20 governed paths change, at least three modules change directly,
  or dependency impact reaches two thirds of a graph with three or more
  modules.

Policy-sensitive inputs include `.code-polishy.json`, `.code-polishy.lock.json`,
manifests, lockfiles, workflows, container inputs, and supported policy-tool
configuration. Thus changing the allowlist cannot make its own pull request
use the narrow lane.

The recommended execution profile consists of strict doctor, applicable gate
checks for the derived selection, recommended tests for changed modules and
reverse dependents, applicable build providers, and offline supply-chain
verification. The full decision invokes the unchanged complete gate with
repository-wide checks, full ordinary tests and builds, online supply-chain,
and artifact enforcement. Supplemental mutation and risk suites are distinct
from every ordinary level. `test --supplemental` runs them as a separate stage
only when an explicit caller request, event-specific checked-in workflow, or
stable-candidate release checklist selects it; release retry evidence follows
the exact-suite invalidation rule above. Credentialed, destructive, and
live-provider work remains an external gate.

### Resume a failed merge gate

`merge-gate --base REF --resume` is an explicit retry mode. It may reuse only a
successful ordinary test-suite command from a prior failed merge-gate report
with the same content identity and a valid local receipt. The identity binds
the exact merge base and candidate, locked Code Polishy release, loaded
configuration, full command plan, platform, and declared command environment
inputs. A candidate, base, policy level, release, configuration, command-plan,
platform, environment, failed-command, or invalid-receipt change prevents
reuse.

Checks, builds, supply-chain commands, artifact-security commands, behavior
proof replays, failed commands, and commands without valid receipts always run
again. A normal `merge-gate` run never reuses receipts. Resume does not reduce
scope, and final clean-candidate validation still applies. Local receipt
digests establish content identity rather than a signature; CI may keep the
report directory in a stronger custody boundary.

Every reused suite gets a new receipt inside the current execution. That
receipt carries validated provenance back to the original executed suite, so a
second identical retry after another late failure remains loadable without
mistaking reused work for a fresh execution.

Default human output keeps the decision to one line:

```text
MERGE GATE: RECOMMENDED against origin/main
```

Use `code-polishy --verbose merge-gate --base REF` for additional selection and
report detail. CI that needs retention stores the managed JSON report and logs,
not parsed or archived terminal output. Human handoffs summarize the outcome
and actionable failures; report paths provide the detailed evidence.

## Behavior and final-state review receipt

Behavior review is optional unless checked-in policy or an additive task
request selects it. Base-aware plans and both gates disclose both `BEHAVIOR
REVIEW` and `FINAL STATE`. Optional review reads no packet, proof, or receipt
and does not change ordinary command selection or runtime.

Before implementation, the agent harness supplies the user's original request
and acceptance criteria to `behavior-review capture-intent` at the task base.
The harness repeats capture before acting on each later correction. Correction
capture may bind a dirty candidate-state digest; it invokes no tests or AI
review. Repeated `--feature` options select configured features immediately;
`behavior-review require --base TASK_BASE` can append feature coverage later
only when that original intent exists. Records are additive.

For selected review, `checkpoint-gate` and `merge-gate` validate the current
clean candidate's receipt against the resolved base, base and candidate policy,
task requirements, and exact feature selection. They replay every cited
red/green proof and force selected ordinary feature suites before further work.
A missing required receipt reports `REQUIRED`; stale, malformed, unresolved,
under-proved, or non-reproducible evidence reports `FAILED`. Either becomes a
`policy.behaviorReview` finding before expensive ordinary commands.

The receipt comes from one review subagent with no inherited conversation. It
classifies observable behavior, checks durable prose and executable correction
residue, and cites packet evidence for every final-state finding. Requested
behavior also needs red/green proof. If the harness cannot start subagents, use
a separate clean AI invocation with only the packet. Follow the [Behavior and
Final-State Review Policy](behavior-review.md) for the trust boundary and
limits.

## AI execution

An AI collaborator should treat the levels differently:

1. Before implementing a non-documentation request, capture the exact
   harness-supplied request at the task base with `behavior-review
capture-intent`. Capture each later correction before acting on it. During
   implementation, run exact named or module tests as useful.
2. For ordinary Markdown-only work, run `format --git-changes`, fix the built-in
   documentation findings, and run no application tests. This needs no user
   approval.
3. While iterating on application source, run `test --changed` for affected modules and reverse
   dependents when exact focused tests are no longer enough.
4. After a completed code-changing task on a long-lived branch, commit the
   candidate, complete behavior review only when status selects it, and run
   `checkpoint-gate --base PREVIOUS_CHECKPOINT`. Start the next task only after
   it records the accepted HEAD.
5. During a task, use `test --changed --base TASK_BASE` or
   `test-levels --base TASK_BASE` (or the compatibility alias `test-plan`) to
   inspect changes since the task boundary. At an ordinary merge checkpoint,
   resolve a trusted merge target separately.
6. At a genuine merge checkpoint, run `merge-gate --base MERGE_TARGET` without
   asking the user to choose a level. It subsumes changed-impact
   validation, so do not run `test --changed` immediately beforehand for the
   same candidate. Report only pass/fail and actionable findings to the user;
   managed report and log paths retain the detailed evidence.
7. Run `test --supplemental` after a green ordinary gate only when the caller
   explicitly requests local hardening, a checked-in workflow explicitly
   invokes it for that event, or the stable-candidate release checklist selects
   it. Focused, recommended, full, `verify`, `gate`, and `merge-gate` exclude
   supplemental execution. After a failed stable-candidate run, use exact
   failed or invalidated suites under the supplemental retry rule rather than
   restarting every suite.

Do not run a checkpoint after every edit or chat turn, and do not turn focused
feedback, documentation, checkout, branch synchronization, conflict-free merge
or rebase, tagging, lock update, or push preparation into `verify`, `gate`, or a
checkpoint. After manually resolving a source conflict, run one affected exact
test; ordinary Markdown still follows the documentation rule. A checkpoint
closes a completed committed source task. Credentialed, destructive, and
live-provider checks remain typed external gates. CI may own the one checked-in
merge workflow for the final candidate.

## Test stable interfaces

- Assert observable outcomes through a module's public interface.
- Combine tightly coupled shallow modules into a deep boundary, then replace
  internal tests with boundary tests. Do not keep both suites indefinitely.
- Avoid change-detector tests that inspect private source spelling, extracted
  helper text, implementation call order, or collaborator choreography unless
  that detail is itself the contract.
- Avoid tautological tests that duplicate the implementation to calculate the
  expected value.
- Validate generated contracts through a consumer; do not merely rerun a
  generator and diff its own output.
- Use table-driven or property tests for parsers, state transitions, and
  invariants where input space is broad.

The parser rejects obvious no-op and pass-with-no-tests commands, and ordinary
source checks reject empty or unconditional-skip Go tests. These intentionally
cheap checks are not semantic proof. Optional supplemental mutation testing can
establish whether realistic production changes make the tests fail. See
[Test Strength and Executable Specification](test-strength.md).

Gherkin remains optional. When governed `.feature` files exist, they must feed
a full executable acceptance suite. A separate supplemental acceptance-mutation
suite is optional hardening. Feature text describes domain behavior, not
classes, endpoints, tables, private state, or mock choreography.

## Test environments

- Use temporary directories, databases, app data, ports, and disposable Git
  repositories.
- Never mutate developer history, user data, or a non-disposable worktree.
- Use a faithful local substitute for local infrastructure.
- Use an in-memory adapter for a remotely owned service.
- Mock a true third party once, at its adapter boundary.
- Exercise production parsers, transports, persistence, IPC, rendering, and
  startup paths for cross-layer risks.
- Missing required capabilities fail. A skip is narrow, named, and visible.

## Whole-application coverage

A full workflow should cross every production layer relevant to the changed
behavior. Depending on the product, that can include:

- backend commands, persistence, transactions, HTTP or IPC, and generated
  frontend decoding;
- browser startup, routing, rendering, user input, keyboard/focus behavior,
  responsive states, and recovery after reload;
- queueing, concurrency, cancellation, approvals, failure, restart, and
  promotion of pending work;
- visual snapshots at named viewports and appearance modes, with reviewed
  baselines and useful failure artifacts;
- content schemas, cross-references, links, generated indexes, and publication
  contracts in an empty consumer checkout;
- before/after measurements for performance-sensitive changes.

A backend handler test does not prove the browser workflow. A component test
does not prove persistence. A screenshot does not prove interaction semantics.
Declare each material layer honestly.

## Browser and visual systems

Browser suites should use an actual browser engine or production desktop shell
when DOM, layout, navigation, accessibility, or renderer integration matters.
Headless execution is acceptable when it exercises the same production path.

Visual suites should define:

- stable fixtures and deterministic data;
- browser/runtime and viewport pins;
- font and animation controls;
- baseline ownership and review rules;
- thresholds that catch meaningful regressions without normalizing noise;
- retained diff images or traces on failure.

Visual diffs complement semantic assertions. They do not replace accessible
names, focus order, keyboard behavior, or state-transition checks.

## Live and nondeterministic dependencies

Keep deterministic fake-provider workflows in the normal full profile. Live
third-party wire checks are typed external approval gates. Invoke them from the
approved external gate with the required credentials and explicit
authorization. Do not declare them as `tests.suites` entries.

A skipped live gate must be reported as skipped; it is not proof of provider
integration. Never silently fall back from a failed live call to a fake and
report the workflow as live-passed.

## Failure iteration

After a broad gate fails:

1. identify the first causal failure;
2. rerun the exact suite or module;
3. fix the implementation or contract;
4. rerun the focused suite;
5. rerun the broad profile only when the focused behavior is stable.

Documentation-only changes may use diff inspection when they touch no code,
manifests, generated contracts, policy, or executable examples.
