#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

"${policy_root}/scripts/test.sh" ./cmd/code-polishy/...
"${policy_root}/scripts/go.sh" run ./cmd/code-polishy --policy-root "${policy_root}" pack verify --source "${policy_root}/tools/fixtures/community-pack"
"${policy_root}/scripts/test-go-mutation.sh"
"${policy_root}/scripts/test-install.sh"
"${policy_root}/scripts/test-release-preflight.sh"
"${policy_root}/scripts/test-javascript-runtime.sh"
"${policy_root}/scripts/test-javascript-bundle.sh"
"${policy_root}/scripts/test-javascript-runner.sh"
"${policy_root}/scripts/test-javascript-project.sh"
