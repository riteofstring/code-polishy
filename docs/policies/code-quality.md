# Code Quality Policy

Code-quality automation should make defects visible while the relevant change
is still small. A gate is trustworthy only when its scope, tools, thresholds,
and skipped paths are explicit.

## Baseline budgets

The default policy is:

| Measure                                                | Production | Tests |
| ------------------------------------------------------ | ---------: | ----: |
| Go cyclomatic complexity (fails at)                    |         12 |    20 |
| TypeScript/JavaScript cyclomatic complexity (fails at) |         10 |    10 |
| TypeScript/JavaScript nesting depth (maximum)          |          4 |     8 |
| TypeScript/JavaScript parameters (maximum)             |          5 |     8 |
| File length (maximum lines)                            |      1,000 | 1,500 |

Generated, vendored, lock, and build-output files are excluded from
edit-oriented text and complexity budgets. Generated executable source still
receives formatting, syntax/compiler, lint, dead-code, coverage, and module
direction checks. Project-specific generated paths belong in
`scope.generated`; do not hide production code in that category.

Markdown (`.md` and `.markdown`, case-insensitive) is excluded from the file
length budget by default because document length is not evidence of a code
monolith. Markdown still receives final-newline and trailing-whitespace checks.

Tests receive a higher Go complexity and file-size budget because table-driven
fixtures and workflow setup are naturally larger. TypeScript/JavaScript tests
keep the same complexity threshold because nested test orchestration becomes
unreliable quickly, but receive explicit depth and parameter budgets instead of
silently inheriting production values. Targets may lower `maxTestDepth` and
`maxTestParams` independently.

These values are maximum shared budgets. A target may lower them, but cannot
raise them or disable a built-in checker.

A target also cannot replace one. A configured check may prove something no
built-in checker can honestly infer, but a check that declares `format`,
`lint`, `typecheck`, `complexity`, `dead-code`, or `architecture` for source
Code Polishy already decides is a `policy.builtInCapability` finding naming the
check and one file it covers. What the check reaches decides it: a command
reaching source no built-in checker decides — another language, another
ecosystem, file types the sealed formatter does not print, or the `.vue`,
`.svelte`, and `.astro` components the sealed bundle never parses — remains the
target's own, and a policy-owned managed command is Code Polishy running its own
module rather than a target selecting an implementation.

## Formatting

Formatting is deterministic and has separate read and write operations.

- `check` runs Go format validation, source hygiene, the sealed JavaScript
  bundle's formatter, and configured `format` providers.
- `format` runs Go's formatter, the sealed bundle's writer, and configured
  commands whose `runOn` includes `format`.
- A format check must not rewrite source.
- A writer must not be guessed for an unknown language.
- Text source ends in a newline and contains no trailing spaces or tabs.

Run Go through the repository's pinned wrapper when one exists. Python uses the
policy-owned Ruff module.

A target that bears JavaScript or TypeScript has its JavaScript, TypeScript,
JSON, CSS, HTML, Markdown, and YAML formatted by the sealed, policy-owned
JavaScript bundle. Code Polishy owns that configuration completely: a target
`.prettierrc`, `prettier.config.*`, or `.prettierignore` is never read and is
reported as unsupported. Generated files and lockfiles are never rewritten,
because their generator owns their bytes. A file the sealed formatter cannot
decide — an unknown file type, a symlink, a path that really names a file
outside the repository, a file that is not UTF-8 text, or one past the size
bound — is a specific coverage finding, never a silent pass. A
target without JavaScript or TypeScript never launches the bundle and formats
its own remaining file types with a configured `format` provider.

## Lint, type checking, and syntax

- TypeScript/JavaScript source is linted by the sealed, policy-owned JavaScript
  bundle. `lint` and `complexity` are therefore built-in capabilities: a target
  pins no ESLint, installs no plug-in, and declares no lint provider. Code
  Polishy owns the configuration completely, so an `eslint.config.*` or
  `.eslintrc*` file is never read and is reported as unsupported, and an inline
  `eslint-disable` or `eslint-enable` comment never takes effect and is
  reported. Except a specific finding through a Code Polishy exception instead.
  The sealed configuration runs the shared complexity, depth, and parameter
  budgets, with production and test source held to their own. A file the linter
  cannot parse or does not analyze is a coverage finding, never a silent pass.
- React activation adds the Hooks rules over the declaring package's source,
  and the central `jsx-a11y` baseline as well when the package declares
  `react-dom`. Activation is resolved by Code Polishy from the conditional
  policy modules; a target neither selects nor repeats those rules.
- TypeScript source is type checked by that same bundle, so `typecheck` is a
  built-in capability too: a target pins no compiler, installs none, and
  declares no type provider. The target still owns its `tsconfig*.json`,
  because the language level, libraries, and strictness of a codebase are facts
  about that codebase. Code Polishy decides which project governs which file —
  the nearest one above it, so a monorepo package is checked under its own
  settings — and requires that a governed file is actually covered. A project is
  read as contained JSON/JSONC data and never executed: an extension chain that
  leaves the repository, a compiler plug-in, and a project reference are each
  refused with their own reason, and a governed file the project's program never
  contained is a coverage finding rather than a pass. Whether a project is
  checked at all is Code Polishy's: `noCheck` asks the compiler to report nothing
  about a project's types, so it is decided here rather than declared. The
  program reads the repository and the bundle's own library declarations and
  nothing else, by what a path really names rather than how it is spelled, so a
  specifier or extension chain that leaves the repository — including through a
  link — resolves to nothing rather than pulling another tree into the check.
  The run emits nothing into the target tree.
