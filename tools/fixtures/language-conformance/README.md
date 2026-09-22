# Language conformance inventory

`ledger.json` binds the first-party extraction inventory to the exact native
reference revisions and to versioned fixture documents. A fixture marked
`planned` is a visible failing gate: the runner records it as blocked and never
mistakes its design stub for parity evidence. Replace the gap with complete
positive, negative, focused, safety, and failure cases before changing the
linked behavior rows to `passing`.

Run an exact reference and candidate pair with:

```text
code-polishy pack conformance \
  --ledger tools/fixtures/language-conformance/ledger.json \
  --reference /exact/reference/code-polishy \
  --reference-policy-root /exact/reference/policy \
  --candidate /exact/candidate/code-polishy \
  --candidate-policy-root /exact/candidate/policy \
  --candidate-pack /exact/candidate/pack
```

The command emits one schema-validated JSON report. Exit status `0` means every
active case matched and every behavior is ready; `1` means a semantic mismatch
or an explicitly unfinished inventory case; `2` means the run was invalid or
could not execute.

Policy-root flags are optional when each executable is already contained by its
matching policy root. Candidate packs are installed into a disposable data home
visible only to the candidate lane, and their exact identities are recorded in
the report.

Every fixture declares a branch, canonical UTC commit timestamp, and explicit
staged or unstaged modified, deleted, and untracked paths. The runner creates
two repositories with the same commit and working state, records the exact Git
binary and version, and rejects commands that change HEAD, the branch, or the
index. Filesystem and Git evidence are compared independently so an index-only
mutation cannot pass as behavioral parity.

The common `expected` outcome applies to both lanes. A fixture may replace the
candidate outcome with `expected.candidate` when the extraction deliberately
changes observable behavior. Every resulting reference/candidate difference
must also appear in `acceptedDifferences` with its exact normalized JSON
pointer and exact canonical JSON values. Wildcards, path-only allowances, and
undeclared differences fail the fixture. Repository, policy, and isolated pack
data roots are the only filesystem values normalized for this comparison.
