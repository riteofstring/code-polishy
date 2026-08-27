#!/usr/bin/env bash

# Sourceable locations and execution helper shared by every script that touches
# the policy-owned JavaScript runtime and tool bundle.
#
# Nothing here resolves node, npm, npx, pnpm, or corepack from PATH, a user
# cache, or a global installation: every path is absolute and policy owned, and
# every execution runs under a closed environment.

javascript_policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

case "$(uname -s)" in
  Darwin) javascript_os_tag="darwin" ;;
  Linux) javascript_os_tag="linux" ;;
  *)
    echo "Unsupported OS for the policy-owned JavaScript runtime: $(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  arm64|aarch64) javascript_arch_tag="arm64" ;;
  x86_64|amd64) javascript_arch_tag="x64" ;;
  *)
    echo "Unsupported architecture for the policy-owned JavaScript runtime: $(uname -m)" >&2
    exit 1
    ;;
esac

javascript_platform_tag="${javascript_os_tag}-${javascript_arch_tag}"
javascript_runtime_root="${javascript_policy_root}/.tools/javascript"
javascript_runtime_dir="${javascript_runtime_root}/${javascript_platform_tag}"
javascript_node="${javascript_runtime_dir}/node/bin/node"
javascript_pnpm="${javascript_runtime_dir}/pnpm/bin/pnpm.cjs"

# The bundle admits no native package, so one materialized tree serves every
# supported host and lives outside the per-platform runtime directory.
javascript_bundle_source="${javascript_policy_root}/tools/javascript"
javascript_bundle_dir="${javascript_runtime_root}/bundle"
javascript_store="${javascript_runtime_root}/store"

# The checked-in files that decide what an installed bundle contains, including
# the fixed runner entry point Code Polishy launches and the modules it loads,
# plus the one file installation generates: the manifest recording what was
# installed.
javascript_bundle_source_files=(package.json pnpm-lock.yaml pnpm-workspace.yaml .npmrc runner.mjs protocol.mjs audit.mjs deadcode.mjs imports.mjs licenses.mjs packages.mjs)
# shellcheck disable=SC2034  # read by the scripts that source this file
javascript_runner="${javascript_bundle_dir}/runner.mjs"
# shellcheck disable=SC2034  # read by the scripts that source this file
javascript_bundle_manifest_name="bundle-manifest.json"

if command -v shasum >/dev/null 2>&1; then
  javascript_digest_command=(shasum -a 256)
elif command -v sha256sum >/dev/null 2>&1; then
  javascript_digest_command=(sha256sum)
else
  echo "A SHA-256 checksum tool (shasum or sha256sum) is required." >&2
  exit 1
fi

# One digest over every checked-in file that determines what gets installed.
# Changing the manifest, the lock, the pnpm settings, the registry origin, or
# the runner invalidates an installed bundle.
javascript_bundle_source_digest() {
  (
    cd "${javascript_bundle_source}" &&
      LC_ALL=C "${javascript_digest_command[@]}" "${javascript_bundle_source_files[@]}" |
      LC_ALL=C "${javascript_digest_command[@]}" | awk '{print $1}'
  )
}

# Run an absolute policy-owned executable with credentials, proxies, loaders,
# package-manager settings, and user configuration removed. Callers set
# javascript_scratch_home to a directory they clean up, so no policy execution
# ever reads or writes the invoking user's home.
javascript_sealed_run() {
  if [[ -z "${javascript_scratch_home:-}" ]]; then
    echo "javascript_scratch_home must name a caller-owned scratch directory." >&2
    exit 1
  fi
  mkdir -p "${javascript_scratch_home}"
  env -i \
    PATH=/usr/bin:/bin \
    HOME="${javascript_scratch_home}" \
    XDG_CONFIG_HOME="${javascript_scratch_home}/config" \
    XDG_DATA_HOME="${javascript_scratch_home}/data" \
    XDG_CACHE_HOME="${javascript_scratch_home}/cache" \
    XDG_STATE_HOME="${javascript_scratch_home}/state" \
    npm_config_userconfig="${javascript_scratch_home}/absent-npmrc" \
    npm_config_globalconfig="${javascript_scratch_home}/absent-npmrc" \
    "$@"
}

# Run the pinned pnpm against a bundle directory, always through the pinned Node
# binary and always against the policy-owned store.
javascript_sealed_pnpm() {
  local bundle_dir="$1"
  shift
  if [[ ! -x "${javascript_node}" || ! -f "${javascript_pnpm}" ]]; then
    echo "The policy-owned JavaScript runtime is not installed at ${javascript_runtime_dir}." >&2
    echo "Run ./tools/install-javascript-runtime.sh first." >&2
    exit 1
  fi
  (
    cd "${bundle_dir}" &&
      javascript_sealed_run "${javascript_node}" "${javascript_pnpm}" \
        --store-dir "${javascript_store}" "$@"
  )
}

# Copy the checked-in bundle source into a directory pnpm may write to. The
# checked-in manifest, lock, and settings are never modified in place by a check.
javascript_copy_bundle_source() {
  local target="$1"
  local source_file
  mkdir -p "${target}"
  for source_file in "${javascript_bundle_source_files[@]}"; do
    cp "${javascript_bundle_source}/${source_file}" "${target}/"
  done
}
