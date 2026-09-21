# Analysis providers

Code Polishy resolves analysis ownership from the composed configuration, exact
installed pack manifests, selected paths, capability, and execution profile.
Language classification alone never establishes analyzer coverage. A selected pack
claims a language boundary within its command paths; each matching command supplies
one capability and profile inside that boundary. Missing, conflicting, or unavailable
capabilities fail closed and cannot enable a native fallback. Source outside every
selected pack boundary retains its existing route until the coordinated native-removal
cutover. Configured project commands retain their execution profiles; a build
command cannot establish structured source-analysis coverage.

Pack manifest version 3 and protocol version 4 form one breaking contract.
Version 2 manifests and protocol 3 messages are rejected without decoding or
translation. Every language declares file-scoped, static, or evaluated discovery;
source recognition comes only from its declared source patterns and canonical
shebang prefixes, and optional test patterns extend core test scheduling for that
language. Built-in language names do not acquire implicit patterns. Every command
names its exact languages and declares self-contained or host-toolchain
execution with explicit network authority. A response
accounts for every requested file exactly once as analyzed or unsupported, retains
a stable namespaced rule identifier, and returns facts needed for core decisions.
Unsupported work blocks a required capability. An operational failure establishes
neither facts nor coverage. Conformance requires a passing source fixture and a
seeded defect with an expected rule for every claimed capability.

The planned replacement contract is a hard release cutover. Its first public
engine accepts only the replacement manifest and protocol, removes native
language ownership in the same release, and rejects repositories without exact
selected packs. It provides no legacy decoder, translator, alias, native fallback,
dual execution, or automatic pack-pin migration. Migration is an explicit,
rollback-capable transaction completed before the engine-lock authority cutover.

Core first supplies a bounded authoritative inventory and the exact repository
selection. Inventory entries carry only generic path, language, module, context,
ownership, and source/metadata/dependency/asset/test/generated/data/development/
control classifications. For static discovery, the pack returns project scopes
with a private identity, language, root, members, entry files, context paths,
selected members, and bounded opaque JSON. Core validates every path against the
inventory, requires selected sources exactly once, and replaces private identities
with invocation-local scope handles. Ecosystem package, workspace, compiler, and
framework shapes never enter the core contract.

Capability requests separate required analysis targets (`files`), permitted
diagnostic paths (`diagnosticFiles`), and selected format writes (`writeFiles`).
They carry only validated scopes. A `scope.sourceContexts` mapping binds generated
source to a contained non-generated ecosystem context while preserving its physical
path for reads, findings, and write protection.

Discovery receives hashes for selected sources and relevant metadata, dependency,
and control inputs. Capability execution receives hashes only for diagnostic files,
scope context, controls, and validated symbolic asset links required by the
operation. Providers report every input they actually read. Core verifies initial
context and reported identities afterward; unrelated regular assets neither consume
context limits nor become mandatory analyzer inputs.

Coordinates are one-based UTF-8 byte positions in original source. Comment bytes
must match that position; truncated comments remain bounded facts marked
incomplete and cannot establish a permitted directive. Format writes require
valid UTF-8 in both original and replacement bytes. Core validates every target
before applying any edit. Generated source and declared data remain non-writable.

Focused type checking reports owned members of the discovered compilation scope.
Focused dead-code analysis retains inventory-wide package reachability, including
unchanged files and metadata-triggered work. Architecture starts only for selected
providers and follows connected discovered scopes. Core validates coverage against
that dependency closure and rejects unrelated findings. Graph evidence groups
ordinary sources by actual package root. Inherited generated files outside that
physical root retain a containing graph root; their effective ecosystem context is
part of the request evidence.

An unavailable selected pack remains a repository error. If its manifest can
still be authenticated independently against the selected receipt digest, core
retains its exact claims and blocks their native fallback. A wholly missing pack
has no trusted claim inventory: it cannot suppress unrelated native analysis or
make the failing run pass.

Official local installation starts from a bounded catalog authenticated by an
exact caller-supplied SHA-256 digest. Catalog version 1 binds each entry's exact
tree identity and byte size, one exact engine version and protocol, platforms,
discovery and execution modes, capabilities, tools, dependencies, licenses, and
provenance attestation. Local catalog URLs are contained `file:` paths; catalog,
artifact, and attestation links or escapes are rejected. Installation reads and
validates the complete artifact before atomically publishing the same in-memory
bytes to the immutable content-addressed store. It executes no pack code and
performs no network access.

Installation and repository selection remain separate. `pack update` installs
one explicit candidate and reports its authority and inventory without changing
policy. `pack remove` deletes only one unambiguous exact identity and never edits
a repository. `pack list` and `doctor --strict` use the same state model to report
installed, selected, missing, incompatible, and corrupt identities. Engine locks
continue to select only the engine; pack lifecycle commands never rewrite them or
pack pins.

