# Git Dependency Evidence

The online supply-chain profile accepts an exact Python Git dependency when a
trusted CI assessment binds its repository, full commit, optional subdirectory,
source contents, and complete resolved `uv.lock` inventory. The assessment must
provide complete vulnerability and license results and an authenticated age
record. Ordinary candidate pin, lock agreement, license, vulnerability, and
minimum-age policy still apply.

## Configure provider trust

Configure `supplyChain.gitEvidence` in `.code-polishy.json`. It has two arrays:
`providers` and `attestations`. A declaration schedules no command, credential
probe, installation, or supplemental test.

Each provider has these exact fields:

| Field          | Meaning                                                                                          |
| -------------- | ------------------------------------------------------------------------------------------------ |
| `name`         | Unique provider identifier used as the signature key ID.                                         |
| `kind`         | `ed25519-ci/v1`.                                                                                 |
| `issuer`       | Canonical HTTPS identity of the authorized CI assessor, without credentials, query, or fragment. |
| `publicKey`    | Standard canonical base64 encoding of the trusted 32-byte Ed25519 public key.                    |
| `policySha256` | Lowercase SHA-256 of the reviewed assessment policy.                                             |
| `scanners`     | Approved scanner identities, each with exact `name`, `version`, and lowercase binary `sha256`.   |

Each attestation has `scope`, `provider`, and `path`. The scope is the exact
repository-relative `uv.lock` path; the provider names one configured trust
entry. The path names one signed artifact. Paths must be canonical and
contained, without globs, traversal, or symbolic links. Each scope has one
artifact, and every artifact path is distinct.

A CI job can download its artifact to
`.code-polishy-reports/git-evidence/<name>.json`. This managed location permits
short-lived evidence without a commit for every assessment. A governed regular
file elsewhere in the repository is also supported. Other excluded locations
are rejected. No secret belongs in any of these declarations. Keep the signing
key in the provider's existing credential mechanism; configure only its public
key locally.

Trust applies to the CI assessor's assertions, including source identity and
publication history. A content hash or a checked-in JSON document alone does
not establish trust. Review changes to the issuer, signing key, policy digest,
and approved scanner identities as changes to that authority.

## Produce the signed assessment

The authorized provider uses its existing repository credentials to obtain the
exact Git commit and contained subdirectory. It verifies the source snapshot
and scans those contents and the complete resolved lock inventory using the
configured policy and scanner. It records source snapshot SHA-256, scanner
identity, advisory-data identity and freshness, every vulnerability, and each
remote package's SPDX license expression. Fetching and assessment must not
install dependencies or run their lifecycle scripts.

Age comes from the provider's authenticated publication or durable
first-observed record for that exact commit. A first-observed record must be
created when the provider actually observes the commit and retained without
backdating or replacement. Author and committer dates and mutable tag dates
cannot supply that record. The record URL must be under the configured issuer,
without credentials, query parameters, fragments, or path traversal.

The supported envelope is DSSE with payload type
`application/vnd.code-polishy.git-evidence.v1+json`. Its fields are
`payloadType`, `payload`, and `signatures`; `payload` is canonical standard
base64 of the exact UTF-8 JSON statement. There is exactly one signature with
`keyid` equal to the provider name and `sig` containing the canonical standard
base64 Ed25519 signature. Sign DSSE's pre-authentication encoding:

```text
DSSEv1 <payload-type byte length> <payload type> <payload byte length> <payload>
```

The statement has exactly these fields:

| Field                    | Meaning                                                                                                                                        |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `protocol`               | `git-evidence/v1`.                                                                                                                             |
| `issuer`, `policySha256` | Exact configured provider identities.                                                                                                          |
| `scope`                  | Exact lockfile path.                                                                                                                           |
| `lockSha256`             | SHA-256 of the current lockfile's raw bytes.                                                                                                   |
| `inventorySha256`        | SHA-256 of the canonical complete remote package inventory described below.                                                                    |
| `issuedAt`, `expiresAt`  | Authenticated RFC 3339 times; issuance is within the last 24 hours and expiry is after verification and no later than 24 hours after issuance. |
| `subjects`               | Exactly one subject per resolved Git package source.                                                                                           |
| `scan`                   | Complete, fresh assessment of the Git contents and resolved inventory.                                                                         |

Each subject has `ecosystem`, `name`, `version`, `repository`, `commit`,
`subdirectory`, `treeSha256`, and `observation`. The ecosystem is `git`;
repository identity retains the `git+https://` or `git+ssh://` scheme and omits
SSH user information. Commit and subdirectory must match the candidate.
`treeSha256` binds the exact source snapshot the provider scanned.
`observation` has `kind` (`publication` or `first-observed`), `record` (the
authenticated record URL), and `timestamp` (the observed RFC 3339 time).

