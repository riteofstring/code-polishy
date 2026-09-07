# Provider beta acceptance

The candidate is Code Polishy `0.25.0-beta.1` on `beta`, based on upstream
`v0.24.11`. Its local native release manifest records the exact source commit
and engine digest. The optional `javascript` pack is `0.1.0-beta.1`; its immutable
installation receipt records the exact tree digest. This is local beta evidence,
not a published or cross-platform release claim.

## Source projects

| Project                       | Source revision                            | Project Astro | Selected JS/TS and Astro files |
| ----------------------------- | ------------------------------------------ | ------------- | ------------------------------ |
| `code-polishy-site`           | `4abfae9db5ea31bb10ebf07a19f9421296f58d22` | 7.2.4         | 11                             |
| `learnportugal-pronunciation` | `4efa440f6ce5ae235741ba957034b44e6d4fc63a` | 5.18.1        | 75                             |

Acceptance uses disposable copies and their existing dependency installations.
Original repositories and exact release locks remain unchanged. The provider
receives existing manifests and TypeScript configuration, effective Code Polishy
policy, and a governed source inventory. Temporary adoption policy supplies module
and test ownership for graph verification. Existing build commands are not used
to establish analyzer coverage.

## Observed behavior

Formatting and linting account for all selected files in both copies. Complexity
finds two functions at or above the configured production limit in the site and
31 in pronunciation. Unused-code analysis accounts for all selected files,
reporting no site findings and 16 pronunciation findings. These counts describe
the recorded source snapshot and policy, not a promise of invariant diagnostics.

Type checking finds errors at original authored locations. The site accounts for
11 files; pronunciation accounts for 43 and explicitly reports 32 excluded by its
existing `src/**/*` compilation selection. Excluded scripts and service-worker
files cannot become successful type coverage. The beta does not claim that either
project passes all Code Polishy policy or that every Astro configuration is supported.

Both source graphs construct successfully after explicit test ownership is supplied.
Pronunciation retains three development-dependency findings in tooling that its
initial adoption policy has not classified as development source. Static JSON
module globs contribute real contained dependency targets. Separate regression
fixtures show provider-to-native edges, forbidden module direction, and omitted
provider coverage blocking graph acceptance.

Each project copy also detects an injected original-line type mismatch,
unreachable inline-script statement, and missing authored import. Focused formatting
changes only the selected probe; all pre-existing file bytes remain unchanged.
Permanent fixtures cover literal route brackets, missing package stylesheets,
Node builtins, excluded files, proposed writes, and ESLint metric semantics.

The optional pack has 12 conformance cases: valid source and a seeded defect for
each of six capabilities. A separate SQLite syntax pack invokes the pinned Node
runtime's real parser on valid and invalid SQL. It deliberately claims lint only;
other language capabilities remain unavailable.

## Bounds and remaining work

The measured provider fits within the existing 128 MiB tree and 16 MiB file bounds.
Its roughly 12,600 files require increasing the pack inventory bound to 20,000.
Real project responses exceed the old 1 MiB limit, so requests and responses share
the existing runner's 8 MiB bound. Exact runtime, pack-tree, input, response, source
coordinate, and write-authority checks remain enforced.

Computed module loads, executable module-glob reachability, project references,
unknown framework syntax, dynamic source-directory configuration, and unavailable
source mappings remain explicitly incomplete. Missing or conflicting providers
cannot trigger an implicit native fallback. Additional framework and language
adapters remain separate implementations through the same boundary.

The historical Electron runtime report remains unresolved; the current source no
longer contains the reported implementation. Workflow-friction follow-ups and the
unreproduced gate-identity report remain deferred. Dashboards, remote telemetry,
aggregate analytics, and tracing integrations are outside this delivery.
