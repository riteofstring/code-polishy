#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-installed-contract.XXXXXX")"
cleanup() {
  rm -rf "${fixture_root}"
}
trap cleanup EXIT

"${policy_root}/scripts/install.sh" --prefix "${fixture_root}/prefix"
mkdir "${fixture_root}/target"
"${fixture_root}/prefix/bin/code-polishy" --repo-root "${fixture_root}/target" lock
"${policy_root}/scripts/test-installed-release.sh" \
  --prefix "${fixture_root}/prefix" \
  --lock "${fixture_root}/target/.code-polishy.lock.json" \
  --fixture first-adoption
