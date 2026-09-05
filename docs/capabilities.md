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

The exact incoming installed release's `lock` command prepares a capability
comparison before atomically replacing the repository lock. Its bounded output
shows added, removed, and changed canonical commands, their release versions,
and workflow documents. Metadata ordering alone does not count as a change.
At most eight changes and two documents per entry appear in the terminal;
the machine document retains every change.

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
These local records authenticate catalog content against captured release
digests; they do not authenticate who performed the upgrade or establish a
trusted upgrade history independently of the repository's local state.

The current lock selects its prepared record. Record publication failure leaves
the outgoing lock active, concurrent writers cannot replace each other's
records, and repeating `lock` for the same release preserves its original
comparison. Discovery is read-only: a missing or damaged record reports an
unavailable delta, and does not reconstruct or repair upgrade history. Retain
the managed record when moving a repository that needs later upgrade inspection.

## Start a task

When the request is ready to implement, supply its exact original text and one
file, directory, or module scope:

```sh
code-polishy task-start --intent-file /tmp/request.txt --files frontend
code-polishy task-start --intent-file /tmp/request.txt --module application \
  --feature checkout --situation deployment
```

`task-start` validates all supplied inputs and composes the complete packet
before publishing the same atomic intent capture used by `behavior-review
capture-intent`. The first capture requires a clean task base; later corrections
may be captured against a dirty candidate. Only explicit `--feature` operands
activate configured features. Request wording never selects them.

The command emits one `task-start/v1` JSON document, bounded to 16 MiB. It
contains the locked release and catalog identity, capture identity and canonical
features, requested and expanded selection, current design documents, selected
operational handoffs, workflow references, configured guards and verification
requirements, final-gate owner,
and ordered next actions. Guard entries preserve capability discovery's
availability and enforcement facts; listing a guard does not execute or
activate it. Document selection uses the same context resolver, with
`task-start` as its actual workflow situation.

Invalid selection, unknown feature operands, unavailable catalog evidence,
invalid selected documents, and oversized packets create no capture. A
candidate or intent journal that changes during preparation prevents publication
of the prepared entry. The command runs no tests, reviews, dependency operations,
or repository-controlled commands. Follow the packet's next actions using the
authoritative component commands and the locked workflow's event rules.
