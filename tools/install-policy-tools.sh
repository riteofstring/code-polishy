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
"${policy_root}/tools/install-packaging.sh"
python_release="$(tr -d '[:space:]' <"${policy_root}/tools/python-version.txt")"
packaging_version="$(tr -d '[:space:]' <"${policy_root}/tools/packaging-version.txt")"
case "$(uname -s)" in
  Darwin) python_os="darwin" ;;
  Linux) python_os="linux" ;;
  *) echo "Unsupported OS for the Python facts environment: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  arm64|aarch64) python_arch="arm64" ;;
  x86_64|amd64) python_arch="x64" ;;
  *) echo "Unsupported architecture for the Python facts environment: $(uname -m)" >&2; exit 1 ;;
esac
python_bin="${policy_root}/.tools/python/${python_os}-${python_arch}/bin/python3.12"
facts_environment="${policy_root}/internal/pythonfacts/.venv"
facts_python="${facts_environment}/bin/python"
facts_environment_valid() {
  [[ -f "${facts_environment}/pyvenv.cfg" ]] && [[ -x "${facts_python}" ]] &&
    [[ "$("${facts_python}" -I -B -c 'import sys; print(".".join(str(value) for value in sys.version_info[:3]))' 2>/dev/null || true)" == "${python_release%%+*}" ]] &&
    [[ "$("${facts_python}" -I -B -c 'import importlib.metadata; print(importlib.metadata.version("packaging"))' 2>/dev/null || true)" == "${packaging_version}" ]] &&
    [[ ! -e "${facts_environment}/bin/pip" ]]
}
if ! facts_environment_valid; then
  facts_staging="$(mktemp -d "${policy_root}/internal/pythonfacts/.venv.staging.XXXXXX")"
  if ! "${python_bin}" -I -B -m venv --copies --without-pip --system-site-packages "${facts_staging}"; then
    rm -rf "${facts_staging}"
    exit 1
  fi
  rm -rf "${facts_environment}"
  mv "${facts_staging}" "${facts_environment}"
  if ! facts_environment_valid; then
    rm -rf "${facts_environment}"
    echo "The contained Python facts environment failed verification." >&2
    exit 1
  fi
fi
"${policy_root}/tools/install-vulture.sh"
"${policy_root}/tools/install-osv-scanner.sh"
"${policy_root}/tools/install-gremlins.sh"
