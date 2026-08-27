#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
shellcheck_version="$(tr -d '[:space:]' <"${policy_root}/tools/shellcheck-version.txt")"
shellcheck_version="${shellcheck_version#v}"

case "$(uname -s)" in
  Darwin) os_tag="darwin" ;;
  Linux) os_tag="linux" ;;
  *) echo "Unsupported OS for policy-owned ShellCheck: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch_tag="aarch64" ;;
  x86_64|amd64) arch_tag="x86_64" ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

archive_platform="${os_tag}.${arch_tag}"
platform_tag="${os_tag}-${arch_tag}"
destination="${policy_root}/.tools/shellcheck/${platform_tag}"
shellcheck_bin="${destination}/shellcheck"

case "${shellcheck_version}:${archive_platform}" in
  0.11.0:darwin.aarch64) expected_sha="339b930feb1ea764467013cc1f72d09cd6b869ebf1013296ba9055ab2ffbd26f" ;;
  0.11.0:darwin.x86_64) expected_sha="c2c15e08df0e8fbc374c335b230a7ee958c313fa5714817a59aa59f1aa594f51" ;;
  0.11.0:linux.aarch64) expected_sha="68a8133197a50beb8803f8d42f9908d1af1c5540d4bb05fdfca8c1fa47decefc" ;;
  0.11.0:linux.x86_64) expected_sha="b7af85e41cc99489dcc21d66c6d5f3685138f06d34651e6d34b42ec6d54fe6f6" ;;
  *)
    echo "No checked-in checksum for ShellCheck ${shellcheck_version} on ${archive_platform}." >&2
    exit 1
    ;;
esac

if [[ -x "${shellcheck_bin}" ]] && \
  [[ "$("${shellcheck_bin}" --version | awk '/^version:/ {print $2}')" == "${shellcheck_version}" ]]; then
  echo "ShellCheck ${shellcheck_version} is already installed at ${shellcheck_bin}."
  exit 0
fi

temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-shellcheck.XXXXXX")"
cleanup() {
  rm -rf "${temporary_dir}"
}
trap cleanup EXIT

archive="shellcheck-v${shellcheck_version}.${archive_platform}.tar.gz"
archive_path="${temporary_dir}/${archive}"
url="https://github.com/koalaman/shellcheck/releases/download/v${shellcheck_version}/${archive}"

echo "Downloading ShellCheck ${shellcheck_version} for ${platform_tag}..."
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
extracted_bin="${temporary_dir}/shellcheck-v${shellcheck_version}/shellcheck"
if [[ ! -x "${extracted_bin}" ]]; then
  echo "ShellCheck archive did not contain the expected executable." >&2
  exit 1
fi

rm -rf "${destination}"
mkdir -p "${destination}"
mv "${extracted_bin}" "${shellcheck_bin}"
chmod +x "${shellcheck_bin}"
printf '%s\n' "${shellcheck_version}" >"${destination}/version.txt"
echo "Installed ShellCheck ${shellcheck_version} at ${shellcheck_bin}."
