# Python Contract Evidence

The repository boundary owns dependency admission and installed-file custody.
It relates a direct requirement to one authoritative lock package, confines
reads to that project's environment, and captures the distribution's recorded
source bytes. The Python facts boundary owns syntax and semantic resolution.
It receives those bytes through the shared bounded parser and resolves compact
facts; it never imports the dependency. The engine owns acceptance and reuse
of the resulting verification state. Keeping these responsibilities separate
prevents a Python resolver from inventing dependency authority or a report
renderer from deciding reachability.

An external consumer preserves one local method only when two independently
resolved facts connect: the local implementation uses the named external
contract, and the admitted distribution defines the corresponding contract.
Naming a real base class cannot preserve an arbitrary method. Registrations
require an owned typed interface because an untyped registration call supplies
no independent evidence of which method the framework consumes. Decorators
bind the exact decorated method to an owned decorator function. Ambiguous
inheritance and unsupported expressions remain failures; guessing Python's
runtime behavior would turn configuration into a dead-code exemption.

One installed distribution can span many parser partitions. Re-exports and
inheritance resolve over their complete compact fact set rather than within
one transport batch. Runtime source takes precedence over a same-module stub
so a declaration in a stub cannot preserve a nonexistent runtime member.
Stub-only modules remain usable when there is no competing runtime source.
All captured sources contribute to identity, including sources that do not
ultimately supply the selected definition.

`METADATA`, `RECORD`, and an applicable Git `direct_url.json` establish current
installation consistency. They do not authenticate the installer or prove
that a locally rewritten source and record match a published artifact.
Dependency security and artifact provenance remain supply-chain concerns.
Containment, duplicate rejection, stable reads, and byte limits are enforced
before semantic analysis so malformed installation state cannot supply partial
positive evidence.

Ignored environment files can change while the Git candidate remains fixed.
Gate identity therefore includes the current admitted distribution snapshots.
The engine rechecks that state before returning reused acceptance and before
publishing a completed gate. Freshness reads group declarations by project and
distribution and hash their captured inputs without repeating semantic
analysis. Unrelated installed distributions and projects are outside that
read set; an empty external-consumer set performs no dependency scan.

The public declaration shapes and supported consumer syntax are documented in
[Code Quality](../policies/code-quality.md#python-ruff-vulture-and-ty).

## Repository-owned contracts and bundled integrations

`scope.pythonContracts` is repository authority for runtime consumption that
static inference cannot discover. A declaration identifies one Python project,
an exact type, decorator, module binding, or runtime entry point, and its
consumed members. Its required reason explains the runtime contract. These
records are reviewed source configuration and participate in gate identity.
They are not independently authenticated dependency evidence, and do not bypass
dependency admission or vulnerability checks. Existing external-consumer
contracts above remain available when installed-source evidence is wanted.

Astroid supplies class ancestry and nested object inference over the complete
contained project source snapshot. Its import resolver accepts only those
snapshots and synthetic external class anchors identified by bundled or
repository declarations. It does not load the target environment, execute
project imports, or enable dependency-specific Astroid transforms. An external
anchor expresses the declared contract; it does not claim to reconstruct an
unavailable library's implementation. Inference failure cannot establish use.

The contract interpreter matches exact source definitions. Type declarations
identify consumed methods, attributes, decorated methods, and optionally
annotated fields. Entry points name `module:attribute`, including nested instance
attributes, with optional consumed methods. Missing or ambiguous entry points
and declarations matching no source definitions produce explicit policy
findings. Unrelated same-named symbols remain subject to dead-code analysis.
Repositories must derive declarations from their real runtime interfaces,
not generate them mechanically from unused-code reports.

Pytest autouse fixtures and module marks are bundled declarations interpreted
by this same mechanism. Standard-library callbacks and SQLite construction
semantics remain release-owned. Third-party model and state-machine behavior
requires explicit declarations; adding another library does not require adding
branches to the analyzer.

A forward flow state tracks local and instance-attribute receivers. Assignments
inside `try` blocks can establish receiver evidence after `None` initialization.
Reassignment invalidates prior evidence; branch joins retain only bindings
proven on every incoming path. Unknown calls invalidate instance-attribute
bindings and names writable by closures. Unknown context managers, exception
handlers, and repeated loop bodies begin conservatively. Ambiguous same-line
writes receive no positive evidence.

The Go-owned Vulture adapter combines contract locations with TypedDict reads,
standard-library protocols, and explicit external-consumer evidence. Complete
source coverage remains mandatory. Invalid configuration is reported as a
contract problem, while failure to obtain the whole project fact set withholds
derivative findings.
