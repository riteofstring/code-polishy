# Release Checklist

A Code Polishy release is one reviewed commit, one annotated `v<VERSION>` tag,
and one shared internal release identity across every artifact published for
that version. Publication may contain only the Linux x64 OCI image or the
complete native archive set. Tags and published digests are immutable. A
maintainer performs every operation that creates a tag, pushes, publishes, or
changes a target lock.

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

5. Select the publication scope before starting builds. For a GHCR-only release,
   build only the `linux-x64` publication directory on a native Linux x86-64
   executor by default; do not build the other native hosts or a release index.
   An amd64 container or virtual machine emulated on an Arm host is not native
   evidence. The maintainer may instead explicitly select the documented
   Docker Desktop fallback for a GHCR-only publication, accepting its slower
   execution and narrower evidence. For a complete native release, build one
   publication directory on every supported host from that exact commit.
   Install pinned policy tools first. On Linux and macOS:

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
   builder or publisher evidence. A complete native release requires
   `darwin-arm64`, `darwin-x64`, `linux-arm64`, `linux-x64`, and `windows-x64`.

6. For a complete native release, combine the five descriptors with
   `release-manifest index`, using one repeated `--artifact-descriptor` per
   host. Skip the index for a GHCR-only release. Build a Linux OCI image only
   from its verified publication directory:

   ```sh
   ./scripts/build-oci-image.sh \
     --publication-dir /release/linux-x64 \
     --image registry.example/code-polishy:v<VERSION> \
     --push
   ```

   Push mode pulls the exact registry digest and exercises the installed
   launcher as the image's declared non-root user. Retain that digest and the
   Buildx SBOM/provenance attestations. Tags aid discovery; examples and
   consumers must use `image@sha256:...`.

7. Exercise one fresh archive installation for every native host in the
   selected publication scope with the descriptor's archive SHA-256. Build the
   archive and exercise it on the same native host architecture, with the
   publication, installed prefix, and disposable fixtures on that executor's
   native filesystem:

   ```sh
   code-polishy install-bundle \
     --source /absolute/path/code-polishy-<version>-<host>.zip \
     --sha256 <archive-sha256> \
     --prefix /absolute/path/to/fresh-prefix
   ```

   Run representative sequential commands through the installed launcher and
   verify the release manifest afterward. Run
   `./scripts/test-installed-release.sh --prefix PREFIX --lock LOCK` for the
   installed target contracts exactly once; use its exact fixture selector only
   for a bounded retry. The complete harness deliberately invokes the stable
   launcher many times, and each invocation verifies the installed release.
   Do not count QEMU, Rosetta, Docker Desktop architecture emulation, or a macOS
   bind mount as native execution. Without a native executor, stop before
   tagging or publishing unless the maintainer explicitly selected the
   GHCR-only Docker Desktop fallback below. That fallback omits this fresh
   native archive-install contract and must report that limitation.

8. Create the annotated tag and rerun preflight:

   ```sh
   git tag -a v<VERSION> -m "Code Polishy <VERSION>" <candidate-commit-id>
   ./scripts/release-preflight.sh <candidate-commit-id>
   ```

   Push `main` and the tag without rewriting history. Publish only the selected
   verified host directories, optional release index, and digest-pinned OCI
   images against that tag. Protect release tags against deletion or update;
   every correction gets a new patch version.

9. Move each consuming repository to the installed release with that release's
   `lock` command. The outgoing lock and guidance govern until the atomic lock
   replacement; incoming guidance governs afterward. Updating this repository's
   self-hosting lock is a separate follow-up commit because the release digest
   names the source commit that produced it.

Credentialed registry publication, repository release creation, tag changes,
and target lock changes remain explicit maintainer actions. The source is
Apache-2.0 licensed.

## GHCR-only Linux x64 runbook

Use this path when the requested release artifact is only the public Linux x64
image at `ghcr.io/riteofstring/code-polishy`. It does not require macOS,
Windows, Linux arm64, or a five-host release index.

The default path runs the entire archive build, fresh-install contract, and
publication sequence on a native Linux x86-64 runner or VM. An amd64 Docker
container on an Apple Silicon or other Arm host is not native evidence:
`uname -m` inside it does not prove the host architecture, and a macOS bind
mount makes repeated release verification slower still. If the process list
contains `qemu-x86_64` or Rosetta while exercising the release, do not record
that execution as native acceptance.

Do not invoke `scripts/build-oci-image.sh` directly on macOS: it intentionally
requires Linux and GNU `sha256sum`. Buildx may assemble the final OCI image, but
Buildx architecture support does not turn an emulated container into acceptable
native release-verification evidence. Do not reconstruct the repository's
Buildx command by hand.

### Explicit Docker Desktop fallback

The maintainer may explicitly select the established Apple Silicon Docker
Desktop fallback for a GHCR-only release. Once selected, the absence of a native
publisher is not a blocker and is not a reason to propose new CI infrastructure.
This path is deliberately narrower than native publication: it builds the
Linux x64 archive and image under AMD64 emulation, omits the fresh native
archive-install contract in step 7, and must not be described as native release
evidence. Expect it to be substantially slower than a native runner.

