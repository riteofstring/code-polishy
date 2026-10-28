# Published Python dependency contracts

Published wheel requirements describe versions consumers may resolve. Exact
development requirements and frozen locks describe the environments used to
build and test that library. Treating both as exact public requirements prevents
otherwise compatible libraries from sharing a consumer environment.

`supplyChain.pythonLibraries` names exact repository-relative `pyproject.toml`
paths. It selects compatibility treatment only for registry requirements in
`[project].dependencies` and `[project.optional-dependencies]`. Other manifests,
development groups, build-system requirements and non-registry sources retain
their existing policy. `project.kind` remains descriptive; a mixed repository
can identify its published libraries without changing its application policy.

Every published registry declaration, including an exact one, requires a matching
exact development-group requirement for the same normalized distribution. Its
extras must include every requested dependency extra. Its marker must be absent
or equal to the published requirement's normalized marker. This explicit pairing
covers every declared branch without evaluating only the current host, sampling
environments, or introducing a marker implication solver. Authors can repeat
the same marker in the development group when equivalent expressions have
different normalized forms. Optional table groups remain distinct from extras
requested from a dependency.

External dependency contract checks use the same development-requirement
pairing. They verify range membership and retain their requirement for one
authoritative exact lock package and matching installed distribution, so
published ranges do not remove external contract evidence.

The sealed Python facts adapter's `version-specifiers` operation uses the
already pinned `packaging` implementation for PEP 440 membership, including its
ordinary prerelease and local-version rules. Go owns library scope, pairing,
coverage, findings and bounded batching. The new operation leaves the existing
`analyze` request and source-fact contract unchanged. No target code, package
installation or ambient parser participates.

Registry constraints are preserved by manifest inventory rather than compared
as source identities. Different ranges, exact versions and environment branches
can describe a valid resolution. The required target-native frozen `lock-sync`
provider establishes universal resolution and manifest/lock agreement, including
contradictory constraints and build isolation. Static compatibility checks do
not substitute for that provider. Dependency review retains its pre-install
boundary and reports that verification as deferred. The full resolved graph
retains release-age, vulnerability, source and configured license coverage.

Dependency comparison keeps declaration groups, dependency extras and markers
in its usage identity, preserving changes even when a lock stays unchanged.
Published runtime and optional changes are labeled package API changes. A change
to a marker or group is represented as removal and addition. The report does not
claim to classify every PEP 440 range change as breaking or nonbreaking.

Membership proves that the tested pin agrees with the declaration, not that all
future versions are compatible. Library release procedures should test the
actual wheel in a clean minimum-version environment, exercise supported extras,
and select additional supported versions as needed. Development-only packages
must not mask missing wheel requirements.
