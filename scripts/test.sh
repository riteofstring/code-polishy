#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"
if [[ $# -eq 0 ]]; then
  set -- ./...
fi
exec "${repo_root}/scripts/go.sh" test -count=1 "$@"
