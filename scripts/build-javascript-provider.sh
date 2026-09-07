#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=tools/javascript-env.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/tools/javascript-env.sh"
javascript_scratch_home="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-provider-build.XXXXXX")"
trap 'rm -r "${javascript_scratch_home}"' EXIT
if (($# > 1)) || { (($# == 1)) && [[ "$1" != /* ]]; }; then
  echo "usage: build-javascript-provider.sh ABSOLUTE_OUTPUT_DIRECTORY" >&2
  exit 2
fi
provider_output="${1:-${javascript_scratch_home}/pack}"
javascript_sealed_run "${javascript_node}" "${javascript_policy_root}/providers/javascript/build.mjs" prepare "${provider_output}"
javascript_sealed_pnpm "${provider_output}/providers/javascript" install --frozen-lockfile --ignore-scripts --offline --node-linker=hoisted
javascript_sealed_run "${javascript_node}" "${javascript_policy_root}/providers/javascript/build.mjs" finish "${provider_output}"
