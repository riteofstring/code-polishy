# Canonical Agent Guidance Design

## Purpose

`templates/AGENTS.md` is installed as the managed prefix of repository
`AGENTS.md` files. This repository's root file mirrors that prefix so the
project follows the same contract it publishes. The canonical prefix is a
small, always-on control plane, not a repository manual or a substitute for
enforcement.

Use code, configuration validation, permissions, tests, and CI for controls
whose violation would be costly. Keep a prose instruction when seeing it before
acting still prevents wasted work or explains how to satisfy the mechanical
control.

## Inclusion test

Add or retain a canonical instruction only when it is durable, actionable, and
meets at least one of these conditions:

- it gives an exact command or workflow whose correct form is not cheaply
  discoverable;
- it warns that an obvious action is unusually slow, unsafe, destructive, or
  unauthorized;
- it states a scope, compatibility, ownership, or completion boundary that an
  agent must know before changing files;
- it captures a recurring failure observed in real work and gives a concrete
  way to avoid it; or
- it records a stable collaboration preference that materially improves the
  maintainer's ability to review or decide.

Prefer wording that names the trigger and required action. An instruction that
only says to be careful, write clean code, or follow best practices does not
qualify.

## Exclusions and placement

Do not put these in canonical guidance:

- repository tours, architecture summaries, or facts already obvious from the
  checkout;
- generic engineering advice without a repository-specific decision;
- module-specific rules or occasional procedures;
- historical rationale, research summaries, plans, or release narration;
- duplicated policy text that gives no useful warning before enforcement; or
- rhetorical formulas whose effect cannot be tied to a concrete review need.

Put detailed procedures and rationale in permanent documentation. Map current
non-local design rationale through `.code-polishy.json` so `design-context`
retrieves it for the exact affected module. Use nested guidance for durable
directory-specific rules. Enforce critical restrictions mechanically; prose is
an aid, not proof of compliance.

## Project principles

A repository may append one final `## Project principles` section after one
blank line. The visible heading is the ownership boundary: the locked release
owns the preceding prefix, while the repository owns the heading through end of
file. Synchronization replaces a stale prefix and preserves a valid principles
section byte-for-byte. A recognized but malformed section blocks the complete
transaction so synchronization cannot erase or normalize repository decisions.

The section has one flat, consecutively numbered list with at most 12 items.
Each item begins with one bold title of at most eight whitespace-delimited words
and continues as one paragraph. The complete repository-owned suffix, including
its required leading blank line and heading, is at most 5 KiB, and each complete
item, including its Markdown and line endings, is at most 400 UTF-8 bytes.
Nested lists, extra headings, tables, code blocks, images, and links are invalid.
These deterministic limits keep the persistent prompt bounded, the list
scannable, and each principle focused on one decision.

Titles use imperative phrasing. Principles state durable, testable repository
decisions and ownership boundaries, not aspirations. They omit paths, commands,
versions, dated notes, current bugs, and process rules about tests, commits, or
delivery. They do not duplicate the canonical procedure or replace mapped
design rationale. Agents edit the section only on the caller's explicit
request. Treat item numbers as stable references: append new principles and
renumber only when explicitly requested. Nested guidance may specialize project
principles for its scope but cannot weaken either the principles or the locked
baseline.

Code Polishy does not create or require `CLAUDE.md` because supported agents
read `AGENTS.md` directly. Install and sync transactionally remove only the two
exact historical managed stubs: the `@AGENTS.md` import and its former prose
redirect. Any custom or non-regular `CLAUDE.md` remains repository-owned and is
ignored by agent-guidance status.

## Updating the canonical file

1. Start from an observed failure, a hidden operational fact, or a deliberate
   contract change. Apply the inclusion test before drafting text.
2. Add or update mechanical enforcement when the rule is safety- or
   correctness-critical.
3. Remove superseded and redundant wording in the same change. Review deletions
   as seriously as additions.
4. During ordinary development, update `templates/AGENTS.md` and the managed
   prefix of root `AGENTS.md` together. Keep them byte-identical when the root
   has no project principles. During release development, change only the
   template while the root remains governed by the outgoing lock. After the
   incoming release's `lock` command performs the atomic cutover, run that
   release's `agents sync` and commit the root update with the self-hosting lock.
   Never use the outgoing release's sync command to author the incoming template.
5. Format both files and run the focused `internal/agents` tests. Keep tests
   focused on durable behavior rather than complete prose snapshots.
6. Record a user-visible contract change in the changelog.

The stable-release supplemental retry rule earns canonical space because it
prevents repeated expensive hardening without weakening evidence. Reuse exact
receipts and rerun only missing, failed, expired, or invalidated suites. A full
run is needed only without a trusted baseline, after shared mutation
infrastructure, toolchain, or selection changes, or when impact is unbounded.
The merge-checkpoint boundary prevents ordinary task completion, commits, and
delivery from being mistaken for permission to run a branch-wide gate. At a
genuine merge or release checkpoint, the final-gate-owner reminder still
prevents duplicate local and CI execution while preserving one required owner.

The vulnerability-age rule records the maintainer's risk preference. When
evidence supports a governed not-affected assessment, waiting preserves the
minimum dependency age instead of admitting fresh code without a reachable
security benefit. Reachable advisories still use the security-fix admission
path.

Digest guidance addresses a recurring agent failure observed across repositories.
A digest earns its cost when a named consumer uses it for authentication,
immutable identity, or evidence binding. Hashes that only mirror mutable local
state or repeat a digest already produced within one trusted operation create
maintenance and runtime cost without adding authority.

The end-to-end simplicity rule addresses another recurring failure: individually
defensible boundaries that collectively add machinery and total failure risk
disproportionate to the current problem. Canonical guidance states the default,
the agent workflow and task-start output repeat it when a solution is chosen,
architecture policy supplies the decision test, and an explicitly requested
architecture review examines material violations. Those surfaces serve different
decisions; the repetition is deliberate rather than a substitute for enforcement.

The reliability clause makes the same system-level concern concrete for
defensive controls. Managed guidance supplies the always-visible default;
repository selectors keep the detailed task-start and gate reminder salient;
mapped design documents and boundary tests own domain-specific invariants. The
reminder stays advisory so it does not become more reliability machinery.

## Size budget

The focused agents test caps the canonical prefix and optional project
principles at 5 KiB each. The resulting root file is at most 10 KiB. These are
engineering budgets that catch accidental growth and reserve instruction space
for repository-owned guidance; they are not empirical performance thresholds.
Do not game either budget with cryptic prose. Remove lower-value or duplicated
material first. Raise a budget only through a deliberate reviewed change when a
valuable broadly applicable rule cannot fit clearly.
