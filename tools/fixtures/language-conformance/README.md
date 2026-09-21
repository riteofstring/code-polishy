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
  --candidate /exact/candidate/code-polishy
```

The command emits one schema-validated JSON report. Exit status `0` means every
active case matched and every behavior is ready; `1` means a semantic mismatch
or an explicitly unfinished inventory case; `2` means the run was invalid or
could not execute.

Every fixture declares a branch, canonical UTC commit timestamp, and explicit
staged or unstaged modified, deleted, and untracked paths. The runner creates
two repositories with the same commit and working state, records the exact Git
binary and version, and rejects commands that change HEAD, the branch, or the
index. Filesystem and Git evidence are compared independently so an index-only
mutation cannot pass as behavioral parity.
