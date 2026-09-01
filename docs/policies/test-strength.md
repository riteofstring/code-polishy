# Test Strength and Executable Specification Policy

Passing tests are useful only when a realistic defect would make them fail.
Coverage, test counts, and green commands do not establish that property by
themselves. Code Polishy combines cheap structural rejection with optional,
separately staged mutation profiles.

## Cheap checks in ordinary code health

Test-suite configuration is rejected when its command is an obvious no-op or
contains a known escape that permits success without running tests, such as
`--passWithNoTests`, `--allow-no-tests`, `--if-present`, collection-only, list,
dry-run, or an empty `go test -run` selection.

Ordinary source checks also reject empty Go test/subtest bodies and Go tests
that do nothing except call `Helper` and unconditionally skip. These checks are
deliberately conservative. Counting assertions or searching source text for an
assertion keyword would reject valid tests that prove behavior through errors,
events, state transitions, processes, or generated consumers while still
missing sophisticated tautologies.

## Optional mutation evidence

Mutation testing is opt-in. A target may declare a suite such as:

```json
{
  "name": "orders-mutation",
  "kind": "mutation",
  "scope": "module",
  "cost": "expensive",
  "modules": ["orders"],
  "argv": ["./scripts/test_mutation.sh", "orders"],
  "runOn": ["supplemental"],
  "timeoutSeconds": 3600
}
```

To make mutation evidence mandatory for that target, add `"mutation"` to
`tests.requiredSupplementalKinds`. Without that explicit target choice, the
absence of a mutation suite or engine is not a policy finding and does not
block adoption.

A repository-scoped mutation suite may cover several modules. Its command and
`paths` must honestly describe the production surface it mutates; a partial
selector must not be represented as complete repository evidence.

The target command is project-specific because test runners, build tags,
workspace roots, generated code, and equivalent-mutant policy are executable
facts. The shared contract requires the command to:

- establish that the ordinary baseline tests execute and pass;
- mutate production behavior, not test source or an inert fixture;
- run the tests capable of observing each mutation;
- fail when the declared efficacy or mutant-coverage threshold is missed;
- report surviving and uncovered mutants rather than reducing them to a green
  coverage percentage;
- restore the source reliably, preferably by working in a disposable copy or
  worktree.

Go projects can use [Gremlins](https://gremlins.dev/0.6/), JavaScript and
TypeScript projects can use
[StrykerJS](https://stryker-mutator.io/docs/stryker-js/usage/), and Python
projects can use a target-pinned mutation runner. Do not weaken dependency,
release-age, provenance, trust, or build-script policy merely to install an
optional engine, and do not invent a repository-owned mutator as an adoption
workaround. Skip the optional suite when no conforming established engine is
available. Incremental or diff-aware mutation is encouraged for routine
hardening, but periodic complete mutation remains necessary when the target
claims complete mutation evidence.

This repository pins Gremlins `v0.6.0` by release checksum. Its self mutation
script copies committed and working-tree state into a disposable Git worktree
and enforces at least 80% test efficacy and 80% mutant coverage. Those numbers
are a baseline, not permission to ignore a surviving mutant in behavior-critical
changed code. The runner supplies those thresholds through a private temporary
configuration because the pinned Gremlins release does not enforce the
equivalent subcommand flags. Before mutation, the runner asks pinned Go which
production files are inactive on the current host and excludes exactly those
files; Gremlins otherwise counts build-incompatible source as uncovered.

The quality module disables Gremlins' conditional-boundary operator. Its
language scanners are tested through observable policy findings, while that
operator predominantly changes private cursor comparisons without changing a
finding. Requiring assertions against those private comparisons would create
change-detector tests. The suite retains conditional negation, arithmetic,
increment/decrement, and negative-inversion mutation and the same 80% gates.

## Supplemental execution

Mutation, acceptance mutation, Gherkin mutation, CRAP, and risk-analysis kinds
default to `cost: expensive` and `runOn: ["supplemental"]`. A supplemental suite
cannot also belong to focused, recommended, or full. Consequently:

```sh
code-polishy test --suite orders-mutation
code-polishy test --supplemental
```

execute supplemental work directly. `test --all`, `verify`, `gate`, and
`merge-gate` exclude it. `test-levels` (and its
`test-plan` compatibility alias) keeps the ordinary profiles visible and, with
a trusted base, reports the level that `merge-gate` will select. It lists
impact-relevant supplemental quality separately but executes none of it.

An AI collaborator should first stabilize focused tests and, at an ordinary
merge checkpoint, run `merge-gate --base <merge-target>` without asking the
user to choose an ordinary level. An ordinary Markdown-only candidate selects
the documentation level and zero application suites. Run the direct
supplemental command after ordinary acceptance when the caller or checked-in
workflow requires local hardening. Credentialed, destructive,
production-mutating, and live-provider probes need external approval; do not
start every mutation suite after every edit.

## Optional Gherkin, mandatory execution

Gherkin is not required universally. When a repository chooses to govern a
`.feature` file, Code Polishy requires:

- a repository-scoped full `acceptance` or `gherkin` suite that parses or
  generates and executes the scenarios.

A repository-scoped supplemental `acceptance-mutation` or `gherkin-mutation`
suite may additionally change example values and prove those values are
connected to application behavior, but it is optional hardening.

Feature text must use domain language and describe externally observable
behavior. Class names, endpoint mechanics, table names, private fields, and
mock call order belong in implementation-level tests, not the acceptance
contract. A checked-in feature file that is never executed is documentation,
not test evidence.

This distinction follows the portable pipeline described in Robert C. Martin's
[Acceptance Pipeline Specification](https://github.com/unclebob/Acceptance-Pipeline-Specification):
ordinary acceptance proves the feature, while acceptance mutation checks that
the specification's example data actually reaches the system under test when a
target elects that additional check.

## CRAP is a prioritizer, not proof

The Change Risk Anti-Pattern metric combines function complexity with missing
coverage:

```text
CRAP(function) = complexity^2 * (1 - coverage)^3 + complexity
```

A score of 30 or more is a strong signal to add characterization tests or
reduce complexity before risky change. A configured `crap` or `risk-analysis`
suite belongs to the supplemental profile and should fail at the target's
enforced ceiling rather than emit a report that nobody must act on.

CRAP identifies where weak tests are especially dangerous; it does not prove
that assertions are meaningful. Mutation testing supplies that complementary
evidence. See Robert C. Martin's
[crap4go](https://github.com/unclebob/crap4go) for the metric and focused-module
workflow.

## Review questions for meaningful tests

[Behavior and final-state review](behavior-review.md) adds separate receipt evidence
for a requested behavioral change: every requested behavior needs a
candidate-bound proof that is red on its declared pre-fix base and green on the
candidate. It uses eligible ordinary suites and does not turn
supplemental, live, credentialed, or destructive hardening into checkpoint or
merge evidence.

For each material behavior, reviewers and AI collaborators should be able to
answer:

1. What observable contract would this test falsify?
2. Which realistic mutation or failure would make it red?
3. Is expected data independently specified, rather than recalculated with the
   implementation's algorithm?
4. Does the test cover boundary, negative, and invalid transitions—not only the
   happy path?
5. Does it exercise the public module boundary or a real generated consumer?
6. Are mocks restricted to true external boundaries instead of reproducing the
   internal call graph?
7. For broad input spaces, would a property, model, state-machine, fuzz, or
   metamorphic test express the invariant more honestly than examples alone?

Coverage-only tests, snapshot-only tests with unreviewed baselines, assertions
that compare a value with itself, and tests that merely prove a mock returned
its configured value do not satisfy this policy.
