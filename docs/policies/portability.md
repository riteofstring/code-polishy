# Portability and External Inputs

Portability policy makes hidden machine and checkout assumptions visible before
they become misleading product behavior. It does not ban configuration or
require an application to stop when an optional integration is unavailable.

## Core advisories

`doctor`, `check`, and `gate` scan governed, non-generated production source for
two high-confidence risks:

- committed absolute paths under a developer home such as `/Users/name/...`,
  `/home/name/...`, or a Windows user profile;
- a repository-root-relative `../sibling` fallback that is not attached to a
  declared external input.

These results render as `WARN`, remain in the structured report, and do not
change an otherwise successful exit status. The scanner intentionally does not
flag every repeated literal, ordinary relative import, test fixture, generated
file, URL, status string, or number. Whether a domain string should be an enum,
refined type, registry, or configuration value needs typed linting, contract
evidence, or review rather than a noisy text heuristic.

Ignored local files such as `.env` are not governed source and are not scanned.
An absolute path in a developer-owned `.env` can be valid configuration. The
target must still report which input source and resolved location it actually
used at runtime.

## Declare external inputs

Use `portability.externalInputs` when product behavior reads a separately owned
repository, directory, file, or service:

```json
{
  "portability": {
    "externalInputs": [
      {
        "name": "catalog",
        "kind": "repository",
        "module": "publishing",
        "sourcePaths": [
          "scripts/publish/**",
          "scripts/lib/catalog-template-*.mjs"
        ],
        "resolution": ["environment", "default"],
        "environment": ["CATALOG_REPO"],
        "unavailableBehavior": "warn",
        "contractSuite": "catalog-input-contract",
        "behaviorSuite": "catalog-warning-browser",
        "siblingFallback": "../shared-content"
      }
    ]
  }
}
```

`resolution` is ordered precedence. `default`, when present, must be last.
`environment` is required exactly when environment resolution is declared.
`siblingFallback` admits one exact root-relative default only for matching
`sourcePaths`; it requires a non-service input and `default` resolution. It
does not suppress machine-home warnings or a different sibling reference in
the same file.

Every declaration names one owning module and two distinct suites:

- `contractSuite` is that module's quick focused suite. It proves precedence,
  resolved-source diagnostics, compatibility validation, valid/empty/missing/
  mismatched inputs, and that an invalid explicit selection does not silently
  switch to another checkout.
- `behaviorSuite` is ordinary evidence included in `full`. For
  `unavailableBehavior: warn`, it proves the application remains usable, the
  warning is visible at the appropriate product surface, and only the dependent
  feature becomes unavailable. It must not turn a failed integration into a
  plausible empty result.

The target chooses the compatibility contract: repository identity, manifest
version, schema, capability file, required roots, or another stable product
fact. Merely checking that a directory or `package.json` exists is insufficient
when an unrelated checkout could satisfy that shape.

## External security-monitoring evidence

Recurring external monitoring is opt-in through
`supplyChain.recurringSecurityMonitoring`. GitLab pipeline schedules live on
the GitLab server, not in `.gitlab-ci.yml`. Checked-in YAML can prove static
image and include pins; it cannot prove that a schedule exists, is enabled, or
has run. An opted-in GitLab repository with dependency graphs therefore
declares one `security-monitoring` provider in the `security` profile: a
`checks` command with
`provides: ["security-monitoring"]` and `runOn: ["security"]`. This is an
external evidence boundary, not a static-YAML heuristic.

Without the opt-in, Code Polishy does not require a provider and emits no
`policy.securityMonitoring` finding. A provider that is declared still runs in
the online security profile and fails closed when its evidence is unavailable.

The provider succeeds only when it can prove all of these facts for the target
repository:

- an enabled server-side schedule invokes the online Code Polishy security
  profile no less often than weekly;
- the scheduled job uses the repository's locked Code Polishy release;
- the latest required scheduled run completed successfully within the allowed
  age; and
- the schedule, job, and run belong to the intended repository rather than a
  similarly named project or unrelated pipeline.

Missing credentials, denied API access, an unavailable GitLab API, absent or
unreadable schedule evidence, or unavailable run evidence is not success. The
provider exits nonzero and names the unavailable evidence in its command output
and retained log so Code Polishy reports the provider failure. It must never
return success merely because a checked-in `.gitlab-ci.yml` looks scheduled.

The provider's GitLab API call is credentialed live work. Keep credentials out
of configuration and logs, use the least authority needed to read the schedule
and runs, and invoke that probe only through an explicit external approval
gate. A repository using another managed scheduler documents an equivalent
provider with the same success and unavailable-evidence behavior. See
[Supply-chain Policy](supply-chain.md#gitlab-ci-control-inputs) for the static
GitLab controls that remain locally enforceable.

## Test placement and cost

Literal scanning and declaration coverage are core deterministic checks. They
read selected source once and add no subprocess or network work. Resolver and
compatibility contracts should use temporary fixtures and remain quick enough
for focused iteration.

A browser or integration test that proves the warning and degraded feature
behavior belongs in the ordinary full profile. A test against an actual sibling
checkout may be another named full integration suite when the environment can
provide the checkout. It is not supplemental mutation/risk evidence and must
not be disguised as such merely because it runs independently.
