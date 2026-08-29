![Code Polishy](./code-polishy-banner.png)

# Code Polishy

Code Polishy is an opinionated, agent-first policy engine that gives coding
agents, developers, and CI one versioned engineering contract instead of
letting each agent decide what "good enough" means. Each governed repository
locks the exact release that enforces it, so the rules and toolchain do not
silently vary between runs.

## What does it do?

The project describes its modules and dependency direction, required checks and
test suites, product-specific build commands, artifact targets, and narrow
exceptions in `.code-polishy.json`. Code Polishy uses that model for fast,
change-focused feedback during implementation and selects the documentation,
recommended, or full validation level for the final merge gate.

Built-in checks cover declared Go and JavaScript/TypeScript architecture
boundaries; exact direct pins, lock consistency, dependency release age, and
standalone executable release age; known vulnerabilities for supported
ecosystems; obvious no-op test
configuration and empty Go tests; formatting; and common language-quality
rules, including policy-owned Ruff C901 that fails at complexity 10 and
policy-owned `ty` type checking with normal defaults. Comments are preserved by
default; `quality.allowComments: false` activates a fail-closed ban on prose
comments and docstrings in handwritten source. Ordinary Markdown has a built-in
formatting and local-link contract. Repository-configured commands and suites
cover builds, product behavior, and ecosystems the shared engine cannot honestly
decide.

## Get running

Start your coding agent in the repository you want Code Polishy to govern and
say:

```text
Set up Code Polishy in this repository. Follow
https://github.com/riteofstring/code-polishy
```

That is the primary installation interface. The agent preserves an existing
Code Polishy lock or selects the latest stable annotated version tag, clones
that exact tag into temporary space, verifies its identity, installs its native
release, configures the target, and runs the adoption checks. Ask for a tag such
as `v1.2.3` only when you want a particular version.

For a first adoption, the agent inventories the target read-only and asks one
bundled setup-wizard question showing optional features, their defaults, and its
repository-specific recommendations before it writes configuration.

Nothing installs from floating `main`, and no GitHub Release asset is required.
Windows uses the same tagged source workflow through native PowerShell and does
not need WSL or Git Bash; Git itself is required. Allow about 1 GB of disk space
for the self-contained policy toolchain. See the complete
[AI-agent runbook](docs/ai-adoption.md) or the
[manual installation details](docs/installation.md).

## Daily commands

```sh
# Resolve current non-local rationale for the source being changed
code-polishy design-context --files internal/example/value.go

# Fast feedback while you are still editing
code-polishy test --changed

# Ordinary Markdown-only work needs formatting, not application tests
code-polishy format --git-changes

# Review a dependency update before keeping it
code-polishy dependency-review --base origin/main

# Final pre-merge validation; do not rerun test --changed first
code-polishy merge-gate --base origin/main
```

Use focused or changed tests while iterating. At a merge checkpoint, run the
merge gate once for the current final candidate. If it fails, repair the
finding and rerun the narrowest relevant check before repeating the merge gate
for the repaired candidate. An ordinary Markdown-only candidate automatically
runs the documentation contract with zero application tests; no user approval
or project opt-in is involved.

## What lives in your repo

- `.code-polishy.lock.json` — exact trusted Code Polishy release.
- `.code-polishy.json` — modules, dependency direction, mapped design context,
  tests, commands, and narrow exceptions.
- `AGENTS.md` — the release-owned operating rules for coding agents.

After setup, `AGENTS.md` is the recurring agent interface; users should not
need to repeat Code Polishy's operating rules in prompts.

Task sessions are optional. See [Agent workflows](docs/agent-workflows.md) when
you need isolated or unattended delivery.

## Need the details?

- [Installation details](docs/installation.md)
- [Adopt Code Polishy](docs/adoption.md)
- [Set up with an AI agent](docs/ai-adoption.md)
- [Browse all documentation](docs/README.md)
- [Agent workflows](docs/agent-workflows.md)
- [Architecture rules](docs/policies/architecture.md)
- [Verification rules](docs/policies/verification.md)
- [Supply-chain rules](docs/policies/supply-chain.md)

Apache-2.0 licensed. See [LICENSE](LICENSE).
