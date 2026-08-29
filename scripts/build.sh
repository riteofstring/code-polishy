#!/usr/bin/env bash
set -euo pipefail













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
