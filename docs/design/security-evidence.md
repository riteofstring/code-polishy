# Security evidence lifetime

Source verification and advisory freshness have different lifetimes. An exact
gate pass remains evidence that its source and tests passed, while advisory
services can change without a repository edit. Online security acceptance is
therefore bounded to 24 hours, independently of source identity and assessment
expiry. Recommended and full merge gates evaluate the online profile; ordinary
checks and documentation-only verification do not claim online coverage.

Policy-owned pnpm audits and explicit OSV uv/Cargo inventory scans reuse raw
output for complete, captured inputs. The supply-chain operation owns the input
snapshot, its immutable identity, and consumption of those same bytes. The cache
uses that snapshot through the ordinary planned-command runner; it never hashes
a live file and then lets an adapter read it independently. Input snapshots are
private and read-only, and are removed after execution, failure or cancellation.

The pnpm snapshot retains the declared lockfile path. The adapter copies only
that snapshot into its sealed audit directory and discovers no other repository
inputs. OSV snapshots contain only the captured public package inventory and an
explicit empty scanner configuration. Their sealed invocations disable resolution
and Rust call analysis, and require every inventoried package in the response.
Missing, repeated or foreign coordinates fail coverage for both ecosystems.
Repository OSV configuration cannot silently filter these inventories; Code
Polishy applies assessment policy to the resulting advisories. The snapshot owner
validates each returned source against the exact private input and normalizes
only that path to its logical projection identity, preserving provider metadata.
Original lockfile scopes survive both execution and replay.

Evidence binds the captured input, logical scanner options, effective selected
environment and installed release identity. Selected environment values are
captured before identification and consumed unchanged. Assessment configuration
is deliberately outside raw-result identity: each consumer reevaluates current
policy, including assessment edits and expiry. Development scans always execute
because an executable alone does not identify the complete tool bundle. Offline
OSV invocations also execute because their local advisory database is not part of
the captured inventory.

Recursive OSV scans and source-aware Go analysis execute normally. Recursive OSV
extractors can follow references to arbitrary filenames, including ignored
requirements files; filename rules and Git inventories cannot establish their
complete inputs. Cache eligibility does not attempt to duplicate those parsers
or ignore rules. Changing an included file or an ignore rule is observed by the
next scan, and even unchanged recursive scans execute. Recursive scans bypass
input identity and cache lookup entirely. Each attempt retains its own bounded
raw report artifact. The audit identity format excludes entries created before
snapshot-bound consumption. Every consumer parses raw output again and applies
current assessment and exploitation rules. Invalid output, operational failures,
cancellation and timeout cannot establish a reusable scan.

Each captured snapshot calculates its input identity once and reuses that identity
through execution and publication.
The persistent store is local verification evidence, not an authenticated claim
from an external actor. Reads require bounded, contained regular files and exact
input identities. Publication is atomic. Cached audit failures retain the
previous last-success time without extending its deadline, and retry on the next
call. Uncached attempts retain separate artifacts rather than looking up history.
Known exploitation and release-age decisions remain their existing online work.

`cleanup --reports` reclaims both the security scan and security refresh trees,
including individual uncached attempts. They use the existing owned-cache cleanup
rules, dry-run accounting, and protection for linked or special entries. Deleting
these regenerable records makes subsequent verification collect fresh evidence.

The gate runner retains its planned command order and bounded logs when a raw
scan is replayed. The report explicitly identifies reused scans and their
original success times. A cached package scan cannot satisfy a repository
provider, pre-install deferral, artifact builder or image scan. Image targets
are prepared and scanned on each online invocation, including reuse of a
historical gate pass, because that pass cannot identify a newly produced image.
This keeps the reuse boundary small and avoids inventing input declarations for
arbitrary repository commands or container builders.

Every repeated online gate runs the normal supply-chain profile while retaining
its successful source and test evidence. Each eligible scan consults the same raw
result cache used by dependency review and post-install verification. There is no
separate cache of accepted security verdicts and no OSV-specific refresh trigger.
The latest refresh report is retained for visibility, bound to the original gate
report, but is never used to skip security evaluation.

Producers mark each finding with its source or security verification phase,
including findings inside suppressions and assessments. Refresh replaces only
security-owned outcomes; rule names and finding payloads do not determine
ownership. Static policy and module findings retain source ownership when an
online profile includes them, preserving one canonical occurrence. Independent
findings from different phases remain separate observations so replacing security
evidence cannot erase source findings, exceptions or assessments. A failed refresh
remains visible and cannot fall back to historical acceptance. Expired policy
assessments still invalidate gate identity through the existing rules.
Before publishing a refresh, the gate revalidates its candidate, policy expiry,
Git evidence and source-analysis evidence. A provider that changes governed
inputs cannot retain the historical source/test pass; restoring the exact
candidate permits a normal security-only retry.

Reports expose coverage status, check time, refresh deadline, scanner version
when available, last successful scans and artifact paths. OSV and native audit
services do not consistently provide a database release identifier; original
provider metadata is retained without inventing one. Scheduled online checks
remain necessary to discover advisories while no verification is requested.
