#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-installed-contract.XXXXXX")"
cleanup() {
  rm -rf "${fixture_root}"
}
trap cleanup EXIT

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

"${policy_root}/scripts/install.sh" --prefix "${fixture_root}/prefix"
mkdir "${fixture_root}/target"
releases=("${fixture_root}"/prefix/releases/*/bin/code-polishy)
if [[ "${#releases[@]}" -ne 1 || ! -x "${releases[0]}" ]]; then
  echo "The temporary installation must contain one exact release binary." >&2
  exit 1
fi
release_root="$(dirname "$(dirname "${releases[0]}")")"
manifest="${release_root}/release-manifest.json"
version="$(awk -F'"' '/"codePolishyVersion"/ { print $4; exit }' "${manifest}")"
release_digest="$(awk -F'"' '/"releaseDigest"/ { print $4; exit }' "${manifest}")"
cat >"${fixture_root}/target/.code-polishy.lock.json" <<EOF
{
  "lockVersion": 2,
  "codePolishyVersion": "${version}",
  "releaseDigest": "${release_digest}",
  "features": ["javascript-bundle"],
  "publication": {
    "indexUrl": "https://example.invalid/release-index.json",
    "indexSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "archives": [
      {"host": "darwin-arm64", "url": "https://example.invalid/code-polishy-${version}-darwin-arm64.zip", "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "size": 1},
      {"host": "darwin-x64", "url": "https://example.invalid/code-polishy-${version}-darwin-x64.zip", "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "size": 1},
      {"host": "linux-arm64", "url": "https://example.invalid/code-polishy-${version}-linux-arm64.zip", "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "size": 1},
      {"host": "linux-x64", "url": "https://example.invalid/code-polishy-${version}-linux-x64.zip", "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "size": 1},
      {"host": "windows-x64", "url": "https://example.invalid/code-polishy-${version}-windows-x64.zip", "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "size": 1}
    ]
  }
}
EOF
"${fixture_root}/prefix/bin/code-polishy" --repo-root "${fixture_root}/target" \
  pack verify --source "${release_root}/tools/fixtures/language-pack"
catalog="${release_root}/tools/fixtures/code-polishy-pack-catalog-v1.json"
catalog_sha256="$(sha256_file "${catalog}")"
XDG_DATA_HOME="${fixture_root}/data" "${fixture_root}/prefix/bin/code-polishy" \
  pack catalog --catalog "${catalog}" --sha256 "${catalog_sha256}" --format json
XDG_DATA_HOME="${fixture_root}/data" "${fixture_root}/prefix/bin/code-polishy" \
  pack install --official sqlite-syntax-proof@1.0.0 --catalog "${catalog}" --sha256 "${catalog_sha256}"
XDG_DATA_HOME="${fixture_root}/data" "${fixture_root}/prefix/bin/code-polishy" \
  --repo-root "${fixture_root}/target" pack list --format json
"${policy_root}/scripts/test-installed-release.sh" \
  --prefix "${fixture_root}/prefix" \
  --lock "${fixture_root}/target/.code-polishy.lock.json" \
  --fixture first-adoption
