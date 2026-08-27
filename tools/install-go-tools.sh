#!/usr/bin/env bash
set -euo pipefail

# Install the pinned Go analyzers a release carries.
#
# The version each one is pinned to is checked in beside every other tool pin,
# because a release records the exact version of every executable it carries and
# the installer probes each against that same file. Naming a version here as
# well would let the two disagree.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
staticcheck_version="$(tr -d '[:space:]' <"${repo_root}/tools/staticcheck-version.txt")"
govulncheck_version="$(tr -d '[:space:]' <"${repo_root}/tools/govulncheck-version.txt")"

mkdir -p "${repo_root}/.tools/bin"
export GOBIN="${repo_root}/.tools/bin"
"${repo_root}/scripts/go.sh" install "honnef.co/go/tools/cmd/staticcheck@${staticcheck_version}"
"${repo_root}/scripts/go.sh" install "golang.org/x/vuln/cmd/govulncheck@${govulncheck_version}"

"${repo_root}/.tools/bin/staticcheck" -version
"${repo_root}/.tools/bin/govulncheck" -version
