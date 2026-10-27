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

`display` describes output filtering and bounds, not the verdict. Filters never
turn a complete failure into a pass. Use `summary.status` rather than counting
the currently displayed findings or parsing the terminal's `PASS` line.

Dependency review uses this same report contract. Its tables distinguish
removals from additions and updates against the explicitly recorded base;
the verdict still covers the candidate's required pre-install checks.
