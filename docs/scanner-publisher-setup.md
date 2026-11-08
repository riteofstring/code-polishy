# Scanner Publisher Setup and Signing

This one-time setup enrolls a maintainer-controlled signing key. It does not
publish a scanner package or change a consumer's scanner selection.

## Prepare the protected environment

In the `riteofstring/code-polishy` GitHub repository, create an Actions environment
named `scanner-publication`. Choose the maintainer reviewers who may approve signing jobs and restrict
deployments to the protected workflow branch. Keep immutable releases enabled. Review the tagged source commit before
approving its signing job: the workflow executes that commit's build and signer.

The environment secret `SCANNER_SIGNING_KEY` holds a PKCS8 PEM Ed25519 private
key. GitHub encrypts it and releases it only to the approved signing job. The
workflow writes it to a private runner temporary file, removes that file on exit,
and never passes the key as a command argument or prints it. Native verification
and final publication jobs receive no signing secret.

## Enroll the production key

GitHub CLI sign-in is not required. After authorizing enrollment, run these
commands from a private local terminal in the repository. They prepare the key
and public bootstrap files without contacting GitHub. Do not paste the private
key into chat.

```sh
set -euo pipefail
umask 077
scanner_key_dir="$(mktemp -d /tmp/code-polishy-scanner-publisher.XXXXXX)"
openssl genpkey -algorithm ED25519 -out "${scanner_key_dir}/private.pem"
openssl pkey -in "${scanner_key_dir}/private.pem" -pubout \
  -out artifact-security/publisher.pem
./scripts/build.sh
./.tools/bin/code-polishy --policy-root "$PWD" scanner-sign \
  --input artifact-security/scanner-manifest.json \
  --key "${scanner_key_dir}/private.pem" --output artifact-security/scanner.json
printf 'Private key for direct transfer to GitHub: %s/private.pem\n' "${scanner_key_dir}"
```

Open the repository's [Actions environments settings](https://github.com/riteofstring/code-polishy/settings/environments)
in your browser. Create or open `scanner-publication`, configure the required
maintainer reviewers and protected workflow branch, then choose **Add environment
secret**. Name it `SCANNER_SIGNING_KEY` and transfer the complete private PEM
file directly into its value field. Save the secret without posting its value
in chat, an issue, a workflow input, or a repository file. Browser access needs
repository permission to manage that environment; it does not use local CLI
credentials. The signing workflow consumes this environment secret automatically.

After GitHub confirms the secret was saved and any maintainer-controlled encrypted
backup is complete, remove the local key directory in that same terminal:

```sh
rm -rf "${scanner_key_dir:?}"
unset scanner_key_dir
```

Commit only `artifact-security/publisher.pem` and the public signed
`artifact-security/scanner.json` bootstrap fixture after reviewing them. The public
key enters consumers' trust boundary through their verified Code Polishy release.
Never commit the temporary directory or its private key. GitHub cannot return a
stored secret; any private-key backup belongs in maintainer-controlled encrypted
storage. Losing the key requires enrollment of a replacement public key in a new
Code Polishy release, not acceptance of a candidate-supplied key.

The bootstrap fixture is for installed-release verification. Consumer gates use
only the signed package in their lock. Before creating the first supporting Code
Polishy release, run its ordinary installed scanner checks on both native Linux
architectures; production enrollment itself supplies no verification receipt.

## Publish a scanner revision

1. Edit `artifact-security/scanner-manifest.json`. Increment `packageVersion`;
   keep `version` and `sourceImage` unchanged for an assessment-only revision.
   Bind evidence to immutable references, preserve release-age admission, and set
   an evidence-supported reassessment deadline. New binary-absence claims require
   reproducible inspection of both scanner architectures.
2. Commit the reviewed inputs and create an annotated `scanner-vMAJOR.MINOR.PATCH`
   tag matching `packageVersion`. Pushing a tag and dispatching publication are
   explicit maintainer actions.
3. Dispatch **Publish scanner manifest** for that tag and approve the protected
   environment job. The workflow stages a signed draft, verifies installed
   scanner and scratch-image behavior on Linux amd64 and arm64, and publishes only
   when both jobs pass. Download the retained reports to inspect accepted findings,
   binary/image identity, database identity and SBOMs.
4. Consumers first upgrade to a Code Polishy release trusting this publisher and
   contract, then preview and apply the published `scanner.json` using the digest
   in `scanner.sha256`. No gate automatically selects that revision.

A failed native check leaves an unpublished draft. Inspect its logs and reports;
do not publish it manually to bypass the checks. A maintainer may delete that
failed draft and rerun the workflow after resolving the failure, or prepare a new
reviewed revision. Updating `artifact-security/scanner.json` for later Code Polishy
release checks is a separate explicit dependency adoption of the verified public
package; it does not require changing the runtime or consumer pins.

## Authorization boundary

Publisher enrollment creates the protected GitHub environment,
generates a fresh publisher key, uploads its private part as that environment's
secret, and commits its public part and signed bootstrap fixture. This grants the
approved workflow authority to sign scanner assessments accepted by releases that
carry the public key. It requires maintainer authorization. Publishing the first
scanner package and upgrading consumer locks remain separate explicit actions.
