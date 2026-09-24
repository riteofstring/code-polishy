#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
rust_version="$(tr -d '[:space:]' <"${policy_root}/tools/rust-version.txt")"
if [[ ! "${rust_version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo 'tools/rust-version.txt must pin an exact Rust release.' >&2
  exit 1
fi
case "$(uname -s)" in
  Linux) os_tag="linux"; archive_os="unknown-linux-gnu" ;;
  Darwin) os_tag="darwin"; archive_os="apple-darwin" ;;
  *) echo "Unsupported Rust installation host: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch_tag="x64"; archive_arch="x86_64" ;;
  aarch64|arm64) arch_tag="arm64"; archive_arch="aarch64" ;;
  *) echo "Unsupported Rust installation architecture: $(uname -m)" >&2; exit 1 ;;
esac
triple="${archive_arch}-${archive_os}"
destination="${policy_root}/.tools/rust/${os_tag}-${arch_tag}"

verified_runtime() {
  local directory="$1" tool
  for tool in rustc rustdoc cargo rustfmt cargo-fmt clippy-driver cargo-clippy; do
    [[ -x "${directory}/bin/${tool}" ]] || return 1
  done
  [[ "$("${directory}/bin/rustc" --version | awk '{print $2}')" == "${rust_version}" ]] &&
    [[ -d "${directory}/lib/rustlib/${triple}/lib" ]] &&
    [[ -f "${directory}/.code-polishy-rust-release" ]] &&
    [[ "$(tr -d '[:space:]' <"${directory}/.code-polishy-rust-release")" == "${rust_version}" ]]
}

if verified_runtime "${destination}"; then
  echo "Rust ${rust_version} is already installed at ${destination}."
  exit 0
fi
mkdir -p "${policy_root}/.tools/rust"
scratch="$(mktemp -d "${policy_root}/.tools/rust/.installation.XXXXXX")"
staging="${scratch}/runtime"
mkdir "${staging}"
cleanup() {
  if [[ -d "${scratch}/previous" && ! -e "${destination}" ]]; then
    mv "${scratch}/previous" "${destination}"
  fi
  rm -rf "${scratch}"
}
trap cleanup EXIT

for component in rustc rust-std cargo rustfmt clippy; do
  archive="${component}-${rust_version}-${triple}.tar.xz"
  extracted="${scratch}/${archive%.tar.xz}"
  curl --fail --silent --show-error --location --retry 3 "https://static.rust-lang.org/dist/${archive}" -o "${scratch}/${archive}"
  "${policy_root}/tools/verify-sha256.sh" "${policy_root}/tools/rust_checksums.txt" "${archive}" "${scratch}/${archive}"
  tar -xJf "${scratch}/${archive}" -C "${scratch}"
  payload="${component}"
  case "${component}" in
    rust-std) payload="rust-std-${triple}" ;;
    rustfmt|clippy) payload="${component}-preview" ;;
  esac
  for directory in bin lib libexec share etc; do
    if [[ -d "${extracted}/${payload}/${directory}" ]]; then
      mkdir -p "${staging}/${directory}"
      cp -R "${extracted}/${payload}/${directory}/." "${staging}/${directory}/"
    fi
  done
  license_directory="${staging}/share/licenses/${component}"
  mkdir -p "${license_directory}"
  for license in LICENSE-APACHE LICENSE-MIT COPYRIGHT; do
    if [[ -f "${extracted}/${license}" ]]; then
      cp "${extracted}/${license}" "${license_directory}/${license}"
    fi
  done
done
printf '%s\n' "${rust_version}" >"${staging}/.code-polishy-rust-release"
if ! verified_runtime "${staging}"; then
  echo 'The staged Rust toolchain is incomplete or has the wrong version.' >&2
  exit 1
fi
if [[ -e "${destination}" || -L "${destination}" ]]; then
  mv "${destination}" "${scratch}/previous"
fi
mv "${staging}" "${destination}"
echo "Installed Rust ${rust_version} at ${destination}."
