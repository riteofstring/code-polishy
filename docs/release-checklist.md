# Release Checklist

A Code Polishy release is one reviewed commit, one annotated `v<VERSION>` tag,
and one shared internal release identity across every artifact published for
that version. Publication contains the complete native archive set and its Linux
x64 OCI image. Tags and published digests are immutable. A maintainer performs
every tag push and target lock change. Pushing an annotated version tag
explicitly authorizes the checked-in release workflow to publish that release;
manual dispatch authorizes the same operation for an existing tag.

1. Bring the candidate to release shape as one reviewed commit. `VERSION` must
   contain a strict `MAJOR.MINOR.PATCH` version, optionally followed by a SemVer
   prerelease suffix such as `-beta.1`, and `CHANGELOG.md` must have
   an exact `## <VERSION> - <YYYY-MM-DD>` section. Remove completed temporary
   plans and obsolete docs. Use focused checks while editing; do not run a full
   gate against a changing worktree.

2. Stop changing the candidate, push that exact commit to `main`, and complete
   its one ordinary final gate. Honor `verification.finalGateOwner`: run locally
   for `local`, or retain the native CI result for `ci`. Ubuntu, macOS, and
   Windows must pass for the same commit. An exact already-passed gate executes
   no commands, and new gate identities may reuse only suite receipts whose
   complete inputs still match.

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

4. From the clean exact candidate after ordinary CI passes, run the read-only
   release preparation command:

   ```sh
   ./scripts/prepare-release-tag.sh
   ```

   It verifies the current commit, clean worktree, version, changelog heading,
   configured `main` upstream, exact pushed candidate, and absence of the
   version tag both locally and remotely. It performs no mutation and prints
   the candidate-specific maintainer commands only after every check passes.

5. Run only the commands printed by release preparation. They create the
   annotated tag, rerun the exact-candidate preflight, and push only that tag.
   Do not construct or provide release-tag commands before preparation passes.
   The version-tag push starts `.github/workflows/release.yml`; the workflow
   verifies that the tag is annotated, points directly at the candidate, and
   matches `VERSION`. Use its manual dispatch only to publish an existing
   annotated tag, such as a tag created before the workflow existed. Never move
   a failed or published tag; every correction gets a new patch version.

6. The release workflow builds `darwin-arm64`, `darwin-x64`, `linux-arm64`,
   `linux-x64`, and `windows-x64` in parallel on matching GitHub-hosted runners.
   Each job installs the pinned toolchain and runs the platform's existing
   release builder:

   ```sh
   ./scripts/build-release.sh --output /absolute/path/to/publication
   ```

   ```powershell
   .\scripts\build-release.ps1 -Output C:\release\code-polishy.zip -PublicationDirectory C:\release\publication
   ```

   The release workflow does not repeat the ordinary gate, unit suites,
   source-install contracts, or supplemental suites already owned by normal
   CI. Each host only verifies the generated manifest, installs its archive into
   a fresh prefix through the recorded SHA-256, and checks the installed
   version. The retained publication contains the archive, internal manifest,
   CycloneDX SBOM, deterministic in-toto/SLSA metadata, and release descriptor.

7. After every host succeeds, the publish job restores the five publication
   directories and reuses the verified Linux x64 archive's engine to combine
   their descriptors into `code-polishy-release-index.json`. It records the
   canonical index SHA-256, creates an index checksum sidecar, and requires the
   complete 27-file publication.

8. The same job gives Buildx an exact BuildKit image and the retained Linux x64
   archive. It does not rebuild source, rerun the native archive contract, or
   repeat ordinary CI. `scripts/build-oci-image.sh` publishes
   `ghcr.io/riteofstring/code-polishy:v<VERSION>` with SBOM and provenance
   attestations, resolves and pulls its registry digest, and exercises the
   installed launcher as the declared non-root user. An existing image tag is
   never overwritten.

   After the image passes, the job creates a draft GitHub Release through
   GitHub's API, uploads the native publication, and makes the release public
   only after every upload succeeds. A failed upload removes the draft; an
   existing release is never overwritten. The workflow uses neither `gh` nor a
   maintainer credential. Its repository-scoped token receives package-write
   access only in the final publish job. Release notes publish the source
   revision, index SHA-256, and digest-pinned OCI image.

9. Move each consuming repository with a publication-backed
   `upgrade plan --index URL --sha256 DIGEST`, inspect its capability and
   diagnostic delta, then use `upgrade apply --plan PATH`. An explicit
   `--accept-new-findings` permits the lock cutover without source cleanup.
   Upgrade does not run a gate. The outgoing lock and guidance govern until
   apply replaces the lock last; incoming guidance governs afterward. Updating
   this repository's self-hosting lock is a separate follow-up commit because
   the release digest names the source commit that produced it.

   For a new public adoption, use `lock --index URL --sha256 DIGEST` from the
   exact installed release so its first committed lock already supports native
   archive setup on every host.

Tag changes, release-workflow dispatch, and target lock changes remain explicit
maintainer actions. A tag push or dispatch authorizes both GitHub and GHCR
publication for that exact release. The source is Apache-2.0 licensed.
