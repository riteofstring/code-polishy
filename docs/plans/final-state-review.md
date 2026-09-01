# Final-State Review Plan

## Status

Implemented on `feature/final-state-review`. Dirty-state capture applies only to
later corrections; the original request, review preparation, finalization, and
gates still require their existing trusted clean boundaries. Hosted
cross-platform acceptance and multi-repository dogfood remain release evidence
for the experimental feature.

## Outcome

Help agents leave a repository in the clean final state the user asked for.

The review covers two related problems:

| Problem                 | What it looks like                                                                                                                                     |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Meta-note contamination | Durable text talks about the prompt, agent, task, PR, or editing process instead of the product or code.                                               |
| Correction residue      | Executable code still remembers an idea the user rejected, usually through a guard, flag, fallback, exclusion, wrapper, test, or configuration switch. |

The feature should catch these problems at a requested review, completed task
checkpoint, or merge. It must not add another AI call after every prompt or
edit.

## Examples

### Meta-note contamination

Bad documentation:

```markdown
For this task, we added a simpler installation flow.
```

Final-state documentation:

```markdown
The installer selects the release locked by the repository.
```

Bad source comment:

```go
// Added because the user asked the agent to retry failed requests.
```

Useful source comment, when repository policy allows comments:

```go
// Requests are retried only when the provider has not accepted them.
```

### Correction residue

The user asks for tomato soup. An agent adds broccoli. The user rejects it.

Bad final code:

```go
func tomatoSoup(ingredients []Ingredient) {
	for _, ingredient := range ingredients {
		if ingredient != Broccoli {
			add(ingredient)
		}
	}
}
```

The result contains no broccoli, but the implementation still carries the
mistake.

Better final code:

```go
func tomatoSoup() {
	add(Tomatoes)
	add(Stock)
}
```

A negative condition is valid when the final requirement is itself negative.
For example, an upload service may explicitly reject executable files. The
review must distinguish a real product rule from a leftover correction.

## Product decision

Extend the existing optional behavior-review workflow. Do not build a second
AI-review pipeline.

Every selected behavior review should use three lenses in one bounded review:

1. Did observable behavior change as requested?
2. Does changed prose describe the final state without narrating the coding
   session?
3. Does the implementation directly express the final intent without retaining
   rejected or superseded ideas?

Reuse the current `on-request`, `merge`, and `checkpoint` selection levels.
Keep `on-request` as the default. A repository that wants this review for every
non-documentation merge can continue to select full-candidate behavior review.

The existing commands remain the public workflow:

```sh
code-polishy behavior-review capture-intent --intent-file PATH
code-polishy behavior-review require --base REF --feature NAME...
code-polishy behavior-review status --base REF
code-polishy behavior-review prepare --base REF
code-polishy behavior-review finalize --base REF
```

No new top-level command is needed for the first release.

## What Code Polishy can guarantee

Code Polishy can deterministically guarantee that:

- the review was selected by exact policy or an exact task request;
- the reviewer received the exact ordered intent journal, selected policy,
  trusted base, candidate, patch, and mapped design context;
- every finding points to evidence in that packet;
- the result and any regression proofs belong to the exact candidate;
- unresolved final-state findings block checkpoint and merge gates;
- changing the candidate invalidates the old review;
- a skipped optional review is reported as `NOT RUN`.

Code Polishy cannot deterministically understand whether every condition is
needed. That judgment remains AI-assisted. The receipt proves what was reviewed
and enforced. It does not turn the semantic judgment into a deterministic fact.

## What ordinary checks already cover

Keep using deterministic checks for facts they can decide:

- `quality.allowComments: false` rejects prose comments and docstrings;
- formatters remove formatting drift;
- compilers and type checkers catch invalid code;
- dead-code and unused-code checks catch unreachable or unreferenced residue;
- complexity checks expose accumulated special cases;
- architecture checks reject forbidden dependency paths;
- tests prove declared observable behavior.

These checks do not reject a live condition such as
`ingredient != Broccoli`. The final-state review covers that gap.

Do not add a broad keyword blacklist for words such as `new`, `old`, `legacy`,
`temporary`, or `note`. Those words can describe real product concepts. A
keyword-only failure would create false positives and teach agents to rename
the residue instead of removing it.

## Capture the real conversation

### Current gap

The current intent workflow captures the original request from a clean task
base. That is enough for a one-shot task. It is not enough for correction
residue.

Corrections normally arrive after implementation has started, while the
worktree may be dirty. Requiring an artificial commit before every correction
would slow active development and encourage meaningless commits.

