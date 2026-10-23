# Test Ownership and Execution

Test ownership identifies the production boundary whose behavior a test
verifies. It is independent of the test's directory, imported collaborators,
and execution cost. Every governed executable test or test helper has one
production owner, and test imports do not authorize production dependencies.

`focusedSuite` binds that owner to quick module coverage. `executionSuite`
identifies a distinct ordinary suite when the test runs elsewhere. Omitting it
means the focused suite executes the test. Both references are validated;
selecting expensive integration evidence never replaces quick module coverage.
When execution is separate, the referenced focused suite must retain at least
one governed test source that it owns and covers in its own execution paths.
Listing production files or moving every owned test to full-only execution
cannot satisfy that check, regardless of the execution suite's declared cost.

The execution suite must list the owned paths, include the full profile, and
have either the same module owner or repository scope. Supplemental execution
cannot satisfy this requirement. Imported helpers are execution inputs of their
real suite, without invented standalone commands. Suite commands remain
responsible for executing tests and rejecting empty runs; path declarations
alone do not prove execution.

Rust files may contain both production items and inline tests. File-level test
ownership does not relax production complexity limits. Parsed built-in test
attributes and lexical `cfg(test)` scopes select the separate Rust test limit;
unproven test contexts retain the production limit. The
[Cargo source facts](cargo-facts.md) contract defines that syntax-only boundary.

Ownership does not schedule tests. Existing cost, profile, changed-impact,
required-kind, and gate rules select execution. Changing a test or its helper
still selects the owner's focused coverage; the full profile retains its
declared integration suites. The ownership record, including a distinct
execution suite, participates in existing review and receipt identities.

Test planning explains each selected suite using its impacted module boundary
or repository profile, and identifies when impact includes the whole repository.
Managed control upgrades retain those selection rules; a declared focused
control suite is usable only when it satisfies the repository's actual upgrade
requirements. Planning does not execute application tests or create a blanket
upgrade exemption.
