# Security evidence lifetime

Source verification and advisory freshness have different lifetimes. An exact
gate pass remains evidence that its source and tests passed, while advisory
services can change without a repository edit. Online security acceptance is
therefore bounded to 24 hours, independently of source identity and assessment
expiry. Recommended and full merge gates evaluate the online profile; ordinary
checks and documentation-only verification do not claim online coverage.

OSV and pnpm audit commands reuse raw output, rather than
an accepted vulnerability decision. The evidence identity binds dependency and
control inputs, exact commands and environments, composed policy and scanner
identity. Applicable root and nested `.gitignore` files also participate, even
when an ignore file is itself ignored and untracked. They are discovered from
the inventory's ancestor directories because tracked SBOMs remain in that
inventory when ignore rules change OSV's view. Editing or removing an applicable
ignore file invalidates reuse. The identity format version prevents older
evidence that omitted these inputs from satisfying this contract.
Source-aware Go analysis runs normally because package inventory alone
cannot identify its complete source and compilation inputs.
Installed releases supply their authoritative release identity; development
scanner binaries use their own executable identity. Development pnpm audits
always execute because a Node executable alone cannot identify its JavaScript
bundle; installed release identity covers both. Raw output remains in a
bounded report artifact. Every consumer parses it again and applies current
assessment and exploitation rules. Invalid output, operational failures,
cancellation and timeout cannot establish a reusable scan.

Input identities are calculated once per relevant scope in a trusted operation.
The persistent store is local verification evidence, not an authenticated claim
from an external actor. Reads require bounded, contained regular files and exact
input identities. Publication is atomic. Scan failures retain the previous
last-success time without extending its deadline, and retry on the next call.
Known exploitation and release-age decisions remain their existing online work.

The gate runner retains its planned command order and bounded logs when a raw
scan is replayed. The report explicitly identifies reused scans and their
original success times. A cached package scan cannot satisfy a repository
provider, pre-install deferral, artifact builder or image scan. Image targets
are prepared and scanned on each online invocation, including reuse of a
historical gate pass, because that pass cannot identify a newly produced image.
This keeps the reuse boundary small and avoids inventing input declarations for
arbitrary repository commands or container builders.

When security expires for an otherwise identical passed gate, only the online
supply-chain profile runs. Its current outcomes replace the historical security
outcomes while source/test evidence and source exceptions remain intact. The
refresh record binds the immutable original gate report, so changing that gate
cannot inherit another candidate's acceptance. A failed refresh remains visible
and cannot fall back to the historical pass. Fresh successful refreshes are
reusable within their deadline when no image needs preparation. Expired policy
assessments still invalidate the gate identity through the existing rules.
Before publishing a refresh, the gate revalidates its candidate, policy expiry,
Git evidence and source-analysis evidence. A provider that changes governed
inputs cannot retain the historical source/test pass; restoring the exact
candidate permits a normal security-only retry.

Reports expose coverage status, check time, refresh deadline, scanner version
when available, last successful scans and artifact paths. OSV and native audit
services do not consistently provide a database release identifier; original
provider metadata is retained without inventing one. Scheduled online checks
remain necessary to discover advisories while no verification is requested.
