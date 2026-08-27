# Conditional Policy Modules

Conditional policy modules keep standard rules in the pinned Code Polishy
checkout. A consuming repository imports the policy once; it does not copy the
same Ruff, React, Electron, or OSV command definitions into local config.

## Compilation model

At startup the engine inventories governed files and package roots, then
compiles detected policy modules into internal commands and coverage contracts:

```text
governed files + manifests + exact dependencies
                     |
                     v
          conditional module registry
                     |
          +----------+-----------+
          |                      |
          v                      v
  managed quality/security   test and config
        commands              requirements
```

Managed commands use the same contained working directories, restricted child
environment, argument-array execution, timeouts, module triggers, and report as
target-declared commands. They are internal fields and cannot be forged or
modified through `.code-polishy.json`.

No conditional module runs a Node binary. Every JavaScript and TypeScript tool
Code Polishy needs lives in the sealed bundle, so a target's own dependency
install exists to let those tools read declarations and resolution metadata,
never to supply an analyzer.

`doctor --strict` prints each active module, exact root, and evidence. A missing
tool, unsupported package manager, absent framework rule, or missing behavior
suite is a finding rather than a skipped check.

## Built-in registry

### Ruff

Any governed `.py` or `.pyi` file activates Ruff. The root is the nearest
ancestor `ruff.toml`, `.ruff.toml`, or `pyproject.toml` containing `[tool.ruff]`,
falling back to the repository root.

The module uses policy-owned Ruff `0.16.0` for:

- selected-file `ruff format --check` during `check` and `gate`;
- selected-file `ruff check --no-fix` during `check` and `gate`;
- selected-file `ruff format` during `format` or `fix`.

It supplies format, lint, and unused/dead-code coverage. Python type checking,
domain architecture, and build semantics still need an applicable shared
module or project-specific provider; Ruff must not be mislabeled as proof it
does not provide.

### The Node quality baseline

Governed JavaScript or TypeScript activates no quality module at all. The
sealed, policy-owned JavaScript bundle owns formatting, linting, complexity,
type checking, dead code, and the import facts module direction is decided from,
so no target pins or installs Prettier, ESLint, a lint plug-in, the TypeScript
compiler, or Knip, and none of them can be selected or replaced.

Production and test source are linted under separate budgets. Both keep the
shared complexity threshold; tests default to depth/parameter limits of eight
while production keeps four/five. Targets may tighten each independently.

The target still owns `tsconfig*.json` because the language level, libraries,
and strictness of a codebase are facts about that codebase. Code Polishy resolves
which TypeScript project governs a file itself — the nearest one above it — and
reads it as contained JSON/JSONC data rather than running a target compiler.
Dead-code reachability works the same way: each governed file belongs to the
nearest package above it, entry points come from Code Polishy's own conventions
plus `scope.entryPoints`, and no analyzer configuration is read from the target.
That same nearest package decides which dependencies a file may import, and
`scope.development` names the source that never ships and may therefore reach a
development dependency.

Vue, Svelte, and Astro files trigger the shared Node tools but do not let those
commands claim module-level quality coverage; their framework-aware compiler,
lint, and type contracts remain explicit project facts.

### React

`react` in any dependency group activates React policy. It activates the
mandatory Hooks rules in the sealed bundle's lint configuration over the
declaring package's source, and the central `jsx-a11y` baseline as well when
`react-dom` exists. The bundle supplies ESLint and both plug-ins, so the target
pins none of them, ships no lint configuration, and neither chooses nor repeats
the shared rules.

Static linting does not prove interaction behavior. A detected React module
therefore requires a repository-scoped full component, browser, or E2E suite.

### Electron

`electron` in any dependency group activates Electron policy. It does not
require a checked-in ESLint configuration that separates the main, preload,
and renderer scopes, because a target supplies no lint configuration at all and
the sealed bundle does not model Electron process scopes. Main/preload/renderer
separation is enforced as module boundaries in the architecture graph.

Electron activation requires a repository-scoped full suite with kind
`electron`, `browser`, or `e2e`. Main/preload/renderer module boundaries and
IPC contracts remain explicit project facts in the architecture graph.

### OSV

A supported dependency manifest or lockfile activates policy-owned
OSV-Scanner `v2.4.0`. It runs recursively in the online `security` profile in
addition to the native pnpm audit and Go `govulncheck`. The release binary is
selected by OS/architecture and verified against a reviewed SHA-256 checksum.

OSV sends dependency identities to its advisory services; it does not replace
the first-class Trivy/OpenVEX artifact module or project-specific repository
secret, SAST, license, and provenance checks.

### Test strength

Mutation testing is optional target-selected hardening rather than an
automatically activated conditional module. A target may declare mutation
suites and may require the kind through `tests.requiredSupplementalKinds`.
Build tags, workspace roots, thresholds, and mutation commands remain
project-specific facts.

The policy does not infer that a formatter, coverage command, dry run, or
successful script is mutation evidence. `kind: mutation` must execute an actual
mutator and fail when its enforced threshold is missed. A repository-scoped
suite covers multiple modules only when its path contract covers all their
production files.

The policy tool installer includes checksum-pinned Gremlins `v0.6.0` for this
repository's own opt-in Go self-test. Targets that choose mutation hardening
must pin a compatible engine in the appropriate ownership boundary. An engine
that cannot be installed under the target's supply-chain policy is skipped; its
absence does not block adoption unless the target explicitly requires it.

Checked-in `.feature` files trigger a related executable-specification
contract: full acceptance execution. Acceptance mutation is optional local
supplemental hardening invoked through the separate `test --supplemental`
stage. Merely storing Gherkin does not count as test coverage.

## Exact overrides

Automatic detection is the baseline and has no global disable switch. When
repository evidence is genuinely misleading, disable one module at one exact
root:

```json
{
  "policyModules": {
    "overrides": [
      {
        "name": "electron",
        "root": "fixtures/package-metadata",
        "mode": "disabled",
        "reason": "fixture reproduces an upstream manifest and is never built",
        "owner": "quality-team",
        "expires": "2026-10-01"
      }
    ]
  }
}
```

A disabled override requires a non-empty reason and owner and expires within
one year. `mode: "enabled"` forces a module at an exact root when conventional
evidence cannot express it and must not include exception metadata.

An override changes only that conditional module. It does not suppress core
language ownership, test, quality, architecture, dependency, or tool coverage;
missing replacement evidence remains visible.
