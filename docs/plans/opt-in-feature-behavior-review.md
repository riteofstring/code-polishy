# Opt-In Feature Behavior Review Plan

## Outcome

Make AI-assisted behavior regression review optional by default and explicit
when used. A user or repository can request review for one or more named
features, require a feature review at merge, require it at every completed
checkpoint, or retain the current strict behavior for every code change.

Normal Code Polishy checks remain deterministic and do not require an AI
reviewer. When behavior review is selected, Code Polishy prepares and validates
the evidence but does not call an AI provider. The coding harness launches a
fresh reviewer and returns its structured result.

Every checkpoint and merge result must say whether behavior regression review
passed, failed, is required, or was not run. An optional review that was not run
does not block. A configured or user-requested review becomes mandatory for the
bound task and cannot be silently removed.

## Product contract

### Review levels

The system has three persistent levels:

| Level        | Behavior                                                                                                 |
| ------------ | -------------------------------------------------------------------------------------------------------- |
| `on-request` | Default. No AI review runs automatically. A user may request one or more configured features.            |
| `merge`      | Review affected features once against the final merge base.                                              |
| `checkpoint` | Review affected features at every completed committed checkpoint and again against the final merge base. |

These are committed-task boundaries. Code Polishy does not review after every
edit, test invocation, build, or application run.

The levels form a strict order:

```text
on-request < merge < checkpoint
```

A feature may strengthen the repository default. It may not weaken it. A
task-level user request may add features to review. It may not disable a
checked-in requirement.

### Required status reporting

`checkpoint-gate`, `merge-gate`, and base-aware planning output must always
include one concise behavior-review status:

```text
BEHAVIOR REVIEW: NOT RUN (optional)
BEHAVIOR REVIEW: REQUIRED (checkout, authentication)
BEHAVIOR REVIEW: PASSED (checkout, authentication)
BEHAVIOR REVIEW: FAILED (checkout)
```

The JSON gate report records the same state, selected features, selection
reasons, required boundary, review ID when present, and receipt path when
present. `NOT RUN` is a successful informational state only when no repository
rule or task request requires review. `REQUIRED` without a current receipt and
`FAILED` both block the gate.

This mirrors the visibility of optional supplemental testing. It does not imply
that behavior review is a deterministic test or that it ran as part of the
ordinary suite profile.

## Checked-in configuration

Add an optional `verification.behaviorReview` object to
`.code-polishy.json`:

```json
{
  "verification": {
    "behaviorReview": {
      "defaultRequiredAt": "on-request",
      "features": [
        {
          "name": "checkout",
          "modules": ["checkout", "payments"],
          "paths": ["web/checkout/**"],
          "suites": ["checkout-contract"],
          "requiredAt": "merge"
        },
        {
          "name": "authentication",
          "modules": ["auth"],
          "suites": ["auth-contract"],
          "requiredAt": "checkpoint"
        },
        {
          "name": "search",
          "modules": ["search"],
          "suites": ["search-unit"]
        }
      ]
    }
  }
}
```

`search` inherits `on-request` and never runs automatically. `checkout` is
required when its scope is affected at merge. `authentication` is required at
each checkpoint and at merge.

To preserve the current review-everything behavior, set:

```json
{
  "verification": {
    "behaviorReview": {
      "defaultRequiredAt": "merge",
      "features": []
    }
  }
}
```

This requires a full-candidate behavior review for every non-documentation
merge candidate. Using `checkpoint` instead also requires the review at every
changed checkpoint. A repository may still declare named features for clearer
reports and feature-specific suites.

### Feature fields

- `name` is a unique lowercase Code Polishy identifier.
- `modules` references existing project modules.
- `paths` uses the repository's existing contained path-pattern rules.
- At least one module or path is required so the feature has an exact scope.
- `suites` references one or more existing ordinary test suites. Commands stay
  owned by `tests.suites`; feature configuration does not duplicate them.
- `requiredAt` is optional and accepts `merge` or `checkpoint`. An omitted value
  inherits `defaultRequiredAt`.

Feature suites must be ordinary, non-live suites that can run in disposable
worktrees without credentials or declared secret environment. Supplemental,
mutation, live, destructive, and credentialed suites remain outside behavior
receipt evidence.

When feature scopes overlap, Code Polishy selects every matching feature and
deduplicates identical suite executions. A changed module also affects features
whose modules depend on it through the existing module graph. Explicitly
requested features are selected even when the current path mapping finds no
direct match.