### Required capture contract

The harness must append every user instruction before the agent acts on that
instruction. This includes corrections such as:

- remove the broccoli;
- do not keep compatibility behavior;
- use the existing component instead;
- that was not requested;
- keep the old payment behavior unchanged.

Capture exact user text. Do not ask the primary agent to summarize it.

Evolve the append-only intent journal so an entry may be captured while the
candidate is dirty. Each entry should record:

- an opaque intent ID;
- the exact UTF-8 instruction bytes and their SHA-256 digest;
- the current committed `HEAD`;
- a deterministic digest of staged, unstaged, deleted, and untracked candidate
  state at capture time;
- the previous journal-entry digest;
- any exact feature names supplied by the harness.

The candidate-state digest is audit evidence. It does not prove that the
harness called capture before the agent changed more files. The harness remains
the trusted boundary for timing and authentic user text.

Use one atomic journal append under the existing interprocess lock. Reject a
capture if the candidate changes while Code Polishy is calculating its state
digest. Keep intent artifacts outside the Git candidate and use restrictive
file permissions.

Make this an atomic intent-journal schema cutover. Do not add compatibility
parsers for unpublished experimental journal versions.

## Build the review packet

`behavior-review prepare` should continue to create one bounded packet. Extend
that packet with final-state evidence.

The packet should contain:

- the exact base and candidate commits;
- the selected features and reasons;
- the ordered original request and every later instruction;
- task requirements and their selected intent IDs;
- the complete readable patch from the trusted base to the final candidate;
- stable IDs for changed patch hunks;
- bounded candidate source context around changed hunks;
- mapped current design documents;
- path roles such as production source, test, current-state documentation,
  product input, plan, changelog, generated file, and fixture;
- the canonical final-state review instructions;
- the expected result path and artifact version.

Parse and classify repository paths once. Pass typed packet data to rendering
and validation. Do not make the reviewer infer path ownership from file names
alone when Code Polishy already knows it.

Packet source context must remain bounded. Prefer the complete changed symbol
or a fixed number of surrounding lines. If Code Polishy cannot provide enough
context safely, the reviewer should return `unknown` rather than inspect the
workspace.

## Prose review rules

The reviewer should flag changed durable prose when it:

- addresses the current agent, prompt, task, PR, or editing session;
- explains that text or code was added, removed, simplified, or changed instead
  of describing the current state;
- records an apology, correction, debate, or rejected attempt;
- leaves temporary implementation instructions for the next agent;
- exposes internal implementation history in user-facing UI, errors, or logs;
- repeats planning or release history inside current-state documentation;
- explains obvious code mechanics without preserving useful rationale.

The reviewer should allow:

- accurate current behavior and user instructions;
- durable rationale that cannot be expressed clearly by code or types;
- explicit product limitations and safety warnings;
- history in changelogs, plans, release notes, and decision records;
- test fixtures that deliberately contain contaminated text as input data;
- machine-consumed source directives allowed by the comment policy.

Historical locations are context, not a blanket exclusion. A plan may discuss
implementation steps, but it should still avoid accidental prompt transcripts
or irrelevant agent narration.

## Executable-code review rules

The reviewer should flag a rejected or superseded idea when it remains only as:

- a negative condition or exclusion;
- a feature flag or configuration switch;
- a fallback or alternate path;
- a compatibility shim with no current contract;
- an adapter, wrapper, alias, or unused parameter;
- a denylist or allowlist entry created only to route around the mistake;
- a test whose only purpose is to preserve the rejected implementation idea;
- an identifier that gives the rejected idea a permanent place in the model;
- debug instrumentation or logging left from the correction;
- duplicated code that keeps both the accepted and rejected approaches alive.

The reviewer should allow such code when the final intent or mapped design
requires it. Common valid cases include:

- an explicit security rejection;
- validation of real external input;
- a current compatibility contract;
- a staged rollout the user requested;
- a domain model where the negative concept is real;
- a regression test for behavior the product must explicitly forbid.

The main question is:

> If the rejected attempt had never happened, would a clean implementation of
> the final request still contain this code?

If the answer is no, the reviewer should create a correction-residue finding.

## Structured findings

Replace free-form final-state findings with bounded structured records. Keep
observable behavior classifications and regression-proof IDs unchanged.

An illustrative result shape is:

```json
{
  "final_state_findings": [
    {
      "kind": "correction-residue",
      "path": "internal/soup/soup.go",
      "line": 42,
      "patch_hunk_sha256": "...",
      "intent_ids": ["intent-remove-broccoli"],
      "summary": "The recipe filters broccoli instead of removing it from recipe construction."
    }
  ]
}
```

