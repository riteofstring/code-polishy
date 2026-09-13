#!/usr/bin/env bash
set -euo pipefail














usage() {
  echo "usage: release-version.sh <version-file>" >&2
  exit 2
}

if [[ "$#" -ne 1 || -z "$1" ]]; then
  usage
fi
version_file="$1"

fail() {
  printf '%s\n' "$@" >&2
  exit 1
}

if [[ ! -f "${version_file}" ]]; then
  fail "There is no VERSION file at ${version_file}, so no release version can be derived."
fi

version_file_content="$(cat "${version_file}" && printf x)"
version_file_content="${version_file_content%x}"
version="${version_file_content%$'\n'}"
prerelease_identifier='(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)'
strict_version="^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)(-${prerelease_identifier}(\\.${prerelease_identifier})*)?$"
if [[ ! "${version}" =~ ${strict_version} ]]; then
  condensed="$(printf '%s' "${version_file_content}" | tr -d '[:space:]')"
  if [[ "${condensed}" =~ ${strict_version} ]]; then
    fail "VERSION carries whitespace around or inside \`${condensed}\` beyond one trailing line ending." \
      "Store the exact released version as the file's only content, ending with one newline."
  fi
  fail "VERSION records \`${version}\`, which is not a strict MAJOR.MINOR.PATCH semantic version with an optional prerelease suffix." \
    "Set VERSION to the exact version this release records; the \`v<VERSION>\` tag name is derived from it."
fi

printf '%s\n' "${version}"
