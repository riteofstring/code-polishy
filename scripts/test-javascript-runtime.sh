#!/usr/bin/env bash
set -euo pipefail






policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verify_sha256="${policy_root}/tools/verify-sha256.sh"
installer="${policy_root}/tools/install-javascript-runtime.sh"
runtime_env="${policy_root}/tools/javascript-env.sh"
checksums="${policy_root}/tools/javascript_runtime_checksums.txt"
binaries="${policy_root}/tools/javascript_runtime_binaries.txt"

fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-javascript-test.XXXXXX")"
cleanup() {
  rm -rf "${fixture_root}"
}
trap cleanup EXIT

fail() {
  echo "test-javascript-runtime: $1" >&2
  exit 1
}

expect_rejected() {
  local description="$1"
  shift
  if "$@" >"${fixture_root}/out" 2>&1; then
    fail "${description} was accepted"
  fi
  if [[ ! -s "${fixture_root}/out" ]]; then
    fail "${description} failed without an explanation"
  fi
}

node_version="$(tr -d '[:space:]' <"${policy_root}/tools/node-version.txt")"
pnpm_version="$(tr -d '[:space:]' <"${policy_root}/tools/pnpm-version.txt")"


for pin in "node:${node_version}" "pnpm:${pnpm_version}"; do
  if [[ ! "${pin#*:}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    fail "${pin%%:*} version pin '${pin#*:}' is not an exact version"
  fi
done



expected_entries="node-v${node_version}-darwin-arm64.tar.gz
node-v${node_version}-darwin-x64.tar.gz
node-v${node_version}-linux-arm64.tar.gz
node-v${node_version}-linux-x64.tar.gz
node-v${node_version}-win-arm64.zip
node-v${node_version}-win-x64.zip
pnpm-${pnpm_version}.tgz"
actual_entries="$(awk '!/^[[:space:]]*#/ && NF { print $1 }' "${checksums}" | LC_ALL=C sort)"
if [[ "$(printf '%s\n' "${expected_entries}" | LC_ALL=C sort)" != "${actual_entries}" ]]; then
  fail "checksum inventory does not match the pinned Node ${node_version} and pnpm ${pnpm_version} archives"
fi
while read -r entry digest extra; do
  [[ -n "${entry}" ]] || continue
  case "${entry}" in \#*) continue ;; esac
  if [[ ! "${digest}" =~ ^[0-9a-f]{64}$ ]]; then
    fail "checksum inventory entry ${entry} has a malformed digest"
  fi
  if [[ -n "${extra}" ]]; then
    fail "checksum inventory entry ${entry} has trailing content"
  fi
done <"${checksums}"


while read -r artifact; do
  [[ -n "${artifact}" ]] || continue
  case "${artifact}" in
    \#*) continue ;;
    /*|*..*) fail "binary inventory entry ${artifact} is not a contained relative path" ;;
  esac
done <"${binaries}"
if ! grep -q 'reflink' "${binaries}"; then
  fail "binary inventory does not record the pnpm prebuilt binaries"
fi



for script in "${installer}" "${runtime_env}"; do
  if grep -nE 'command -v (node|npm|npx|pnpm|corepack)\b' "${script}"; then
    fail "$(basename "${script}") looks up a JavaScript tool on PATH"
  fi
  if grep -nE '^[[:space:]]*(node|npm|npx|pnpm|corepack)[[:space:]]' "${script}"; then
    fail "$(basename "${script}") invokes a JavaScript tool as a bare command"
  fi
done
if ! grep -q 'env -i' "${runtime_env}"; then
  fail "the shared runtime environment does not execute under a closed environment"
fi
if ! grep -q 'javascript_sealed_run' "${installer}"; then
  fail "installer does not execute the staged runtime under a closed environment"
fi


if (
  # shellcheck source=tools/javascript-env.sh
  source "${runtime_env}"
  unset javascript_scratch_home
  javascript_sealed_run true
) >/dev/null 2>&1; then
  fail "a sealed run without a caller-owned scratch home was accepted"
fi


payload="${fixture_root}/payload.bin"
printf 'sealed javascript runtime fixture\n' >"${payload}"
if command -v shasum >/dev/null 2>&1; then
  payload_digest="$(LC_ALL=C shasum -a 256 "${payload}" | awk '{print $1}')"
else
  payload_digest="$(LC_ALL=C sha256sum "${payload}" | awk '{print $1}')"
fi
good_inventory="${fixture_root}/good.txt"
printf '# fixture\npayload.bin %s\n' "${payload_digest}" >"${good_inventory}"
"${verify_sha256}" "${good_inventory}" payload.bin "${payload}" \
  || fail "matching digest was rejected"


tampered="${fixture_root}/tampered.bin"
printf 'sealed javascript runtime fixture (tampered)\n' >"${tampered}"
expect_rejected "a mismatched digest" "${verify_sha256}" "${good_inventory}" payload.bin "${tampered}"
expect_rejected "an unlisted entry" "${verify_sha256}" "${good_inventory}" absent.bin "${payload}"
expect_rejected "a missing file" "${verify_sha256}" "${good_inventory}" payload.bin "${fixture_root}/absent.bin"
expect_rejected "a missing inventory" "${verify_sha256}" "${fixture_root}/absent.txt" payload.bin "${payload}"
expect_rejected "wrong usage" "${verify_sha256}" "${good_inventory}" payload.bin

duplicate_inventory="${fixture_root}/duplicate.txt"
printf 'payload.bin %s\npayload.bin %s\n' "${payload_digest}" "${payload_digest}" >"${duplicate_inventory}"
expect_rejected "a duplicated entry" "${verify_sha256}" "${duplicate_inventory}" payload.bin "${payload}"

malformed_inventory="${fixture_root}/malformed.txt"
printf 'payload.bin %s\n' "$(printf '%s' "${payload_digest}" | tr 'a-f' 'A-F')" >"${malformed_inventory}"
expect_rejected "an uppercase pinned digest" "${verify_sha256}" "${malformed_inventory}" payload.bin "${payload}"

short_inventory="${fixture_root}/short.txt"
printf 'payload.bin %s\n' "${payload_digest:0:32}" >"${short_inventory}"
expect_rejected "a truncated pinned digest" "${verify_sha256}" "${short_inventory}" payload.bin "${payload}"

echo "test-javascript-runtime: all checks passed"
