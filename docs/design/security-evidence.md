# Security evidence lifetime

Source verification and advisory freshness have different lifetimes. An exact
gate pass remains evidence that its source and tests passed, while advisory
services can change without a repository edit. Online security acceptance is
therefore bounded to 24 hours, independently of source identity and assessment
expiry. Recommended and full merge gates evaluate the online profile; ordinary
checks and documentation-only verification do not claim online coverage.

Only policy-owned pnpm audit commands with a sealed environment and an explicit
lockfile input reuse raw output, rather than an accepted vulnerability decision.
The engine's audit operation reads that lockfile once, identifies the captured
bytes, and supplies them through a private, read-only snapshot to the audit
adapter before publishing evidence. The snapshot retains the declared relative
path and is removed after execution, including failures and cancellation. The
adapter copies only that snapshot into its sealed audit directory; it does not
read the live lockfile again, load repository configuration, or discover other
dependency files. Changing or restoring the live lockfile during an audit cannot
associate a result with different bytes. No change-detection hashes are needed.
Evidence also binds the logical command and environment, composed policy, and
installed release identity, which covers both Node and the JavaScript bundle.
Development pnpm audits always execute because a Node executable alone cannot
identify the bundle.

OSV source scans and source-aware Go analysis execute normally. Recursive OSV
extractors can follow references to arbitrary filenames, including ignored
requirements files; filename rules and Git inventories cannot establish their
complete inputs. Cache eligibility does not attempt to duplicate those parsers
or ignore rules. Changing an included file or an ignore rule is observed by the
next scan, and even unchanged recursive scans execute. Ineligible scans bypass
input identity and cache lookup entirely. Each attempt retains its own bounded
raw report artifact. The audit identity format excludes entries created before
snapshot-bound consumption. Every consumer parses raw output again and applies
current assessment and exploitation rules. Invalid output, operational failures,
cancellation and timeout cannot establish a reusable scan.

Input identities are calculated once per relevant scope in a trusted operation.
The persistent store is local verification evidence, not an authenticated claim
from an external actor. Reads require bounded, contained regular files and exact
input identities. Publication is atomic. Cached audit failures retain the
previous last-success time without extending its deadline, and retry on the next
call. Uncached attempts retain separate artifacts rather than looking up history.
Known exploitation and release-age decisions remain their existing online work.

The gate runner retains its planned command order and bounded logs when a raw
scan is replayed. The report explicitly identifies reused scans and their
original success times. A cached package scan cannot satisfy a repository
provider, pre-install deferral, artifact builder or image scan. Image targets
are prepared and scanned on each online invocation, including reuse of a
historical gate pass, because that pass cannot identify a newly produced image.
This keeps the reuse boundary small and avoids inventing input declarations for
arbitrary repository commands or container builders.

When security expires for an otherwise identical passed gate, or the repository
requires recursive OSV or image scans, only the online supply-chain profile
runs. Producers mark each finding with its source or security verification
phase, including findings inside suppressions and assessments. Refresh replaces
only security-owned outcomes; rule names and finding payloads do not determine
ownership. Static policy and module findings retain source ownership when an
online profile includes them, preserving one canonical occurrence. Independent
findings from different phases remain separate observations
so replacing security evidence cannot erase source findings, exceptions, or
assessments. The refresh record binds the immutable original gate report, so changing that gate
cannot inherit another candidate's acceptance. A failed refresh remains visible
and cannot fall back to the historical pass. Fresh successful refreshes are
reusable within their deadline when no recursive OSV scan or image preparation
is required. Expired policy assessments still invalidate the gate identity
through the existing rules.
Before publishing a refresh, the gate revalidates its candidate, policy expiry,
Git evidence and source-analysis evidence. A provider that changes governed
inputs cannot retain the historical source/test pass; restoring the exact
candidate permits a normal security-only retry.

Reports expose coverage status, check time, refresh deadline, scanner version
when available, last successful scans and artifact paths. OSV and native audit
services do not consistently provide a database release identifier; original
provider metadata is retained without inventing one. Scheduled online checks
remain necessary to discover advisories while no verification is requested.
