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

## Release-owned framework contracts

Known public framework APIs need shared semantic models rather than a local
reachability declaration for each consumer. The framework resolver reuses the
bounded source trees and compact project binding resolver. It recognizes exact
pytest autouse decorators and module marks, SQLite connection row-factory
writes, and Hypothesis state-machine teardown overrides. It never imports or
executes target dependencies. Unknown frameworks continue to use the explicit
external-contract mechanism described above.

Positive evidence is a source location and symbol, not a global name whitelist.
Re-exports and inheritance must resolve to the named external API. Receiver
construction, parameter annotations, aliases, and active context bindings must
be unambiguous. Custom factories and rebindings cannot prove a standard SQLite
connection. When Vulture's line-based output cannot distinguish two same-named
writes, neither write is hidden by the built-in contract.

`framework_contracts` exports `framework_members` as its embedded Python API.
The Go-owned Vulture adapter calls that API with the already parsed project and
combines its exact locations with TypedDict, Pydantic, and configured contract
evidence. The existing coverage-failure boundary still withholds derivative
findings if complete project facts are unavailable.
