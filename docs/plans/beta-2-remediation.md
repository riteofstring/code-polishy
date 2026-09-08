# Beta 2 remediation

## Outcome and boundaries

Make provider-backed checks complete their declared execution, preserve protected
inputs, and analyze supported local code without treating remote browser scripts
as missing local modules. Keep framework parsing inside the optional JS/TS pack.
Preserve all existing native checks, exact execution enforcement, source ownership,
and incomplete status for unsupported analysis.

The source base is `fa05fdf29ea75e70eff47e66ef33641ab5dabf87` on `beta`.
Implementation lives on `fix/beta-2-remediation`. The retrospective-review branch,
untracked issue documents, consuming repository locks, and published snapshots
remain separate inputs. This work does not publish artifacts or update tags.

## 1. Provider execution and operation selection

Prepare one provider invocation through the pack boundary: exact runtime and pack,
working root, capability/profile, selected files, and structured request. Reuse that
preparation for planning and execution. Empty selections produce no planned work.
Keep exact comparisons in both planned and artifact runners; persist the execution
root and request digest so changes cannot reuse a passed identity. Diagnostics name
the mismatched command and identity fields without revealing request or environment
values. Preserve honest failure reports for unavailable runtime or pack inputs.

Account for architecture work where the engine actually invokes it, including
ownership discovery, rather than leaving its config-list position in the quality
plan. Preserve existing test-view and cleanup execution contracts.

Split the JS/TS manifest declarations into formatting and analysis profiles.
Formatting performs formatting; check and gate retain all six capabilities. Core
continues respecting other providers' explicit profile declarations.

## 2. Protected contained asset links

Keep the contained regular-file reader and ContentDigest strict. Add one shared
read-only identity operation for an inventoried asset link: link text, contained
canonical target, and bounded target content identities. Reject broken links,
cycles, escapes, executable/control targets, and changes during verification.

Use that identity consistently in formatting snapshots and provider context.
Protect link entries from writes and avoid treating directory links as text files.
Target content remains governed at its canonical source paths. Provider edits must
reject every symlink component. No new configuration or external-link support.

## 3. External browser scripts

The JS/TS framework adapter distinguishes processed local module references,
inline source, and browser-loaded external scripts. A literal URL or template
whose static prefix proves a complete HTTP(S) authority may establish an external
boundary; interpolation that can change the origin remains unsupported.

Analyze the local expression and preserve original locations. Record bounded
provider notes identifying remote-code boundaries; surface those through existing
informational findings. Do not fetch remote bytes, invent package dependencies,
or weaken genuinely unknown local reachability. Keep spread and unresolved dynamic
sources explicit. No hostname, product filename, or new runtime-input declaration.

## 4. Compilation units

Retain nearest enclosing tsconfig.json or jsconfig.json as deterministic ownership.
Application, tooling, and tests can use separate existing configuration files.
Record the effective configuration and required compiler overrides in bounded
provider notes. Keep allowJs/checkJs enabled, excluded selected source incomplete,
and unsupported project references explicit. Add behavioral fixtures for nested
units, exclusions, missing types, and the existing strict policy. Do not invent an
automatic merge of ambiguous sibling configurations or execute configuration code.

## 5. Immutable literal data modules

Extend existing scope.data protection to explicitly selected .js/.mjs literal data
modules. Keep control inputs, executable file modes, shebangs, other executable
languages, exclusions, and generated-output overlap forbidden. Declared data takes
precedence over language selection without hiding module dependency obligations.

The native JS/TS parser validates a bounded, non-evaluating literal grammar:
export default of a literal value; a single const literal binding followed by its
default export; or a named exported const literal binding. Literal values comprise
JSON-like primitives, arrays, and plain object properties. Reject imports, calls,
getters, spreads, computed keys/values, prototype setters, arbitrary statements,
and malformed syntax. Neither importing nor evaluating the module is permitted.

Run validation during checking and before formatting protected data. Preserve
bytes exactly and bind them to existing input evidence. Data is excluded from
executable provider requests, while remaining governed by ownership, contracts,
and integrity checks. Existing JSON/JSONC/YAML behavior remains available. No new
artifact registry, provider capability, or format-selection subsystem.

## Verification and delivery

Use focused regressions after each coherent change: prepared provider execution
and rejected identity mutations; protected asset links and unsafe writes; external
script boundaries and local defects; compilation units; literal-data admission
and rejection. Exercise a small installed protocol-v2 provider gate and check its
saved report, plus exact formatter behavior with real protected inputs.

Do not run mutation/signing suites, a cross-platform release matrix, or the launch
PDF catalogue. Preserve the existing failed launch evidence. Read-only project
comparisons can establish that an internal tool failure disappeared; application
findings and adoption work remain separately reported. Commit completed verified
batches, documenting any remaining limitations without claiming project gate
acceptance from a passing fixture or successful installation.
