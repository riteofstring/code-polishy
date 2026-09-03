# Canonical Agent Guidance Design

## Purpose

`templates/AGENTS.md` is installed verbatim into managed repositories. This
repository's root `AGENTS.md` mirrors it so the project follows the same contract
it publishes. The canonical file is a small, always-on control plane, not a
repository manual or a substitute for enforcement.

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

## Updating the canonical file

1. Start from an observed failure, a hidden operational fact, or a deliberate
   contract change. Apply the inclusion test before drafting text.
2. Add or update mechanical enforcement when the rule is safety- or
   correctness-critical.
3. Remove superseded and redundant wording in the same change. Review deletions
   as seriously as additions.
4. Update `templates/AGENTS.md` and the root `AGENTS.md` together and keep them
   byte-identical. During release development, do not use the prior locked
   release's `agents sync` command to author the next release's template.
5. Format both files and run the focused `internal/agents` tests. Keep tests
   focused on durable behavior rather than complete prose snapshots.
6. Record a user-visible contract change in the changelog.

The stable-release supplemental retry rule earns canonical space because it
prevents repeated expensive hardening without weakening evidence: run the full
set once, then rerun failed or invalidated suites by exact name. Targeted
evidence composes with still-valid passes; only shared mutation infrastructure,
toolchain, or selection changes, or unbounded impact, require a full repeat.

## Size budget

The focused agents test caps canonical guidance at 5 KiB. The number is an
engineering budget that catches accidental growth and reserves instruction
space for repository-owned nested guidance; it is not an empirical performance
threshold. Do not game the budget with cryptic prose. Remove lower-value or
duplicated material first. Raise the budget only through a deliberate reviewed
change when a valuable broadly applicable rule cannot fit clearly.
