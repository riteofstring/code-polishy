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
  foundation therefore tests its consumers. With `--base`, selection includes
  committed branch changes plus the current working tree.
- `--suite` runs exact named suites for diagnosis or an authorized special
  workflow.
- `test-levels` and its compatibility alias `test-plan` execute no tests. They
  show a terminal-safe ASCII table of all four scopes, changed and impacted
  modules, and their cost mix. With a trusted base, they report the exact
  policy-selected ordinary level, reasons, and the one `merge-gate` execution
  path; without a base, they report diagnostic advice only.
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
  it selects and runs either an impact-scoped recommended profile or the
  complete full gate. Default output gives a concise level/base summary;
  `--verbose` adds the detailed selection reasons, execution plan, progress
  events, timings, and report notes.

Without a base, the diagnostic planner advises full when the selection expands
to the whole repository, when at least 20 governed paths change, when at least
three modules change directly, or when dependency impact reaches at least two
thirds of a graph with three or more modules. It advises recommended for
narrower work. A deletion or a policy, dependency, workflow, container, ESLint,
Knip, Ruff, TypeScript, or OSV input expands selection to the whole repository.

## Optional adaptive merge gate

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

The names must be existing modules. An omitted `verification.mergeGate`
retains full-gate behavior. The list is an allowlist only: target configuration
cannot remove shared escalation rules. `verification.trustedMergeTarget` gives
agents checked-in merge-target guidance. Resolve an explicit task target first,
then this field, `origin/HEAD`, `origin/main`, and `origin/master`. Record the
selected source in the task handoff and fail on invalid configured guidance.

`merge-gate --base REF` derives the change from `merge-base(REF, HEAD)` plus
the current staged, unstaged, and untracked work. It exposes no `--files`,
`--module`, `--suite`, or caller-selected policy level. CI should supply a
trusted PR base SHA or push-before SHA; a missing, option-like, or invalid base
fails instead of guessing.

Recommended is selected only when every changed path maps to exactly one
allowlisted module and the existing impact planner recommends it. Full is
forced when:

- adaptive merge verification is not configured;
- a deletion or a shared policy-sensitive input expands selection to the whole
  repository;
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
from both ordinary levels. `test --supplemental` runs them as a separate local
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

## AI execution

An AI collaborator should treat the three scales differently:

1. During implementation, run exact named or module tests as useful.
2. While iterating, run `test --changed` for affected modules and reverse
   dependents when exact focused tests are no longer enough.
3. At an ordinary merge checkpoint, resolve a trusted merge target. Optionally
   run `test-levels --base MERGE_TARGET` (or the compatibility alias
   `test-plan`) for its read-only diagnostic output.
4. At a genuine merge checkpoint, run `merge-gate --base MERGE_TARGET` without
   asking the user to choose recommended or full. It subsumes changed-impact
   validation, so do not run `test --changed` immediately beforehand for the
   same candidate. Report only pass/fail and actionable findings to the user;
   detailed receipts remain in command logs or task artifacts.
5. Run `test --supplemental` after a green ordinary gate when the caller or
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
- Avoid tests that inspect private source spelling, extracted helper text, or
  implementation call order unless that order is itself the contract.
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
