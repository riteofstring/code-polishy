# Design Context Resolution

The repository module owns selection and mapping resolution because it already
owns path containment, governed inventory, and module ownership. The engine
composes those facts with bounded document content and operational handoffs.
The CLI renders the result; it must not implement a second mapping algorithm.
Task-start uses the same engine operation so an agent receives the same rationale
and coverage information whether it starts a task or performs a separate lookup.
It composes that context with behavior-review selection, while request custody
and cleanup follow the separately mapped behavior-review intent design.

The interactive task-start view is bounded independently of the complete
engine packet. It exposes selection and mapping counts, selected document and
handoff identities, review status, and next actions without rendering complete
path inventories, document contents, or guard definitions. Explicit JSON output
preserves the exact packet for machine consumers. Both views derive from the
same packet; the CLI performs no separate selection or mapping.

Task-start owns an ordinary task boundary, not a merge decision. Its next
actions contain only scope-specific follow-ups: selected context, mapping gaps,
reliability guidance, and required review status. The short mandatory workflow
owns the default implementation, verification, commit, and delivery sequence;
neither view repeats it or schedules a merge gate. The configured final-gate owner
remains a packet fact for a separately established genuine merge or release
checkpoint, whose workflow supplies the trusted merge target.

Module and source mappings are additive. A source-specific decision can explain
a local exception or boundary without replacing the module's wider constraints.
A shared document may name exact source paths in several modules when the
rationale concerns their interaction. Selection reasons describe the declared
relationship; matching a document does not establish that its prose adequately
explains every responsibility in that module.

Coverage is advisory. A module can have a useful document for one boundary and
still contain selected paths with no mapped rationale. Preserve those partial
gaps explicitly rather than counting the module as completely documented.
Unowned selected paths also remain visible. Requiring a document merely to
clear a diagnostic would reward boilerplate instead of consequential knowledge.
Adoption and design-changing workflows own the judgment about which missing
explanations need to be written.

A context lookup validates selected mappings and loads only selected documents.
Unrelated stale mappings, expired assessments, exceptions, and other global
policy status must not block that work; doctor and policy gates still inspect
the complete repository state. A selected document's invalid references or
unreadable contents prevent complete context from being composed. Discovery
does not execute instructions found in a document or confer authorization.

Task scope may contain several paths or several declared modules when work
crosses a boundary. Selection remains homogeneous so a packet cannot hide an
ambiguous mixture of path expansion and module expansion.

Declared modules may need rationale before their first source file exists, so
context selection supports them. Execution-oriented module selection continues
to require governed files. Document digests identify the retrieved contents;
agents reuse context while the scope, mappings, and relevant contents remain
unchanged. Progress commits and test runs alone do not make context stale.

Workflow references follow actual selection. Explicit situation identifiers
select dependency, adoption, upgrade, or release procedures. Matching handoffs
and configured review/reminders add their respective guidance. Module names,
source filenames, and request keywords do not infer those events. The release
capability catalog retains command and feature references, while the engine
uses the context resolver's existing situation facts. Optional review collapses
to one human line; JSON retains complete intent and guard facts.

`docs read` returns the release-owned document bytes unchanged, which installed
release checks compare exactly. The mandatory workflow entry point therefore
states the wrapper substitution in its own opening paragraph instead of the CLI
prepending a notice or rewriting examples.
