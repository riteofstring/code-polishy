# Workflow facts and runner declarations

The workflow module parses bounded inputs and returns semantic facts. Repository
callers supply contained reads so this module does not depend on repository
ownership or filesystem policy. The same reader supplies the workflow and the
supported actionlint declaration at `.github/actionlint.yaml` or
`.github/actionlint.yml`. Dual declarations are ambiguous and fail.

Runner labels describe infrastructure available to the project. Exact labels
can extend actionlint's known runners; wildcard declarations and diagnostic
suppression settings cannot weaken shared workflow checks. The declaration is
a policy-sensitive control input because it changes accepted workflow facts.

Workflow pin checks, recurring-security detection, and final-gate detection use
this same boundary. Generated workflows without a repository declaration retain
the default runner rules. Successful runner recognition does not waive action
pinning, job dependencies, credentials, permissions, or schedule validation.

Event-condition evaluation supports the shared final-gate and scheduled-scan
consumers. Unknown expressions cannot prove activity. Direct shell commands
and optional-failure facts support [CI final-gate ownership](ci-gate-ownership.md).
Job IDs and dependency references use the same lowercase canonical form as
actionlint's case-insensitive job graph. Event-activity queries index jobs once
and retain each prerequisite's result, so shared dependency chains take time
proportional to the jobs and edges instead of the number of paths. A job remains
inactive until all prerequisites succeed, which also rejects dependency cycles.
Each query owns its results so one event cannot contaminate another.
Run-step facts carry the effective shell, including step, job, and workflow
default precedence and statically known runner or container defaults. Ownership
analysis requires a standard named shell for the dedicated command format;
custom templates and unresolved defaults cannot establish ownership.
Run-step facts also identify unmodeled startup settings inherited from container,
workflow, job, and step environments. Nonempty or unknown `BASH_ENV` and
`SHELLOPTS` values prevent the script alone from proving gate execution. Exact
empty overrides clear inherited values; environment expressions remain unknown.
The same precedence rules cover `PS4`, whose value is evaluated later during
tracing. Only known constant prefixes without expansion or escape syntax can
supply proof. Constant and empty overrides replace inherited trace expansions.

Action input facts also let runtime coverage connect `setup-node` to declared
Node release-artifact pins without interpreting arbitrary workflow expressions.
