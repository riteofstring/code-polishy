#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python_release="$(tr -d '[:space:]' <"${policy_root}/tools/python-version.txt")"

if [[ ! "${python_release}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\+[0-9]{8}$ ]]; then
  echo "tools/python-version.txt must pin CPython and a python-build-standalone tag." >&2
  exit 1
fi
python_version="${python_release%%+*}"

case "$(uname -s)" in
  Darwin) os_tag="darwin"; archive_os="apple-darwin" ;;
  Linux) os_tag="linux"; archive_os="unknown-linux-gnu" ;;
  *) echo "Unsupported OS for policy-owned CPython: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch_tag="arm64"; archive_arch="aarch64" ;;
  x86_64|amd64) arch_tag="x64"; archive_arch="x86_64" ;;
  *) echo "Unsupported architecture for policy-owned CPython: $(uname -m)" >&2; exit 1 ;;
esac

platform_tag="${os_tag}-${arch_tag}"
archive="cpython-${python_release}-${archive_arch}-${archive_os}-install_only.tar.gz"
runtime_root="${policy_root}/.tools/python/${platform_tag}"
python_bin="${runtime_root}/python"
release_marker="${runtime_root}/.code-polishy-python-release"
checksum_inventory="${policy_root}/tools/python_runtime_checksums.txt"
verify_sha256="${policy_root}/tools/verify-sha256.sh"

for required_tool in awk curl tar; do
  if ! command -v "${required_tool}" >/dev/null 2>&1; then
    echo "${required_tool} is required to install the policy-owned CPython runtime." >&2
    exit 1
  fi
done

python_reported_version() {
  "$1" -I -B -c 'import sys; print(".".join(str(value) for value in sys.version_info[:3]))'
}

runtime_release() {
  tr -d '[:space:]' <"$1"
}

if [[ -x "${python_bin}" ]] && [[ -f "${release_marker}" ]] &&
  [[ "$(runtime_release "${release_marker}")" == "${python_release}" ]] &&
  [[ "$(python_reported_version "${python_bin}" 2>/dev/null)" == "${python_version}" ]]; then
  echo "CPython ${python_release} is already installed at ${runtime_root}."
  exit 0
fi

temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-python.XXXXXX")"
staging="${policy_root}/.tools/python/.staging-${platform_tag}.$$"
cleanup() {
  rm -rf "${temporary_dir}" "${staging}"
}
trap cleanup EXIT

archive_path="${temporary_dir}/${archive}"
url="https://github.com/astral-sh/python-build-standalone/releases/download/${python_release#*+}/${archive}"
echo "Downloading CPython ${python_release} for ${platform_tag}..."
curl -fsSL "${url}" -o "${archive_path}"
"${verify_sha256}" "${checksum_inventory}" "${archive}" "${archive_path}"

mkdir -p "${staging}"
tar -xzf "${archive_path}" --strip-components=1 -C "${staging}"
staged_python="${staging}/bin/python3.12"
if [[ ! -x "${staged_python}" ]] || [[ "$(python_reported_version "${staged_python}" 2>/dev/null)" != "${python_version}" ]]; then
  echo "The CPython archive did not contain the expected ${python_version} runtime." >&2
  exit 1
fi
ln -s "bin/python3.12" "${staging}/python"
printf '%s\n' "${python_release}" >"${staging}/.code-polishy-python-release"

mkdir -p "$(dirname "${runtime_root}")"
rm -rf "${runtime_root}"
mv "${staging}" "${runtime_root}"
echo "Installed CPython ${python_release} at ${runtime_root}."
