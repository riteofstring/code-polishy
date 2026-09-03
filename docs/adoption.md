# Adopting Code Polishy

Adoption is complete when the target config describes real project facts and
`doctor --strict` can prove that shared conditional policy modules plus the
remaining project-specific providers cover every executable module.

For agent-driven setup, including exact-tag selection, source installation, and
target configuration, use the copy-paste invocation and runbook in
[AI-Agent Setup and Adoption](ai-adoption.md). The numbered guide below
remains the detailed policy reference and is also linked from that runbook.

## 1. Inventory the project

Before editing config, list:

- every executable language, manifest, lockfile, and package root;
- every Python `.py` and `.pyi` file, each contained `pyproject.toml` project,
  its `src` layout when present, nested project boundaries, and project-local
  `.venv` directories;
- modules that own meaningful concepts, not merely folders named `utils`;
- dependency direction between those modules;
- generated, vendored, archived, and build-output paths;
- existing package roots, project-specific tools, and target-local copies of
  tooling the installed Code Polishy release already supplies;
- build commands and checks that are genuinely product-specific;
- focused tests for each module and whole-repository workflows;
- backend, frontend, UI, visual, content, CLI, or other product capabilities;
- databases, filesystems, remote owned services, and true third parties;
- separately owned repositories, directories, files, and services, plus every
  CLI/config/environment/default source used to locate them;
- cross-language contracts and their authoritative owner.

Inspect what existing green scripts actually select. A command name such as
`quality` or `test` is not evidence that all packages are covered.

For Python, treat the nearest contained `pyproject.toml` as the project owner.
A nested project owns its own source and `.venv`; do not let an ancestor
project absorb it. The shared inventory handles flat, `src`, regular-package,
and namespace-package layouts. Give every project a `project.requires-python`
range with a minimum stable minor supported by the pinned Ruff. Record
target-specific build or product checks, but remove any target Python
architecture command: the built-in import graph enforces module direction.
[Architecture Policy](policies/architecture.md) owns the exact resolution and
coverage rules.

## 2. Lock the release

Code Polishy is installed on the machine from an exact reviewed version tag;
see [Installation](installation.md). From the target repository, require the
exact release it will run and start from the minimal configuration:

```sh
"${HOME}/.local/share/code-polishy/releases/<version>-<releaseDigest>/bin/code-polishy" lock
cp <code-polishy-checkout>/templates/minimal/.code-polishy.json .code-polishy.json
```

`.code-polishy.lock.json` and `.code-polishy.json` are the only policy files the
repository checks in. Never vendor the engine or copy individual checker files;
that creates divergent policy forks.

## 3. Declare project capabilities

`project.kind` is descriptive and open-ended. `project.capabilities` activates
coverage requirements that are meaningful across stacks:

```json
{
  "project": {
    "kind": "application",
    "capabilities": ["backend", "frontend", "ui", "visual"]
  }
}
```

Use `content` for content-specific full validation, `cli` for a command-line
surface, and other lowercase identifiers when a target config needs them.
Unknown capabilities remain descriptive; built-in capabilities have the test
requirements documented in the verification policy.

## 4. Model modules and direction

Each executable file must match exactly one module. A module may contain more
than one language when it genuinely owns one concept, though separate runtime
or deployment boundaries are often clearer as separate modules.

```json
{
  "modules": [
    {
      "name": "knowledge-domain",
      "paths": ["internal/knowledge/**", "web/src/knowledge/domain/**"]
    },
    {
      "name": "knowledge-delivery",
      "paths": ["internal/http/knowledge/**", "web/src/knowledge/ui/**"],
      "dependsOn": ["knowledge-domain"]
    }
  ]
}
```

The graph must be acyclic. If two modules depend on each other, find the shared
concept owner or deepen them into one boundary; do not encode the cycle.

For Go, JavaScript, TypeScript, and Python, Code Polishy extracts imports and
enforces the graph itself. Python source is assigned to its contained project
before the graph is read, so a nested project and an ancestor with overlapping
names do not share resolution. For other languages, connect a target-native
architecture checker through `checks` with the `architecture` capability.

### Map current design rationale

Use the optional `documentation.design` index for concise, current rationale
that a coding agent needs before changing a module or a particular source file.
Each entry owns one Markdown file under `docs/design/` and selects exactly one
module or a bounded set of exact source paths:

