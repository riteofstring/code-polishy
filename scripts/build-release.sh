#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
caller_root="$(pwd -P)"
output=""

usage() {
  echo "usage: build-release.sh --output DIR" >&2
  exit 2
}

while (($#)); do
  case "$1" in
    --output)
      if (($# < 2)); then
        usage
      fi
      output="$2"
      shift 2
      ;;
    --output=*)
      output="${1#*=}"
      shift
      ;;
    *) usage ;;
  esac
done

if [[ -z "${output}" ]]; then
  usage
fi
if [[ "${output}" != /* ]]; then
  output="${caller_root}/${output}"
fi
while [[ "${output}" != "/" && "${output}" == */ ]]; do
  output="${output%/}"
done
if [[ -e "${output}" ]]; then
  echo "The release publication directory already exists: ${output}" >&2
  exit 1
fi

scratch="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-release-build.XXXXXX")"
cleanup() {
  rm -rf "${scratch}"
}
trap cleanup EXIT INT TERM HUP

"${policy_root}/scripts/install.sh" \
  --prefix "${scratch}/prefix" \
  --publication-dir "${output}" \
  --build-only
