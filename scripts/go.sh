#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
required_version="$(tr -d '[:space:]' < "${repo_root}/scripts/go_version.txt")"
required_tag="go${required_version#go}"

case "$(uname -s)" in
  Darwin) os_tag="darwin" ;;
  Linux) os_tag="linux" ;;
  MINGW*|MSYS*|CYGWIN*) os_tag="windows" ;;
  *) os_tag="" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch_tag="amd64" ;;
  arm64|aarch64) arch_tag="arm64" ;;
  *) arch_tag="" ;;
esac

version_for() {
  GOTOOLCHAIN=local "$1" version 2>/dev/null | awk '{print $3}'
}

# The pinned toolchain lives in one place. Nothing is taken from an ambient
# PATH or an environment override, so this script runs the Go the checkout
# carries or none at all.
go_bin=""
if [[ -n "${os_tag}" && -n "${arch_tag}" ]]; then
  go_bin="${repo_root}/.tools/go/${os_tag}-${arch_tag}/go/bin/go"
  [[ "${os_tag}" == "windows" ]] && go_bin="${go_bin}.exe"
fi
if [[ -z "${go_bin}" || ! -x "${go_bin}" ]]; then
  echo "Code Polishy requires the pinned Go ${required_tag}." >&2
  echo "Install it with ./tools/install-go.sh." >&2
  exit 1
fi
if [[ "$(version_for "${go_bin}")" != "${required_tag}" ]]; then
  echo "Code Polishy requires ${required_tag}; found $(version_for "${go_bin}")." >&2
  exit 1
fi

go_bin_directory="$(dirname "${go_bin}")"
export PATH="${go_bin_directory}:${PATH}"
export GOTOOLCHAIN=local
if [[ $# -eq 0 ]]; then
  exec "${go_bin}" version
fi
exec "${go_bin}" "$@"
