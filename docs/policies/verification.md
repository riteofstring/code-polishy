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
or another deliberate test-strength layer mandatory for that target.

Kinds and tools are otherwise open. A Playwright browser workflow and an
Electron CDP harness can both be `browser`; a local screenshot comparator and a
hosted service can both be `visual`. The target owns how evidence is produced.

## Execution profiles

```sh
code-polishy test --module MODULE
code-polishy test --changed [--base MERGE_TARGET]
code-polishy test --suite SUITE
code-polishy test-levels [--base MERGE_TARGET]
code-polishy test-plan [--base MERGE_TARGET]
code-polishy test --recommended [--base MERGE_TARGET]
code-polishy test --all
code-polishy test --supplemental
code-polishy verify [--tests-only]
code-polishy gate
code-polishy merge-gate --base MERGE_TARGET
```

- `--module` runs only focused suites attached to the named modules. This is
  the narrowest explicit iteration mode.
- `--changed` maps changed and deleted paths to modules, computes reverse
  dependents from the module DAG, and runs their focused suites. A change to a
  foundation therefore tests its consumers. An ordinary Markdown-only delta
  selects zero application suites. With `--base`, selection includes committed
  branch changes plus the current working tree.
- `--suite` runs exact named suites for diagnosis or an authorized special
  workflow.
- `test-levels` and its compatibility alias `test-plan` execute no tests. They
  show a terminal-safe ASCII table of all four scopes, changed and impacted
  modules, and their cost mix. For an ordinary Markdown-only delta, they add a
  first-class documentation row with zero application suites. With a trusted
  base, they report the exact policy-selected level, reasons, and the one
  `merge-gate` execution path; without a base, they report diagnostic advice
  only.
- `--recommended` runs focused suites for impacted modules plus repository
  suites marked recommended whose `paths` match the change. Without `--base`,
  it uses uncommitted changes from `HEAD`. With `--base`, it uses the merge base
  with `HEAD`, all committed branch changes, current worktree changes, and
  untracked files.
- `--all` runs every suite marked `full`; it does not include supplemental
  test-strength work.
- `--supplemental` runs every separately declared supplemental suite. Use an
  exact `--suite` for one module's mutation or risk analysis.
- `verify` runs the full test profile and then build providers.
- `gate` adds strict coverage, repository-wide code health, and online
  supply-chain enforcement. Neither `verify` nor `gate` silently runs
  supplemental suites.
- `merge-gate` is the executable ordinary merge policy. Given a trusted base,
  it selects the documentation contract, an impact-scoped recommended profile,
  or the complete full gate. Default output gives a concise level/base summary;
  `--verbose` adds the detailed selection reasons, execution plan, progress
  events, timings, and report notes.

## Test-quality reminder

When a candidate contains an added or modified test file, Code Polishy prints
one prominent, non-blocking reminder once per invocation at high-signal change
checkpoints:

- the default `check`, `check --git-changes`, and `check --staged`;
- the default changed-scope `test` command and `test --changed`;
- `test-levels` and its `test-plan` compatibility alias; and
- `merge-gate`.

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

Without a base, the diagnostic planner advises full when an application
selection expands to the whole repository, when at least 20 governed paths
change, when at least three modules change directly, or when dependency impact
reaches at least two thirds of a graph with three or more modules. It advises
recommended for narrower application work. A non-documentation deletion or a
policy, dependency, workflow, container, ESLint, Knip, Ruff, `ty`, TypeScript,
or OSV input expands analysis to the whole repository.

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
from every ordinary level. `test --supplemental` runs them as a separate local
stage. Credentialed, destructive, and live-provider work remains an external
gate.

Default human output keeps the decision to one line:

```text
MERGE GATE: RECOMMENDED against origin/main
```

For debugging or machine audit logs, run `code-polishy --verbose merge-gate`.
Verbose mode retains the stable `MERGE POLICY` receipt, complete execution
telemetry, and report notes. CI may archive that output and should make the
resulting status required for merge. Human handoffs should summarize the
outcome and concrete failures rather than repeat the receipt.

## Optional behavior regression review receipt

A repository may require a behavior-regression receipt for non-documentation
merge candidates:

```json
{
  "verification": {
    "behaviorReview": {
      "required": true
    }
  }
}
```

This is a strict opt-in: omit `behaviorReview` to disable the requirement;
`required: false`, a missing `required`, or an unknown field is invalid
configuration. The requirement applies when either the resolved merge base or
the candidate enables it, so the candidate cannot disable an existing gate.
With the option enabled, `merge-gate` validates the current clean candidate's
receipt against the resolved base and replays every cited red/green proof before
it starts ordinary recommended or full work. A missing, stale, malformed,
unresolved, under-proved, or non-reproducible review becomes a
`policy.behaviorReview` finding. The built-in documentation level bypasses this
receipt.

The receipt comes from a packet-only review plus red/green regression proof for
each requested behavior. The agent runtime must supply and isolate the fresh
reviewer; local artifacts cannot authenticate reviewer identity or context. The
receipt is additional merge evidence, not a replacement for ordinary tests,
policy checks, human approval, or supplemental hardening. Follow the
[Behavior Regression Review Policy](behavior-review.md) for the required
sequence, artifact boundary, replay behavior, and limits.

## AI execution

An AI collaborator should treat the levels differently:

1. During implementation, run exact named or module tests as useful.
2. For ordinary Markdown-only work, run `format --git-changes`, fix the built-in
   documentation findings, and run no application tests. This needs no user
   approval.
3. While iterating on application source, run `test --changed` for affected modules and reverse
   dependents when exact focused tests are no longer enough.
4. At an ordinary merge checkpoint, resolve a trusted merge target. Optionally
   run `test-levels --base MERGE_TARGET` (or the compatibility alias
   `test-plan`) for its read-only diagnostic output.
5. At a genuine merge checkpoint, run `merge-gate --base MERGE_TARGET` without
   asking the user to choose a level. It subsumes changed-impact
   validation, so do not run `test --changed` immediately beforehand for the
   same candidate. Report only pass/fail and actionable findings to the user;
   detailed receipts remain in command logs or task artifacts.
6. Run `test --supplemental` after a green ordinary gate when the caller or
   checked-in workflow requires local hardening. Focused, recommended, full,
   `verify`, `gate`, and `merge-gate` exclude supplemental execution.

Do not ask after every edit, and do not silently turn a request for focused
feedback into an ordinary merge checkpoint, `verify`, or `gate`. An explicit
request for a scoped command remains scoped feedback. Credentialed,
destructive, and live-provider checks remain typed external gates. CI may run
its checked-in merge workflow.

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
