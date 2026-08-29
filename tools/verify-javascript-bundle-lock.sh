#!/usr/bin/env bash
set -euo pipefail










if [[ "$#" -ne 1 ]]; then
  echo "usage: verify-javascript-bundle-lock.sh <pnpm-lock.yaml>" >&2
  exit 2
fi

lockfile="$1"
if [[ ! -f "${lockfile}" ]]; then
  echo "Missing bundle lockfile ${lockfile}." >&2
  exit 1
fi

fail() {
  echo "Bundle lockfile ${lockfile}: $1" >&2
  exit 1
}



if [[ "$(awk -F': ' '/^lockfileVersion:/ { gsub(/'"'"'/, "", $2); print $2; exit }' "${lockfile}")" != "9.0" ]]; then
  fail "is not lockfile version 9.0"
fi


if ! awk '/^settings:/ { in_settings = 1; next }
  /^[^[:space:]]/ { in_settings = 0 }
  in_settings && $1 == "autoInstallPeers:" && $2 == "false" { found = 1 }
  END { exit found ? 0 : 1 }' "${lockfile}"; then
  fail "does not record autoInstallPeers: false"
fi



importer_count="$(awk '/^importers:/ { in_importers = 1; next }
  /^[^[:space:]]/ { in_importers = 0 }
  in_importers && /^  [^[:space:]]/ { count++ }
  END { print count + 0 }' "${lockfile}")"
if [[ "${importer_count}" != "1" ]]; then
  fail "declares ${importer_count} importers instead of exactly one"
fi


while read -r specifier; do
  [[ -n "${specifier}" ]] || continue
  if [[ ! "${specifier}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    fail "declares non-exact direct specifier '${specifier}'"
  fi
done < <(awk '/^importers:/ { in_importers = 1; next }
  /^[^[:space:]]/ { in_importers = 0 }
  in_importers && $1 == "specifier:" { print $2 }' "${lockfile}")



resolutions="$(grep -c '^    resolution: {' "${lockfile}" || true)"
if [[ "${resolutions}" -lt 1 ]]; then
  fail "resolves no packages"
fi
registry_resolutions="$(grep -c '^    resolution: {integrity: sha512-' "${lockfile}" || true)"
if [[ "${resolutions}" != "${registry_resolutions}" ]]; then
  offending="$(grep -n '^    resolution: {' "${lockfile}" |
    grep -v '{integrity: sha512-' | head -5 || true)"
  fail "resolves packages without registry integrity:
${offending}"
fi


for prohibited_key in patchedDependencies overrides packageExtensions \
  allowedDeprecatedVersions neverBuiltDependencies onlyBuiltDependencies \
  ignoredOptionalDependencies supportedArchitectures pnpmfileChecksum; do
  if grep -q "^${prohibited_key}:" "${lockfile}"; then
    fail "declares prohibited key '${prohibited_key}'"
  fi
done



for prohibited_source in 'version: git' 'version: https' 'version: http' \
  'version: file:' 'version: link:' 'version: workspace:' 'tarball:' \
  'repo:' 'commit:' 'patch_hash' '@jsr/'; do
  if grep -qF "${prohibited_source}" "${lockfile}"; then
    fail "declares prohibited source '${prohibited_source}'"
  fi
done
