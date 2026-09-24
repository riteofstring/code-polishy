# End-to-End Reliability

Defensive machinery can look robust at one boundary while making the end-to-end
system harder to operate, resume, or repair. This is robustness theater: added
controls signal rigor but do not address a demonstrated failure or reduce
system-wide risk. Code Polishy provides a scoped advisory for repository areas
where reliability changes deserve that wider check.

The invariant is:

> Reliability machinery must address a demonstrated failure or requirement and
> improve the end-to-end system. Add or tighten limits, terminal states, retry
> ceilings, fail-closed gates, identity bindings, digests, receipts, or recovery
> machinery only after accounting for existing platform behavior and confirming
> that the change preserves valid results and ordinary recovery, reduces total
> complexity, and lowers total failure risk.

This is not a prohibition on bounded resources, integrity evidence, terminal
outcomes, or fail-closed security boundaries. Those controls are often required
for correctness, authorization, safety, or cost containment. The design must
make that requirement explicit and show that the complete system becomes
simpler or more reliable, including its valid results and ordinary recovery.

## Configure the reminder

Select reliability-sensitive modules, exact source paths, or both:

```json
{
  "quality": {
    "reliabilityReminder": {
      "modules": ["jobs", "state"],
      "sourcePaths": ["src/runtime/retry.ts"]
    }
  }
}
```

Module and path selectors are additive. Module matching uses direct ownership,
not transitive dependency impact. `sourcePaths` are exact, portable repository
paths and must belong to one declared module. Code Polishy does not infer a
match from filenames, request wording, source keywords, or an AI classification.

When a planned `task-start` selection matches, its human and JSON output include
the fixed reminder, matched selectors, policy path, and four questions. A
checkpoint or merge gate repeats the reminder when the actual candidate changes
a configured module or path. Unconfigured and unrelated scopes stay quiet.

## Apply the decision test

Before introducing or tightening a matching mechanism, answer:

1. What demonstrated failure or requirement does this machinery address?
2. What simpler solution or existing platform capability already addresses it?
3. What valid results or ordinary recovery paths could the change discard,
   invalidate, or complicate?
4. Across implementation, operation, and recovery, does the change reduce total
   complexity and lower total failure risk?

Evaluate the actual end-to-end path. A database transaction, scheduler,
provider, queue, test harness, or deployment platform may already own the
proposed guarantee. Duplicating it can create divergent state, another identity,
and a second recovery protocol. Prefer a direct correction to new lifecycle,
evidence, or recovery machinery. Conversely, a platform guarantee that does not
cover the demonstrated failure is not a reason to omit a necessary control.

Preserving accepted work is one part of this system-level test. For example, a
retry budget may pause a job while retaining its identity, completed outputs,
and resumable cursor. A terminal failure that discards those facts needs a
stronger requirement than the existence of a retry limit. The same scrutiny
applies when machinery adds coordination or failure modes without discarding
work directly.

## AI-agent workflow

The managed `AGENTS.md` carries the always-visible default. `task-start` is the
primary contextual surface because it already receives the agent's planned
files or modules and returns mapped design context. The agent applies the four
questions while choosing the implementation; it does not create a questionnaire
artifact or expose private reasoning.

If the change establishes or alters a non-local reliability or recovery
invariant, update the repository's mapped design document. Encode critical
behavior in an observable boundary test using temporary state. Report the
resulting behavior and verification concisely at delivery.

The reminder is advisory. It does not change command selection, exit status,
authorization, behavior-review requirements, receipts, or gate acceptance. A
repository may independently select behavior review for a feature, but this
reminder never activates it.