- Go runs `go vet` with the pinned Go toolchain.
- Shell runs `bash -n` and the policy-pinned ShellCheck.
- Python automatically activates policy-owned Ruff formatting, correctness,
  and unused-code checks. Python type checking and architecture, plus Rust,
  Java, unknown frameworks, content schemas, SQL, protobuf, and other
  ecosystems use a target-native provider when no shared module exists.

Do not call a repository type-safe because JavaScript syntax parses or because
only a small handpicked directory is included. Production source should be
inside formatter, linter, and typechecker scope unless it is genuinely
generated or immutable.

## Complexity

Complexity budgets are tripwires. When a function crosses one:

1. Identify whether it owns too many states or responsibilities.
2. Prefer a state model, table, deep helper, or different module boundary.
3. Avoid splitting branches into forwarding helpers merely to lower the score.
4. Add an exception only for a specific path/subject with an owner and expiry.

The policy uses "fails at" semantics: Go complexity 12 is a finding, not an
allowed maximum. Code Polishy translates that semantic for the sealed JavaScript
bundle itself, and a project-specific provider must translate it to its own
native tool (for example, an ESLint maximum of 9 produces a finding at
complexity 10).

## Dead code

Dead code is not harmless inventory. It obscures owners, increases dependency
surface, confuses tools and agents, and preserves superseded behavior.

- Go uses the pinned Staticcheck default correctness set, including `U1000`, on
  affected packages.
- TypeScript/JavaScript dead code comes from the sealed JavaScript bundle. A
  target installs, pins, and configures nothing: a target `knip.json`,
  `.knip.json`, or `package.json#knip` is reported rather than read, and every
  analyzer plug-in is disabled, because a plug-in learns a framework's entry
  points by loading that framework's configuration file, which means executing
  target code.
  Reachability is a property of a package tree, so the analysis covers a whole
  one at a time: each governed file belongs to the nearest package above it, and
  the outermost package above that one decides the tree, so an export a sibling
  package uses is used. Governed source no package declares, and source of a
  file type the analyzer cannot read without a compiler, are coverage findings
  rather than passes. The analysis reads the repository and the bundle's own
  library declarations and nothing else, by what a path really names rather than
  how it is spelled, so an import that leaves the repository — including through
  a link — reads as absent and the source only it reaches is reported as
  unreachable rather than kept alive by a tree the repository does not contain.
- Python activates Ruff's unused-code checks automatically. Other languages
  need an explicit project command where a reliable tool is available.
- Delete unused files, exports, dependencies, scripts, routes, config, docs,
  aliases, and tests in the same change that replaces them.
- Do not retain backward-compatibility paths unless the product currently has a
  real compatibility contract.

Generated entry points, plugin discovery, reflection, framework conventions,
and runtime-loaded assets are reachable without an import. Code Polishy treats
every test file, every `index`, `main`, or `cli` module at a package root or its
`src` directory, and every `*.config.*` module beside a package manifest as an
entry point. Declare anything else in `scope.entryPoints`, which is a fact about
the repository rather than analyzer configuration. Prefer an exact entry
declaration over a broad ignore.

## File length

File length finds likely monoliths; it does not tell you how to split them.
It applies to governed source and selected configuration formats, not Markdown
documentation.

A good extraction creates a deeper responsibility boundary with:

- one clear owner;
- a smaller public interface;
- hidden sequencing and representation;
- explicit dependencies;
- behavior tests at the new boundary.

A poor extraction creates many one-function files and leaves every caller aware
of the same internal sequence. When a large file is embedded HTML/CSS/JS inside
a server string, move executable code into normal source files so it can be
formatted, linted, typechecked, and tested.

## Test source quality

Ordinary code health rejects only high-confidence vacuity: obvious no-op or
pass-with-no-tests suite commands, empty Go test/subtest bodies, and Go tests
that unconditionally skip without other behavior. It intentionally does not
count assertions or infer meaning from test-framework keywords.

Targets may address semantic weakness with an isolated supplemental mutation
profile. That separation keeps fast feedback fast while providing optional
evidence that the suite would catch a defect. See
[Test Strength and Executable Specification](test-strength.md).

## Selection modes

- `--git-changes` checks tracked changes from `HEAD` and untracked files. It is
  the default for local iteration.
- `--staged` checks staged files only when their worktree content is identical
  to the index; it refuses a staged/unstaged divergence instead of checking the
  wrong bytes.
- `--all` checks every tracked and unignored file in scope. CI and policy
  upgrades should use it.
- `--files` checks an explicit bounded list.

When `.code-polishy.json`, `.code-polishy.lock.json`, a dependency or container
manifest, a lockfile, a workflow, a deletion, or a rename changes,
`check` expands automatically to the full repository. Those changes can alter
the meaning or reachability of unchanged files.

Configured commands may use `paths` to avoid unrelated work in change mode.
Empty `paths` means always run. Repository-wide gates still use `--all`.

## No misleading green gates

- Missing tools fail; they do not silently skip.
- There is no CLI switch that turns external enforcement off while preserving
  a green result.
- `doctor --strict` verifies per-language commands, tools, and architecture
  membership; `check` includes the same coverage inspection.
- `gate` is the one repository-wide merge/release interface.
- CI and local development use the same checked-in entrypoints.
- After a broad failure, fix and rerun the narrowest relevant check before
  repeating the broad gate.
