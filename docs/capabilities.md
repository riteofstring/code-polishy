# Repository Capability Discovery

Use the repository's exact locked release to discover the commands and
declarations available for the current repository:

```sh
code-polishy capabilities
code-polishy capabilities --format json
code-polishy capabilities --query "source validation" --format json
```

The inventory includes release commands, selected language packs and their
capabilities, project and module capabilities, configured checks, and behavior
review features. Each entry names its source, version or configuration pointer,
scope, enforcement boundary, workflow documents, and availability. A missing
pack or unavailable release catalog stays visible with a reason. A scoped
check with no matching current source is reported as inapplicable.

The shipped `docs/capabilities.json` catalog is bound to the exact release
identity through its manifest digest. Discovery reads only that release's
catalog. A different installed version cannot supply missing metadata, and a
catalog that does not match the lock is unavailable. Repository declarations
can still be inspected when release metadata is unavailable.

Queries return deterministic candidates from canonical names, aliases, and
descriptions. Exact normalized names and aliases rank first. Queries accept at
most 1,024 UTF-8 bytes and 16 terms and show at most 20 candidates, with the
complete matching count. An unfiltered inventory contains at most 2,048 entries;
its `capabilities/v1` JSON document is bounded to 4 MiB.

Discovery never activates a behavior feature. After the caller identifies a
feature, pass its exact canonical name or declared alias to a behavior-review
command. Alias identity uses Unicode NFKC normalization, case folding, and
whitespace normalization; captures record the canonical feature name. Partial
matches, descriptions, and query rankings grant no activation.

The command runs no configured checks, tests, repository commands, dependency
operations, or pack installation. It creates no intent journal or review
packet. Workflow references identify documents to read; they do not execute
their instructions. See [Agent Workflows](agent-workflows.md) and
[Behavior Review](policies/behavior-review.md) for the next selected operation.

## Inspect an upgrade

`upgrade plan` prepares a capability comparison without replacing the
repository lock. Its bounded output shows added, removed, and changed canonical
commands, their release versions, and workflow documents. Metadata ordering
alone does not count as a change. At most eight changes and two documents per
entry appear in the terminal; the machine document retains every change.

The candidate comes from a checksum-pinned publication index. Planning installs
and verifies one immutable current-host release before comparison, while the
incoming v2 lock records the complete published host set.

The comparison uses the incoming catalog and the exact outgoing release from
the same installation prefix. Both catalogs must authenticate against their
release locks. First adoption, an older release without authenticated catalog
metadata, a missing installation, or an invalid catalog produces an explicit
unavailable delta. The command never reconstructs capabilities from changelogs
or another installed version.

The `UPGRADE RECORD` path identifies a deterministic
`capability-upgrade-record/v1` JSON document under
`.code-polishy-reports/capability-upgrades/`, bounded to 8 MiB. Its `delta` field
is the same `capability-delta/v1` object returned as `upgradeDelta` by
`capabilities --format json`; human capability inspection displays the same
comparison. Each record captures the catalog bytes and release identity inputs
needed to verify the comparison even after removing the outgoing installation.
Adopting a scanner package preserves this comparison: the scanner envelope does
not change either release catalog. The stored record retains the original
upgrade inputs; concurrent-write protection still compares the complete lock.
These local records authenticate catalog content against captured release
digests; they do not authenticate who performed the upgrade or establish a
trusted upgrade history independently of the repository's local state.

The incoming planned lock selects its prepared record; after apply it becomes
the current record. Publication failure leaves the outgoing lock active,
concurrent writers cannot replace each other's records, and repeating `lock`
for the same published release preserves its publication metadata. Discovery is
read-only: a missing or damaged record reports an unavailable delta, and does
not reconstruct or repair upgrade history. Retain the managed record when
moving a repository that needs later upgrade inspection.

## Start a task

Retain known rationale and review requirements while release, policy, scope,
mappings and relevant documents remain unchanged. Ordinary work needs no task
initialization when this context is already available. For missing scoped context:

```sh
./code-polishyw design-context --module application
./code-polishyw design-context --new-path retrospective/README.md
./code-polishyw design-context --files frontend --new-path frontend/new.ts
```

Context discovery returns rationale and the scope's pre-edit review requirement
using the same policy selection as `task-start`. Select an explicit feature only
when requested by its canonical name or exact alias. Required review still needs
the exact request captured before editing:

```sh
./code-polishyw task-start --intent-file - --module application \
  --feature checkout --situation deployment
```

`task-start` is also available when its optional packet is useful. It accepts
existing files plus repeatable `--new-path` operands, or declared modules. Planned
paths are contained and resolved without creating them; `--files` remains strict.
Optional packets accept dirty checkouts and record HEAD without asserting that
uncommitted work was verified. Optional review rejects unused intent and creates
no journal. Required capture retains a clean initial boundary; later
artifact-affecting corrections use `behavior-review capture-intent`.

The human packet reports the task base, selection, review requirement, selected
rationale and handoffs, any selected reliability reminder and useful follow-ups.
Unmapped paths are facts, not an instruction to start a maintenance task. Inactive
guard and gate metadata stays in JSON. The ordinary implementation, verification,
commit and delivery sequence lives in the [workflow entry point](agent-workflows.md).

Explicit situations select additional procedures: `dependencies` selects the
supply-chain guide, `adoption` and `upgrade` select the adoption runbook, and
`release` selects the release checklist. Matching handoffs select their operating
guide; selected review and reliability reminders select their own policies.
A situation selects guidance, not a gate or an external operation. Filenames,
module names, and request keywords do not infer a lifecycle event.

Use `--format json` when automation needs the complete `task-start/v2` packet,
which remains bounded to 16 MiB. It contains the task base, locked release and
catalog identity, any capture identity and canonical features, requested and
expanded selection, current design documents, selected operational handoffs,
workflow references, any structured reliability reminder, configured
guards and verification requirements,
final-gate owner, and scoped next actions. Ordinary delivery follows the workflow
entry point. Task-start never selects a merge gate; that requires a separately established
genuine merge or release checkpoint. Optional tasks omit review-status actions.
Guard entries preserve capability discovery's availability and enforcement
facts; listing a guard does not execute or activate it. Document selection uses
the same context resolver, with `task-start` as its actual workflow situation.

Invalid selection, unknown feature operands, unavailable catalog evidence,
invalid selected documents, missing or unused intent, and oversized packets
create no capture. A candidate or intent journal that changes during preparation
prevents publication of the prepared entry. The command runs no tests, reviews,
dependency operations, or repository-controlled commands. Follow the packet's
next actions using the authoritative component commands and the locked
workflow's event rules.
