# Test Ownership and Execution

Test ownership identifies the production boundary whose behavior a test
verifies. It is independent of the test's directory, imported collaborators,
and execution cost. Every governed executable test or test helper has one
production owner, and test imports do not authorize production dependencies.

`focusedSuite` binds that owner to quick module coverage. `executionSuite`
identifies a distinct ordinary suite when the test runs elsewhere. Omitting it
means the focused suite executes the test. Both references are validated;
selecting expensive integration evidence never replaces quick module coverage.

The execution suite must list the owned paths, include the full profile, and
have either the same module owner or repository scope. Supplemental execution
cannot satisfy this requirement. Imported helpers are execution inputs of their
real suite, without invented standalone commands. Suite commands remain
responsible for executing tests and rejecting empty runs; path declarations
alone do not prove execution.

Ownership does not schedule tests. Existing cost, profile, changed-impact,
required-kind, and gate rules select execution. Changing a test or its helper
still selects the owner's focused coverage; the full profile retains its
declared integration suites. The ownership record, including a distinct
execution suite, participates in existing review and receipt identities.
