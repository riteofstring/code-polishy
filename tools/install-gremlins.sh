#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gremlins_version="$(tr -d '[:space:]' <"${policy_root}/tools/gremlins-version.txt")"

case "$(uname -s)" in
  Darwin) os_tag="darwin" ;;
  Linux) os_tag="linux" ;;
  *) echo "Unsupported OS for policy-owned Gremlins: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch_tag="arm64" ;;
  x86_64|amd64) arch_tag="amd64" ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

platform_tag="${os_tag}_${arch_tag}"
asset="gremlins_${gremlins_version#v}_${platform_tag}.tar.gz"

case "${gremlins_version}:${platform_tag}" in
  v0.6.0:darwin_amd64) expected_sha="6b00937bb51a9ac994371b9835572320f1f26f4e3ffce7b212b8b85afa0a12ae" ;;
  v0.6.0:darwin_arm64) expected_sha="90de858ffad3eea5018e41cdf28747b7003966684b9420f4605eb10bdad0c301" ;;
  v0.6.0:linux_amd64) expected_sha="b02a42e47935f891c9a411d68c07e211c7082609e79c2435b67c85ee9658c538" ;;
  v0.6.0:linux_arm64) expected_sha="686542674e54559afb7e86d6994d342c19a856cfd970c315748989537936664c" ;;
  *) echo "No checked-in checksum for Gremlins ${gremlins_version} on ${platform_tag}." >&2; exit 1 ;;
esac

destination="${policy_root}/.tools/bin/gremlins"
if [[ -x "${destination}" ]] && "${destination}" --version 2>&1 | grep -Fq "${gremlins_version#v}"; then
  echo "Gremlins ${gremlins_version} is already installed at ${destination}."
  exit 0
fi

temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-gremlins.XXXXXX")"
cleanup() {
  rm -rf "${temporary_dir}"
}
trap cleanup EXIT

archive="${temporary_dir}/${asset}"
url="https://github.com/go-gremlins/gremlins/releases/download/${gremlins_version}/${asset}"
echo "Downloading Gremlins ${gremlins_version} for ${platform_tag}..."
curl -fsSL "${url}" -o "${archive}"

if command -v shasum >/dev/null 2>&1; then
  actual_sha="$(LC_ALL=C LANG=C shasum -a 256 "${archive}" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  actual_sha="$(sha256sum "${archive}" | awk '{print $1}')"
else
  echo "A SHA-256 checksum tool (shasum or sha256sum) is required." >&2
  exit 1
fi

if [[ "${expected_sha}" != "${actual_sha}" ]]; then
  echo "Checksum mismatch for ${asset}." >&2
  exit 1
fi

tar -xzf "${archive}" -C "${temporary_dir}"
if [[ ! -f "${temporary_dir}/gremlins" ]]; then
  echo "Gremlins archive did not contain the expected binary." >&2
  exit 1
fi
mkdir -p "${policy_root}/.tools/bin"
mv "${temporary_dir}/gremlins" "${destination}"
chmod +x "${destination}"
"${destination}" --version
