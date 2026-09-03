#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
"${policy_root}/tools/install-go.sh"
"${policy_root}/tools/install-go-tools.sh"
"${policy_root}/tools/install-javascript-runtime.sh"
"${policy_root}/tools/install-javascript-bundle.sh"
"${policy_root}/tools/install-shellcheck.sh"
"${policy_root}/tools/install-ruff.sh"
"${policy_root}/tools/install-ty.sh"
"${policy_root}/tools/install-python.sh"
"${policy_root}/tools/install-vulture.sh"
"${policy_root}/tools/install-osv-scanner.sh"
"${policy_root}/tools/install-gremlins.sh"