Supported finding kinds should initially be closed to:

- `meta-note`;
- `correction-residue`;
- `unknown-final-state`.

Validation must prove that:

- the path and line belong to the candidate packet;
- the referenced patch hunk exists and its digest matches;
- every intent ID exists in the selected ordered journal;
- the finding kind is known;
- text fields are bounded UTF-8 without terminal control characters;
- duplicate findings are rejected;
- an empty finding set is represented explicitly.

An `unknown-final-state` finding blocks completion. The agent must improve the
packet context, narrow the requested review, or ask the user for a real product
decision. It must not guess.

## Review and fix workflow

1. The harness captures the original request before implementation.
2. The harness appends every correction before the next agent action.
3. The agent works normally and uses narrow tests only when useful.
4. Code Polishy decides whether review is optional, task-requested,
   merge-required, checkpoint-required, or full-candidate.
5. `prepare` creates one exact packet with behavior and final-state evidence.
6. One isolated reviewer examines behavior, prose, and executable residue.
7. Requested observable behavior receives the existing red/green regression
   proofs.
8. Final-state findings cite packet evidence and require no synthetic test.
9. `finalize` rejects unintended behavior, unknown behavior, missing proofs,
   and every final-state finding.
10. The primary agent removes the problem at its source and commits the fix.
11. Candidate changes invalidate the packet, proofs, result, and receipt.
12. The workflow prepares and reviews the new candidate again.

The reviewer never edits the repository. The primary agent owns the fix.

## Gate behavior and output

Do not run semantic review during ordinary active development. Run it only when
the existing behavior-review decision selects it.

Checkpoint and merge output should include one concise final-state line:

```text
FINAL STATE: NOT RUN
FINAL STATE: PASSED (checkout)
FINAL STATE: FAILED (2 findings)
```

On failure, print only actionable summaries and exact locations:

```text
FINAL STATE: FAILED
internal/soup/soup.go:42 correction residue: broccoli is filtered instead of removed at the source
README.md:18 meta note: describes the editing task instead of current behavior
```

Keep complete packets, results, and evidence in managed report artifacts. Quiet
output must not print the user's full prompt or large code excerpts.

An optional review must remain visible as `NOT RUN`, matching the disclosure
used for other optional hardening work.

## Agent guidance

Add two short rules to the generated agent guidance and versioned workflow
documentation:

> Write final-state artifacts. Do not leave notes about the prompt, agent,
> task, PR, rejected attempt, or editing process in source, documentation, logs,
> errors, or product copy.

> When the user rejects behavior, remove it at its source. Do not preserve the
> rejected idea in guards, exclusions, flags, fallbacks, tests, names,
> configuration, or compatibility paths unless the final requirement needs it.

These rules reduce contamination even when semantic review is optional and not
selected.

## Internal architecture

### `internal/finalstate`

Add a small domain package that owns:

- typed patch-hunk and source-context evidence;
- path-role inputs;
- structured finding types and validation;
- stable evidence digests;
- bounded result normalization.

It should not depend on CLI output, providers, process execution, or Git
commands.

### `internal/behaviorreview`

Keep ownership of:

- the append-only intent journal;
- packet preparation;
- reviewer instructions;
- behavior classifications and regression-proof binding;
- the combined review result and receipt.

Use `internal/finalstate` as a domain dependency rather than adding more
unrelated validation to the behavior-review package.

### `internal/repository`

Own the deterministic candidate-state snapshot used for correction capture.
Reuse the same staged, unstaged, deleted, and untracked semantics used by dirty
review selection. Read each external file once and reject candidate changes
during capture.

### `internal/engine`

Keep selection, gate integration, status reporting, invalidation, and failure
mapping in the engine. The engine must not parse reviewer prose.

### CLI

Keep current behavior-review commands. Render typed results. Do not add a
provider SDK or make the CLI launch an AI model.

## Test plan

### Intent capture

- original request captured at a clean task base;
- correction captured with staged changes;
- correction captured with unstaged changes;
- correction captured with deleted and untracked files;
- exact ordering across several corrections;
- concurrent append serialization;
- edited, removed, reordered, duplicated, or truncated entries rejected;
- candidate mutation during snapshot rejected;
- symlink, non-regular, oversized, invalid UTF-8, and control-character inputs
  rejected;
- report files never enter candidate selection.

### Meta-note review

