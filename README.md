![Code Polishy](./code-polishy-banner.png)

# Code Polishy

**One policy for all your repos. Deterministic constraints agents can't
ignore.**

Code Polishy gives every coding agent the same definition of done and enforces
it before code merges.

## What it does

Code Polishy keeps today's agent code from becoming tomorrow's cleanup.

- Stops agents from tangling parts of your codebase together.
- Flags giant files and hard-to-follow functions before they become expensive
  to change.
- Makes "done" include the tests, builds, and project checks your repo requires.
- Protects your software supply chain from surprise dependency changes and
  known vulnerabilities.
- Shows where repository space goes and what caused its growth.

Agents catch problems while the change is still fresh, and one final gate stops
unresolved issues before merge.

For sensitive changes, an optional experimental
[behavior review](docs/policies/behavior-review.md) can compare the result with
the user's request.

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

After adoption, a developer cloning the repository needs one setup command:

```sh
./code-polishyw setup
```

PowerShell uses `.\code-polishyw.ps1 setup`. The small checked-in wrapper reads
the repository lock, reuses or installs that exact release in the shared user
store, and does not contain the Code Polishy toolchain itself.

## How agents use it

A coding agent reads the workflow bundled with the repo's locked release, starts
a scoped task, checks the change, and runs the final gate at a merge checkpoint:

```sh
# Read the version-matched workflow
./code-polishyw docs read agent-workflows

# Start a scoped task
./code-polishyw task-start --module MODULE

# Check the code you changed
./code-polishyw test --changed

# Review dependency risk before accepting an update
./code-polishyw dependency-review --base origin/main

# Enforce the policy at a merge checkpoint
./code-polishyw merge-gate --base origin/main
```

The locked [agent workflow](docs/agent-workflows.md) covers long-lived branch
checkpoints, behavior review, failed-gate recovery, and CI evidence transfer.

## Languages

Code Polishy keeps its built-in tools fixed until you upgrade, so local and CI
checks stay consistent. Built-in support covers Go, JavaScript, TypeScript,
Python, and shell scripts.

- **Go, JavaScript, and TypeScript:** Formats code and catches likely bugs,
  type errors, unused code, and overly complex functions. Locked dependencies
  are checked for known vulnerabilities.
- **Python:** Formats and lints each contained project, then checks complexity,
  dead code, types, module direction, and locked dependencies.
- **Shell scripts:** Catches syntax errors and common safety problems.

Code Polishy also rejects empty Go tests and obvious test commands that can
pass without running tests. Optional mutation testing can provide deeper proof.

Other languages can use reviewed [language packs](docs/adding-a-language.md) or
repo-owned checks. Code Polishy runs and enforces them but does not supply their
tools.

## What stays in your repo

Four checked-in files keep every agent aligned:

- `.code-polishy.lock.json` keeps the policy and tools stable until you choose
  to upgrade.
- `.code-polishy.json` describes your code boundaries, tests, commands, and
  exceptions once.
- `AGENTS.md` gives every coding agent the same operating instructions.
- `CLAUDE.md` imports those instructions for Claude Code.

Your prompts can stay focused on what you want built.

## More

- [All docs](docs/README.md)
- [Agent workflows](docs/agent-workflows.md)
- [Repository size](docs/repository-size.md)
- [Architecture rules](docs/policies/architecture.md)
- [Test rules](docs/policies/verification.md)
- [Dependency rules](docs/policies/supply-chain.md)

Apache-2.0 licensed. See [LICENSE](LICENSE).
