# Generated JavaScript exhausts the lint result limit before exemptions apply

Reported: 2026-10-09. Affected release: Code Polishy 0.28.2.

Resolved in 0.28.3 by applying rule activation before analysis and returning
[bounded lint result pages](../design/javascript-lint-results.md).

## Failure and impact

The sealed JavaScript linter counts generated-code complexity and nesting diagnostics toward its
5,000-result limit before the Go policy layer applies generated-code exemptions. A repository with
several legitimate generated validators therefore receives a blocking `policy.tool` error instead
of a completed quality check. The same modules pass when checked individually.

This blocks Setta's combined check and checkpoint gate. Its separate `quality.fileLength` findings
are advisory warnings and are unrelated to this failure.

## Reproduction

Environment: macOS arm64, Setta commit `82b409a09`, locked Code Polishy 0.28.2 with release digest
`e7c8a7bb0a1df0e9a2bcdec2a3ad70b63cbeced0c9db9e26c393cdcff0d7c903`.
The sibling Setta checkout has the dependencies and built Workspace declarations required by its
normal checks. From that checkout:

```sh
./code-polishyw check \
  --module realtime-frontend \
  --module review-frontend \
  --module experiment-lens-frontend
```

The command exits nonzero with:

```text
ruleId: policy.tool
subject: javascript-bundle
the sealed JavaScript bundle rejected the lint request: the lint operation produced more than the 5000 result limit
```

The normal checkpoint reproduces it too:

```sh
./code-polishyw checkpoint-gate --base 8671b4d5fe5ab3c44a946d46aaa2555ab17c6675
```

The existing Setta report is
`.code-polishy-reports/checkpoint-gate/d4daf6373aa108244fadba737e21860266cc3abe93a888d6fb7194a1caa920d7/report.json`.
It records one blocking `policy.tool` error and 30 advisory file-length warnings.

Each module fits within the limit in an independent invocation:

```sh
./code-polishyw check --module realtime-frontend
./code-polishyw check --module review-frontend
./code-polishyw check --module experiment-lens-frontend
```

## Measured trigger

The following files are already declared in Setta's `scope.generated` and have checked producers.
They are AJV-generated validators, not handwritten application logic. Invoking the unchanged sealed
runner separately for each file with complexity 9, depth 4, parameter limit 5, React and
accessibility checks enabled produced these intermediate records:

| File                                                  | Complexity | Nesting depth | Comments | Total |
| ----------------------------------------------------- | ---------: | ------------: | -------: | ----: |
| `apps/realtime/src/api/generatedValidators.js`        |         20 |         2,123 |        8 | 2,151 |
| `apps/review/src/api/generatedValidators.js`          |         37 |         2,092 |        4 | 2,133 |
| `apps/experiment-lens/src/api/generatedValidators.js` |         96 |         2,173 |        9 | 2,278 |
| Combined                                              |        153 |         6,388 |       21 | 6,562 |

These three files alone produce 6,541 complexity/depth findings plus 21 comment records. Combining
them exceeds the runner's limit even though their exempt findings should not reach the final
quality report. No installed tool files or repository exemptions were modified to obtain this
evidence.

## Cause

In the 0.28.2 source:

- `tools/javascript/runner.mjs` defines `MAXIMUM_LINT_RESULTS = 5000` at line 74.
- `lint()` collects ESLint findings and comment records, then checks their aggregate length at
  lines 405–409. It rejects the request before returning a result.
- `internal/quality/javascript.go` already knows whether a group should check complexity through
  `javascriptLintGroup.checkComplexity`. However, `JavaScriptLintFindings()` calls `bundle.Lint()`
  before applying that value in `javascriptLintResultFindings()`.
- The exempt diagnostics are therefore still generated and consume the bounded transport budget.
  The later filtering cannot run because the bundle has already rejected the request.

## Expected behavior and acceptance

Generated-code exemptions should take effect before exempt diagnostics consume the result budget.
Keep syntax, correctness, security, producer verification, and applicable comment policy intact.
Avoid increasing an arbitrary global limit or silently truncating real findings to make this
repository pass.

An appropriate fix could carry the already-resolved rule activation into the sealed lint request
and use bounded batches where needed. The implementation should preserve bounded resource use and
complete applicable findings, including when one file has many real diagnostics.

Regression coverage should demonstrate that:

1. Several valid declared generated files with more than 5,000 otherwise-exempt complexity/depth
   diagnostics complete a combined check without `policy.tool` failure.
2. A genuine applicable diagnostic in one of those generated files remains visible.
3. Handwritten files retain their normal complexity checks, and splitting a selection into batches
   does not change the applicable findings.
4. The Setta combined reproducer and checkpoint proceed past this analyzer failure without changing
   Setta's generated output or weakening its policy.

## Current workaround

Setta's hosted CI checks modules separately using the same rules. That provides scoped static-check
evidence but does not satisfy the repository-wide checkpoint. The checkpoint remains visibly
failed until the upstream issue is fixed.
