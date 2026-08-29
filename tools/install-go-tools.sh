#!/usr/bin/env bash
set -euo pipefail








repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
staticcheck_version="$(tr -d '[:space:]' <"${repo_root}/tools/staticcheck-version.txt")"
govulncheck_version="$(tr -d '[:space:]' <"${repo_root}/tools/govulncheck-version.txt")"

mkdir -p "${repo_root}/.tools/bin"
export GOBIN="${repo_root}/.tools/bin"
"${repo_root}/scripts/go.sh" install "honnef.co/go/tools/cmd/staticcheck@${staticcheck_version}"
"${repo_root}/scripts/go.sh" install "golang.org/x/vuln/cmd/govulncheck@${govulncheck_version}"

"${repo_root}/.tools/bin/staticcheck" -version
"${repo_root}/.tools/bin/govulncheck" -version