Keep configuration version 3. This is an optional additive field, and an
existing version 3 configuration keeps its current meaning: no behavior review
is required. The repository lock, schema, templates, generated guidance, and
release must still adopt the field atomically because older releases reject
unknown configuration.

## One-time user requests

Persistent rules live only in `.code-polishy.json`. A one-time user request is
stored in the managed behavior-review journal and applies only to the bound
task.

At task start, the harness may select features while preserving the exact user
request:

```sh
code-polishy behavior-review capture-intent \
  --intent-file PATH \
  --feature checkout \
  --feature search
```

If the user asks for more coverage later, add it without rewriting the original
request:

```sh
code-polishy behavior-review require \
  --base TASK_BASE \
  --feature authentication
```

`require` needs a clean committed candidate, an intent captured at the task
base, and one or more configured feature names. It appends an additive
requirement record tied to the selected intent entries and task base. Repeating
the command unions features. There is no command that removes a task-level
requirement.

This permits review to be requested after implementation while preserving the
original pre-code request. If no intent was captured before implementation,
Code Polishy must say that an intent-bound review is unavailable. It must not
accept a new paraphrase as the original request. A separate advisory diff review
without an intent receipt is outside this gate contract.

Add a read-only diagnostic command:

```sh
code-polishy behavior-review status --base TASK_BASE
```

It reports affected, configured, task-requested, required, completed, and
missing feature reviews without running tests, creating a packet, or invoking
an AI reviewer.

## Review workflow

### No review selected

1. The user makes an ordinary request.
2. The harness captures the request at a clean task boundary when available.
3. The agent implements and commits the change.
4. Checkpoint and merge planning find no persistent or task-level review
   requirement.
5. The ordinary Code Polishy gate runs and reports
   `BEHAVIOR REVIEW: NOT RUN (optional)`.

No packet, AI result, regression proof, or behavior receipt is required.

### Review selected

1. Resolve the exact base and clean candidate.
2. Select the union of:
   - features explicitly requested for the task;
   - affected features required by candidate configuration;
   - affected features required by base configuration; and
   - the full candidate when the repository default requires all changes.
3. Run `behavior-review prepare --base REF`. The packet includes the selected
   feature definitions, why each feature was selected, the original captured
   requests, full exact patch, and mapped design documents.
4. The coding harness launches a fresh AI reviewer with only the packet. Code
   Polishy does not select a provider, hold credentials, or make a network
   request.
5. The reviewer classifies observable behavior for the selected scope as
   `requested`, `preserved`, `unintended`, or `unknown`.
6. Every `requested` behavior receives one or more red-before/green-after
   proofs using suites allowed by its feature definition. `unintended`,
   `unknown`, unresolved findings, or missing proofs block finalization.
7. `behavior-review finalize --base REF` writes a receipt bound to the exact
   base, candidate, configuration, selected features, captured intent, packet,
   reviewer result, and proofs.
8. The checkpoint or merge gate independently validates the receipt, replays
   the existing red/green proofs, forces the selected feature suites into the
   candidate test plan, and then runs the remaining ordinary gate.

Keep the current proof creation and gate replay model in this cutover. Review
frequency is the primary cost reduction. Proof replay remains the deterministic
check that the recorded evidence still fails on the pre-fix code and passes on
the exact candidate. Optimize duplicate execution only after real timing data
shows it is necessary.

### Fixing a detected regression

An `unintended` or `unknown` behavior never becomes a warning-only pass when
review was requested or required:

1. Finalization or the gate blocks.
2. The coding agent fixes the candidate.
3. The fix is committed.
4. The changed candidate invalidates the old packet, result, proofs, and
   receipt.
5. The selected review runs again against the fixed candidate.

Code Polishy does not automatically fix regressions.

## Deterministic boundary

Code Polishy deterministically owns:

- feature and boundary selection from validated configuration and exact Git
  state;
- additive task-request records;
- base, candidate, configuration, intent, packet, result, proof, and receipt
  binding;
- strict result schemas and allowed classifications;
- red/green command execution and replay;
- required, passed, failed, and not-run gate decisions; and
- refusal of stale, missing, weakened, or malformed evidence.

The AI reviewer owns the semantic judgment about what behavior changed and
whether it appears requested. That judgment is non-deterministic and can miss a
regression. Product copy and documentation must describe this as deterministic
enforcement of an AI-assisted behavior review, not deterministic regression
detection.

The coding harness owns the authentic user request, capture timing, clean
reviewer context, and AI invocation. Code Polishy continues to authenticate
none of those provider facts by itself.