```json
{
  "documentation": {
    "design": [
      {
        "path": "docs/design/knowledge-domain.md",
        "module": "knowledge-domain"
      },
      {
        "path": "docs/design/knowledge-import.md",
        "sourcePaths": ["internal/knowledge/import.go"]
      }
    ]
  }
}
```

Each document, module, and direct source path has one owner. A direct source
mapping replaces its module mapping for that file, so an agent receives at
most one current design document per selected file. Mapped files must be
contained, regular Markdown; direct sources must be contained, regular,
governed source owned by exactly one module.

Resolve only the documents relevant to the work:

```sh
code-polishy design-context --files internal/knowledge/import.go
code-polishy design-context --module knowledge-domain
```

The command prints stable repository-relative document paths and nothing else.
An empty result is valid when the selected source has no non-local rationale.
Plans, historical evidence, and superseded decisions stay outside this index;
open them only when the task specifically requires them.

Markdown that is consumed by the product, a build, a generator, a test fixture,
or an agent prompt must not use the default documentation-only merge level.
Declare each such exact input explicitly:

```json
{
  "documentation": {
    "productInputs": ["prompts/system.markdown", "content/runtime.md"]
  }
}
```

Entries are unique, contained, repository-relative Markdown file paths. They
only escalate verification; they cannot add files to the documentation lane or
weaken a check. `AGENTS.md`, `CLAUDE.md`, `SKILL.md`, and Markdown under any
`skills` or `templates` directory are built-in control inputs and need no
declaration.

### Declare external inputs

Add `portability.externalInputs` for separately owned repositories,
directories, files, or services. Each declaration names its owning module,
source files, ordered resolution sources, unavailable behavior, a quick focused
contract suite, and a distinct ordinary full behavior suite. A root-relative
sibling fallback is allowed only when it is explicit through
`siblingFallback` and those suites prove its behavior.

For `unavailableBehavior: warn`, the target should continue operating while
showing the resolved source and compatibility problem clearly and disabling
only the dependent feature. Do not translate a missing or mismatched input into
an apparently valid empty result. See
[Portability and External Inputs](policies/portability.md).

## 5. Define scope narrowly

- `scope.generated` remains discoverable. Generated executable source receives
  format validation, syntax/compiler, lint, dead-code, tool-coverage, and
  dependency-direction checks, while `format` never rewrites generator-owned
  bytes. It skips only edit-oriented text and complexity budgets.
