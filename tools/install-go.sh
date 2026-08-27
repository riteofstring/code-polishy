#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
go_version="$(tr -d '[:space:]' < "${repo_root}/scripts/go_version.txt")"
go_version="${go_version#go}"

case "$(uname -s)" in
  Darwin) os_tag="darwin" ;;
  Linux) os_tag="linux" ;;
  *) echo "Unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch_tag="amd64" ;;
  arm64|aarch64) arch_tag="arm64" ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

archive="go${go_version}.${os_tag}-${arch_tag}.tar.gz"
destination="${repo_root}/.tools/go/${os_tag}-${arch_tag}"
go_bin="${destination}/go/bin/go"
if [[ -x "${go_bin}" ]] && [[ "$(GOTOOLCHAIN=local "${go_bin}" version | awk '{print $3}')" == "go${go_version}" ]]; then
  echo "Go ${go_version} is already installed at ${destination}/go."
  exit 0
fi

temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-go.XXXXXX")"
cleanup() { rm -rf "${temp_dir}"; }
trap cleanup EXIT

for required_tool in curl tar awk; do
  if ! command -v "${required_tool}" >/dev/null 2>&1; then
    echo "${required_tool} is required to install the pinned Go toolchain." >&2
    exit 1
  fi
done

expected_sha="$(awk -v archive="${archive}" '$1 == archive { print $2 }' "${repo_root}/tools/go_checksums.txt")"
if [[ ! "${expected_sha}" =~ ^[0-9a-f]{64}$ ]]; then
  echo "No pinned checksum for ${archive}." >&2
  exit 1
fi
curl -fsSL "https://go.dev/dl/${archive}" -o "${temp_dir}/${archive}"
if command -v shasum >/dev/null 2>&1; then
  actual_sha="$(shasum -a 256 "${temp_dir}/${archive}" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  actual_sha="$(sha256sum "${temp_dir}/${archive}" | awk '{print $1}')"
else
  echo "A SHA-256 checksum tool (shasum or sha256sum) is required." >&2
  exit 1
fi
if [[ "${actual_sha}" != "${expected_sha}" ]]; then
  echo "Checksum mismatch for ${archive}." >&2
  exit 1
fi

tar -xzf "${temp_dir}/${archive}" -C "${temp_dir}"
mkdir -p "$(dirname "${destination}")"
rm -rf "${destination}"
mkdir -p "${destination}"
mv "${temp_dir}/go" "${destination}/go"
echo "Installed Go ${go_version} at ${destination}/go."
