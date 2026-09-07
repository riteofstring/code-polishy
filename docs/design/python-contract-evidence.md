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

Architecture resolution does not build the Python semantic model. The pinned
Ruff graph runs on the selected source paths with type-checking imports enabled
and disabled. Their validated difference preserves type-only edges while the Go
policy engine applies runtime cycle traversal only to executable dependency
relations. A focused check therefore scales with its selection. A full gate
selects the complete project and retains whole-project cycle coverage.
Repository declarations add source-bound dynamic edges and runtime boundaries
without causing unrelated files to enter a focused graph. TypedDict and
dead-code analysis keep their separate complete-project fact model because
those quality checks need cross-file semantics. They run only for explicit
complete selections and merge gates; focused checks defer that global
conclusion.

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

The Go-owned Vulture adapter combines contract locations with TypedDict schemas,
standard-library protocols, and explicit external-consumer evidence. Complete
source coverage remains mandatory. Invalid configuration is reported as a
contract problem, while failure to obtain the whole project fact set withholds
derivative findings.

A semantically resolved TypedDict is a structural mapping schema. Its declared
fields remain schema members even when consumers cross return, mixin,
serialization, or dynamic mapping boundaries that erase the exact receiver
type. Dead-code analysis therefore retains those field declarations while still
allowing the TypedDict class itself and ordinary annotated attributes to be
reported unused.

## Operator-controlled runtime loaders

`scope.pythonRuntimeLoaders` declares an open runtime import boundary. It is
separate from finite computed-import inventories and from dead-code retention.
The repository delegates unknown target selection to an operator and supplies
an exact source-bound loader, ASCII module/object grammar, and rejecting runtime
protocol declaration. The architecture check binds that declaration to its
current project, module, source digest, and source locations. It never imports
the selected module and does not run a project-wide Python parser to reinterpret
the reviewed declaration.

Acceptance produces an informational architecture coverage record that explicitly
states that unknown targets are not statically verified. A successful local
source graph proves only its represented edges; it cannot prove dependency
direction or cycle freedom for future operator-selected targets. The protocol
check runs after import, so neither that check nor syntax validation establishes
import safety or dependency admission. Supply-chain policy remains independent;
a package installed only on an operator's host is outside repository lock scans.

Known finite local targets belong in `scope.pythonComputedImports`; each becomes
an ordinary dependency edge and must satisfy module direction. Unknown targets
remain delegated by the loader declaration and do not become guessed local
edges. Existing `scope.pythonContracts` entry points express repository exports
that external hosts consume. Changed source digests, module identities, source
locations, and unsupported input grammars remain coverage errors.