- task narration in current-state Markdown fails;
- agent narration in a source comment fails when comments are allowed;
- process narration in an error, log, or product string fails;
- accurate current-state documentation passes;
- durable code rationale passes;
- changelog and plan history pass in their proper path roles;
- fixture text is not mistaken for shipped copy;
- the word `note`, `new`, `old`, or `legacy` alone never causes failure;
- a reviewer finding with a fabricated path or line is rejected.

### Correction-residue review

- the tomato-soup broccoli guard fails;
- removing broccoli from recipe construction passes;
- a rejected feature flag fails;
- an unnecessary fallback or compatibility wrapper fails;
- an explicit security deny rule passes;
- a real external-input validation rule passes;
- a requested staged rollout passes;
- a legitimate negative regression test passes;
- a test that merely preserves a rejected internal approach fails;
- insufficient context returns blocking `unknown-final-state`.

### Gate integration

- unconfigured review reports `NOT RUN` and does not launch a reviewer;
- a task-requested feature runs both final-state lenses;
- merge-required and checkpoint-required features enforce findings;
- full-candidate review covers documentation and source changes;
- documentation-only work stays cheap unless review is explicitly selected;
- a finding blocks finalization and both gates;
- a fix invalidates the old packet and receipt;
- a clean rereview passes;
- resume never reuses semantic review or final-state findings;
- quiet and verbose output remain bounded;
- managed artifacts remain ignored and contained.

### Installed release and platforms

- exercise original request, correction, failed residue review, clean fix, fresh
  review, checkpoint, and merge through an installed release;
- run the same workflow natively on Windows without WSL;
- test long paths, concurrent journal locks, interrupted writes, and artifact
  containment on Windows, macOS, and Linux;
- prove the installed reviewer template, docs, schema, and binary share one
  release identity.

## Security and privacy

User requests may contain private information. Intent artifacts must:

- stay below `.code-polishy-reports` with restrictive permissions;
- remain excluded from Git by the managed ignore rule;
- never appear in normal terminal output;
- have strict per-entry and total-journal size limits;
- be sent only to the explicitly selected reviewer provider by the harness;
- remain available for user-controlled deletion with the rest of the managed
  report artifacts.

Do not silently redact request text. Redaction can change intent and invalidate
the review. The harness must keep secrets out of prompts or use a provider and
retention policy acceptable to the user.

## Rollout

1. Add the two prevention rules to the next-release agent template and
   versioned workflow docs.
2. Cut over the intent journal to ordered dirty-state correction capture.
3. Add the final-state domain types, evidence IDs, and strict validation.
4. Extend packet preparation with path roles, patch-hunk IDs, and bounded source
   context.
5. Update the isolated reviewer instructions and result schema atomically.
6. Make finalization and gates block every structured final-state finding.
7. Add concise `NOT RUN`, `PASSED`, and `FAILED` reporting.
8. Add package, engine, CLI, installed-release, and native Windows coverage.
9. Dogfood the workflow on real corrections in at least two repositories.
10. Ship it with the behavior-review feature still marked experimental.

Dogfood evidence should record:

- how often the review catches real contamination;
- false positives, especially around valid negative requirements;
- contamination found later that the review missed;
- review and fix time;
- extra AI cost;
- whether the cited location and fix are clear;
- whether users disable the review because it is noisy.

## Acceptance criteria

The work is ready when:

- the original request and later corrections are captured exactly and in order;
- correction capture works safely during a dirty active-development state;
- the tomato-soup guard produces a correction-residue finding;
- a direct tomato-soup implementation passes;
- task narration in durable prose produces a meta-note finding;
- legitimate history, rationale, security rejection, validation, and
  compatibility contracts pass;
- no keyword alone can fail a candidate;
- review runs only when requested or configured at checkpoint or merge;
- optional skips are visible as `FINAL STATE: NOT RUN`;
- findings are candidate-bound, evidence-backed, bounded, and blocking;
- changing the candidate makes prior evidence stale;
- the complete installed workflow passes on Linux, macOS, and Windows;
- permanent policy and workflow docs replace this temporary plan.

## Explicit non-goals

- promising perfect semantic detection;
- running AI after every prompt or file save;
- automatically rewriting prose or code;
- banning all comments, negative conditions, feature flags, fallbacks, or
  compatibility code;
- failing on a list of suspicious words;
- replacing ordinary tests, architecture checks, regression proofs, or human
  review;
- giving Code Polishy direct access to a provider account;
- storing the parent agent conversation outside the exact captured intent
  journal.
