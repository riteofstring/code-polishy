# Python provider

The first-party `python` pack owns Python source recognition, static project
discovery, tool invocation, and Python-specific facts. Core retains governed
inventory, exact read and write authority, policy enforcement, tool
authentication, diagnostics, and coverage decisions.

Static discovery assigns selected source to its nearest governed
`pyproject.toml`. Nested projects remain independent, and a focused operation
materializes only the authenticated members and metadata of selected scopes.
The exact carried CPython and packaging parser derive the supported target from
`project.requires-python`, validate policy-owned Ruff settings, and bind root,
`src`, and in-tree backend source roots into opaque scope data. Invalid metadata
withholds only its project scope while unrelated projects remain analyzable.
Ruff and CPython run against the isolated tree. They cannot enumerate the target
repository or observe an ambient project environment.

Ruff format uses stdin and returns proposed edits; it never changes the
materialized source, the target repository, or a cache. Ruff lint runs with
`--no-cache` in both a sealed baseline lane and a target-configuration lane.
The adapter deduplicates their findings while preserving the release-owned
baseline. The carried CPython runtime tokenizes comments and resolves
docstrings without importing target modules. Ruff also supplies McCabe
measurements while CPython supplies matching function depth and parameter
facts for core-owned limits. `ty` checks every authenticated member of a
selected project scope and may report diagnostics on unchanged members.

Architecture analysis runs Ruff's isolated complete and runtime import graphs
over every authenticated member of the selected project scope. CPython supplies
the authored static import site and distinguishes imports guarded by a bound
`typing.TYPE_CHECKING` name and package re-exports. The adapter reconciles both
sources, resolves local modules against the most specific authenticated source
root, and returns runtime, type-only, and re-export facts. Unresolved relative or
project-local imports remain unresolved facts, and ambiguous module locations
are findings. Invalid module layouts invalidate their scope. Recognized calls
bound to `__import__`, `importlib.import_module`, or `pkgutil.resolve_name`
require an exact `python.computed-import` declaration. The adapter binds the
declaration to its project, importer module, source digest, callsite, and
authenticated inputs. Finite targets may come from an exact list, a strict
digest-bound JSON selection, or a validated PEP 621 entry-point group;
module-object registries retain only their module component. Every target must
stay in its declared namespace and resolve to exactly one governed project
module. Valid targets become `proven-dynamic` facts, while missing, stale,
escaping, ambiguous, or malformed evidence makes the importer incomplete. The
adapter does not guess from runtime strings. Core retains module ownership,
dependency direction, cycle, and coverage enforcement.

Vulture 2.16 scans every authenticated member of a selected project scope during
complete checks and gates. Target `noqa` suppression is disabled, while Vulture's
bundled import whitelists and release-owned standard-library callback protections
remain active. The adapter returns dead-code facts for core to interpret as the
existing `quality.deadCode` finding. A scope with PEP 621 entry points or a bound
runtime reachability declaration fails incomplete until those contracts move to
the pack; it never runs a reduced analysis and presents it as complete.

The executable quality slice claims format, lint, complexity, type checking,
plain complete-project dead code, static architecture facts, and finite
computed-import declarations for its recognized callsites. TypedDict and
framework retention, dynamic references, external attributes, in-tree build
backend hooks, cross-module loader-alias parity, runtime loaders, dependency
evidence, and project-environment resolution remain migration work and cannot be
inferred from these claims. The pack is not eligible for the ownership cutover
until every Python ledger row has executable evidence.
