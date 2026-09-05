#!/usr/bin/env bash
set -euo pipefail











if [[ "$#" -ne 1 ]]; then
  echo "usage: verify-javascript-bundle-tree.sh <bundle-dir>" >&2
  exit 2
fi

bundle_dir="$1"
if [[ ! -d "${bundle_dir}/node_modules" ]]; then
  echo "The JavaScript bundle at ${bundle_dir} is not materialized." >&2
  exit 1
fi





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




prebuilt_binaries="$(
  cd "${bundle_dir}" && find node_modules \( -type f -o -type l \) \
    \( -name '*.node' -o -name '*.wasm' -o -name '*.exe' \
    -o -name '*.dll' -o -name '*.so' -o -name '*.dylib' \) -print
)"
wasm_checksums="$(dirname "${BASH_SOURCE[0]}")/javascript_wasm_checksums.txt"
binary_digest() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  else
    shasum -a 256 "$1" | awk '{ print $1 }'
  fi
}
while IFS= read -r binary; do
  [[ -n "${binary}" ]] || continue
  expected="$(awk -v candidate="${binary}" '$2 == candidate { print $1 }' "${wasm_checksums}")"
  actual=""
  if [[ -f "${bundle_dir}/${binary}" && ! -L "${bundle_dir}/${binary}" ]]; then
    actual="$(binary_digest "${bundle_dir}/${binary}")"
  fi
  if [[ -z "${expected}" || "${actual}" != "${expected}" ]]; then
    echo "The JavaScript bundle contains an unapproved or altered prebuilt binary: ${binary}" >&2
    exit 1
  fi
done <<<"${prebuilt_binaries}"

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
