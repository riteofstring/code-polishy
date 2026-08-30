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
- Uses a [review subagent](docs/policies/behavior-review.md) to compare behavior
  before and after each non-documentation checkpoint or merge candidate, then
  independently replays its red/green regression proofs at the applicable gate.

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

The checkpoint gate validates one committed task against the previous accepted
commit, runs affected checks and tests, then records the passing HEAD. The merge
gate chooses how much final validation the whole branch needs. Fix any finding,
rerun the narrowest useful check, and rerun the applicable gate. Both gates keep
bounded command logs and a machine-readable run report under
`.code-polishy-reports`. `agents install` and `agents sync` keep that workspace
artifact root in the repository's `.gitignore`; CI retains required evidence
through an explicit artifact handoff. Resume never reuses checks, builds,
security work, or behavior proofs.

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

- [All docs](docs/README.md)
- [Agent workflows](docs/agent-workflows.md)
- [Architecture rules](docs/policies/architecture.md)
- [Behavior regression review](docs/policies/behavior-review.md)
- [Test rules](docs/policies/verification.md)
- [Dependency rules](docs/policies/supply-chain.md)

Apache-2.0 licensed. See [LICENSE](LICENSE).
