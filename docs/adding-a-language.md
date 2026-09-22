# Adding a Language Pack

A pack connects independently owned analyzers to Code Polishy's policy engine.
The engine resolves the exact installed pack, validates its output, and keeps
module boundaries, source classifications, comments, metrics, coverage, and gate
decisions authoritative. Framework adapters can share a language provider; a
framework does not require its own pack or a second project configuration fragment.

## Pack contract

A local pack contains `code-polishy-pack.json`, `README.md`, contained adapter
entries, declared tools, and conformance projects. Manifest version 3 and protocol
version 4 form one breaking contract; version 2 manifests and protocol 3 messages
are rejected rather than translated. Use
`schema/code-polishy-pack.schema.json` for the manifest,
`schema/code-polishy-pack-request-v4.schema.json` for requests, and
`schema/code-polishy-pack-response-v4.schema.json` for responses. Executable
request and response examples live under `tools/fixtures/language-pack/examples`.
The complete `tools/fixtures/language-pack` proof pack ships with every release,
so authors can run and modify the same verified example without a source checkout.
Declare an exact pack version, the one exact `engineVersion` that may load it,
supported platforms, languages with source patterns and/or exact shebang prefixes,
optional language-specific test patterns and explicit capability absences, one discovery mode, dependency and
metadata patterns, command languages,
capabilities, execution profiles and type, timeouts, network authority, and
permitted environment names. `file-scoped` discovery accepts no metadata patterns;
`static` and `evaluated` discovery require explicit metadata patterns.

Commands provide `format`, `lint`, `typecheck`, `complexity`, `dead-code`,
`architecture`, `build`, `dependency-policy`, `lock-sync`, `release-age`, or
`security`. A host toolchain names every exact policy-owned tool it needs and
marks at most one as the adapter launcher:

```json
{
  "name": "analyze",
  "argv": ["bin/analyze.mjs"],
  "languages": ["typescript"],
  "execution": {
    "type": "host-toolchain",
    "network": "none",
    "tools": [
      { "id": "node", "name": "node", "version": "24.18.0", "launcher": true }
    ]
  },
  "capabilities": ["lint"],
  "profiles": ["check", "gate"],
  "timeoutSeconds": 60
}
```

Every command declares `self-contained` or `host-toolchain` execution and currently
declares `network: none`. A self-contained command omits `tools`; a host-toolchain
command requires one or more exact tool identities. Tool executables and SHA-256
identities come from the verified engine installation's governed inventory. The
engine exposes each path as `CODE_POLISHY_TOOL_<ID>` and prepends the one declared
launcher to the adapter command. An ambient executable, target package, version
range, or missing tool cannot substitute. Native contained executable adapters
use self-contained execution. A pack with subordinate runtime binaries lists
their exact paths in top-level `executables`; installation rejects missing paths
and binds executable mode into the immutable receipt.

Each command/capability pair requires a passing fixture and a real seeded defect
producing `findings` with `expectedRules` after core policy evaluation. Function
metrics use the same core thresholds and rule identities during normal analysis
and conformance; their provider response may contain measurements without any
provider finding. Expected rules name either a provider rule or the exact core
function rule. Operational failure does not count as
defect detection. Fixtures select nonempty, distinct source paths. The SQLite
syntax proof under `tools/fixtures/language-pack` demonstrates a non-native
language using a real parser; it deliberately supplies lint only.

## Requests and responses

The engine sends bounded JSON requests on standard input. Every request carries
the exact pack/toolchain identity, operation, capability, project root, repository
selection, mode, profile, modules, effective policy, governed inventory, and hashed
read context. Inventory entries expose generic path, language, module, context,
provider ownership, and source/metadata/dependency/asset/test/generated/data/
development/control classifications.

For `static` or `evaluated` discovery, the first request has `operation: discover`.
Return one or more discovered scopes with a private `id`, declared language,
contained root, provider-owned source members, member entry files, contained
context paths, selected members, and bounded non-null JSON `data`. Every requested
source must appear in `selected` exactly once. Discovery cannot return findings,
coverage, facts, edits, or scope handles. Core validates all paths against its
inventory, verifies reported input identities, and replaces private IDs with
ordered invocation-local handles. File-scoped commands receive engine-created
scopes directly.

