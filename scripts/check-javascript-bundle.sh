#!/usr/bin/env bash
set -euo pipefail

# Prove that the sealed JavaScript tool bundle's committed lock is the complete,
# authoritative, and currently accurate record of what is installed.
#
# Usage: check-javascript-bundle.sh [--write-inventory]
#
# This is the repository's lock-sync provider for the bundle manifest. It runs
# entirely offline: the lock is checked for prohibited sources, checked against
# the manifest with pnpm's frozen resolution, and then reconciled package by
# package against the materialized tree and its licenses.
#
# `--write-inventory` regenerates the checked-in inventory instead of comparing
# against it. It is the only supported way to update that file.

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=tools/javascript-env.sh
source "${policy_root}/tools/javascript-env.sh"

inventory="${policy_root}/tools/javascript_bundle_inventory.txt"
write_inventory=false
case "${1-}" in
  "") ;;
  --write-inventory) write_inventory=true ;;
  *)
    echo "usage: check-javascript-bundle.sh [--write-inventory]" >&2
    exit 2
    ;;
esac

# Licenses Code Polishy accepts inside the bundle. Each is permissive or, for
# MPL-2.0, file-level copyleft over files the bundle ships unmodified and never
# links into Code Polishy's own source.
approved_licenses="Apache-2.0 BSD-2-Clause BSD-3-Clause BlueOak-1.0.0 CC0-1.0 ISC MIT MPL-2.0 Python-2.0"

temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-javascript-bundle-check.XXXXXX")"
cleanup() {
  rm -rf "${temporary_dir}"
}
trap cleanup EXIT
javascript_scratch_home="${temporary_dir}/home"

"${policy_root}/tools/verify-javascript-bundle-lock.sh" \
  "${javascript_bundle_source}/pnpm-lock.yaml"

# pnpm resolves nothing here: a manifest the lock does not already satisfy is a
# failure. The checked-in files are copied first so no check can rewrite them.
javascript_copy_bundle_source "${temporary_dir}/bundle"
javascript_sealed_pnpm "${temporary_dir}/bundle" \
  install --lockfile-only --frozen-lockfile --offline >/dev/null

if [[ ! -d "${javascript_bundle_dir}" ]]; then
  echo "The policy-owned JavaScript bundle is not installed at ${javascript_bundle_dir}." >&2
  echo "Run ./tools/install-javascript-bundle.sh first." >&2
  exit 1
fi

# The manifest proves the installed tree was built from exactly this checked-in
# source and that none of its bytes changed afterwards.
"${policy_root}/tools/javascript-bundle-manifest.sh" verify "${javascript_bundle_dir}"
"${policy_root}/tools/verify-javascript-bundle-tree.sh" "${javascript_bundle_dir}"

# The pinned pnpm reports the license of every installed package. Flattening its
# grouped report is the only JavaScript here; which licenses are acceptable and
# whether the result matches the lock are decided below.
installed_inventory="${temporary_dir}/inventory.txt"
# shellcheck disable=SC2016  # the flattener is JavaScript; bash must not expand it
javascript_sealed_run env "npm_config_store_dir=${javascript_store}" \
  "${javascript_node}" "${javascript_pnpm}" \
  --dir "${javascript_bundle_dir}" --config.storeDir="${javascript_store}" \
  licenses list --json |
  javascript_sealed_run "${javascript_node}" -e '
    let raw = "";
    process.stdin.on("data", (chunk) => (raw += chunk)).on("end", () => {
      const rows = [];
      for (const [license, packages] of Object.entries(JSON.parse(raw)))
        for (const entry of packages)
          for (const version of entry.versions)
            rows.push(`${entry.name}@${version}\t${license}`);
      process.stdout.write(rows.sort().join("\n") + "\n");
    });
  ' >"${installed_inventory}"

# The inventory must account for exactly the packages the lock names: an extra
# package means the tree drifted, and a missing one means the report is partial.
locked_packages="${temporary_dir}/locked.txt"
awk '/^packages:/ { in_packages = 1; next }
  /^[^[:space:]]/ { in_packages = 0 }
  in_packages && /^  [^[:space:]]/ { gsub(/^  '"'"'?|'"'"'?:$/, ""); print }' \
  "${javascript_bundle_source}/pnpm-lock.yaml" | LC_ALL=C sort >"${locked_packages}"
inventoried_packages="${temporary_dir}/inventoried.txt"
awk -F'\t' '{ print $1 }' "${installed_inventory}" | LC_ALL=C sort >"${inventoried_packages}"
if ! diff -u "${locked_packages}" "${inventoried_packages}"; then
  echo "The installed JavaScript bundle does not match the packages its lock names." >&2
  exit 1
fi

unapproved="$(awk -F'\t' -v approved="${approved_licenses}" '
  BEGIN { split(approved, list, " "); for (position in list) allowed[list[position]] = 1 }
  !($2 in allowed) { print $1 " declares unapproved license " $2 }
' "${installed_inventory}")"
if [[ -n "${unapproved}" ]]; then
  echo "The installed JavaScript bundle contains unapproved licenses:" >&2
  printf '%s\n' "${unapproved}" >&2
  exit 1
fi

inventory_header="# Every package installed in the sealed, policy-owned JavaScript tool bundle,
# as \"<name>@<version><tab><SPDX license>\", reported by the pinned pnpm from
# the materialized tree rather than derived from the lock alone.
#
# Regenerate with ./scripts/check-javascript-bundle.sh --write-inventory
# whenever the bundle manifest or lock changes; a manual edit is drift, not an
# update."

if [[ "${write_inventory}" == true ]]; then
  {
    printf '%s\n' "${inventory_header}"
    cat "${installed_inventory}"
  } >"${inventory}"
  echo "Wrote ${inventory}."
  exit 0
fi

if [[ ! -f "${inventory}" ]]; then
  echo "Missing bundle inventory ${inventory}." >&2
  exit 1
fi
checked_in="${temporary_dir}/checked-in.txt"
grep -v '^#' "${inventory}" >"${checked_in}" || true
if ! diff -u "${checked_in}" "${installed_inventory}"; then
  echo "tools/javascript_bundle_inventory.txt does not match the installed bundle." >&2
  echo "Run ./scripts/check-javascript-bundle.sh --write-inventory after reviewing the change." >&2
  exit 1
fi