Repository migration is one explicit hard-cutover transaction. `pack migration
plan` accepts the complete replacement set as repeated exact catalog selections,
installs and runs every declared fixture, inventories native, selected-pack,
generated-source, and custom-command claims, then evaluates the candidate policy
without changing repository selection. The versioned durable plan binds the
catalog, engine version, engine-lock bytes, before/after configuration bytes,
coverage ledger, and diagnostic snapshots. Missing replacement ownership or a
new error diagnostic blocks application.

`pack migration apply` reauthenticates the catalog and installed trees, rejects
stale repository or diagnostic evidence, and atomically replaces only the
configuration's complete `packs` value. The engine lock must match before and
after. `pack migration rollback` restores the exact preserved configuration only
while the applied bytes and engine lock still match the plan; it refuses to
overwrite later edits. Installed but unselected immutable packs may remain. No
step translates old manifests, merges implicit pins, invokes native fallback for
a missing replacement claim, or makes the outgoing engine understand the new
protocol.

A host toolchain declaration requests one or more exact policy-owned tool versions
and marks at most one as the adapter launcher. Core resolves every executable from
a verified installed release, checks its bytes against that release's manifest,
binds the ordered identities into analysis, and exposes each path through a
tool-ID-specific environment variable. It never resolves a tool from the target
project or ambient PATH. Providers carry their own pinned dependencies in the
integrity-checked installed pack tree.

The core interprets source-comment and function facts. JavaScript machine
directives use the existing policy grammar. Unsupported directive grammars do not
establish permission to include prose. Function complexity uses the language's
effective limit; new languages use the shared TypeScript limit until a distinct
language policy is justified. Depth and parameter limits retain the existing
production/test distinction. Generated source retains correctness checks while
remaining exempt from handwritten style limits.

Providers contribute imports to the common source graph. The core derives node
ownership and classification and retains dependency-direction and cycle checks.
Graph input records bind the installed provider, declared languages, protocol,
effective policy, inputs, toolchain, facts, and resolution. Native Python evidence
continues to require its existing project and analyzer contract. New graph
languages require provider evidence; merely naming a language does not validate it.

Framework interpretation belongs in separately installed providers. The optional
JS/TS provider reads existing package metadata and configuration as data and uses
sealed parsers and checkers. It does not execute target check scripts, compiler
plugins, or lint configurations. Framework-specific parsers and mappings stay
inside that provider. Local reports and diagnostics remain part of Code Polishy;
dashboards, remote telemetry, and aggregate analytics are outside its product scope.

Conformance context comes from the verified pack inventory beneath each declared
fixture project. It never depends on an enclosing checkout's tracked files or
ignore rules. Fixture language classification uses the same manifest declarations
as installed analysis. Operational fixture failures retain the analyzer's reason.

Function facts are interpreted once when the core constructs adapter findings.
Conformance uses that same interpretation: its expected status and rule include
the resulting core policy finding, even when the provider successfully returns
measurements without judging them. Provider rules retain their pack namespace;
core function rules retain their core identity. A fixture must detect its actual
seeded policy violation; a count of returned facts cannot substitute.

Managed JSON and SARIF reports retain provider rule namespaces and graph evidence.
Their schema accepts declared language and ecosystem identifiers and binds pack
facts to their version 3 protocol and exact provider identity. Native Python fact
variants retain their existing protocol contracts.

Provider planning and execution share preparation of the exact toolchain command,
pack root, profile, capability, selected files, and serialized request. Empty
selections produce no invocation. Architecture operations appear at their actual
execution phase, including test-ownership discovery. Persisted gate identities
bind the working root and request digest. Exact command enforcement remains in
both planning and artifact recording; mismatches expose field names without
printing request or environment values. Provider notes use existing informational
findings and do not turn a successful capability into a policy failure.

Explicit scope.data selections may protect .js and .mjs literal modules. The
native parser accepts a default literal export, a single const literal binding
followed by its default export, or a named exported const literal binding. Values
are finite numbers, strings, booleans, null, arrays, and plain object properties.
Imports, calls, getters, spreads, computed keys, prototype setters, duplicate keys,
other statements, and malformed syntax are rejected without evaluating source.
Validation runs during checks and before formatting; protected bytes are never
reformatted. Control inputs, executable modes, shebangs, excluded paths, and
generated-output overlaps cannot acquire data protection. Data retains module
ownership, dependency obligations, and input hashes while leaving executable
capability selections. Existing structured-data formats remain supported.
