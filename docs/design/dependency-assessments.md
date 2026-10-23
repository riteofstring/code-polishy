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
and moderate severity. High and critical findings permit only demonstrated
non-exposure. Critical assessments expire within seven days, retain their
critical severity, and require renewed review when the traced integration or
its input authority changes. Unknown-severity and known-exploited findings
remain blocking.
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

OSV severity uses the scanner's group score and matching advisories' categorical
`database_specific.severity` metadata. GitHub-reviewed advisories may provide a
severity without a CVSS vector. Missing group scores therefore do not erase an
available upstream classification. Diagnostics identify the exact sources;
disagreement retains the highest recognized severity, while unsupported values
or unnormalized CVSS metadata without a usable score or categorical label stay
unknown. Metadata from
unrelated advisories cannot influence a group. No network lookup or operator
override is needed to repair metadata already present in the scanner response.

## Go toolchain containment

The pinned Go standard library implements the repository's rooted filesystem
boundary. Go 1.26.6 can follow a Windows junction during `Root.Mkdir` and create
a directory outside that boundary before the caller's later validation rejects
the path. Retain the official Go 1.26.9 correction in subsequent build and carried
toolchains. A preliminary path check cannot provide the same protection against
concurrent filesystem changes.

The exact early admission, affected callers, considered mitigations, upstream
delta, and fresh-code risk are recorded in the
[Go toolchain assessment](../security/dependency-assessments.md#go-toolchain).
The Windows release contract verifies containment, preservation of the outgoing
lock on rejection, and normal upgrade recovery after removing the junction.

## Go TLS verification

Online release-age decisions use HTTPS metadata through Go's default certificate
verification. A network attacker with a chain through a compromised intermediate
can influence those decisions on a host whose trust store still accepts that
intermediate. Digest authentication of release archives does not authenticate
registry release dates. Go 1.26.10 applies the upstream denylist to both native
platform and Go certificate verification, preserving ordinary online checks
without depending on every consuming host's trust-store update schedule.
The same exact toolchain assessment records the input trace, alternatives,
official patch delta, freshness risk, and admission expiry.
