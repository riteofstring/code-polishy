# Code Quality Policy

Code-quality automation should make defects visible while the relevant change
is still small. A gate is trustworthy only when its scope, tools, thresholds,
and skipped paths are explicit.

## Baseline budgets

The default policy is:

| Measure                                                | Production | Tests |
| ------------------------------------------------------ | ---------: | ----: |
| Go cyclomatic complexity (fails at)                    |         12 |    20 |
| Python cyclomatic complexity (fails at)                |         10 |    10 |
| TypeScript/JavaScript cyclomatic complexity (fails at) |         10 |    10 |
| TypeScript/JavaScript nesting depth (maximum)          |          4 |     8 |
| TypeScript/JavaScript parameters (maximum)             |          5 |     8 |
| File length (maximum lines)                            |      1,000 | 1,500 |

Generated, vendored, lock, and build-output files are excluded from
edit-oriented text and complexity budgets. Generated executable source still
receives format validation, syntax/compiler, lint, dead-code, coverage, and
module-direction checks, while format writers leave generator-owned bytes
alone. Project-specific generated paths belong in `scope.generated`; do not
hide production code in that category.

Hand-written structured data is a separate governed category. `scope.data`
keeps its identity-sensitive bytes out of style formatting and formatting
writes while retaining syntax, schema, security, product-provider, ownership,
test, and gate coverage.

Markdown (`.md` and `.markdown`, case-insensitive) is excluded from the file
length budget by default because document length is not evidence of a code
monolith. Markdown receives final-newline, trailing-whitespace, sealed-format,
UTF-8, containment, regular-file, size, and local-link checks.

Tests receive a higher Go complexity and file-size budget because table-driven
fixtures and workflow setup are naturally larger. Python and
TypeScript/JavaScript tests keep the same complexity threshold as production
because nested test orchestration becomes unreliable quickly. JavaScript and
TypeScript tests receive explicit depth and parameter budgets instead of
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

A target has its Markdown formatted by the sealed, policy-owned bundle even
when it contains no JavaScript or TypeScript. A target that bears JavaScript or
TypeScript also has its JavaScript, TypeScript, JSON, CSS, HTML, and YAML
formatted by that bundle. Code Polishy owns the configuration completely: a target
`.prettierrc`, `prettier.config.*`, or `.prettierignore` is never read and is
reported as unsupported. Generated files and lockfiles are never rewritten,
because their generator owns their bytes. A file the sealed formatter cannot
decide — an unknown file type, a symlink, a path that really names a file
outside the repository, a file that is not UTF-8 text, or one past the size
bound — is a specific coverage finding, never a silent pass. A
target without JavaScript or TypeScript launches the bundle only when Markdown
is selected and formats its remaining file types with configured providers.

### Hand-written structured data

`scope.data` names hand-written `.json`, `.jsonc`, `.yaml`, and `.yml` product
inputs whose bytes may be identity-sensitive. The category is intentionally
narrow: configuration rejects executable source, dependency and lock inputs,
tool configuration, CI, Dockerfiles, other policy-sensitive controls, and any
overlap with `scope.exclude` or `scope.generated`.

