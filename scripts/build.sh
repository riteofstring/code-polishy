#!/usr/bin/env bash
set -euo pipefail

# Build the Code Polishy binaries with the pinned toolchain.
#
# Usage: build.sh [output-directory]
#
# A release carries both: `code-polishy` is the policy engine, and
# `code-polishy-launcher` is the stable entry point the installer places outside
# the release store so a target runs the exact release its lock names.
#
# Development builds land in the checkout's own tool directory. The release
# installer passes the staged directory it is about to verify, so a release
# binary is never written into the checkout first.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_dir="${1:-${repo_root}/.tools/bin}"
if [[ "$#" -gt 1 ]]; then
  echo "usage: build.sh [output-directory]" >&2
  exit 2
fi
cd "${repo_root}"
mkdir -p "${output_dir}"
"${repo_root}/scripts/go.sh" build -trimpath -o "${output_dir}/code-polishy" ./cmd/code-polishy
exec "${repo_root}/scripts/go.sh" build -trimpath \
  -o "${output_dir}/code-polishy-launcher" ./cmd/code-polishy-launcher
