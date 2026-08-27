#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
osv_version="$(tr -d '[:space:]' <"${policy_root}/tools/osv-scanner-version.txt")"

case "$(uname -s)" in
  Darwin) os_tag="darwin" ;;
  Linux) os_tag="linux" ;;
  *) echo "Unsupported OS for policy-owned OSV-Scanner: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch_tag="arm64" ;;
  x86_64|amd64) arch_tag="amd64" ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

platform_tag="${os_tag}_${arch_tag}"
asset="osv-scanner_${platform_tag}"

case "${osv_version}:${platform_tag}" in
  v2.4.0:darwin_amd64) expected_sha="088119325156321c34c456ac3703d6013538fd71cbac82b891ab34db491e4d66" ;;
  v2.4.0:darwin_arm64) expected_sha="9ca3185ad63e9ab54f7cb90f46a7362be02d80e37f0123d095a54355ea202f5d" ;;
  v2.4.0:linux_amd64) expected_sha="15314940c10d26af9c6649f150b8a47c1262e8fc7e17b1d1029b0e479e8ed8a0" ;;
  v2.4.0:linux_arm64) expected_sha="44e580752910f0ff36ec99aff59af20f65df1e859aa31e5605a8f0d055b496e9" ;;
  *) echo "No checked-in checksum for OSV-Scanner ${osv_version} on ${platform_tag}." >&2; exit 1 ;;
esac

destination="${policy_root}/.tools/bin/osv-scanner"
if [[ -x "${destination}" ]] && "${destination}" --version 2>&1 | grep -Fxq "osv-scanner version: ${osv_version#v}"; then
  echo "OSV-Scanner ${osv_version} is already installed at ${destination}."
  exit 0
fi

temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-osv.XXXXXX")"
cleanup() {
  rm -rf "${temporary_dir}"
}
trap cleanup EXIT

download="${temporary_dir}/${asset}"
url="https://github.com/google/osv-scanner/releases/download/${osv_version}/${asset}"
echo "Downloading OSV-Scanner ${osv_version} for ${platform_tag}..."
curl -fsSL "${url}" -o "${download}"

if command -v shasum >/dev/null 2>&1; then
  actual_sha="$(LC_ALL=C LANG=C shasum -a 256 "${download}" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  actual_sha="$(sha256sum "${download}" | awk '{print $1}')"
else
  echo "A SHA-256 checksum tool (shasum or sha256sum) is required." >&2
  exit 1
fi

if [[ "${expected_sha}" != "${actual_sha}" ]]; then
  echo "Checksum mismatch for ${asset}." >&2
  exit 1
fi

mkdir -p "${policy_root}/.tools/bin"
mv "${download}" "${destination}"
chmod +x "${destination}"
"${destination}" --version