The capability request's `files` require explicit coverage. `diagnosticFiles`
alone permit source findings and additional coverage; `writeFiles` alone authorize
selected format edits. Validated `scopes` carry handles, languages, roots, members,
entry files, context paths, and the pack's canonical opaque data. Each policy source
carries its scope handles, provider owner, physical path, generic source context,
language, and classifications. Context never grants diagnostic or write authority.
Return the ordered requested handles in `scopeHandles`. Record every selected source
and each additional dependency or configuration read in `inputs`, using contained
repository-relative paths and SHA-256 digests. The engine verifies identities after
execution, including import targets. Changed inputs invalidate analysis.

`policy.declarations` is always an explicit array. Each item has a namespaced
`kind`, positive contract `version`, one or more invocation `scopes`, exact
governed `inputs`, and bounded object `data`. Declaration inputs add only exact
non-source paths contained by a bound scope or source paths already in its members;
they grant read context, never findings or writes. Core includes only declarations
selected for the exact pack, capability, and validated scopes. The owning pack
interprets the versioned data; core treats it as opaque. An author must reject an
unsupported kind or version instead of guessing another shape. For example:

```json
{
  "kind": "python.contract",
  "version": 1,
  "scopes": ["scope-1"],
  "inputs": ["pyproject.toml"],
  "data": {
    "project": "pyproject.toml",
    "kind": "entry-point",
    "target": "app.main:run",
    "reason": "The host invokes this entry point."
  }
}
```

Requests allow at most 4,096 declarations, 10,000 inputs per declaration,
100,000 declaration inputs in aggregate, 64 KiB per declaration data object, and
2 MiB of declaration data in aggregate. The production validator rejects unknown scope
handles, duplicate handles or declarations, scalar data, excessive depth, and
invalid names at their exact indexed fields.

Return exactly one JSON response with `protocolVersion: 4` and one status:

- `pass`: nonempty evidence, no findings, complete coverage.
- `findings`: at least one finding and explicit coverage.
- `incomplete`: unsupported paths with concrete reasons; required work blocks.
- `operational-failure`: a failure description, with no analysis coverage or facts.

For every required path, `coverage.analyzed` or `coverage.unsupported` must account
for it exactly once. Both arrays are explicit, including when empty. Findings
include the requested capability, original path, stable `rule`, subject, and
message. Locations use one-based UTF-8 byte columns. The engine namespaces rules
as `pack.<name>.<rule>`; providers cannot claim core policy rule identities.

Architecture returns authored imports with original locations, resolved targets,
package identity when applicable, and runtime/type-only/re-export/proven-dynamic
edge kinds. Lint supplies lexical comment facts when comments are
forbidden; raw text is capped at 65,536 UTF-8 bytes and truncation sets `complete`
to false. Set `machineDirective` only for a complete line or shebang whose syntax
is a machine directive. Lint may supply static `literals` with path, one-based
line and UTF-8 byte column, decoded value, and `rootContext`; core retains the
machine-path, sibling-reference, and external-input policy decision. Complexity
supplies function complexity, depth, and parameter counts.
Explicit empty fact collections distinguish an inspected file without those facts
from omitted evidence. The core derives ownership and classifications and applies
its existing dependency, cycle, directive, and metric policies.

A successful format-write response may include edits for distinct selected
editable files. The engine checks every edit before writing. Generated source,
declared data, and context-only paths cannot become write targets. Other operations
and unsuccessful responses cannot return edits. Providers must not write source
directly.

Requests are bounded at 64 MiB and responses at 16 MiB. Additional limits bound
inventory, context, scope count and data, findings, fact collections, and text
fields. Standard error is a bounded diagnostic stream, not evidence of successful
analysis. Unknown fields, extra JSON values, escaping paths, missing identities,
malformed facts, and contradictory success claims are rejected. Semantic validation
identifies the exact JSON field and collection index with its expected constraint,
for example
`facts.comments[0].kind: expected Line, Block, Docstring, HTML, or Shebang`.

Validate a document without installing or executing a pack. Response validation
requires the exact request because coverage, scope handles, findings, facts, and
edits are request-bound:

