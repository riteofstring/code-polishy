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
