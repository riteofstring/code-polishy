# Documentation

The root [README](../README.md) is the short product introduction. Use this map
when you need the operating contract or policy details behind it.

From a managed repository, the installed CLI reads the documentation carried by
that repository's locked release:

```sh
code-polishy docs list
code-polishy docs find behavior review
code-polishy docs read agent-workflows
code-polishy docs read adding-a-language
```

The checked-in `catalog.json` owns stable topic identifiers, titles, summaries,
and aliases. Search is local, bounded, and deterministic.

## Start here

- [Installation](installation.md) explains exact-tag source installation,
  native releases, manifests, and target locks.
- [AI-Agent Setup and Adoption](ai-adoption.md) is the authoritative procedural
  runbook for setup and upgrades performed by a coding agent.
- [Adopting Code Polishy](adoption.md) is the detailed project-modeling
  reference for modules, capabilities, checks, tests, and CI.

## Operating Code Polishy

- [Agent Workflows](agent-workflows.md) covers ordinary interactive work,
  native subagents, and optional task sessions.
- [Isolated Task Sessions](task-sessions.md) specifies disposable-worktree
  boundaries, promotion, and artifacts.
- [Artifact Security](artifact-security.md) documents shared container scanning,
  producer contracts, OpenVEX, and optional behavior-review evidence custody.

## Policy reference

- [Architecture](policies/architecture.md)
- [Behavior and final-state review](policies/behavior-review.md), including
  experimental opt-in feature policy and task requests
- [Code quality](policies/code-quality.md)
- [Conditional modules](policies/conditional-modules.md)
- [Exceptions](policies/exceptions.md)
- [Portability and external inputs](policies/portability.md)
- [Security](policies/security.md)
- [Supply chain](policies/supply-chain.md)
- [Test strength](policies/test-strength.md)
- [Verification and testing](policies/verification.md)

## Maintainer reference

- [Policy Engine Architecture](policy-engine-architecture.md) maps the runtime
  ownership boundaries and execution model.
- [Source Comment Policy Design](design/source-comment-policy.md) explains the
  default-preserving switch and the rationale for repositories that select the
  strict no-comment boundary.
- [Source Provenance](source-provenance.md) records public design references,
  toolchain origins, and implementation boundaries.
- [Release Checklist](release-checklist.md) defines the immutable annotated-tag
  release process.
- [Adding a Community Language](adding-a-language.md) defines local language
  packs, conformance verification, exact installation, and adapter protocol.
