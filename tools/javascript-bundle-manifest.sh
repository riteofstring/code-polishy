#!/usr/bin/env bash
set -euo pipefail













# shellcheck source=tools/javascript-env.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/javascript-env.sh"

if [[ "$#" -ne 2 ]]; then
  echo "usage: javascript-bundle-manifest.sh <write|verify> <bundle-dir>" >&2
  exit 2
fi

manifest_scratch="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-bundle-manifest.XXXXXX")"
cleanup() {
  rm -rf "${manifest_scratch}"
}
trap cleanup EXIT
javascript_scratch_home="${manifest_scratch}/home"
javascript_sealed_run "${javascript_node}" \
  "${javascript_bundle_source}/bundle-manifest.mjs" \
  "$1" "$2" "${javascript_policy_root}"
