# Release Checklist

A Code Polishy release is one reviewed commit, one annotated `v<VERSION>` tag,
and one shared internal release identity across all native archives and Linux
OCI images. Tags and published digests are immutable. A maintainer performs
every operation that creates a tag, pushes, publishes, or changes a target lock.

1. Bring the candidate to release shape as one reviewed commit. `VERSION` must
   contain the strict `MAJOR.MINOR.PATCH` version, and `CHANGELOG.md` must have
   an exact `## <VERSION> - <YYYY-MM-DD>` section. Remove completed temporary
   plans and obsolete docs. Use focused checks while editing; do not run a full
   gate against a changing worktree.

2. Stop changing the candidate and complete its one ordinary final gate. Honor
   `verification.finalGateOwner`: run locally for `local`, or retain the native
   CI result for `ci`. Ubuntu, macOS, and Windows must pass for the same commit.
   An exact already-passed gate executes no commands, and new gate identities
   may reuse only suite receipts whose complete inputs still match.

3. Mutation testing is optional for releases. Run it only when explicitly
   requested by the caller or invoked by a checked-in event workflow; the
   release version alone does not select it. When selected, verify the harness
   first with `code-polishy test --suite mutation-wrapper-contract`.

   Use `code-polishy test --supplemental --resume` for the selected hardening
   inventory. Run the complete set without a trusted baseline, after shared
   mutation infrastructure, toolchain, or selection changes, or when impact
   cannot be bounded. Otherwise rerun only missing, failed, expired, or
   invalidated suites. Other supplemental work requires its own selection.
   Tagging, installation, lock updates, and push preparation do not invalidate
   unchanged evidence.

4. From the clean exact candidate, run the read-only preflight with Git's
   lowercase full commit object ID:

   ```sh
   ./scripts/release-preflight.sh <candidate-commit-id>
   ```

   It verifies the current commit, clean worktree, version, changelog heading,
   and either an absent tag or an annotated tag pointing directly at the
   candidate.

5. Build one native publication directory on each supported host from that
   exact commit. Install pinned policy tools first. On Linux and macOS:

   ```sh
   ./scripts/build-release.sh --output /absolute/path/to/publication
   ```

   On Windows x64:

   ```powershell
   .\scripts\build-release.ps1 -Output C:\release\code-polishy.zip -PublicationDirectory C:\release\publication
   ```

   Retain each archive, `.sha256`, internal manifest, CycloneDX SBOM,
   deterministic in-toto/SLSA provenance metadata, and `.release.json`
   descriptor. This metadata is reproducible digest binding, not authenticated
   builder or publisher evidence. The five required hosts are `darwin-arm64`,
   `darwin-x64`, `linux-arm64`, `linux-x64`, and `windows-x64`.

6. Combine the five descriptors with `release-manifest index`, using one
   repeated `--artifact-descriptor` per host. Publication succeeds only when
   every sidecar validates and every descriptor names the same version and
   source revision. Build Linux OCI images only from their verified publication
   directories:

   ```sh
   ./scripts/build-oci-image.sh \
     --publication-dir /release/linux-x64 \
     --image registry.example/code-polishy:v<VERSION> \
     --push
   ```

   Retain the exact image digest and Buildx SBOM/provenance attestations. Tags
   aid discovery; examples and consumers must use `image@sha256:...`.

7. Exercise one fresh archive installation per native host with the descriptor's
   archive SHA-256:

   ```sh
   code-polishy install-bundle \
     --source /absolute/path/code-polishy-<version>-<host>.zip \
     --sha256 <archive-sha256> \
     --prefix /absolute/path/to/fresh-prefix
   ```

   Run representative sequential commands through the installed launcher and
   verify the release manifest afterward. Run
   `./scripts/test-installed-release.sh --prefix PREFIX --lock LOCK` for the
   installed target contracts; use its exact fixture selector only for a
   bounded retry.

8. Create the annotated tag and rerun preflight:

   ```sh
   git tag -a v<VERSION> -m "Code Polishy <VERSION>" <candidate-commit-id>
   ./scripts/release-preflight.sh <candidate-commit-id>
   ```

   Push `main` and the tag without rewriting history. Publish the verified host
   directories, release index, and digest-pinned OCI images against that tag.
   Protect release tags against deletion or update; every correction gets a new
   patch version.

9. Move each consuming repository to the installed release with that release's
   `lock` command. The outgoing lock and guidance govern until the atomic lock
   replacement; incoming guidance governs afterward. Updating this repository's
   self-hosting lock is a separate follow-up commit because the release digest
   names the source commit that produced it.

Credentialed registry publication, repository release creation, tag changes,
and target lock changes remain explicit maintainer actions. The source is
Apache-2.0 licensed.