The canonical inventory is a compact UTF-8 JSON array, without a trailing
newline. Every object has fields in this order: `ecosystem`, `name`, `version`,
`source`. Include all resolved remote packages, remove exact duplicates, and
sort lexicographically by the four values joined with NUL characters. Git
packages use ecosystem `git` and source
`git+<scheme>://<host>/<repository>@<full-commit>`, followed by
`#subdirectory=<contained-path>` when present. Registry packages use ecosystem
`pypi` and source `registry:<exact-registry-URL>`. Local packages are omitted;
unsupported sources fail coverage. Use JSON string escaping without ASCII-only
Unicode escaping; escape `<`, `>`, `&`, U+2028, and U+2029 as the corresponding
lowercase `\u` sequences, matching the protocol's Go JSON encoding.

The `scan` object has exactly these fields:

| Field                                | Meaning                                                                                                                         |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------- |
| `coverage`                           | `complete`, or an explicit unavailable status described below.                                                                  |
| `target`                             | `git-contents-and-resolved-lock/v1`.                                                                                            |
| `scanner`                            | An exact configured scanner identity.                                                                                           |
| `completedAt`                        | Assessment completion, within one hour before issuance.                                                                         |
| `advisoryVersion`, `advisoryUpdated` | Advisory snapshot identity and update time; the snapshot predates scan completion and is no more than 24 hours old at issuance. |
| `vulnerabilities`                    | Explicit array, including an empty array for a complete clean result.                                                           |
| `licenses`                           | Exactly one license result for each canonical remote inventory coordinate.                                                      |

Every vulnerability has `ecosystem`, `name`, `version`, `source`, `id`,
`severity`, and `knownExploited`. The first four fields identify an exact
inventory entry. Severity is `low`, `medium`, `high`, `critical`, or `unknown`;
known exploitation is an explicit boolean. Every license result has
`ecosystem`, `name`, `version`, `source`, and `expression`. Missing, extra, or
duplicate coordinates fail coverage. A registry package sharing a Git
dependency's claimed name and version does not prove the Git contents safe.

The shipped `schema/code-polishy-git-evidence.schema.json` defines envelope and
statement structure. Objects reject unknown, duplicate, missing, and
case-variant keys. Duplicate-key and 16-level nesting checks run before object
decoding; schema resolution reads only shipped resources. Signature, trust,
identity, freshness, and complete inventory checks remain separate requirements.
Arrays must be present and non-null. Each signed artifact and decoded payload
is bounded to 4 MiB, with at most 1,024 Git subjects and 8,192 vulnerability or license
results. The repository inventory is separately bounded to 512 lockfiles,
16 MiB per lock, and 32 MiB in total.

## Verify and retain evidence

After the provider exports its artifact to the configured path, run
`code-polishy supply-chain`. Local signature verification reads contained,
bounded regular files and uses the configured public key. It does not retrieve
private source, read the signing key, install dependencies, or execute the
provider.

Recursive public OSV scanning excludes uv locks. A registry-only uv lock is
projected into public PyPI package coordinates and scanned separately; results
retain the original lockfile scope. A lock containing Git sources receives
vulnerability and license coverage from its complete signed assessment.
Public registry release-age checks remain active. Non-public registry
identities are never queried at PyPI; absent authenticated publication coverage
for such packages remains an explicit failure. Other ecosystems retain their
native OSV scan inputs.

JSON and SARIF reports retain verified artifact receipts, including provider,
path, artifact digest, assessment-policy digest, lock and inventory digests,
local retrieval time, and expiry. A verified receipt records authenticated
evidence; findings still determine admission. Gate identities also bind current evidence and its
validity. Changed artifacts, trust, source identity, policy, or expiry prevent
reuse, and a change during verification prevents acceptance. Reading the same
fresh artifact updates its local retrieval time without invalidating its
otherwise unchanged reusable identity.

## Resolve evidence failures

| Outcome                                        | Supported action                                                                                                                   |
| ---------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `unavailable`                                  | Configure provider trust and export its signed assessment to the declared path.                                                    |
| `authentication`                               | Restore the provider's existing repository credentials and rerun assessment; the signed scan reports `authentication-unavailable`. |
| `unsupported-scanner`                          | Select an approved scanner that assesses the exact contents and resolved inventory; an unsupported scan reports `unsupported`.     |
| `invalid`, `untrusted`, `stale`, or `expired`  | Correct the artifact or trust mismatch and obtain fresh evidence for the current candidate.                                        |
| `coverage` or `age-coverage`                   | Supply the missing complete scan, inventory, source identity, or durable observation record.                                       |
| Vulnerability, license, or minimum-age failure | Resolve the actual policy finding; fresh signatures alone cannot remove it.                                                        |

General exceptions cannot suppress Git evidence or Git vulnerability failures.