Declared data remains a normal governed file. It must be contained, regular,
size-bounded, UTF-8 text and is parse-validated without a rewrite. It remains
visible to module ownership, change selection, declared schema/security/product
providers, tests, and gates. `check` reports malformed data, while every
formatting path leaves its bytes unchanged. `scope.data` is therefore not an
exclusion or a way to evade product validation. The configuration patterns and
format-provider boundary are defined in [Adopting Code Polishy](../adoption.md#5-define-scope-narrowly).

## Source comments and docstrings

`quality.allowComments` is a boolean and defaults to `true`. When it is omitted
or `true`, Code Polishy emits neither `policy.sourceComment` nor
`policy.sourceCommentCoverage`; formatting, lint, complexity, and every other
quality check remain active. Set it to `false` to enable the strict source
comment policy for the whole repository.

The strict policy rejects prose comments and docstrings in governed handwritten
source. It covers selected production source, tests, scripts, and styles.
Generated source skips this edit-oriented check, and `scope.exclude` removes
only inputs the scope policy permits outside governed selection; it cannot hide
protected handwritten source. The built-in scanners cover Go,
JavaScript/TypeScript, Python, shell, CSS, HTML, and PowerShell. An executable
language without a supported scanner, or a selected source file the policy
cannot read or parse, is never treated as clean.

A prose annotation is a `policy.sourceComment` finding at its exact
path-and-line-column subject. Missing scanner coverage is a
`policy.sourceCommentCoverage` finding. Both fail closed while the strict policy
is active and cannot be exempted: exceptions may not name any `policy.*` check,
and a target cannot waive individual paths, directives, or suppressions. The
single switch is the explicit repository-wide `quality.allowComments` choice.

Only the following full annotations are allowed. The shown whitespace and
delimiters are significant. A placeholder is one machine-consumed value
validated by the named language or tool; it is never a place to append prose.
Where a directive has a required location, matching its bytes alone is
insufficient.

- A first-byte shebang, `#!<non-empty command>`, where the source language
  recognizes a shebang. A shell shebang also classifies a file whose extension
  is otherwise unknown, including multi-extension templates such as
  `job.sbatch.template`; a known built-in extension keeps its normal language.
- Python encoding declarations on line 1, or line 2 only when line 1 is blank
  or begins with a comment. The entire comment must match one of these forms,
  where the character classes are literal regular-expression syntax:

  ```text
  #[ \t]*coding[:=][ \t]*[-_.A-Za-z0-9]+[ \t]*
  #[ \t]*-\*-[ \t]*coding[:=][ \t]*[-_.A-Za-z0-9]+[ \t]*-\*-[ \t]*
  ```

- A shell inclusion directive,
  `# shellcheck source=<canonical repository-relative path>`. The path must be
  a regular shell source file inside the repository, with no whitespace, and
  the immediately following line must be a `source` or `.` command with an
  operand. That operand may be dynamic; the directive supplies ShellCheck with
  its exact canonical repository-relative target.
- TypeScript triple-slash references before code, exactly one of
  `/// <reference path="<value>" />`,
  `/// <reference types="<value>" />`,
  `/// <reference lib="<value>" />`, or
  `/// <reference no-default-lib="true" />`; each `<value>` is non-empty,
  single-line, and contains no double quote.
- The first content in a recognized JavaScript or TypeScript test file may be
  exactly `// @vitest-environment jsdom` or
  `/* @vitest-environment jsdom */`.
- Go directives:
  - the Go comment group attached directly to `import "C"`, which is cgo's
    preamble; an unaffiliated `#cgo` line or C snippet is not allowed;
  - exactly one `//go:build <valid Go build expression>` before `package`,
    separated from it by a blank line;
  - `//go:embed <non-empty Go embed argument list>` attached to a variable;
  - `//go:generate <non-empty command>` at line start, with no leading or
    trailing whitespace;
  - `//line <file>:<positive-line>[:<positive-column>]` at line start, with no
    whitespace or colon in its non-empty file name;
  - `//go:debug <name>=<value>` at line start before `package` in a `main`
    package or Go test file, where `name` is `[A-Za-z][A-Za-z0-9_]*` and `value` is
    `[A-Za-z0-9_.-]+`;
  - `//go:linkname <TOKEN> [<TOKEN>]` attached to a function or variable,
    `//go:wasmimport <TOKEN> <TOKEN>` attached to a bodyless function,
    `//go:wasmexport <TOKEN>` attached to a function with a body, or
    `//export <TOKEN>` attached to the same-named function in a file that
    imports `C`, where `TOKEN` is `[A-Za-z_][A-Za-z0-9_./-]*`;
  - exactly `//go:nointerface`, `//go:noescape`, `//go:nosplit`,
    `//go:noinline`, `//go:norace`, `//go:nocheckptr`, `//go:nowritebarrier`,
    `//go:nowritebarrierrec`, `//go:yeswritebarrierrec`, `//go:systemstack`,
    `//go:cgo_unsafe_args`, `//go:uintptrescapes`, `//go:uintptrkeepalive`, or
    `//go:registerparams` attached to a function; `//go:noescape` requires a
    bodyless function.

Lint, coverage, type-check, and test suppressions, TODO markers, commented-out
code, explanatory headers, and near-misses of an allowed directive are prose
violations. Python module, class, and function docstrings are prose comments.
CSS and HTML have no allowed directive. A canonical HTML doctype remains markup,
while a non-empty inline `script` or `style` body requires its owning source
scanner and fails coverage in the HTML lane. Invalid directive grammar or
placement is a source-comment finding; a scanner or parser that cannot decide
the source at all produces coverage failure rather than an implicit allowance.

Put durable non-local rationale in a current Markdown document selected through
the optional `documentation.design` index. Each entry maps a file under
`docs/design/` to exactly one module or bounded list of concrete source paths;
direct source ownership replaces module ownership for that file. Before editing
source, run `code-polishy design-context --files <path...>` or
`code-polishy design-context --module <name...>`. The command prints stable
repository-relative paths only and returns no more than one current document
per selected file. It deliberately excludes plans, historical evidence, and
superseded decisions; see [Adopting Code Polishy](../adoption.md#map-current-design-rationale)
for the mapping schema.

## Lint, type checking, and syntax

- TypeScript/JavaScript source is linted by the sealed, policy-owned JavaScript
  bundle. `lint` and `complexity` are therefore built-in capabilities: a target
  pins no ESLint, installs no plug-in, and declares no lint provider. Code
  Polishy owns the configuration completely, so an `eslint.config.*` or
  `.eslintrc*` file is never read and is reported as unsupported, and an inline
  `eslint-disable` or `eslint-enable` comment never takes effect. It also fails
  the source-comment rule when `quality.allowComments` is `false`.
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
- Python automatically activates policy-owned Ruff formatting, sealed lint, and
  C901 complexity checks; Vulture dead-code analysis; and `ty` type checking
  with normal diagnostic severity. Python architecture is built in and needs no
  target-native provider. Rust, Java, unknown frameworks, content schemas, SQL,
  protobuf, and other ecosystems use a target-native provider when no shared
  module exists.

### Python Ruff, Vulture, and ty

Python quality uses the same project inventory as Python architecture. Every
selected `.py` and `.pyi` file must be assigned to one contained project; a
nested project is analyzed separately. A missing project, malformed inventory,
escaping path, unreadable input, omitted tool output, or malformed structured
tool result is a coverage finding, never a clean result.

Each governed Python project must declare `project.requires-python` with a
minimum stable Python minor supported by the pinned Ruff (`py37` through
`py315`). Code Polishy derives the oldest permitted minor and passes it as
`--target-version` to every managed Ruff format, lint, complexity, and import
graph invocation. It also passes the validated project roots, including `src`
when present, so isolated import sorting and graph analysis resolve first-party
packages consistently.

Ruff format and lint share a policy-owned line length of 88. A target may state
the same value, but a different or malformed `line-length` or
`lint.pycodestyle.max-line-length` in `pyproject.toml`, `ruff.toml`, or
`.ruff.toml` is a configuration finding before any formatter writes.

Ruff has three policy-owned boundaries:

1. An isolated lint baseline runs `B`, `C4`, `E`, `F`, `I`, `PIE`, `RUF`,
   `SIM`, and `UP` with `noqa` ignored. Its findings, including `F`
   unused-import diagnostics, are lint; they are not Python dead-code
   reachability findings.
2. An isolated C901 pass applies Code Polishy's Python complexity ceiling and
   also ignores `noqa`.
3. A separate target-configured Ruff pass may add repository rules and style
   choices, but cannot disable, exclude, or alter either managed pass.

The policy chooses the exact governed files for every pass and parses each
managed diagnostic into a normal finding. A target Ruff configuration can make
the target pass stricter; it cannot weaken the sealed lint or complexity
baseline or alter the managed Python version, source roots, or line length.

Vulture `2.16` is the sole Python dead-code provider. It analyzes the full
governed contained project at fixed 60% confidence through the release-carried
CPython `3.12.13+20260728` from python-build-standalone, rather than a target
or ambient Python installation. Target Vulture configuration is ignored.
Missing, unreadable, malformed, or incomplete analysis evidence is a coverage
finding, never a clean result. Generated Python remains governed by this
analysis; generated classification does not suppress dead-code coverage.

Vulture infers exact reachable symbols from PEP 621 `project.scripts`,
`project.gui-scripts`, and every `project.entry-points.*` table. For a symbol
reached dynamically rather than through a static import or those conventions,
use optional `scope.pythonDynamicReferences`. Each item requires all three
exact fields, with no wildcards:

```json
{
  "scope": {
    "pythonDynamicReferences": [
      {
        "project": "services/api/pyproject.toml",
        "module": "service.plugins",
        "symbol": "load"
      }
    ]
  }
}
```

`project` is a canonical repository-relative contained-project manifest path;
`module` and `symbol` are exact Python identifier chains. Duplicates, globs, and
other patterns are invalid. Every declared or inferred reference must resolve to
one current project, module, and symbol; a stale or ambiguous reference fails
rather than becoming a broad ignore.
`scope.entryPoints` remains a path-level reachability declaration and cannot
substitute for this exact Python symbol contract.

`ty` runs with the release-owned configuration and structured output. Each
diagnostic becomes one `quality.typecheck` finding with the contained path,
reported line and column, rule, bounded message, and a subject of the form
`<rule>:<sha256>`. The digest covers that exact diagnostic identity, so an
exception copied from one finding matches only that finding. Use the central
exception contract with the exact check, path, and subject; remove it when the
diagnostic is fixed. A count ceiling is not a substitute: a new error must not
be hidden by fixing another one.

For a project with any declared dependency, `ty` receives only that project's
validated `<project>/.venv` through its explicit Python option. A missing,
escaping, malformed, or interpreter-less environment produces one actionable
`quality.typecheckCoverage` finding for the project instead of an
unresolved-import cascade. A dependency-free project uses the sealed
dependency-free analysis. Ambient `VIRTUAL_ENV`, `PYTHONPATH`, shell startup,
and executable lookup do not decide the environment.
That project-local `.venv` is only `ty`'s dependency-resolution input; it never
selects Vulture's interpreter.

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

The policy uses "fails at" semantics: Go complexity 12 and Python or
TypeScript/JavaScript complexity 10 are findings, not allowed maximums. Code
Polishy translates that semantic for policy-owned tools itself: Python C901 and
the sealed JavaScript bundle receive a native maximum of 9 to produce a finding
at complexity 10. A project-specific provider must translate the same way.

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
- Python dead-code analysis is Vulture `2.16` at the fixed 60% confidence
  threshold described above. Ruff `F` remains sealed lint only. Other languages
  need an explicit project command where a reliable tool is available.
- Delete unused files, exports, dependencies, scripts, routes, config, docs,
  aliases, and tests in the same change that replaces them.
- Do not retain backward-compatibility paths unless the product currently has a
  real compatibility contract.

Generated entry points, plugin discovery, reflection, framework conventions,
and runtime-loaded assets are reachable without an import. Code Polishy treats
every test file, every `index`, `main`, or `cli` module at a package root or its
`src` directory, and every `*.config.*` module beside a package manifest as an
entry point. Declare anything else in path-level `scope.entryPoints`, which is a
fact about the repository rather than analyzer configuration. Python dynamic
symbols use the stricter `scope.pythonDynamicReferences` contract above. Prefer
an exact declaration over a broad ignore.

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
