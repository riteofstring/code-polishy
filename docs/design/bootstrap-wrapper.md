# Repository Bootstrap Wrapper

## Decision

Adoption installs two small repository-local launchers: `code-polishyw` for
POSIX shells and `code-polishyw.ps1` for PowerShell. They contain bootstrap and
dispatch logic, not a Code Polishy release or its toolchain. The wrapper reads
the repository lock and delegates ordinary commands to the exact release in
the shared per-user installation prefix.

`setup` is the only wrapper action allowed to acquire anything. A version-two
lock contains one HTTPS archive URL, checksum, and size for every supported
host. Those values are derived from an exact checksum-pinned publication index
by initial adoption or the upgrade planner. The wrapper selects the current host, downloads the
archive, verifies its checksum, and uses the archive's own engine to perform the
bounded bundle installation. It does not clone Code Polishy, install a language
toolchain, or build a release.

Wrapper `setup --source PATH` is an explicit recovery path for a local Code
Polishy checkout. The source installer still proves that the built release
satisfies the target lock. Version-one locks have no archive authority and
therefore require this explicit source path; they never silently regain the old
clone-and-build path.

## Trust boundary

The target lock remains the release authority. Source identity checks prevent a
mutable branch or lightweight tag from standing in for a release, but the
installer must also verify the completed staged release and prove that its
version, release digest, host, and capabilities satisfy the target lock. That
proof occurs before a release directory, launcher, or publication output is
committed. A source checkout cannot silently replace the lock with what it
built.

Normal wrapper dispatch never clones, downloads, builds, changes `PATH`, or
selects a fallback release. If the exact installed release cannot be verified
through the stable launcher, it fails with the explicit setup command. Shared
storage lets repositories with the same lock reuse one installation while
different locked releases coexist. A repository-reviewed archive checksum
authenticates bytes relative to that repository's lock; it does not establish
builder identity or replace the separately planned provenance work.

## Adoption ownership

The agent-guidance transaction owns both wrappers alongside `AGENTS.md`,
`CLAUDE.md`, and the report-artifact ignore rules. Installation creates missing
wrappers; synchronization replaces only files carrying the managed marker and
restores the canonical mode. An unrelated pre-existing wrapper is a conflict,
so the entire transaction preserves every target. `agents check` reports
missing, stale, non-regular, or conflicting wrappers.

The root wrapper paths are built-in sensitive control inputs. A byte-identical
copy of the locked release's canonical template is a managed control artifact,
not application source, so adoption does not distort the target's module model.
A modified or conflicting copy loses that classification and remains subject to
normal executable-source coverage in addition to the adoption-status finding.

## Publication boundary

The publication index is canonical, bounded, versioned, and checksum-pinned by
the adoption or upgrade request. Every descriptor must name one version, source revision,
and release digest across the complete supported host set. The planner resolves
archive names relative to the HTTPS index URL and copies those exact URLs,
checksums, and sizes into the next repository lock. Setup trusts only that
reviewed lock and the installed manifest verification; mutable release-page
metadata never participates in selection.

Initial adoption can pass the index URL and checksum to `lock`. That operation
requires the executing installed release to match the index and records the
same version-two publication authority without redownloading an archive. A
source-only or private adoption can omit the index and deliberately receive a
legacy lock whose wrapper requires an explicit local source checkout.

## Upgrade boundary

Upgrade is deliberately two phase. `upgrade plan` accepts either a verified
publication index or an exact clean local source checkout. It installs the
current-host candidate without changing repository authority, authenticates the
capability delta, and runs the outgoing and incoming engines against the same
repository. A source candidate is bound to the checkout's unchanged commit and
installed manifest and produces a version-one lock; it does not invent archive
authority for hosts that were not published. The durable plan identifies added,
removed, and changed diagnostics so a policy bump cannot disguise application
cleanup as a simple version edit.

`upgrade apply` uses the same path for publication-backed and source-backed
plans. It revalidates the outgoing lock, installed candidate, and incoming
diagnostic snapshot. New error diagnostics require an explicit acceptance or a
new plan after cleanup. Apply stages canonical guidance, both wrappers, ignore
rules, and the incoming lock as one rollback-capable transaction; the lock is
renamed last. That rename is the guidance cutover. Upgrade never edits governed
application source.
