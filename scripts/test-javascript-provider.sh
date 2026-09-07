#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=tools/javascript-env.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/tools/javascript-env.sh"
javascript_scratch_home="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-provider-test.XXXXXX")"
trap 'rm -r "${javascript_scratch_home}"' EXIT
javascript_sealed_run "${javascript_node}" --test "${javascript_policy_root}/providers/javascript/provider.test.mjs"
