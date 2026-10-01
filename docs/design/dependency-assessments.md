# Dependency assessment decisions

Release age and vulnerability applicability answer different questions. A
release-age assessment admits new code before the ordinary waiting period. A
vulnerability assessment explains how an exact advisory applies to an already
resolved package. Neither assessment can supply evidence for the other merely
because the package names match.

Dependency assessments are technical decisions that agents may own without
human sign-off. Both dispositions require an identified assessor, exact
coordinates, technical evidence, a remediation plan, review date, and expiry.
The plan may share the checked-in evidence document; a second approval or
external issue is not intrinsic to the decision.

`not-affected` establishes why the advisory's input and API prerequisites cannot
occur. `risk-accepted` permits a bounded, justified residual risk only for low
and moderate severity. High findings permit only demonstrated non-exposure;
critical, unknown-severity, and known-exploited findings remain blocking.
Reports identify the actual assessment owner without implying human approval.
Exact matching, expiry, severity ceilings, and unused-assessment checks still
apply to both dispositions.

Autonomy does not make fresh releases safe. The 30-day delay gives compromised
or defective releases time to be discovered. A reachable advisory alone cannot
justify bypassing that protection. Before a security-fix admission, the assessor
must compare concrete exploitability and harm during the remaining wait with
the provenance, change scope, and unknown risks of the fresh code. An aged fix
or practical mitigation takes precedence. Early admission needs evidence that
waiting is materially riskier and no such alternative suffices.

The validator can check the record's structure, not the truth of a call-path
analysis or risk comparison. Source review and the detailed dependency workflow
own those judgments. Additional free-text fields, URL-shaped placeholders, and
tests that merely repeat an assessment's prose would not close that gap.
