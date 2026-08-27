#!/usr/bin/env bash
set -euo pipefail

# Run the pinned ShellCheck the Code Polishy policy root carries.
#
# There is one binary. A release carries the ShellCheck version it pins, and so
# does this repository's own checkout once the pinned tools are installed.
# Nothing is taken from an ambient PATH, a host installation, or an environment
# override, so what a shell check decided cannot depend on the machine it ran
# on.

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
required_version="$(tr -d '[:space:]' <"${policy_root}/tools/shellcheck-version.txt")"
required_version="${required_version#v}"

case "$(uname -s)" in
  Darwin) os_tag="darwin" ;;
  Linux) os_tag="linux" ;;
  *)
    echo "Unsupported OS for the pinned ShellCheck: $(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch_tag="aarch64" ;;
  x86_64|amd64) arch_tag="x86_64" ;;
  *)
    echo "Unsupported architecture for the pinned ShellCheck: $(uname -m)" >&2
    exit 1
    ;;
esac

shellcheck_bin="${policy_root}/.tools/shellcheck/${os_tag}-${arch_tag}/shellcheck"
if [[ ! -x "${shellcheck_bin}" ]]; then
  echo "Pinned ShellCheck ${required_version} is not installed at ${shellcheck_bin}." >&2
  echo "Run ./tools/install-policy-tools.sh in a Code Polishy checkout." >&2
  exit 1
fi

installed_version="$("${shellcheck_bin}" --version 2>/dev/null | awk '/^version:/ {print $2}')"
if [[ "${installed_version}" != "${required_version}" ]]; then
  echo "Pinned ShellCheck must be ${required_version}, not ${installed_version:-unknown}." >&2
  exit 1
fi

exec "${shellcheck_bin}" "$@"
