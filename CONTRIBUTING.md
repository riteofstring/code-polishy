# Contributing

Code Polishy changes can affect every consuming repository. Keep changes small,
explicit, and reviewable.

1. State the failure class or coverage gap being addressed.
2. Preserve target-project independence.
3. Update runtime validation, schema, templates, and policy docs together.
4. Add a disposable-repository test for new enforcement behavior.
5. Run focused verification while working:

   ```sh
   ./tools/install-policy-tools.sh
   ./scripts/test.sh
   ./bin/code-polishy doctor --strict
   ./bin/code-polishy check --all
   ```

6. Before merge, resolve the trusted merge target from an explicit target,
   checked-in guidance, `origin/HEAD`, then `origin/main` or `origin/master`.
   Run `./bin/code-polishy merge-gate --base <merge-target>` without asking the
   user to choose a level; it is the sole autonomous ordinary authority. For
   ordinary Markdown-only work, format the candidate and skip application
   tests. Run `test --supplemental` as a separate stage when the task or release
   workflow requires local supplemental hardening. Credentialed, destructive,
   production-mutating, and live-provider probes remain external approval gates.

7. Add the public version entry to `CHANGELOG.md` when preparing a release.

Do not weaken a default merely to make one consuming repository green. Fix that
repository, add a general conditional policy module or narrowly justified
project-specific provider, or use an exact expiring exception.

Use semantic versioning for releases:

- patch: fixes that do not change intended pass/fail behavior;
- minor: additive checks, conditional modules, providers, or config; before
  1.0, an intentional atomic default cutover also uses a minor release;
- major after 1.0: changed defaults, removed config, or broadly new blocking
  behavior.

Contributions are licensed under Apache-2.0. Keep third-party source and assets
out unless their terms are compatible and their provenance is recorded.
