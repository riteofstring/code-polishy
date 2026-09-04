# Authenticated Release Publication

Status: blocked

Target release: unassigned

This work is explicitly not part of v0.23.0 or v0.24.0. Assigning it to a
release requires a later, explicit planning decision and must not alter the
existing v0.24 plan implicitly.

## Outcome

Authenticate the environment that builds every native release archive, bind
each archive to its canonical SBOM, authenticate the combined release identity,
and independently verify the resulting Sigstore evidence before it can satisfy
Code Polishy policy.

Publication custody is not build provenance. Uploading an externally built
archive and signing it later proves only the publisher boundary. Every required
native host must therefore build within an authenticated environment or emit
independently authenticated compatible provenance at its build boundary.

## Starting point

v0.23.0 produces complete deterministic CycloneDX SBOMs and in-toto provenance
metadata using official models. That metadata binds release inputs and digests
reproducibly but does not authenticate a builder or publisher. It remains useful
input to this plan and must never be accepted as a substitute for the evidence
defined here.

## Publication boundary

Prefer an explicitly triggered, protected release workflow that builds the five
native artifacts from the exact release commit. At each authenticated build
boundary:

- use `actions/attest` pinned by its complete commit digest;
- grant `id-token: write`, `attestations: write`, and
  `artifact-metadata: write` only to the attestation job that requires them;
- create provenance for every archive;
- create an attestation binding every archive to its canonical SBOM;
- attest the combined release index separately;
- preserve Buildx attestations for OCI images; and
- publish every resulting Sigstore bundle with the release.

If a required host cannot run in that workflow, its external builder must emit
compatible evidence authenticating the build environment. A later GitHub
publisher signature does not satisfy that requirement.

## Independent verification

Use `github.com/sigstore/sigstore-go` behind a narrow verification boundary.
Its types and transitive dependencies must not enter unrelated core packages.
An admitted, versioned, and digested trusted root must come from an already
trusted launcher, bootstrap, or explicit verifier input; it must never be
learned from the unverified candidate release.

Verification first establishes the signature, trusted root, certificate
identity, issuer, transparency-log inclusion and time, and required TSA
evidence. Only after cryptographic verification succeeds may Code Polishy
decode a predicate and enforce the expected repository, workflow, source
revision and ref, event, builder, artifact name, and digest.

The release workflow remains an explicit maintainer-authorized action even when
the builders run in CI.

## Dependency admission

No executable component may enter the workflow or verifier without one exact,
automatic inventory, release-age, and vulnerability lane under the then-current
locked authority.

- `actions/attest` must use a full 40-character commit pin bound to an exact
  upstream release. Its inventory binds action metadata, distributed entry
  points, an exact lock or upstream SBOM, and all resolved executable packages
  to that commit. Hard and preferred age checks and automated vulnerability
  checks cover the complete graph. Missing or irreconcilable evidence blocks
  adoption.
- sigstore-go belongs to an exact Go module graph receiving the hard age check,
  `govulncheck`, OSV, direct-dependency review, and release-SBOM coverage.
- The trusted root uses an exact admitted identity, digest, provenance, schema
  validation, and semantic reconciliation. Updates are reviewed inputs rather
  than ambient network state.
- Every new lane participates in the checked-in weekly online supply-chain
  workflow and fails closed when its advisory source or vulnerability database
  is unavailable.

A checksum, commit pin, SBOM, upstream scan, or manual note cannot substitute
for a missing lane. An unauthenticated signature or candidate-supplied trust
root cannot substitute for authenticated build identity and independent
verification.

## Current blocker record

This record is current through 2026-09-04.

The newest age-admissible `actions/attest` release is v4.2.2, published
2026-08-04 at commit `1e69f48acb82d1966a394da916b4c1698aa569d6`.
Its exact lock contains `@sigstore/core` 2.0.0, affected by
`GHSA-jfc7-64v2-mr8c`, a DSSE payload-type binding failure fixed only in 3.2.1.
It also contains two nested copies of undici 6.25.0 affected by seven published
advisories: `GHSA-35p6-xmwp-9g52`, `GHSA-8xcm-r25x-g524`,
`GHSA-g8m3-5g58-fq7m`, `GHSA-m8rv-5g2x-5cg5`,
`GHSA-p88m-4jfj-68fv`, `GHSA-v3r7-h72x-cjcm`, and
`GHSA-vxpw-j846-p89q`. Attestation correctness is the component's direct
purpose, so the DSSE finding cannot be dismissed as unrelated code.

The specialized `actions/attest-build-provenance` and `actions/attest-sbom`
actions delegate to `actions/attest`; the latter is deprecated. They do not
provide a separate dependency boundary around these findings.

The newest age-admissible sigstore-go release is v1.3.0, published 2026-07-30.
It requires `golang.org/x/crypto` 0.54.0. `GO-2026-5932` affects the module from
its first release, has no fixed version, and has no assessable severity. The
advisory identifies the `openpgp` package family. The planned narrow verifier
path does not import that family, but the current complete-module OSV lane still
reports the unknown-severity finding, which cannot be admitted under existing
policy.

The repository therefore adds neither component, an attestation workflow, nor
a candidate-supplied trust root while this record remains unresolved. Work may
resume when upstream releases have admissible exact graphs or after an
explicitly authorized design change preserves authenticated build identity and
independent verification without a blanket vulnerability waiver.

## Implementation sequence

1. Establish an authenticated builder for every required native host without
   substituting a later publisher identity.
2. Establish age, dependency-inventory, and vulnerability coverage for the
   pinned attestation action before adding it to a workflow.
3. Add a protected, explicitly triggered release workflow with commit-pinned
   `actions/attest` and job-scoped OIDC and attestation permissions.
4. Create and retain archive provenance, archive-to-SBOM attestations, a release
   index attestation, and OCI attestations for the same release identity.
5. Integrate the narrow sigstore-go verifier with a trusted root obtained
   outside the unverified candidate.
6. Verify cryptographic identity and transparency evidence before applying
   Code Polishy's predicate and artifact policy.
7. Update the release checklist, publication descriptors, installation path,
   and terminology to require the authenticated bundles.

## Verification

The implementation must prove that:

- each required archive is built by an authenticated expected builder, and a
  publisher-only attestation is rejected as build provenance;
- every archive-to-SBOM relationship and the combined release index are covered
  by the expected Sigstore bundles;
- signature, trust-root, certificate, issuer, transparency, and time validation
  completes before predicate data can influence a policy result;
- attestation verification rejects the wrong repository, workflow, revision,
  ref, builder, event, subject name, or artifact digest;
- a trust root supplied only by the unverified candidate is rejected;
- DSSE payload-type mutations and predicate-type substitutions are rejected;
- local deterministic metadata is not accepted as authenticated publication
  evidence; and
- the complete action and Go dependency graphs reach their declared scanners
  and cannot disappear through another packaging form.

## Completion criteria

- Every native archive has authenticated build provenance.
- Every archive is bound to its canonical SBOM, and the combined release
  identity is independently verifiable.
- Publication-only identity is never reported as build provenance.
- Verification establishes cryptographic and transparency evidence before
  using predicates for policy.
- Trust roots are exact, admitted, and independent of the candidate they
  verify.
- Every executable dependency is exactly pinned, admitted, reviewed,
  inventoried, and represented in release evidence.
- No vulnerability, incomplete inventory, or unavailable scanner is bypassed
  merely to assign this plan to a release.
