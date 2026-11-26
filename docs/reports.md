# Machine-readable Reports

Use `--format json` for one machine document or read the managed `report.json`
named by the command. `--output PATH` writes an additional explicit report.
The report protocol and schema identify its structure.

## Verdict and exit status

`summary.status` is the canonical verdict for the complete evaluation:

| Value             | Meaning                                                                                 |
| ----------------- | --------------------------------------------------------------------------------------- |
| `passed`          | Required checks passed; warnings, information, and policy-accepted findings may remain. |
| `failed`          | At least one blocking finding or executed gate failure remains.                         |
| `review-required` | Selected behavior review still requires completion.                                     |

For report-producing evaluation commands, exit `0` means success, exit `1`
means policy or verification failure, and exit `2` means usage or operational
failure. An operational failure may prevent report publication; inspect the
exit status and diagnostic rather than assuming a missing report passed.
Other command-specific protocols, such as `capabilities/v1`, have their own
documented fields and do not claim an evaluation verdict.

```python
import json

with open("report.json", encoding="utf-8") as stream:
    report = json.load(stream)
passed = report["summary"]["status"] == "passed"
```

## Capture once, inspect the saved report

Keep the command's original exit status before displaying or filtering output.
This POSIX example works even when the invoking shell uses `set -e`:

```sh
result_status=0
./code-polishyw check --git-changes > /tmp/polishy-check.log 2>&1 || result_status=$?
printf '%s\n' "$result_status" > /tmp/polishy-check.status
cat /tmp/polishy-check.log
exit "$result_status"
```

The log begins its final result with the verdict, complete managed report path,
selection and principal blockers. Read that named file directly; substitute the
path printed by this invocation:

```sh
jq '.command, .summary, .requestedSelection, .changedTestScope' "$report_path"
jq '.findings[] | select(.severity == "warning")' "$report_path"
jq '.findings[] | select(.severity == "error")' "$report_path"
jq '.testCommands, .notes' "$report_path"
```

In PowerShell, capture `$LASTEXITCODE` immediately after the native command,
then use `Get-Content -Raw $reportPath | ConvertFrom-Json` for inspection. Preserve
the captured status if later commands filter or display output. A pipeline's
last command can exit successfully even when verification failed.

These reads inspect a historical result. Its recorded command, selection, base,
execution metadata and gate identity describe that invocation, not the current
checkout. `summary.status` recovers the published verdict; the captured exit
status also covers failures that prevented publication. A missing report is
never a pass. Evaluation commands with a different `--format`, filter or display
limit run verification again; they are not report viewers.

## Complete results and display

`summary` includes total errors, warnings, information, suppressed findings,
and reviewed findings. `findings` carries all occurrences with their
rule, path, `subject`, message, severity, and remedy. Accepted exceptions and
assessments retain their own evidence instead of disappearing.

Python unused-code candidates and generated JavaScript semantic lint are
warnings, not assertions that a defect has been established. Syntax, supported
unreachable Python control flow, and missing analyzer evidence still fail.
These warnings alone require no cleanup, investigation, waiver, or acknowledgement
to complete a task. Investigate when relevant to the requested work or supported
by evidence of a defect; repetition alone does not require another review.

The default terminal view starts with the complete verdict, managed report path,
selection and at most five blocking findings, followed by an omitted count.
Warnings and information are summarized, with a compact security status and
accepted-assessment counts. Optional candidates remain explicitly optional; when
they are the only findings the result can be `PASS — 37 advisory warnings; no
action required.` Repetition does not select investigation or acknowledgement.

Complete diagnostics and remedies remain in the saved JSON. Explicit evaluation
output options (`--verbose`, `--display-limit all`, filters or `--group-by`)
retain individual occurrences and summary groups, with the requested bound.
Use these options on the original invocation when needed; inspect saved JSON
for later views. The routine test-quality banner lives in verification policy,
not every check or test result. Test-scope facts remain in the machine report.

Optional analysis candidates carry `fields.analysisOutcome: advisory-candidate`.
JSON and SARIF retain diagnostics and guidance regardless of terminal condensation.
Display filters select their requested view; the managed report stays complete.

Focused Python checks state in `notes` that global dead-code analysis was not
run. Project inventory in `analysisContext` alone does not prove analysis ran.
When ty uses an explicitly mapped runtime, a `quality.typecheckEnvironment`
informational finding identifies its manifest and lists analyzed paths in
`relatedLocations`. This records analysis even when type errors were found; it is not a
dependency security or lock-synchronization receipt.

`display` describes output filtering and bounds, not the verdict. Filters never
turn a complete failure into a pass. Use `summary.status` rather than counting
the currently displayed findings or parsing the terminal's `PASS` line.

Terminal findings reference shared remedy (`R`), command (`C`), and generated-code
producer (`G`) blocks. Identical guidance appears once per output stream, even
across different rules; command groups retain exact arguments and working
directories. Reviewed vulnerabilities reference assessment (`A`) blocks while
listing each advisory's observed severity. Groups cover the displayed findings
only. JSON and SARIF retain each finding's complete data.
Follow-up command and producer blocks for warnings and information are labeled
`optional investigation`; command references use `optional=C` and command blocks
use `optional:`. Blocking
findings retain `next=C` and `next:` recovery commands. Optional producer blocks
retain their inputs, prerequisites, generation and verification commands without
selecting those commands for execution. In machine reports, `nextCommand` and
`generation` retain the available actions; the finding's severity determines
whether it blocks. Their presence does not make investigation mandatory.
Independent task and policy obligations still apply.
Printed commands use POSIX shell syntax on Unix and PowerShell invocation and
quoting on Windows.

Dependency review uses this same report contract. Its tables distinguish
removals from additions and updates against the explicitly recorded base;
the verdict still covers the candidate's required pre-install checks.

## Test execution time

`testCommands[].result.executionDurationNanoseconds` measures work in this invocation.
A reused suite records zero current execution and retains its original cost in
`priorDurationMilliseconds`. Gate artifacts already retain historical duration
separately from attempts. Resource-wait duration is separate from execution.
Never add historical receipt duration to the current invocation's runtime.

The existing test plan shows declared cost, scope, last comparable execution,
observation age and reuse decision. A retained gate execution is comparable when
its resolved command, scope, paths, module selection, environment names, timeout
and host platform match. Source or environment values can differ; this is an
observation, not a prediction or reusable pass. Exact matching receipts also
supply their original execution time. Missing suitable history is `unknown`.
A `quick` suite observed at a minute or more is highlighted as slow feedback,
without skipping tests or asking for cost approval.
