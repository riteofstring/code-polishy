#!/usr/bin/env bash
set -euo pipefail

# Acquire the exact policy-owned JavaScript runtime: the pinned Node build and
# the pinned pnpm release. Both are downloaded from their allowlisted origins,
# verified against checked-in SHA-256 values, staged, and only then moved into
# place with a same-directory rename.
#
# Nothing here resolves node, npm, npx, pnpm, or corepack from PATH, a user
# cache, or a global installation. Every execution uses an absolute staged path
# under a closed environment with a scratch HOME, so user configuration cannot
# change what is installed. No package manager runs, so no dependency lifecycle
# script executes during acquisition.

# shellcheck source=tools/javascript-env.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/javascript-env.sh"

policy_root="${javascript_policy_root}"
node_version="$(tr -d '[:space:]' <"${policy_root}/tools/node-version.txt")"
pnpm_version="$(tr -d '[:space:]' <"${policy_root}/tools/pnpm-version.txt")"
inventory="${policy_root}/tools/javascript_runtime_checksums.txt"
binary_inventory="${policy_root}/tools/javascript_runtime_binaries.txt"
verify_sha256="${policy_root}/tools/verify-sha256.sh"

platform_tag="${javascript_platform_tag}"
runtime_root="${javascript_runtime_root}"
destination="${javascript_runtime_dir}"
node_relative="node/bin/node"
pnpm_relative="pnpm/bin/pnpm.cjs"

for required_tool in curl tar awk diff find; do
  if ! command -v "${required_tool}" >/dev/null 2>&1; then
    echo "${required_tool} is required to install the policy-owned JavaScript runtime." >&2
    exit 1
  fi
done

temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-javascript.XXXXXX")"
staging="${runtime_root}/.staging-${platform_tag}.$$"
cleanup() {
  rm -rf "${temporary_dir}" "${staging}"
}
trap cleanup EXIT

# Nothing this installer runs may see the invoking user's configuration, and the
# scratch home it uses instead disappears with the temporary directory.
javascript_scratch_home="${temporary_dir}/home"

# List every prebuilt binary artifact below a component root, as sorted paths
# relative to that root.
list_binary_artifacts() {
  (
    cd "$1" && find . -type f \
      \( -name '*.node' -o -name '*.wasm' -o -name '*.exe' \
      -o -name '*.dll' -o -name '*.so' -o -name '*.dylib' \) -print
  ) | sed 's|^\./||' | LC_ALL=C sort
}

installed_versions_match() {
  local root="$1"
  [[ -x "${root}/${node_relative}" && -f "${root}/${pnpm_relative}" ]] || return 1
  [[ "$(javascript_sealed_run "${root}/${node_relative}" --version 2>/dev/null)" == "v${node_version}" ]] || return 1
  [[ "$(javascript_sealed_run "${root}/${node_relative}" "${root}/${pnpm_relative}" --version 2>/dev/null)" \
    == "${pnpm_version}" ]] || return 1
  return 0
}

if installed_versions_match "${destination}"; then
  echo "Node ${node_version} and pnpm ${pnpm_version} are already installed at ${destination}."
  exit 0
fi

node_archive="node-v${node_version}-${platform_tag}.tar.gz"
pnpm_archive="pnpm-${pnpm_version}.tgz"

echo "Downloading Node ${node_version} for ${platform_tag}..."
curl -fsSL "https://nodejs.org/dist/v${node_version}/${node_archive}" \
  -o "${temporary_dir}/${node_archive}"
"${verify_sha256}" "${inventory}" "${node_archive}" "${temporary_dir}/${node_archive}"

echo "Downloading pnpm ${pnpm_version}..."
curl -fsSL "https://registry.npmjs.org/pnpm/-/${pnpm_archive}" \
  -o "${temporary_dir}/${pnpm_archive}"
"${verify_sha256}" "${inventory}" "${pnpm_archive}" "${temporary_dir}/${pnpm_archive}"

mkdir -p "${runtime_root}" "${staging}" "${temporary_dir}/extract"
tar -xzf "${temporary_dir}/${node_archive}" -C "${temporary_dir}/extract"
mv "${temporary_dir}/extract/node-v${node_version}-${platform_tag}" "${staging}/node"
tar -xzf "${temporary_dir}/${pnpm_archive}" -C "${temporary_dir}/extract"
mv "${temporary_dir}/extract/package" "${staging}/pnpm"

# pnpm is the only supported package manager, so the package managers bundled
# with the Node distribution are removed instead of shipped unused.
rm -rf \
  "${staging}/node/bin/npm" \
  "${staging}/node/bin/npx" \
  "${staging}/node/bin/corepack" \
  "${staging}/node/lib/node_modules/npm" \
  "${staging}/node/lib/node_modules/corepack"
retained_managers="$(ls -A "${staging}/node/lib/node_modules" 2>/dev/null || true)"
if [[ -n "${retained_managers}" ]]; then
  echo "Node ${node_version} bundles unexpected package managers: ${retained_managers}" >&2
  exit 1
fi

# The Node distribution ships no prebuilt binary beyond its own executable, and
# the pinned pnpm release ships an exact, reviewed set. Any change to that set
# must be reviewed before Code Polishy executes it.
node_binaries="$(list_binary_artifacts "${staging}/node")"
if [[ -n "${node_binaries}" ]]; then
  echo "Node ${node_version} shipped unexpected prebuilt binaries:" >&2
  printf '%s\n' "${node_binaries}" >&2
  exit 1
fi
expected_pnpm_binaries="$(awk '!/^[[:space:]]*#/ && NF { print $1 }' "${binary_inventory}" | LC_ALL=C sort)"
if ! diff -u \
  <(printf '%s\n' "${expected_pnpm_binaries}") \
  <(list_binary_artifacts "${staging}/pnpm"); then
  echo "pnpm ${pnpm_version} does not match tools/javascript_runtime_binaries.txt." >&2
  exit 1
fi

if ! installed_versions_match "${staging}"; then
  echo "Staged runtime is not exactly Node ${node_version} and pnpm ${pnpm_version}." >&2
  exit 1
fi

rm -rf "${destination}"
mv "${staging}" "${destination}"
echo "Installed Node ${node_version} and pnpm ${pnpm_version} at ${destination}."
