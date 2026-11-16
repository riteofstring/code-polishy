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
Other command-specific protocols, such as `task-start/v2`, have their own
documented fields and do not claim an evaluation verdict.

```python
import json

with open("report.json", encoding="utf-8") as stream:
    report = json.load(stream)
passed = report["summary"]["status"] == "passed"
```

## Complete results and display

`summary` includes total errors, warnings, information, suppressed findings,
and reviewed findings. `findings` carries actionable occurrences with their
rule, path, `subject`, message, severity, and remedy. Accepted exceptions and
assessments retain their own evidence instead of disappearing.

Python unused-code candidates and generated JavaScript semantic lint are
warnings, not assertions that a defect has been established. Syntax, supported
unreachable Python control flow, and missing analyzer evidence still fail.
These warnings alone require no cleanup, investigation, waiver, or acknowledgement
to complete a task. Investigate when relevant to the requested work or supported
by evidence of a defect; repetition alone does not require another review.
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
