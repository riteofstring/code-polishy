# Native Rust Test Evidence

Rust suites use the existing ownership, cost, profile, selection, artifact and
receipt mechanisms. A suite's `rust` selector replaces its arbitrary `argv`;
it identifies a contained Cargo manifest, optional packages/features and a
Nextest filter. Cargo default features remain the default. Native execution
uses the pinned host compiler, frozen resolution and the governed Cargo cache.
It does not enable every feature or claim another target was tested.

The pinned Nextest runner supplies unit and integration outcomes. Each attempt
receives a fresh managed artifact directory and release-owned Nextest settings.
Nextest's state directory also lives there, never in the read-only source view.
The settings bind a JUnit report to that suite and disable retries within Nextest.
User configuration is disabled. Project Nextest configuration currently requires
explicit support and fails preflight: silently ignoring default filters could
enable live probes, while importing setup scripts could add unapproved execution.
Existing suite retries remain the only outcome-bearing retry authority.
Configuration is written as data, with correctly escaped paths and names.
The shared artifact preparer retains native compiler environment overrides.
Rust, Cargo and Nextest control variables cannot be inherited through a suite's
application environment. Application diagnostics such as `RUST_LOG` and
`RUST_BACKTRACE` remain valid declared inputs. The host SDK search environment remains available and
participates in receipt identity alongside declared application inputs.

Typed selectors, including package names, feature choices and filters, remain
part of the suite identity used for retries, historical comparisons and reusable
receipts. A reusable suite runs in the existing read-only input view while using
the governed Cargo cache and target directory. Native report declarations are
present in both direct execution and the gate plan.
Regression proofs use that same execution and artifact boundary. An assertion
failure can establish the expected red exit; compiler failures, empty runs or
invalid artifacts cannot become red evidence merely by matching a process code.

The `nextest-junit` artifact contract validates complete case identities and
declared counts, not just an XML root. Empty, wholly skipped, partial, duplicated
or malformed evidence cannot establish a successful test run. A normal Nextest
test-failure exit must agree with recorded failures; other failed exits remain
operational failures. Cancellation and timeouts never become report-bearing
success. Required artifacts remain contained regular files, bounded and hashed
once for reusable evidence by the existing artifact owner.

Doctests are not Nextest tests. Their compiler behavior, including compile-fail,
no-run and ignored examples, requires separate version-bound evidence before
doctest coverage can be claimed. This native suite contract does not turn a
successful build, a benchmark invocation or a live app probe into test evidence.

## Suite configuration

A module suite uses ordinary ownership and profile declarations, for example:

```json
{
  "name": "core-unit",
  "kind": "unit",
  "scope": "module",
  "modules": ["core"],
  "rust": {
    "manifest": "Cargo.toml",
    "packages": ["core"]
  },
  "runOn": ["focused", "recommended", "full"],
  "cost": "quick"
}
```

The manifest defaults to `Cargo.toml`. Without `packages`, Cargo's selected
manifest and workspace default members determine the packages tested. Optional
`features`, `noDefaultFeatures` and Nextest `filter` fields refine selection.
Cargo validates package specifications and feature names; valid punctuation and
Unicode are not restricted to Code Polishy's module-identifier grammar. A filter
that selects no executed tests fails. Declare integration and expensive suites
separately when they require different ownership, profiles or resources.

Do not supply `argv`, `retryArgv`, a non-root `cwd` or a replacement `nextest.xml`
artifact for a native Rust suite. Native execution owns those fields and requires
its result report. Frozen testing requires a reviewed lock and a prepared Cargo
cache; it never installs dependencies or repairs resolution.