- `scope.data` is the non-rewrite category for hand-written product data. Use
  narrow patterns that can match only `.json`, `.jsonc`, `.yaml`, or `.yml`, for
  example:

  ```json
  {
    "scope": {
      "data": [
        "product-data/**/*.json",
        "product-data/**/*.jsonc",
        "product-data/**/*.yaml",
        "product-data/**/*.yml"
      ]
    }
  }
  ```

  Data stays contained, owned, selected by tests and gates, UTF-8 and
  syntax-validated. Configuration rejects patterns that overlap executable
  source, custom-language source, generated or excluded paths, dependency
  manifests, locks, tool configuration, CI, Dockerfiles, or other protected
  control inputs. `format` leaves its bytes unchanged, and a target command
  cannot run in the `format` profile while `scope.data` is declared. See
  [Code Quality](policies/code-quality.md#hand-written-structured-data) for
  the parse-only behavior.

- `scope.exclude` is outside target selection. It is for dependencies, output,
  immutable vendor trees, or archives governed elsewhere.
- `scope.entryPoints` names governed source something outside the repository
  loads without importing it, so reachability analysis does not call it dead.
  Test files, `index`/`main`/`cli` modules at a package root or its `src`
  directory, and `*.config.*` modules beside a package manifest are already
  entry points; declare only what those conventions cannot cover, and declare it
  exactly rather than as a broad pattern.
- `scope.pythonDynamicReferences` is separate from path-level
  `scope.entryPoints`. It declares only a Python symbol Vulture cannot reach
  through imports or inferred PEP 621 `project.scripts`,
  `project.gui-scripts`, or `project.entry-points.*`. Each item is an exact
  `{project,module,symbol}` object: all fields are required, `project` is the
  canonical repository-relative contained-project `pyproject.toml` path, and
  `module` and `symbol` are identifier chains; wildcards are not allowed. A
  stale or ambiguous reference fails instead of preserving a symbol broadly.
  [Code Quality](policies/code-quality.md#python-ruff-vulture-and-ty) owns the
  full Python reachability contract.
- `scope.development` names governed source that never ships: build and tool
  configuration, scripts, and harnesses. Only that source and tests may import
  a package declared in `devDependencies`, so declare what the product does not
  carry rather than what merely runs outside production.

Declare source the engine does not recognize natively instead of letting it
look like inert data:

```json
{
  "scope": {
    "languages": [
      { "name": "elixir", "paths": ["lib/**/*.ex", "test/**/*.exs"] }
    ]
  }
}
```

Custom rules extend detection and cannot replace built-in language ownership.
A governed file matching two custom rules is a policy error.

Target exclusions cannot hide executable source, manifests, lockfiles, or CI
workflows. Inspect the result:

```sh
code-polishy list-files --all
```

Install the canonical agent guidance and workspace hygiene once after writing
the lock:

```sh
code-polishy agents install
```

The locked release owns the entire `AGENTS.md`. Installation creates a missing
canonical file or accepts an exact existing copy idempotently; it preserves and
reports a conflict for any existing noncanonical file. It also installs the
release's exact one-line `CLAUDE.md` import (`@AGENTS.md`) when absent and
appends the exact `/.code-polishy-reports/` rule to the root `.gitignore` when
needed, preserving all existing rules, line endings, and file mode. A differing
import or a non-regular target is preserved as an explicit conflict, so no
planned adoption change is written. On policy upgrades, run `agents sync`; it
requires an existing `AGENTS.md`, replaces all stale guidance bytes, upgrades
the exact former managed Claude redirect, and repairs a missing report-artifact
rule in the same transaction. `doctor --strict` rejects a missing, stale, or
conflicting adoption surface.

## 6. Inspect conditional policy modules

Do not copy standard quality commands into every target. Run:

```sh
code-polishy doctor --strict
```

The inventory notes show which shared modules were detected and why. The
standard conditional behavior is:

- Python source activates policy-owned Ruff formatting, sealed `B`, `C4`, `E`,
  `F`, `I`, `PIE`, `RUF`, `SIM`, and `UP` lint, and C901 complexity. Ruff uses
  the project's declared Python target, detected package roots, and the shared
  88-character line length. Vulture `2.16` provides full-project dead-code
  analysis at fixed 60% confidence through carried CPython
  `3.12.13+20260728` from python-build-standalone; `ty` provides first-class
  type checking. The shared project inventory assigns nested projects
  separately and recognizes direct `src` plus in-tree PEP 517 backend roots and
  their direct `src`; ty receives those roots explicitly. A dependency-bearing
  project uses only its contained `.venv`; a missing environment produces a
  clear coverage finding instead of ambient-Python behavior. A target pins and
  installs neither tool, its own Python runtime, nor an architecture provider.
- A target bearing JavaScript or TypeScript is formatted by the sealed,
  policy-owned JavaScript bundle; it pins and installs no formatter itself.
- A target bearing JavaScript or TypeScript is linted by that same bundle
  under the shared complexity, depth, and parameter budgets; it pins and
  installs no linter or lint plug-in either.
- TypeScript source is type checked by that same bundle under the target's own
  `tsconfig*.json`, read as contained JSON/JSONC data; it pins and installs no
  compiler either.
- A declared React dependency adds Hooks policy, JSX accessibility policy for web React, and a full
  component/browser/E2E test requirement.
- A declared Electron dependency adds a full Electron/browser/E2E test
  requirement.
- A supported dependency graph adds policy-owned OSV scanning to the online
  vulnerability profile.

`ty` reports each diagnostic as its own exact finding. For brownfield debt,
copy that finding's check, path, and fingerprinted subject into one owned,
justified, expiring exception; do not suppress the command or use an error-count
ceiling. See
[Python Ruff, Vulture, and ty](policies/code-quality.md#python-ruff-vulture-and-ty).

JavaScript and TypeScript dead code comes from their sealed bundle across the
whole package tree a file belongs to; a target pins and installs no analyzer.
Python dead code comes only from Vulture. Its version-matched standard-library
whitelists, in-tree PEP 517 hooks, and PEP 621 entry points are inferred; use
exact `scope.pythonDynamicReferences` only for remaining dynamic symbols rather
than substituting `scope.entryPoints` or a Vulture ignore. This is imported
policy, not a target-authored adapter.

An incorrect activation may be disabled only by an exact-root
`policyModules.overrides` entry with `mode: "disabled"`, `reason`, `owner`, and
an expiry within one year. Force-enable a module with `mode: "enabled"`; enable
overrides do not carry exception metadata. There is no repository-wide disable.

## 7. Connect only project-specific checks

Use `checks` only for a capability a conditional module or built-in checker
cannot honestly infer, such as a production build, a frozen lock install, a
custom content schema, or a product-specific security check. It is not a way to
run a formatter, linter, type checker, or dead-code analyzer of your own: a
check declaring one of those for source Code Polishy already decides is refused
rather than run beside the built-in one. Each command declares what it proves,
which modules it covers, and in which profile it may run:

Remove target-selected Python architecture, complexity, typecheck, and dead-code
providers when adopting this release. Built-in Ruff graph evidence, Ruff C901,
Vulture, and `ty` own those capabilities directly; no legacy provider or
transition layer runs beside them.

```json
{
  "name": "frontend-build",
  "provides": ["build"],
  "argv": ["pnpm", "build"],
  "cwd": "frontend",
  "modules": ["frontend"],
  "runOn": ["build"],
  "environment": [],
  "timeoutSeconds": 900
}
```

Commands are argument arrays, never shell strings. `cwd` and checked-in
executables must resolve inside the target repository. `paths` are optional
change triggers; `modules` are capability coverage and change triggers. No
paths are appended to `argv` automatically.

Commands receive a small operational environment (`PATH`, home/temp/locale,
CI, and Go build-cache variables), not every credential in the policy process.
List additional variable names in `environment` only when that command needs
them; values remain outside config and are never printed by Code Polishy.

Common capabilities are:

- `format`, `lint`, `typecheck`, `complexity`, `dead-code`, `architecture` for
  code health;
- `build` for production compilation;
- `security` for ecosystem vulnerability, SAST, repository secret, license, or
  provenance scans. Declared container artifacts use the first-class
  artifact-security surface below rather than a custom Trivy wrapper. A pnpm
  project that declares `supplyChain.allowedLicenses` needs no license provider:
  Code Polishy reads what its installed dependencies declare after a normal
  frozen, script-disabled installation on the current host. Do not use
  `--force` just to materialize foreign-platform optional packages; run the
  ordinary gate on that compatible platform when its complete license evidence
  is required.
- `lock-sync` for a frozen native install or equivalent proof that manifests
  and lockfiles agree. A pnpm project needs none: Code Polishy reads its lockfile
  itself.
- `dependency-policy` and `release-age` when the engine does not have a shared
  module for the target package ecosystem.
- `security-monitoring` when `supplyChain.recurringSecurityMonitoring` opts a
  GitLab or another non-GitHub CI repository into proving that its server-side
  schedule runs the online security profile at least weekly. It is an external
  provider, not a claim that static pipeline YAML can prove cadence; use the
  success and unavailable-evidence contract in
  [Portability and External Inputs](policies/portability.md#external-security-monitoring-evidence).

For a dependency ecosystem whose manifest is not recognized by the engine,
add `custom-dependencies` to the owning module's `capabilities`. This makes all
four dependency providers (`dependency-policy`, `lock-sync`, `release-age`, and
`security`) mandatory rather than relying on filename guesses.

Use `supply-chain` for local/frozen checks, `supply-chain-online` for registry
freshness, and `security` for vulnerability or provenance services. Online
policy runs all three profiles; offline policy runs only `supply-chain`.
Built-in Go dependency commands can receive explicitly named variables through
`supplyChain.environment` when a private ecosystem requires them. Node and pnpm
facts come from the sealed JavaScript bundle instead, which runs under an
environment built from nothing, so nothing named there reaches it.

The shared release-age baseline is a 30-day hard minimum across supported
resolved graphs and declared standalone executables. The dependency baseline
also requires low-or-above native vulnerability audits plus structured OSV and
sets a non-blocking 90-day preference for new direct runtime dependencies
during `dependency-review --base REF`. A release younger than 30 days requires
an exact typed `releaseAgeAssessment`. A low or moderate vulnerability requires
a separate exact `vulnerabilityAssessment` with independent approval and a
severity-bounded expiry. A high finding can only be an exact `not-affected`
decision with a `false-positive` or `unreachable` basis and a 30-day maximum;
it is never high risk acceptance. General exceptions cannot waive either
control, and critical, unknown-severity, or known-exploited findings remain
blocking.

Go, Shell, Python, and common Node stacks gain the built-in or conditional
coverage above. Any capability still shown as missing needs a module-scoped
provider; a generic parser should not pretend to understand an unknown
framework's compiler, aliases, generated modules, or dependency graph.

## 8. Declare container artifacts

For a conventional image, declare the build inputs and let Code Polishy own the
scanner:

```json
{
  "supplyChain": {
    "artifactSecurity": {
      "targets": [
        {
          "name": "worker-image",
          "module": "worker-delivery",
          "mode": "dockerfile",
          "dockerfile": "deploy/worker.Dockerfile",
          "context": ".",
          "platform": "linux/amd64",
          "openVex": "security/worker.openvex.json"
        }
      ]
    }
  }
}
```

`archive` mode captures a bounded existing Docker/OCI archive. For a complex
product build, `command` mode runs a target-owned producer and supplies
`CODE_POLISHY_ARTIFACT_OUTPUT`; the producer writes only a version-1 manifest
and archive beneath that private directory. Scanner provenance, database
refresh/freshness, isolation, Trivy invocation, OpenVEX reconciliation, SBOMs,
reports, hashes, and cleanup remain shared. See
[Artifact Security](artifact-security.md).

## 9. Define ordinary and supplemental test suites

Module suites are the narrow iteration interface:

```json
{
  "name": "knowledge-domain-unit",
  "kind": "unit",
  "scope": "module",
  "cost": "quick",
  "modules": ["knowledge-domain"],
  "argv": ["./scripts/go.sh", "test", "./internal/knowledge/..."],
  "runOn": ["focused", "recommended", "full"]
}
```

Standard repository suites can join the impact-based recommended set:

```json
{
  "name": "application-integration",
  "kind": "integration",
  "scope": "repository",
  "cost": "standard",
  "argv": ["./scripts/test_integration.sh"],
  "paths": ["cmd/**", "internal/**", "frontend/**"],
  "runOn": ["recommended", "full"]
}
```

Expensive repository suites remain full-only:

```json
{
  "name": "browser-workflows",
  "kind": "browser",
  "scope": "repository",
  "cost": "expensive",
  "argv": ["pnpm", "test:browser"],
  "cwd": "frontend",
  "runOn": ["full"]
}
```

Mutation evidence is optional. A target that deliberately opts into it keeps
the suite isolated from the ordinary profiles and may make the choice explicit
with `tests.requiredSupplementalKinds: ["mutation"]`:

```json
{
  "name": "knowledge-domain-mutation",
  "kind": "mutation",
  "scope": "module",
  "cost": "expensive",
  "modules": ["knowledge-domain"],
  "argv": ["./scripts/test_mutation.sh", "knowledge-domain"],
  "runOn": ["supplemental"],
  "timeoutSeconds": 3600
}
```

Declaring a supplemental suite or `tests.requiredSupplementalKinds` records
evidence required for a selected hardening event; neither declaration schedules
the suite. Initial adoption, ordinary edits, changed tests, checkpoint and
merge gates, guidance synchronization, and a Code Polishy lock upgrade leave
supplemental hardening `NOT RUN`. Run it only when the caller explicitly
requests it, a checked-in workflow explicitly invokes it for that event, or the
release checklist selects a stable release candidate.

Every module needs a focused suite. Content repositories can use schema, link,
cross-reference, or publication-contract validation instead of conventional
unit tests. Every repository also needs a full repository suite.

Gherkin is optional. If the repository contains governed `.feature` files,
declare a repository full `acceptance` or `gherkin` suite. Supplemental
`acceptance-mutation` or `gherkin-mutation` is optional hardening. Do not check
in feature text that no executable pipeline consumes.

Useful commands while working are:

```sh
code-polishy test --module knowledge-domain
code-polishy test --changed
code-polishy test --suite browser-workflows
code-polishy test-levels --base origin/main
code-polishy test --recommended --base origin/main
code-polishy test --all
code-polishy checkpoint-gate --base PREVIOUS_CHECKPOINT
code-polishy merge-gate --base origin/main
```

Use `code-polishy test --supplemental` only for a caller-selected hardening
stage, an event-specific checked-in workflow, or the stable-candidate release
checklist. It is not a routine command merely because supplemental evidence is
declared.

For a candidate containing only ordinary Markdown, run
`code-polishy format --git-changes` and skip application tests. The final merge
gate automatically selects the built-in documentation contract with zero
application suites; it does not require target opt-in or user authorization.

For unattended work, or when the caller explicitly requests isolation, let the
caller choose writable modules before the worker starts and reuse one
disposable session for setup, implementation, and all of those tests:

```sh
code-polishy task-session \
  --module knowledge-domain --promote \
  -- ./scripts/run-autonomous-task.sh
```

The boundary uses module paths from the exact starting commit. A candidate
cannot widen itself by editing config or an environment variable. Repositories
whose governed material is prose—such as an Obsidian vault—use the same model:
declare each writable folder as a module and leave other notes, attachments,
and `.obsidian/` outside the selected modules. See
[Isolated Task Sessions](task-sessions.md).

An explicit `--module` stays narrow. `--changed` maps Git changes to modules and
also runs focused suites for reverse dependents, because a domain change can
break unchanged delivery code. `--recommended` adds matching quick and standard
repository suites. `--all` runs every suite in the full profile, including
expensive browser/visual/E2E suites. It intentionally excludes supplemental
mutation and risk analysis. Run `code-polishy test --supplemental` as a
separate hardening stage only when the caller explicitly requests it, a
checked-in workflow explicitly invokes it for that event, or the release
checklist selects a stable release candidate. Credentialed, destructive, and
live-provider probes remain external approval gates.

For browser and visual suites, use the target's real Chrome/Chromium, Electron,
Playwright, screenshot-diff, or hosted visual system. Keep deterministic browser
workflows separate from live third-party-provider tests.

## 10. Make architecture claims executable

For each material workflow, record:

1. the concept and its one owner;
2. invalid states that should not be constructible;
3. where untrusted values become refined domain values;
4. the public module interface and hidden machinery;
5. atomic side-effect boundaries;
6. ports for owned remote services and true third parties;
7. generated contract consumers;
8. focused boundary and full workflow evidence.

Dependency rules prevent imports from bypassing the design. Compilers,
constructors, transactions, and tests prevent invalid state and partial
mutation. No single folder convention can provide all four.

## 11. Establish the baseline

Fix deterministic failures immediately: formatting, invalid syntax, floating
dependencies, missing lockfiles, unpinned Actions, and missing module coverage.

For debt that cannot move safely in one change, use one exact exception with a
check, path, subject, reason, owner, and expiry. Exceptions cannot suppress
policy coverage or tool prerequisites.

```sh
code-polishy doctor --strict
code-polishy check --all
code-polishy test --all
code-polishy supply-chain --offline
code-polishy gate
```

## 12. CI and authorization

CI runs the same installed command as local development, against the exact
release the lock names:

```yaml
- name: Run complete repository policy gate
  shell: bash
  run: code-polishy gate
```

Every repository gets the documentation-only merge level by default. For other
candidates, most repositories should keep the complete gate. A mixed
content-and-code repository may opt in to
`verification.mergeGate.recommendedModules` and run
`code-polishy merge-gate --base "$TRUSTED_BASE_SHA"`. CI, not an agent,
must derive that SHA from the pull-request base or push-before event. The
command accepts no file list or requested level and automatically escalates
control, product-input, mixed, policy, dependency, workflow, container,
unowned, non-allowlisted, and broad-impact changes to the complete gate.
Require the resulting status for merge.

Each checkpoint or merge gate that executes work writes a managed versioned JSON
report and bounded command logs below `.code-polishy-reports/<gate>/`. The
report is the durable record of the command plan, exact base and candidate,
release/configuration identity, structured outcomes, findings, and final
status. Terminal output stays concise: phase progress, a bounded failure tail,
and the affected log path. Retain the JSON report and logs when CI needs audit
evidence; do not make archived verbose terminal output the audit surface.

`merge-gate --base REF --resume` is available only after a failed merge-gate
report with the same content identity. It reuses eligible successful ordinary
test suites only. Checks, builds, supply-chain and artifact-security phases,
behavior-proof replays, failed phases, and phases without a valid receipt run
again. A changed exact base, candidate, release, configuration, command plan,
platform, or declared command environment invalidates reuse. A normal merge
gate does not reuse prior work.

### Keep behavior and final-state evidence in custody

This workflow is experimental; its installed-release Unix and native Windows
acceptance contracts and real multi-repository dogfood must meet the release
checklist.

At the start of a source task, the agent harness should pass the user's original
request and supplied acceptance criteria to `behavior-review capture-intent` at
the task-base commit, then repeat capture before acting on each correction.
Correction capture accepts staged, unstaged, deleted, and untracked candidate
state and records its digest. Capture is cheap and invokes neither tests nor an
AI reviewer. Repositories may define named features at `on-request`, `merge`,
or `checkpoint`; users may select configured features during capture or append
them later with `behavior-review require`.

After the candidate is committed, use `behavior-review status --base TASK_BASE`
to inspect the decision. `NOT RUN` means optional review was skipped. When
review is required, start a review subagent with no inherited conversation and
give it only the generated packet. If the harness cannot start subagents, use a
separate clean AI invocation with only that packet.

When selected review crosses CI jobs, CI must retain the complete
`.code-polishy-reports/behavior-review` directory from intent capture through
preparation, proof, finalization, and the applicable gate, or transfer it as an
explicit trusted artifact. Checkpoint and merge gates replay every cited proof
and force the selected feature suites; the primary agent or harness must keep
the review subagent packet-only. Repositories with no behavior-review policy
need no AI artifact and retain their ordinary gate runtime. See
[Behavior and Final-State Review](policies/behavior-review.md) before relying on
the workflow.

The runner must already have the locked release installed; Code Polishy is
never downloaded during a check. Bootstrap target dependencies from frozen
inputs before strict doctor, because doctor verifies that declared executables
exist.

Agents can inspect the exact installed operating contract with
`code-polishy docs list`, locate a topic with `docs find`, and read it with
`docs read`. The stable launcher uses the repository lock to select the exact
release and the commands perform no network access.

A repository may reserve direct profile commands for explicit non-merge
workflows. Install the guidance from `templates/AGENTS.md`: routine focused or
changed tests during source work; `test --changed --base TASK_BASE` for a
task-bound comparison; `checkpoint-gate --base PREVIOUS_CHECKPOINT` after each
completed committed task on a long-lived branch; documentation formatting
without application tests for ordinary Markdown; then one
`merge-gate --base MERGE_TARGET` for the final candidate. A checkpoint gate
runs changed-scope verification and records the accepted HEAD. A merge gate
selects and executes documentation, recommended, or full ordinary verification
without making the selection a human approval prompt. Its test reminder keeps
the merge-wide count and, when the current candidate has a valid checkpoint
receipt, also shows the latest task slice and its base.
`test-levels` remains a read-only diagnostic, and it lists supplemental quality
separately without selecting it. Credentialed, destructive, production-mutating,
and live-provider probes remain typed external approval gates. Direct profile
commands always mean their complete declared profile, not a best-effort subset.

## 13. Upgrade intentionally

The [AI-Agent Setup and Adoption upgrade procedure](ai-adoption.md#upgrades) is
the authority for both agent-driven and manual upgrades. Select and verify one
exact annotated version tag, install that release, read the intervening
`CHANGELOG.md` entries, rewrite `.code-polishy.lock.json` from the exact release,
adapt the target configuration, and run the required ordinary verification. A
lock upgrade leaves supplemental hardening `NOT RUN` unless the caller, an
event-specific checked-in workflow, or the stable-candidate release checklist
selects it. Never install from floating `main` or let a check select a release the lock does not
name.

The outgoing lock and guidance retain authority through candidate verification.
The exact incoming release's `lock` command is the sole pre-cutover mutation;
it atomically replaces the lock, after which incoming guidance governs all
configuration, verification, and delivery work.
