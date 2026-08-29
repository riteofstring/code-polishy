#!/usr/bin/env bash
set -euo pipefail












# shellcheck source=tools/javascript-env.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/javascript-env.sh"

verify_lock="${javascript_policy_root}/tools/verify-javascript-bundle-lock.sh"
bundle_manifest="${javascript_policy_root}/tools/javascript-bundle-manifest.sh"

if ! command -v find >/dev/null 2>&1; then
  echo "find is required to install the policy-owned JavaScript bundle." >&2
  exit 1
fi

"${verify_lock}" "${javascript_bundle_source}/pnpm-lock.yaml"

if "${bundle_manifest}" verify "${javascript_bundle_dir}" >/dev/null 2>&1; then
  echo "The policy-owned JavaScript bundle is already installed at ${javascript_bundle_dir}."
  exit 0
fi

temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-javascript-bundle.XXXXXX")"
staging="${javascript_runtime_root}/.staging-bundle.$$"
cleanup() {
  rm -rf "${temporary_dir}" "${staging}"
}
trap cleanup EXIT

javascript_scratch_home="${temporary_dir}/home"
javascript_copy_bundle_source "${staging}"

echo "Fetching the policy-owned JavaScript bundle from the npm registry..."
javascript_sealed_pnpm "${staging}" fetch

echo "Materializing the policy-owned JavaScript bundle offline..."
javascript_sealed_pnpm "${staging}" install --offline --frozen-lockfile --ignore-scripts

"${javascript_policy_root}/tools/verify-javascript-bundle-tree.sh" "${staging}"

"${bundle_manifest}" write "${staging}"
"${bundle_manifest}" verify "${staging}"

rm -rf "${javascript_bundle_dir}"
mv "${staging}" "${javascript_bundle_dir}"
echo "Installed the policy-owned JavaScript bundle at ${javascript_bundle_dir}."
