#!/usr/bin/env bash
set -euo pipefail

# Verify that a materialized policy-owned JavaScript tool bundle carries nothing
# Code Polishy refuses to execute.
#
# Usage: verify-javascript-bundle-tree.sh <bundle-dir>
#
# The bundle admits no native add-on and no downloaded executable, so one
# materialized tree is valid on every supported host and contains only the
# JavaScript, declarations, and data its packages published. pnpm already
# refuses to build a dependency during installation; this proves the result.

if [[ "$#" -ne 1 ]]; then
  echo "usage: verify-javascript-bundle-tree.sh <bundle-dir>" >&2
  exit 2
fi

bundle_dir="$1"
if [[ ! -d "${bundle_dir}/node_modules" ]]; then
  echo "The JavaScript bundle at ${bundle_dir} is not materialized." >&2
  exit 1
fi

prebuilt_binaries="$(
  cd "${bundle_dir}" && find node_modules \( -type f -o -type l \) \
    \( -name '*.node' -o -name '*.wasm' -o -name '*.exe' \
    -o -name '*.dll' -o -name '*.so' -o -name '*.dylib' \) -print
)"
if [[ -n "${prebuilt_binaries}" ]]; then
  echo "The JavaScript bundle at ${bundle_dir} contains prebuilt binaries:" >&2
  printf '%s\n' "${prebuilt_binaries}" >&2
  exit 1
fi

# pnpm's isolated linker resolves every package through a relative symlink inside
# the bundle. A link that names an absolute path or climbs out of the bundle
# would let a file no lock admitted be loaded as a policy tool.
link_leaves_bundle() {
  local link="$1" target="$2" depth=0 segment
  case "${target}" in /*) return 0 ;; esac
  while read -r segment; do
    case "${segment}" in
      '' | .) ;;
      ..)
        depth=$((depth - 1))
        if [[ "${depth}" -lt 0 ]]; then
          return 0
        fi
        ;;
      *) depth=$((depth + 1)) ;;
    esac
  done < <(printf '%s/%s\n' "$(dirname "${link}")" "${target}" | tr '/' '\n')
  return 1
}

escaping_links="$(
  cd "${bundle_dir}" && find node_modules -type l -print | while read -r link; do
    target="$(readlink "${link}")"
    if link_leaves_bundle "${link}" "${target}"; then
      printf '%s -> %s\n' "${link}" "${target}"
    fi
  done
)"
if [[ -n "${escaping_links}" ]]; then
  echo "The JavaScript bundle at ${bundle_dir} contains symlinks that leave it:" >&2
  printf '%s\n' "${escaping_links}" >&2
  exit 1
fi

# pnpm records what it installed, what it had to build, and what it left out.
# The bundle admits no build and no skipped package, so the installed tree is
# complete on every supported host rather than quietly host specific.
metadata="${bundle_dir}/node_modules/.modules.yaml"
if [[ ! -f "${metadata}" ]]; then
  echo "The JavaScript bundle at ${bundle_dir} records no installation metadata." >&2
  exit 1
fi

expect_metadata() {
  local description="$1"
  local pattern="$2"
  if ! grep -qF "${pattern}" "${metadata}"; then
    echo "The JavaScript bundle at ${bundle_dir} ${description}." >&2
    exit 1
  fi
}

pnpm_version="$(tr -d '[:space:]' <"$(dirname "${BASH_SOURCE[0]}")/pnpm-version.txt")"
expect_metadata "was installed by a package manager other than the pinned pnpm ${pnpm_version}" \
  "\"packageManager\": \"pnpm@${pnpm_version}\""
expect_metadata "contains dependencies that still need a build" '"pendingBuilds": []'
expect_metadata "skipped packages instead of installing the complete tree" '"skipped": []'
expect_metadata "was installed from a registry other than the npm registry" \
  '"default": "https://registry.npmjs.org/"'
expect_metadata "was installed with implicit hoisting" '"hoistedDependencies": {}'
