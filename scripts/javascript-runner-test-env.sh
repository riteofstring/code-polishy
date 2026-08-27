#!/usr/bin/env bash

# The fixture environment the sealed runner's contract tests share.
#
# Each of those scripts exercises the installed bundle the same way: it stands
# up disposable fixtures under a temporary root, writes one request to the fixed
# entry point through the pinned runtime, and reads one response. Sourcing this
# keeps that setup in one place, so a test script is only the operations it
# covers. Set javascript_test_name before sourcing; it names the fixture root
# and prefixes every failure.

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=tools/javascript-env.sh
source "${policy_root}/tools/javascript-env.sh"

bundle_source="${policy_root}/tools/javascript"
# Every checked-in module the runner loads, so a fixture can stand one up
# outside the installed bundle without reaching into it.
# shellcheck disable=SC2034  # read by the scripts that source this file
runner_sources=("${bundle_source}"/*.mjs)
# shellcheck disable=SC2034  # read by the scripts that source this file
node_version="$(tr -d '[:space:]' <"${policy_root}/tools/node-version.txt")"
# shellcheck disable=SC2034  # read by the scripts that source this file
pnpm_version="$(tr -d '[:space:]' <"${policy_root}/tools/pnpm-version.txt")"

# The sourcing script names itself, so a failure says which contract failed and
# two fixture roots never collide.
javascript_test_name="${javascript_test_name:?the sourcing script must name itself}"

# Resolved physically: a file operation admits only a normal absolute root, and
# TMPDIR is allowed to be a symlink or to end in a separator.
fixture_root="$(cd "$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-${javascript_test_name}.XXXXXX")" && pwd -P)"
cleanup() {
  rm -rf "${fixture_root}"
}
trap cleanup EXIT
# shellcheck disable=SC2034  # read by the sealed launcher in javascript-env.sh
javascript_scratch_home="${fixture_root}/home"

fail() {
  echo "${javascript_test_name}: $1" >&2
  exit 1
}

# Every check runs the installed bundle, which the repository's toolchain
# installation materializes.
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
