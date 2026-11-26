# Design Context Resolution

The repository module owns selection and mapping resolution because it already
owns path containment, governed inventory, and module ownership. The engine
composes those facts with bounded document content and operational handoffs.
The CLI renders the result; it must not implement a second mapping algorithm.
Context discovery reports policy and caller-selected review requirements without
capturing intent. Required review uses `behavior-review capture-intent`, whose request custody
and cleanup follow the mapped
behavior-review intent design. Context carries scoped rationale and handoffs;
the workflow owns implementation, verification and delivery.

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

Task scope may contain several existing paths and explicitly planned paths, or
several declared modules. `--files` remains strict; `--new-path` validates
containment, all existing parents, exclusions and an absent leaf without creating
anything. Planned paths participate in prospective ownership, rationale and review
selection but remain separate from expanded existing paths in the report.
They never imply analysis or trigger a fallback to repository-wide selection.

Missing context is retrieved before edits, then reused until its release,
policy, scope, mappings or relevant documents change. Lookup accepts ordinary
dirty work without hashing candidate state or writing a request journal. Agents
record Git HEAD separately when a task base is needed. Required review alone
needs a clean initial request boundary; an invalid boundary cannot be repaired
by mislabeling it a correction.

Declared modules may need rationale before their first source file exists, so
context selection supports them. Review impact includes explicitly selected
modules and their transitive reverse dependents alongside file-derived owners,
even when those modules contain no files. Execution-oriented module selection
continues to require governed files. Document digests identify the retrieved contents;
agents reuse context while the scope, mappings, and relevant contents remain
unchanged. Progress commits and test runs alone do not make context stale.

Operational handoffs follow actual selection. Explicit situation identifiers
match declared handoffs; the agent workflow identifies release-owned procedures
for dependency, adoption, upgrade or release work. Module names, source filenames
and request keywords do not infer those events. The capability catalog retains
command and feature references. Human context output states the review
requirement; JSON retains selected features, selection reasons and document
contents.

A single complete `docs read` returns release-owned bytes unchanged. Batched
reads resolve every topic through the authenticated catalogue before writing,
preserve requested order and deduplicate aliases by topic identity. Exact
catalogued paths are accepted without admitting arbitrary filesystem reads.
One-topic section reads return the exact ATX heading and its subsections, ignoring
fenced code headings. Heading ancestry disambiguates repeated names; errors offer
concrete pinned-wrapper reads. Multi-topic and section output identify source
paths. The wrapper performs its normal integrity verification once for the whole
request; no daemon, persistent read ledger or launcher bypass is needed.
