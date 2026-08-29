#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ty_version="$(tr -d '[:space:]' <"${policy_root}/tools/ty-version.txt")"

case "$(uname -s)" in
  Darwin) os_tag="apple-darwin" ;;
  Linux) os_tag="unknown-linux-gnu" ;;
  *) echo "Unsupported OS for policy-owned ty: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch_tag="aarch64" ;;
  x86_64|amd64) arch_tag="x86_64" ;;
  *) echo "Unsupported architecture for policy-owned ty: $(uname -m)" >&2; exit 1 ;;
esac

archive_platform="${arch_tag}-${os_tag}"
archive="ty-${archive_platform}.tar.gz"

case "${ty_version}:${archive_platform}" in
  0.0.65:aarch64-apple-darwin) expected_sha="528f0eb7564ac42dded760762c94ee48d107752874c5697af2f7a49e3db244ba" ;;
  0.0.65:x86_64-apple-darwin) expected_sha="17f5eabf61e2cf9973a2fd6807367d491e4a684cb3566802d151713f65ca429a" ;;
  0.0.65:aarch64-unknown-linux-gnu) expected_sha="c91e3a43700291aa984dc01c32c69e01288f9b85167985d3b1e15b0d9c532818" ;;
  0.0.65:x86_64-unknown-linux-gnu) expected_sha="a8e061c140d0f9a9d50259bc8abf457a97dd099e7ba72a989cd9bf1fceb58d6b" ;;
  *) echo "No checked-in checksum for ty ${ty_version} on ${archive_platform}." >&2; exit 1 ;;
esac

destination="${policy_root}/.tools/bin/ty"
if [[ -x "${destination}" ]] && [[ "$("${destination}" --version | awk '{ print $2 }')" == "${ty_version}" ]]; then
  echo "ty ${ty_version} is already installed at ${destination}."
  exit 0
fi

temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-ty.XXXXXX")"
cleanup() {
  rm -rf "${temporary_dir}"
}
trap cleanup EXIT

archive_path="${temporary_dir}/${archive}"
url="https://github.com/astral-sh/ty/releases/download/${ty_version}/${archive}"
echo "Downloading ty ${ty_version} for ${archive_platform}..."
curl -fsSL "${url}" -o "${archive_path}"

if command -v shasum >/dev/null 2>&1; then
  actual_sha="$(LC_ALL=C LANG=C shasum -a 256 "${archive_path}" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  actual_sha="$(sha256sum "${archive_path}" | awk '{print $1}')"
else
  echo "A SHA-256 checksum tool (shasum or sha256sum) is required." >&2
  exit 1
fi

if [[ "${expected_sha}" != "${actual_sha}" ]]; then
  echo "Checksum mismatch for ${archive}." >&2
  exit 1
fi

tar -xzf "${archive_path}" -C "${temporary_dir}"
extracted_bin="${temporary_dir}/ty-${archive_platform}/ty"
if [[ ! -x "${extracted_bin}" ]]; then
  echo "ty archive did not contain the expected executable." >&2
  exit 1
fi

mkdir -p "${policy_root}/.tools/bin"
mv "${extracted_bin}" "${destination}"
chmod +x "${destination}"
if [[ "$("${destination}" --version | awk '{ print $2 }')" != "${ty_version}" ]]; then
  echo "Installed ty does not report ${ty_version}." >&2
  exit 1
fi
"${destination}" --version
