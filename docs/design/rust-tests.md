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

## Doctest evidence

A separate suite with `rust.doctests: true` runs frozen `cargo test --doc` with
the same manifest, package and feature selection. It does not run Nextest or
import Nextest's project settings. Its `filter` field is forbidden because a
Nextest expression is not a rustdoc selector. Ignored examples remain ignored;
the runner never opts into them or into live-provider execution implicitly.

The stable compiler's JSON and JUnit test formats still require unstable
options. Instead, the adapter is bound to Rust 1.97.1's Cargo JSON and captured,
uncolored libtest `pretty` output. It requires a successful final Cargo build,
every announced case, matching terminal counts and a complete test run. Compiler
upgrades must validate this exact output contract before doctests can execute;
no nightly flags, bootstrap override or deprecated logfile is used.

The managed `rustdoc.json` artifact records each compiler-emitted batch and case.
Batch identity is execution-local, not an invented package identity. This retains
multiple workspace crates and Rust 2024's merged and standalone doctest batches,
including repeated display names in different batches. Cases distinguish runtime,
expected-panic, compile-only and compile-fail checks from ignored examples.
Compile-only checks do not claim runtime execution. A suite must check at least
one example; an empty or wholly ignored run fails.

Ordinary example failures, including examples that fail to compile unexpectedly,
are doctest failures. A crate/build-script compilation failure has no successful
Cargo build and cannot establish a regression-proof red result. Truncated,
partial or inconsistent output, cancellation, timeout and artifact failures
remain operational outcomes. The original Cargo exit is retained. Direct tests,
gate execution, regression proofs and receipt reuse share the adapter and fresh
artifact boundary. Capturing output cannot replace a previous attempt's report.

This contract follows the pinned
[libtest formatter](https://github.com/rust-lang/rust/blob/8bab26f4f68e0e26f0bb7960be334d5b520ea452/library/test/src/formatters/pretty.rs)
and [rustdoc execution](https://github.com/rust-lang/rust/blob/8bab26f4f68e0e26f0bb7960be334d5b520ea452/src/librustdoc/doctest.rs).
A successful build, benchmark invocation or live app probe alone is not test
evidence.

## Source evidence

Native source checks separately reject empty built-in Rust test bodies, including
ignored and conditionally declared tests. Parsed entry attributes distinguish
test functions from helpers and closures in a `cfg(test)` scope. Production code
co-located with tests retains production complexity limits; a conditional test
attribute alone cannot relax them. The [source-facts contract](cargo-facts.md#parsed-source-facts)
owns these checks. A nonempty body is not proof of meaningful assertions or a
substitute for the nonempty structured execution evidence above.

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

Declare a distinct doctest suite using `"rust": {"manifest": "Cargo.toml",
"doctests": true}`. Its required `rustdoc.json` artifact is also native-owned.
Unit/integration and doctest results do not substitute for each other or schedule
each other; ordinary suite ownership, profiles and explicit selection still apply.
