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

The engine reads the snapshot from the bounded regular Git blob at the exact
base revision. An absent configuration selects first-adoption behavior;
malformed or unsupported historical evidence fails comparison. Required base
features and their suites survive removal from the candidate, so policy edits
cannot silently remove the review obligations being evaluated.