## Policy selection and anti-bypass rules

Create one typed behavior-review decision before gate commands are planned. It
contains the exact base and candidate, status, boundary, selected features,
selection reasons, required suites, and whether a full-candidate review is
required. The terminal renderer, JSON report, packet preparation, receipt
validation, and command plan consume this one decision.

Selection must follow these rules:

- No configuration and no task request means optional and not run.
- A task request only adds selected features.
- `checkpoint` implies both checkpoint and final merge review.
- A merge-only feature does not require a checkpoint receipt.
- At final merge, feature impact is calculated against the real merge target,
  not only the latest checkpoint.
- Base and candidate configurations are both evaluated. Requirements are
  unioned so a candidate cannot disable the rule that must review itself.
- A `.code-polishy.json` change remains a full-gate policy-sensitive change.
- Unknown features, missing modules, invalid patterns, or unsuitable suites are
  configuration failures, never silent skips.
- A stale or incomplete selected review blocks before expensive ordinary gate
  commands run.
- Resume never reuses behavior-review proof commands. Existing exact-identity
  rules for ordinary suite reuse remain unchanged.

Ordinary Markdown-only candidates retain the documentation gate when no
feature review was explicitly selected. A task-requested feature review or a
configured feature that deliberately includes a Markdown product input may
make review required.

## Architecture and implementation ownership

### `internal/policy`

- Add `BehaviorReviewPolicy` and `BehaviorReviewFeature` to `Verification`.
- Apply `defaultRequiredAt: on-request` when the object is present without a
  value.
- Validate levels, unique feature names, module references, contained path
  patterns, suite references, suite eligibility, and monotonic feature
  requirements.
- Update the strict JSON schema and schema-drift tests.
- Replace the test that currently rejects all `behaviorReview` configuration
  with positive and negative contract coverage.

### `internal/repository`

- Reuse the existing candidate path ownership, deletion handling, module graph,
  and reverse-dependent impact primitives.
- Expose only the exact matching facts needed by behavior-review selection.
- Keep policy meaning outside the repository layer.

### `internal/behaviorreview`

- Add typed feature selections and additive requirement records to the managed
  journal boundary.
- Extend capture, packet, result, proof, and receipt schemas with versioned
  feature identities and selection digests.
- Require each reviewed behavior to name one or more selected features, or the
  explicit full-candidate scope.
- Restrict proof suites to the selected feature's configured suites.
- Preserve current containment, size, UTF-8, permission, atomic-write, digest
  chain, and stale-artifact rules.

### `internal/engine`

- Build the one authoritative behavior-review decision from task requests,
  base configuration, candidate configuration, and candidate impact.
- Skip receipt loading and proof replay when the decision is optional and no
  review was requested.
- Fail before ordinary commands when review is required but incomplete.
- Force selected feature suites into checkpoint or merge execution and
  deduplicate commands already selected by the ordinary test plan.
- Attach the structured behavior-review status to every gate report.

### `internal/gaterun` and reporting

- Add the selected behavior-review status and feature list to versioned gate
  reports and receipts.
- Render exactly one concise human status line.
- Preserve command-level proof logs and existing failure tails.
- Treat missing required evidence as a policy finding and artifact I/O or
  cleanup failure as an operational error.

### `cmd/code-polishy`

- Accept repeated `--feature` on `capture-intent`.
- Add `behavior-review require --base REF --feature NAME...`.
- Add read-only `behavior-review status --base REF`.
- Keep `prepare`, `finalize`, and `regression-proof` strict and provider-neutral.
- Update help, examples, exit behavior, and concise status rendering.

### Documentation and agent surfaces

- Update the behavior-review policy, verification policy, agent workflow,
  adoption guide, architecture, release checklist, README, Polishy skill, and
  generated guidance from one authoritative workflow.
- State that intent capture is cheap and performs no AI review or tests.
- Teach agents to honor configured requirements and explicit user requests
  without inferring feature names from prose.
- State `NOT RUN` when optional review was skipped.
- Keep mutation and behavior-review terminology separate.

## Verification plan

### Policy and matching

- omitted policy defaults to no review;
- explicit `on-request`, `merge`, and `checkpoint` behavior;
- unique names and valid module, path, and suite references;
- missing scope, empty suites, invalid patterns, and ineligible suite rejection;
- overlapping features and command deduplication;
- direct path, deleted path, module, and reverse-dependent feature impact;
- a feature strengthening, but never weakening, the repository default;
- base/candidate union preventing same-change policy removal from bypassing a
  review; and
