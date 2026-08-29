#!/usr/bin/env bash










policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=tools/javascript-env.sh
source "${policy_root}/tools/javascript-env.sh"

export bundle_source="${policy_root}/tools/javascript"
javascript_test_node_version() {
  tr -d '[:space:]' <"${policy_root}/tools/node-version.txt"
}
javascript_test_pnpm_version() {
  tr -d '[:space:]' <"${policy_root}/tools/pnpm-version.txt"
}



javascript_test_name="${javascript_test_name:?the sourcing script must name itself}"



fixture_root="$(cd "$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-${javascript_test_name}.XXXXXX")" && pwd -P)"
cleanup() {
  rm -rf "${fixture_root}"
}
trap cleanup EXIT
export javascript_scratch_home="${fixture_root}/home"

fail() {
  echo "${javascript_test_name}: $1" >&2
  exit 1
}



if [[ ! -x "${javascript_node}" || ! -f "${javascript_runner}" ]]; then
  fail "the policy-owned runtime and bundle are not installed; run ./tools/install-policy-tools.sh"
fi

run_runner() {
  javascript_sealed_run "$@" "${javascript_node}" "${javascript_runner}"
}
expect_runner_rejected() {
  local description="$1"
  local request="$2"
  shift 2
  if printf '%s' "${request}" | run_runner "$@" >"${fixture_root}/out" 2>&1; then
    fail "${description} was accepted"
  fi
  if ! grep -q '"error":"' "${fixture_root}/out"; then
    fail "${description} failed without a structured error"
  fi
}