Reuse the provisioned components that produced the previous successful image;
do not rediscover or recreate them during a release. On the maintainer's current
workstation these are the `code-polishy-v02412` `docker-container` Buildx
builder and the `local/code-polishy-oci-client:v0.24.11` Linux AMD64 client.
Before starting, inspect both, require the builder's digest-pinned BuildKit
image, confirm the target GHCR tag is absent, and verify the public annotated
source tag. Use a new temporary clean clone of that tag and a new absent
publication directory.

Populate the clone's Linux tool cache only from an exact digest-pinned prior
Code Polishy image after confirming the candidate did not change any tool pin
or bundle lock. Give the emulated client only the temporary clone, publication
directory, Docker socket, copied Buildx configuration, and a temporary Docker
configuration. Read the existing GHCR credential through Docker Desktop's
credential helper, pass it over standard input, never print it, and remove the
temporary Docker configuration on exit. Inside that one client invocation:

1. verify the exact commit, annotated tag, clean tree, Linux AMD64 environment,
   existing builder, and release preflight;
2. run `scripts/build.sh`, then `scripts/build-release.sh` for `linux-x64`; and
3. run `scripts/build-oci-image.sh --push` unchanged so SBOM/provenance
   attestations, exact registry digest resolution, digest pull, and the non-root
   launcher smoke test remain mandatory.

Run this sequence once. Retain the temporary publication until the public
digest is verified, then report both the digest and the omitted native
fresh-install evidence. A failure stops the publication; do not overwrite the
tag, switch to `latest`, or improvise a different Buildx command.

Before building, select an already provisioned Buildx builder and inspect it:

```sh
docker buildx use <approved-builder-name>
docker buildx inspect --bootstrap
```

The reported driver must be `docker-container`. The default `docker` driver
cannot preserve the required SBOM and provenance attestations. Provisioning the
builder must use an exact BuildKit image admitted under the supply-chain policy;
never pull an ambient mutable builder tag during a release.

Authenticate Docker once as a maintainer with package-write access. Keep the
credential outside the repository and command history:

```sh
printf '%s' "$GHCR_TOKEN" | \
  docker login ghcr.io --username "$GHCR_USER" --password-stdin
```

From the clean tagged source commit, verify the tag and build only the Linux x64
publication. Before the registry push, install that archive and complete step 7
on the same native executor. Then let the repository script push and verify the
image:

```sh
release_version="$(tr -d '[:space:]' < VERSION)"
candidate_commit="$(git rev-parse HEAD)"
test "$(git rev-parse "v${release_version}^{commit}")" = "$candidate_commit"
./scripts/release-preflight.sh "$candidate_commit"

publication_root="/absolute/path/code-polishy-${release_version}"
./scripts/build-release.sh \
  --output "${publication_root}/linux-x64"

image_tag="ghcr.io/riteofstring/code-polishy:v${release_version}"
./scripts/build-oci-image.sh \
  --publication-dir "${publication_root}/linux-x64" \
  --image "$image_tag" \
  --push
```

The final `image=...@sha256:...` line is the consumer identity. The script
creates a bounded context containing the verified release root, not the source
checkout or unrelated local files. It emits attestations, resolves the raw
registry index digest, pulls that exact digest, and exercises the launcher as
the declared non-root user. Do not separately repeat those build or smoke-test
steps, and do not publish `latest`.

GHCR visibility is package-wide. On the first successful publication, open the
package settings and change visibility to **Public**. This exposes every version
in the package, so publish corrections under a new patch tag rather than moving
or overwriting an existing tag.

Confirm a public pull without reusing maintainer credentials. Set `digest_ref`
to the exact final value reported by the script:

```sh
digest_ref="ghcr.io/riteofstring/code-polishy@sha256:<index-digest>"
anonymous_config="$(mktemp -d)"
docker --config "$anonymous_config" pull \
  --platform linux/amd64 "$digest_ref"
rmdir "$anonymous_config"
```

Stop at the first failed prerequisite instead of retrying the build:

| Symptom                                      | Resolution                                                                              |
| -------------------------------------------- | --------------------------------------------------------------------------------------- |
| Host is Arm or a release process uses QEMU   | Use native Linux, or the explicitly selected fallback without claiming native evidence. |
| Script prints usage immediately on macOS     | Use native Linux, or run the established fallback's Linux client.                       |
| Buildx reports unsupported attestations      | Select an admitted `docker-container` builder.                                          |
| GHCR returns denied or unauthorized          | Refresh the maintainer credential with package-write access.                            |
| Publication output already exists            | Choose a new absent output directory; never overwrite release evidence.                 |
| Anonymous pull returns unauthorized          | Change the GHCR package visibility to Public.                                           |
| The final version or digest smoke test fails | Fix the release and publish a new patch version; never move the tag.                    |
