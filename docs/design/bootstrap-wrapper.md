# Repository Bootstrap Wrapper

## Decision

Adoption installs two small repository-local launchers: `code-polishyw` for
POSIX shells and `code-polishyw.ps1` for PowerShell. They contain bootstrap and
dispatch logic, not a Code Polishy release or its toolchain. The wrapper reads
the repository lock and delegates ordinary commands to the exact release in
the shared per-user installation prefix.

`setup` is the only action allowed to acquire anything. When the locked release
is absent, it clones the selected Code Polishy source at the exact version tag,
requires an annotated tag that points directly to the clean checkout, verifies
`VERSION`, installs the pinned policy tools, and invokes the source installer.
The canonical public repository is the default. `--source` is an explicit,
ephemeral override for private mirrors or local testing and is never recorded
in the target repository.

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
different locked releases coexist.

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

Source bootstrap is the initial portable path because the release lock already
contains everything needed to verify a locally built release. Direct archive
download remains a separate optimization. It requires an authenticated,
versioned publication index that binds each supported host archive to the
locked release before the wrapper can safely choose and download one.