```sh
code-polishy pack validate --kind manifest --input code-polishy-pack.json
code-polishy pack validate --kind request --input request-v4.json
code-polishy pack validate \
  --kind response \
  --input response-v4.json \
  --request request-v4.json
```

The command always emits `code-polishy-pack-validation/v1` JSON and exits 1 for
an invalid contract. Its report records document digests and bounded schema,
decode, or semantic issues without printing request or environment contents.

## Ownership, verification, and installation

An explicitly selected pack owns the complete language boundary within its declared
command paths. Its matching command owns a capability/profile, while an omitted
capability fails closed instead of returning to native analysis. Competing claims
fail. Missing or failed
selected packs remain repository errors. Authenticated retained claims block
native fallback for their exact language boundary. A missing pack without a
trusted manifest cannot disable unrelated analyzers. Source outside every selected
pack boundary retains its current route until the coordinated native-removal
release. Whole-program checks must own coherent discovered scopes;
partial handoffs cannot imply whole-project coverage. Ordinary configured commands
still run, but their successful exit does not establish structured source coverage.
Architecture providers run through the architecture command so graph policy is
always evaluated.

When the absence itself is intentional, declare it on the language instead of
shipping a no-op command:

```json
{
  "unsupportedCapabilities": [
    {
      "capability": "format",
      "reason": "this language pack does not specify a formatter"
    }
  ]
}
```

The declared absence satisfies inventory checks but makes an actual format request
fail with that reason. A language cannot both provide and mark the same capability
unsupported.

For a local source pack, obtain and review it through a trusted channel, then run:

```sh
code-polishy pack verify --source ./example-pack
code-polishy pack install --source ./example-pack
code-polishy pack root
```

Verification executes declared conformance fixtures. Installation executes no pack
code. It rejects links and special files, hashes the entire tree, and atomically
publishes an immutable receipt under the local pack store. The tree is bounded at
20,000 files, 128 MiB total, and 16 MiB per file.

An official local catalog adds a separate authenticated distribution boundary:

```sh
code-polishy pack catalog --catalog ./catalog.json --sha256 DIGEST
code-polishy pack install --official example-rust@1.0.0 --catalog ./catalog.json --sha256 DIGEST
code-polishy pack list
```

The caller supplies the reviewed catalog digest. The catalog binds the exact pack
tree, compatibility, authority, tools, dependencies, licenses, and provenance
attestation. Its artifact and attestation must be contained regular local files;
catalog operations neither access the network nor execute pack code. Use `pack
update example-rust --to 1.1.0` with the same catalog arguments to install an
explicit candidate. Updating, installing, or removing a pack never changes a
repository's selection or engine lock. If multiple trees share a name and
version, removal requires their exact `--digest`.

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

`pack list --format json` provides the versioned machine-readable state document.
Human output and `doctor --strict` distinguish installed, selected, missing,
incompatible, and corrupt identities.

For the coordinated breaking release, migrate a repository with an explicit
complete pack set instead of editing one pin at a time:

```sh
code-polishy pack migration plan \
  --catalog ./catalog.json \
  --sha256 DIGEST \
  --select shell@1.0.0 \
  --select python@1.0.0
code-polishy pack migration apply --plan .code-polishy-reports/pack-migrations/ID/plan.json
```

Planning installs and verifies only the named artifacts, records current native,
pack, generated-source, and custom-command coverage, and leaves repository policy
untouched. Apply rejects coverage gaps, new error diagnostics, changed evidence,
or an engine-lock change, then atomically replaces the complete `packs` array.
The plan prints an exact rollback command. Rollback restores the preserved
configuration only if doing so cannot overwrite later edits. This is a hard
cutover: there is no legacy decoder, automatic pin translation, alias, dual
execution, or native fallback for a missing replacement claim.

Use valid source, seeded defects, excluded inputs, stricter policy, mapping errors,
and tool failures to test a provider. Then exercise its exact installed identity
in a disposable project through ordinary checks and gates. Unsupported cases stay
visible. Additional language implementations and distribution services are separate
work; an adapter does not need dashboards, telemetry, or an AI acceptance system.
