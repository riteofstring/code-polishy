# Historical Review Policy

Review compares obligations from both sides of a change. Applying the incoming
configuration schema to the base would prevent a repository from upgrading;
treating an older configuration as absent would let the upgrade erase required
review evidence. Historical configuration is therefore evidence, separate from
the candidate policy that authorizes execution.

The policy boundary reads a bounded, duplicate-free historical review snapshot.
For schema 3 it projects only modules, test suites, test classification, and
behavior-review requirements. Its dedicated schema reuses unchanged module and
suite definitions. Unrelated historical settings do not become active policy.
Feature descriptions remain absent when the historical format did not require
them; the reader does not invent rationale. Current-format snapshots still pass
the complete current configuration validator.

Schema 3 assigned tests through module paths. The snapshot represents those
historical ownership relations explicitly for impact analysis, without
inventing a focused suite or changing the candidate's ownership rules. Suite
defaults preserve the command identity used by historical review evidence.
Only the candidate configuration governs current checks and test execution.
The planned runner enforces the same command identity for captured and streamed
analyzers; stream forwarding retains the gate attempt and log boundary described
in [Execution observability](execution-observability.md).

The engine reads the snapshot from the bounded regular Git blob at the exact
base revision. An absent configuration selects first-adoption behavior;
malformed or unsupported historical evidence fails comparison. Required base
features and their suites survive removal from the candidate, so policy edits
cannot silently remove the review obligations being evaluated.

## npm dependency comparison

Modern npm lock inventory preserves canonical package names, installation paths,
and source identity independently of the current registry configuration. A Git
commit change remains visible even when the package version stays the same.
Artifact URL changes remain visible even when the package version is unchanged;
the comparison retains the complete observed URL without assuming a registry
download layout. Removing or replacing a dependency from a former private
registry requires no access to that registry. Candidate source admission remains
separate: the currently configured registry must identify the exact package and
artifact before its publication time can be used. Malformed or contradictory
identities and credential-bearing sources remain comparison errors.

## Cargo dependency comparison

Dependency review reads Cargo declarations and lock records through the shared
bounded inventory reader, including immutable historical Git blobs. It never
runs Cargo, installs dependencies or executes historical project code. Current
pin/source policy remains separate: a historical branch or private registry can
be compared and removed without being admitted in the candidate. Unsafe and
ambiguous history fails comparison without copying credential-bearing values
into errors.

Cargo bare versions are compatible ranges, so the review represents `1.2.3` as
`^1.2.3`; only an exact `=1.2.3` declaration becomes an exact version. Renamed
packages retain their declared package identity. Runtime, optional, development
and build usage remain distinct. Workspace definitions and override declarations
have separate review roles. An inherited dependency is a `cargo-workspace`
reference, including an explicit workspace path when present; it is not assigned
a guessed registry version or package identity. Cargo owns effective workspace
resolution.

Lock rows have the `resolved` role rather than being called transitive merely
because their direct declaration is inherited or elsewhere. These rows retain
each lock scope even when another manifest already declares that version. Git
coordinates preserve the observed commit separately from an optional declared
ref. Private registry rows retain their exact registry identity, including the
sparse protocol, and cannot select public crates.io age queries. Historical
non-HTTPS registries can be compared for removal without becoming authenticated
or admitted sources. Exact new public runtime and optional declarations
use the shared preferred-age advisory; unused workspace definitions and unresolved
inheritance are not described as new runtime dependencies.

## Dependency comparison presentation

For declared Python libraries, [compatibility contracts](python-library-dependencies.md)
retain runtime and optional groups, dependency extras and environment markers.
Changing these declarations remains a package API change even without a new
resolved version. Registry constraints are inventoried separately; frozen
resolution owns their satisfiability.

The explicit base remains authoritative for comparison. A pre-adoption base
has no lock graph, so a branch-wide review cannot describe removals of packages
that appeared only later on that branch. A separate explicit task-base review
provides that narrower delta without substituting for branch-wide admission.
Removals appear in their own table before additions and updates, preserving all
rows and their manifest or lock scopes. Preferred-age warnings name the exact
package and version in both their subject and message.
