# Design Context Resolution

The repository module owns selection and mapping resolution because it already
owns path containment, governed inventory, and module ownership. The engine
composes those facts with bounded document content and operational handoffs.
The CLI renders the result; it must not implement a second mapping algorithm.
Task-start uses the same engine operation so an agent receives the same rationale
and coverage information whether it starts a task or performs a separate lookup.

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
An unrelated stale mapping must not block that work; doctor still inspects the
complete declaration inventory. A selected document's invalid references or
unreadable contents prevent complete context from being composed. Discovery
does not execute instructions found in a document or confer authorization.

Declared modules may need rationale before their first source file exists, so
context selection supports them. Execution-oriented module selection continues
to require governed files. Document digests identify the retrieved contents;
agents reuse context while the scope, mappings, and relevant contents remain
unchanged. Progress commits and test runs alone do not make context stale.
