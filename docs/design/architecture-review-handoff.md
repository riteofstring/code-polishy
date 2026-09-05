# Architecture Review Handoff

Architecture acceptance lives outside the source tree. A fresh CI checkout
therefore needs an explicit transfer before its base-aware merge gate. This
repository uses `scripts/architecture-review-handoff.sh`; adopting repositories
can use the same procedure with their own authenticated artifact transport.

The publishing operator first obtains a clean-context review and finalizes it
against the exact base CI will use. For a push, that base is the previous remote
main commit; for a pull request, it is the target commit. After all source edits
and commits, prepare the local handoff:

```sh
./scripts/architecture-review-handoff.sh export BASE
```

Export requires a passing architecture-review status from the locked release.
It writes a separate Git commit containing only the preparation binding,
receipt, packet, and result. Its sole parent is the exact source candidate.
The reference name binds both the full base and candidate commit identities:
`refs/code-polishy/architecture-review/BASE/CANDIDATE`. Source branches contain
neither duplicated review sources nor generated acceptance files.

Export never pushes. With explicit publication authorization, publish main and
the printed reference together so CI cannot race evidence publication:

```sh
git push --atomic origin main refs/code-polishy/architecture-review/BASE/CANDIDATE
```

CI restores only the exact reference from its configured origin, using its
read-only repository token. Origin writers are the trusted evidence publishers;
fork-controlled artifacts and local checkout files do not supply acceptance.
Repositories that require a separate reviewer identity or human approval must
enforce that additional boundary in their publication permissions. Git object
identities establish integrity, not proof of clean reviewer context.

Restore requires an absent managed architecture-review directory, checks the
candidate parent and exact four regular artifact paths, enforces the packet
and result byte limits, and stages complete files before publishing the
directory. It never extracts an archive or executes artifact content. The
merge gate still validates JSON, digests, citations, acceptance, review base,
ancestry, instructions, mapped documents, and current semantic topology.
Transport success alone cannot pass the gate. A missing or invalid handoff
remains a failure with a publication remedy.

The focused handoff suite uses disposable repositories and synthetic content
to test transport and rejection. Synthetic content is never an acceptance of
this repository. Behavioral acceptance validation remains owned by the
architecture-review tests and the final merge gate.
