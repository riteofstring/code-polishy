# Authenticated Scanner Manifests

The scanner package is a signed JSON envelope, independently versioned and
published from this repository. It references the immutable official Trivy image
index; it contains no executable or Dockerfile. Code Polishy owns image
acquisition, its minimal runtime builder, Docker restrictions, severity thresholds,
size limits, database source and freshness, and OpenVEX reconciliation.

## Publisher and execution contract

`artifact-security/publisher.pem` is the approved Ed25519 public key carried by
an installed Code Polishy release. Candidate packages and repository configuration
cannot replace it. A consumer-selected digest authenticates transport bytes;
the signature authenticates the assessment publisher independently. Verification
uses Go's standard library and accepts a PKIX PEM Ed25519 public key only.

The envelope contains base64 `payload` and `signature` fields. The signed message
is the UTF-8 bytes `code-polishy-scanner-manifest/v1`, one NUL byte, and the exact payload
bytes. Assessment consumption verifies signatures before decoding claims for acceptance. Duplicate keys, unknown
payload fields, malformed evidence, expired assessments and unsupported contracts
are rejected. The payload's SHA-256 is the manifest identity in scan reports;
the update command's SHA-256 identifies the downloaded envelope. Both identities
are reproducible; signatures do not permit replacing the payload.

The payload has `packageVersion` for the assessment package and `version` for the
underlying Trivy binary. It also carries `sourceImage`, `contract`, `reassessBy`,
`openVex`, and compact `evidence` references. Evidence links should identify
immutable source revisions and reproducible inspection results. A package
revision may change assessments while keeping the image and binary version.

`code-polishy-trivy/v1` binds both Linux amd64 and arm64 to the existing minimal
scratch runtime, copied upstream executable and CA bundle, `GODEBUG=http2client=0`,
and the enabled `version`, anonymous database-download and offline archive-scan
commands. Only the private database cache reaches the networked downloader.
Offline scans have no network, credentials, host environment or runtime socket;
mounts, environment, dropped capabilities and rootless user mapping remain
Code Polishy-owned. Changes invalidating these assumptions require a new contract
and reassessment. A package cannot negotiate extra commands or privileges.

The release-owned `artifact-security/scanner-contract.txt` declares its execution
ABI. Compatibility checks read that declaration from the verified incoming
release, rather than applying the outgoing engine's contract version. The
signature's format domain stays independent of execution-contract revisions.
Changing the declaration requires the corresponding runtime review and
reassessment; it exposes no configurable commands or privileges.

## Bounds and lock lifecycle

The signed payload is at most 4 KiB; acquisition accepts an envelope of at most
8 KiB, including base64 and signature overhead. The complete lock remains bounded
to 32 KiB by the Go reader and both bootstrap wrappers. Atomic lock publication
validates that total bound before replacement. Application OpenVEX retains its
existing 64 KiB bound; scanner assessments deliberately use the smaller package
bound and compact evidence references.

`scanner-update --manifest HTTPS_URL --sha256 ENVELOPE_SHA256` previews an exact
candidate. Adding `--apply` verifies it and writes its complete signed envelope
into the lock. Gates use that envelope, never a newer package or the unsigned
source manifest. Setup needs no scanner download beyond the existing upstream
image acquisition. Restoring the checkout's lock restores the package selection.

Scanner adoption and Code Polishy authority cutover share the repository write
lock. Scanner update captures the expected lock before network acquisition and
checks that state and the executing installed release identity while holding the
write lock. A stale
operation cannot replace the other's selection. Abandoned preparation files do
not become authority. Ordinary Code Polishy upgrades preserve the scanner and
verify its publisher, payload shape and contract against the incoming release,
including during upgrade apply. Retaining an expired pin does not authorize its
assessments: adoption and scanning still require a current deadline. Incompatible
signers, malformed payloads or contracts block the upgrade rather than silently
replacing selection.

Retained capability comparisons match the Code Polishy release independently of
the scanner envelope. Scanner adoption preserves the original authenticated
catalog proofs and comparison, even after removal of the outgoing installation.
All other lock fields must still match. Upgrade plans and write transactions keep
full-lock comparisons so a changed scanner selection invalidates stale cutovers.

The first supporting Code Polishy upgrade introduces this lock field. Initial
adoption explicitly runs `scanner-update` afterward; configured artifact gates
fail closed until an authenticated package is selected. Update and recovery
commands remain available when the old assessment deadline has passed. Expiry
blocks suppression authorization, including security refresh of historical gate
evidence. Acceptance rechecks the deadline after scanning as well.

Planning projects optional OpenVEX enforcement steps from the pinned packet and
target documents. Runtime signature and policy validation authorize execution.
Empty documents omit the enforcement scan in both the plan and execution, keeping
reusable gate evidence consistent with the commands actually required.

Reports retain the package identity and actual scanner platform beside the binary version, source image and
database digest. A changed payload changes gate identity. Fresh advisory data
still changes findings without changing either tool pin; artifact security runs
again during security refresh rather than trusting an old accepted verdict.

## Admission and publication

The unsigned `artifact-security/scanner-manifest.json` is the publisher's source
input, never runtime authority. Its binary `version` drives the existing Trivy
`releaseArtifacts` GitHub release-age lookup and exact security-fix assessment.
JSON version files expose their top-level exact `version`; ambiguous or oversized
JSON is rejected. Assessment package revisions do not reset the binary's age.

The manually dispatched scanner workflow builds the existing tools, runs online
supply-chain admission, signs using the protected publisher secret, and stages a
draft package. Native amd64 and arm64 jobs install Code Polishy and run the
existing installed scanner/self-scan and scratch-image checks using that signed
candidate. Both checks must pass before publication. Reports and SBOMs, including
their advisory database identities, are retained as release evidence. The GitHub
repository's immutable-release setting freezes the completed publication.

`artifact-security/scanner.json` is a public signed bootstrap fixture for ordinary
Code Polishy installed-release checks. It is prepared during publisher setup and
refreshed explicitly when needed. Its unit test authenticates the checked-in
bytes and contract and validates OpenVEX at its recorded timestamp, independently
of current expiry. Virtual-time tests retain successful renewal after bootstrap
expiry and rejection of expired publication candidates. Signing and native
candidate checks still enforce the current assessment deadline. Runtime gates still require the envelope in
the consumer lock; the fixture is not an implicit selection or fallback.

Disposable keys exist only in temporary test directories. No test key is shipped
as an approved publisher. The RSA JWK applicability
assessment is grounded in its mapped execution-path evidence. Binary absence
claims require reproducible inspection for both architectures; negative string
searches do not establish absence.