- strict default reproducing the current every-code-change requirement.

### Task requests

- feature selection during initial intent capture;
- later additive selection against the same task base;
- multiple additions producing a stable union;
- unknown feature, missing pre-code intent, unrelated base, dirty candidate,
  reordered record, edited record, and broken journal rejection;
- no removal or replacement operation; and
- multi-task branches retaining each task's selected features at final merge.

### Review artifacts and proofs

- packet includes exact feature definitions and reasons;
- result behavior references only selected features;
- requested behavior cites proofs from eligible feature suites;
- preserved behavior requires no red/green proof;
- unintended and unknown behavior block;
- configuration, feature scope, task request, candidate, or proof changes stale
  the receipt;
- gate replay still proves red on the pre-fix code and green on the candidate;
  and
- selected candidate suites run once even when several features reference them.

### Gate behavior and reporting

- optional review missing: gate continues and reports `NOT RUN`;
- user-requested review missing: gate blocks and names the features;
- merge feature at checkpoint: checkpoint continues and reports optional;
- merge feature at merge: receipt and replay required;
- checkpoint feature: both checkpoint and merge require review;
- strict repository default: every non-documentation candidate requires review;
- passed and failed status in human and JSON output;
- documentation behavior with and without explicit feature selection;
- resume does not reuse proof replay; and
- ordinary checks, tests, builds, and supply-chain selection remain unchanged
  when review is optional and not run.

### Installed release and platforms

- Extend the installed-release fixture with optional, task-requested,
  feature-required, checkpoint-required, and strict-all repositories.
- Exercise request capture, later feature addition, clean review, regression
  fix, proof replay, finalization, checkpoint, and merge through only the
  installed launcher.
- Run equivalent native Windows coverage with PowerShell and Windows paths.
- Confirm a repository without behavior-review configuration never needs an AI
  artifact and retains its ordinary gate runtime.

### Dogfood

Measure at least these real workflows across more than one repository:

- routine refactor with `NOT RUN` and no added latency;
- one user-requested feature review;
- one merge-required sensitive feature;
- one checkpoint-required long-running agent task;
- one strict full-candidate repository;
- one unintended behavior that is fixed and reviewed again; and
- one late user request that succeeds because intent was captured before code.

Record review completion rate, wall time, proof execution time, corrective
attempts, reviewer disagreements, false alarms, missed regressions found later,
and whether status messages told the operator what to do next. Keep the feature
experimental until real dogfood shows it catches useful problems beyond
ordinary tests often enough to justify its cost.

## Delivery sequence

1. Commit this plan without changing runtime behavior.
2. Add policy types, schema, validation, feature matching, and exact tests.
3. Add additive task feature requests and versioned artifact bindings.
4. Add the authoritative engine decision and optional/default gate behavior.
5. Add feature suite forcing, command deduplication, and structured reporting.
6. Add CLI options, `require`, `status`, help, and contract tests.
7. Update permanent docs, skill, templates, generated guidance, and release
   surfaces together.
8. Extend Unix installed-release, Windows runtime, and dogfood coverage.
9. Install the exact candidate, run changed tests, run the final merge gate,
   push the exact commit, and require hosted CI before release.
10. Update `VERSION`, the release lock, changelog, and tag only during the final
    coordinated release cutover.

## Explicit non-goals

- Code Polishy directly calling an AI provider;
- automatic provider, model, or API-key configuration;
- claiming deterministic semantic regression detection;
- guessing feature names or review requirements from natural-language keywords;
- automatically fixing a detected regression;
- reviewing after every edit, test, build, or application run;
- making optional behavior review part of ordinary unit, integration, or
  mutation profiles;
- using supplemental, live, destructive, or credentialed suites as receipt
  proofs; and
- adding visual-review evidence in this cutover.

## Completion criteria

The simplified system is complete when:

- repositories with no behavior-review configuration run exactly their normal
  deterministic gates and clearly report that optional review was not run;
- a user can add one or more configured features to a task before or after
  implementation when the original intent was captured at the task base;
- repositories can require selected features at merge or checkpoint;
- repositories can retain strict review for every code change;
- required review cannot be removed through task flags, artifact edits, or a
  same-candidate policy weakening;
- every passed review remains bound to exact intent, code, configuration,
  features, proofs, and gate replay;
- the AI provider boundary remains outside Code Polishy; and
- installed Unix, native Windows, full local gates, and hosted CI all pass on
  the exact release candidate.
