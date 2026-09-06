# Optional Git assessments

Local dependency review must work without a custom attestation service.
`supplyChain.gitEvidence.required` explicitly selects the stronger signed
assessment policy; its default is false. Provider declarations alone do not
activate that policy. Disabled evidence is neither read nor credited in gate
receipts, so stale artifacts cannot imply verified coverage.

Public registry scans cannot establish arbitrary Git-content security or an
authenticated publication date. The default therefore reports a nonblocking
Git-source coverage warning. It preserves exact pin and lock checks and runs
public registry vulnerability and minimum-age checks even in mixed Git and
registry locks. Public scan projections omit Git coordinates and never invent
registry versions or publication dates for them.

Opting in retains complete inventory binding, signature verification, scanner
trust, expiry, license, vulnerability, and authenticated-age requirements.
Missing or invalid evidence blocks the selected policy. Its receipt remains
part of gate identity and freshness validation.

See [Git dependency evidence](../git-dependency-evidence.md) for configuration
and the signed assessment protocol.
