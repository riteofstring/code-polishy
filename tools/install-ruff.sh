#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ruff_version="$(tr -d '[:space:]' <"${policy_root}/tools/ruff-version.txt")"

case "$(uname -s)" in
  Darwin) os_tag="apple-darwin" ;;
  Linux) os_tag="unknown-linux-gnu" ;;
  *) echo "Unsupported OS for policy-owned Ruff: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch_tag="aarch64" ;;
  x86_64|amd64) arch_tag="x86_64" ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

archive_platform="${arch_tag}-${os_tag}"
archive="ruff-${archive_platform}.tar.gz"

case "${ruff_version}:${archive_platform}" in
  0.16.0:aarch64-apple-darwin) expected_sha="ce6564491a2cc4b0659f45ee174dbef17e4dec24e03a9c03d313b5430bc21099" ;;
  0.16.0:x86_64-apple-darwin) expected_sha="3d9ef6228c4eeb26d593c398b2dc5250e0f6d6425933db2993fcf30d49c78b69" ;;
  0.16.0:aarch64-unknown-linux-gnu) expected_sha="879d4f0ca1a7f21a4afc6ef9345118b8a75aa2bc4aae9e41e0474994d0ef0a4f" ;;
  0.16.0:x86_64-unknown-linux-gnu) expected_sha="98001c995a134d95f9bc83106a7f94b552971b583f1c0ab75fb656a881e13865" ;;
  *) echo "No checked-in checksum for Ruff ${ruff_version} on ${archive_platform}." >&2; exit 1 ;;
esac

destination="${policy_root}/.tools/bin/ruff"
if [[ -x "${destination}" ]] && [[ "$("${destination}" --version)" == "ruff ${ruff_version}" ]]; then
  echo "Ruff ${ruff_version} is already installed at ${destination}."
  exit 0
fi

temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-ruff.XXXXXX")"
cleanup() {
  rm -rf "${temporary_dir}"
}
trap cleanup EXIT

archive_path="${temporary_dir}/${archive}"
url="https://github.com/astral-sh/ruff/releases/download/${ruff_version}/${archive}"
echo "Downloading Ruff ${ruff_version} for ${archive_platform}..."
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
extracted_bin="${temporary_dir}/ruff-${archive_platform}/ruff"
if [[ ! -x "${extracted_bin}" ]]; then
  echo "Ruff archive did not contain the expected executable." >&2
  exit 1
fi

mkdir -p "${policy_root}/.tools/bin"
mv "${extracted_bin}" "${destination}"
chmod +x "${destination}"
"${destination}" --version
