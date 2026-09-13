#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=tools/javascript-env.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/tools/javascript-env.sh"
javascript_scratch_home="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-provider-test.XXXXXX")"
trap 'rm -r "${javascript_scratch_home}"' EXIT
provider_test_root="${javascript_scratch_home}/pack"
"${javascript_policy_root}/scripts/build-javascript-provider.sh" "${provider_test_root}"
cp "${javascript_policy_root}/providers/javascript/provider.test.mjs" \
  "${javascript_policy_root}/providers/javascript/fixtures.mjs" \
  "${provider_test_root}/providers/javascript/"
javascript_sealed_run "${javascript_node}" --test \
  "${provider_test_root}/providers/javascript/provider.test.mjs"
