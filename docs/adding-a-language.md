# Adding a language pack

A pack connects independently owned analyzers to Code Polishy's policy engine.
The engine resolves the exact installed pack, validates its output, and keeps
module boundaries, source classifications, comments, metrics, coverage, and gate
decisions authoritative. Framework adapters can share a language provider; a
framework does not require its own pack or a second project configuration fragment.

## Pack contract

A local pack contains `code-polishy-pack.json`, `README.md`, contained adapter
entries, pinned tools, and conformance projects. Manifest and protocol version 2
form one public contract. Use `schema/code-polishy-pack.schema.json` for the
manifest. Declare an exact version, supported platforms, languages and source
patterns, dependency manifests, command paths, capabilities, execution profiles,
timeouts, and permitted environment names.

Commands provide `format`, `lint`, `typecheck`, `complexity`, `dead-code`,
`architecture`, `build`, `dependency-policy`, `lock-sync`, `release-age`, or
`security`. An optional runtime reference requests an exact policy-owned tool:

```json
{
  "name": "analyze",
  "argv": ["bin/analyze.mjs"],
  "runtime": { "name": "node", "version": "24.18.0" },
  "capabilities": ["lint"],
  "profiles": ["check", "gate"],
  "timeoutSeconds": 60
}
```

Node is the first supported runtime reference. Its executable and SHA-256 identity
come from the verified engine installation. An ambient executable, target package,
version range, or missing runtime cannot substitute. Native contained executable
adapters can omit the runtime reference.

Each command/capability pair requires a passing fixture and a real seeded defect
returning `findings` with `expectedRules`. Operational failure does not count as
defect detection. Fixtures select nonempty, distinct source paths. The SQLite
syntax proof under `tools/fixtures/language-pack` demonstrates a non-native
language using a real parser; it deliberately supplies lint only.

## Requests and responses

The engine sends one bounded JSON request on standard input. Existing operation,
capability, project root, selected files, modules, mode, and profile fields are
joined by exact pack/runtime identity, full-versus-focused scope, effective policy,
source classifications, entry points, and hashed read context.

Selected files are diagnostic and potential format-write targets. Context supplies
necessary project information and never grants write authority. Record every
selected source and each additional dependency/configuration input in `inputs`,
using contained repository-relative paths and SHA-256 digests. The engine verifies
identities after execution, including import targets. Changed inputs invalidate
analysis.

Return exactly one JSON response with `protocolVersion: 2` and one status:

- `pass`: nonempty evidence, no findings, complete coverage.
- `findings`: at least one finding and explicit coverage.
- `incomplete`: unsupported paths with concrete reasons; required work blocks.
- `operational-failure`: a failure description, with no analysis coverage or facts.

For every selected path, `coverage.analyzed` or `coverage.unsupported` must account
for it exactly once. Both arrays are explicit, including when empty. Findings
include the requested capability, original path, stable `rule`, subject, and
message. Locations use one-based UTF-8 byte columns. The engine namespaces rules
as `pack.<name>.<rule>`; providers cannot claim core policy rule identities.

Architecture returns authored imports with original locations, resolved targets,
package identity when applicable, and runtime/type-only/re-export/proven-dynamic
edge kinds. Lint supplies complete lexical comment facts when comments are
forbidden. Complexity supplies function complexity, depth, and parameter counts.
Explicit empty fact collections distinguish an inspected file without those facts
from omitted evidence. The core derives ownership and classifications and applies
its existing dependency, cycle, directive, and metric policies.

A successful format-write response may include edits for distinct selected
editable files. The engine checks every edit before writing. Generated source,
declared data, and context-only paths cannot become write targets. Other operations
and unsuccessful responses cannot return edits. Providers must not write source
directly.

Requests and responses are bounded at 8 MiB. Additional limits bound file counts,
findings, fact collections, and text fields. Standard error is a bounded diagnostic
stream, not evidence of successful analysis. Unknown fields, extra JSON values,
escaping paths, missing identities, malformed facts, and contradictory success
claims are rejected.

## Ownership, verification, and installation

An explicitly selected pack owns each matching path/capability/profile it claims,
replacing the corresponding native route. Competing claims fail. Missing or failed
selected packs do not enable a hidden fallback. Unclaimed native syntax retains
its existing analyzer. Whole-program checks must own coherent compilation units;
partial handoffs cannot imply whole-project coverage. Ordinary configured commands
still run, but their successful exit does not establish structured source coverage.
Architecture providers run through the architecture command so graph policy is
always evaluated.

Obtain and review a pack through a trusted channel, then run:

```sh
code-polishy pack verify --source ./example-pack
code-polishy pack install --source ./example-pack
code-polishy pack root
```

Verification executes declared conformance fixtures. Installation executes no pack
code. It rejects links and special files, hashes the entire tree, and atomically
publishes an immutable receipt under the local pack store. The tree is bounded at
20,000 files, 128 MiB total, and 16 MiB per file.

Select the printed exact identity in the target's existing `.code-polishy.json`:

```json
{
  "packs": [
    {
      "name": "example-rust",
      "version": "1.0.0",
      "digest": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    }
  ]
}
```

The repository lock continues to select the engine release. A pack is unavailable
until that exact tree is installed on the current machine. Installed paths do not
belong in project policy. Default storage is
`${XDG_DATA_HOME:-$HOME/.local/share}/code-polishy/packs/` on Unix and
`%LOCALAPPDATA%\CodePolishy\packs\` on Windows.

Use valid source, seeded defects, excluded inputs, stricter policy, mapping errors,
and tool failures to test a provider. Then exercise its exact installed identity
in a disposable project through ordinary checks and gates. Unsupported cases stay
visible. Additional language implementations and distribution services are separate
work; an adapter does not need dashboards, telemetry, or an AI acceptance system.
