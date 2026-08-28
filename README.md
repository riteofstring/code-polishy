![Code Polishy](./code-polishy-banner.png)

# Code Polishy

**One policy for all your repos. Deterministic constraints agents can't
ignore.**

Each repo describes its code boundaries, tests, and commands. Code Polishy
applies the same policy with the same locked tools, so the same code gets the
same result.

## What it does

- Keeps code inside your module boundaries.
- Flags giant files, deep nesting, and overly complex functions.
- Runs required tests, builds, and project checks.
- Checks dependency pins, lock files, and known vulnerabilities.
- Requires dependency releases to be at least 30 days old.

The policy ships with Code Polishy. The project details live in your repo.
Agents get fast feedback while they work. One final gate checks the code before
merge.

## Set it up

Open a coding agent in your repo. Paste this:

```text
Set up Code Polishy in this repository. Follow
https://github.com/riteofstring/code-polishy.
```

The agent keeps any version already locked by the repo. For a new setup, it
uses the latest stable version tag. Ask for a tag such as `v1.2.3` when you need
a specific version.

Git is required. Allow about 1 GB of disk space. Windows works without WSL or
Git Bash.

See the [agent setup guide](docs/ai-adoption.md) or the
[manual setup guide](docs/installation.md) for the full process.

## Use it

```sh
# Fast checks while coding
code-polishy test --changed

# Review a dependency update
code-polishy dependency-review --base origin/main

# Final check before merge
code-polishy merge-gate --base origin/main
```

Fix the reported problem. Rerun the smallest useful check. Run the merge gate
again when the final code is ready.

## Languages and tools

Built-in checks currently cover Go, JavaScript, TypeScript, shell scripts, and
Python. Code Polishy ships exact versions of the tools it trusts. Agents,
developers, and CI use the same binaries.

- Go uses `gofmt`, `go vet`, Staticcheck, govulncheck, and built-in complexity
  checks.
- JavaScript and TypeScript use Prettier, ESLint, TypeScript, and Knip on a
  locked Node runtime.
- Shell scripts use Bash syntax checks and ShellCheck.
- Python uses Ruff.
- Dependencies use OSV-Scanner and package-manager audits.

Code Polishy also rejects empty Go tests and test commands that can report
success without running tests. Optional mutation tests check that changing real
code makes the test suite fail.

Other languages use commands declared by the repo. Code Polishy still decides
when they run and whether they passed.

## What lives in your repo

- `.code-polishy.lock.json` locks the exact Code Polishy release.
- `.code-polishy.json` defines your code boundaries, tests, commands, and
  exceptions.
- `AGENTS.md` tells coding agents how to work in the repo.

After setup, tell your agent what to build. `AGENTS.md` carries the rules.

## More

- [All docs](docs/README.md)
- [Agent workflows](docs/agent-workflows.md)
- [Architecture rules](docs/policies/architecture.md)
- [Test rules](docs/policies/verification.md)
- [Dependency rules](docs/policies/supply-chain.md)

Apache-2.0 licensed. See [LICENSE](LICENSE).
