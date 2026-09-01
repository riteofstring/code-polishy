![Code Polishy](./code-polishy-banner.png)

# Code Polishy

**One policy for all your repos. Deterministic constraints agents can't
ignore.**

Code Polishy gives every coding agent the same definition of done and enforces
it before code merges.

Current built-in support includes Go, JavaScript, TypeScript, Python, and shell
scripts.

## What it does

Code Polishy keeps today's agent code from becoming tomorrow's cleanup.

- Stops agents from tangling parts of your codebase together.
- Flags giant files and hard-to-follow functions before they become expensive
  to change.
- Makes "done" include the tests, builds, and project checks your repo requires.
- Protects your software supply chain from surprise dependency changes and
  known vulnerabilities.
- Can bind the user's exact request to named product features, then enforce an
  experimental opt-in [review subagent](docs/policies/behavior-review.md)
  workflow for sensitive behavior changes.

Agents catch problems while the change is still fresh, and one final gate stops
unresolved issues before merge.

## How it works

![Code Polishy architecture: agents, developers, and CI run repository changes
through shared policy checks, returning findings or a merge-ready
result.](./code-polishy-architecture.svg)

## Set it up

Setup starts with one prompt to a coding agent:

```text
Set up Code Polishy in this repository. Follow
https://github.com/riteofstring/code-polishy.
```

Each repo keeps its current Code Polishy version until you choose to upgrade.
A new setup uses the latest stable version tag. Ask for a tag such as `v1.2.3`
when you need a specific version.

Git is required. Allow about 1 GB of disk space. Windows x64 works without WSL
or Git Bash.

See the [agent setup guide](docs/ai-adoption.md) or the
[manual setup guide](docs/installation.md) for the full process.

## How agents use it

A coding agent runs these commands as it works:

```sh
# Read the documentation carried by this repo's Code Polishy release
code-polishy docs read agent-workflows

# Save the user's request before changing code; this runs no tests or AI review
code-polishy behavior-review capture-intent --intent-file REQUEST_FILE

# Inspect any configured or task-requested feature review
code-polishy behavior-review status --base TASK_BASE

# Check the code you changed
code-polishy test --changed

# Review dependency risk before accepting an update
code-polishy dependency-review --base origin/main

# Accept one completed task on a long-lived branch
code-polishy checkpoint-gate --base PREVIOUS_CHECKPOINT

# Enforce the policy before merge
code-polishy merge-gate --base origin/main

# After a failed merge gate, reuse only matching passed ordinary tests
code-polishy merge-gate --base origin/main --resume
```

Before coding, Code Polishy can bind the original request to the starting
commit. The checkpoint gate validates one completed task, and the merge gate
validates the whole branch. Each reports whether behavior review was optional,
required, passed, or failed; selected reviews replay their proofs and force the
configured feature suites. Gate reports stay under `.code-polishy-reports`.
Resume never reuses checks, builds, security work, or behavior proofs.
Agents can use `code-polishy docs list`, `docs find`, and `docs read` from any
directory to retrieve the exact documentation shipped with the running release.

## Languages

Code Polishy keeps its built-in tools fixed until you upgrade, so local and CI
checks stay consistent.

- **Go, JavaScript, TypeScript, and Python:** Formats code and catches likely
  bugs, type errors, unused code, and overly complex functions. Locked
  dependencies are checked for known vulnerabilities.
- **Shell scripts:** Catches syntax errors and common safety problems.

Code Polishy also rejects empty Go tests and obvious test commands that can
pass without running tests. Optional mutation testing can provide deeper proof.

Other languages can connect repo-owned checks. Code Polishy runs and enforces
them but does not supply their tools.

## What stays in your repo

Three checked-in files keep every agent aligned:

- `.code-polishy.lock.json` keeps the policy and tools stable until you choose
  to upgrade.
- `.code-polishy.json` describes your code boundaries, tests, commands, and
  exceptions once.
- `AGENTS.md` gives every coding agent the same operating instructions.

Your prompts can stay focused on what you want built.

## More

- Run `code-polishy docs list` for every version-matched documentation topic.
- [All docs](docs/README.md)
- [Agent workflows](docs/agent-workflows.md)
- [Architecture rules](docs/policies/architecture.md)
- [Behavior regression review](docs/policies/behavior-review.md)
- [Test rules](docs/policies/verification.md)
- [Dependency rules](docs/policies/supply-chain.md)

Apache-2.0 licensed. See [LICENSE](LICENSE).
