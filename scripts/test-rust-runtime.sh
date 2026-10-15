#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
fixture="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-rust-runtime.XXXXXX")"
cleanup() { rm -rf "${fixture}"; }
trap cleanup EXIT
mkdir -p "${fixture}/tools" "${fixture}/fake-bin" "${fixture}/archives"
cp "${policy_root}/tools/install-rust.sh" "${policy_root}/tools/verify-sha256.sh" "${fixture}/tools/"
printf '1.2.3\n' >"${fixture}/tools/rust-version.txt"
case "$(uname -s)" in
  Linux) os_tag="linux"; archive_os="unknown-linux-gnu" ;;
  Darwin) os_tag="darwin"; archive_os="apple-darwin" ;;
  *) echo 'Rust installer contract requires Linux or macOS.' >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch_tag="x64"; archive_arch="x86_64" ;;
  aarch64|arm64) arch_tag="arm64"; archive_arch="aarch64" ;;
  *) echo 'Unsupported Rust installer contract architecture.' >&2; exit 1 ;;
esac
triple="${archive_arch}-${archive_os}"
for component in rustc rust-std cargo rustfmt clippy; do
  stem="${component}-1.2.3-${triple}"
  payload="${component}"
  case "${component}" in
    rust-std) payload="rust-std-${triple}" ;;
    rustfmt|clippy) payload="${component}-preview" ;;
  esac
  root="${fixture}/archives/${stem}"
  mkdir -p "${root}/${payload}"
  printf 'fixture license\n' >"${root}/LICENSE-MIT"
  if [[ "${component}" == rust-std ]]; then
    mkdir -p "${root}/${payload}/lib/rustlib/${triple}/lib"
    printf 'standard library\n' >"${root}/${payload}/lib/rustlib/${triple}/lib/libstd.rlib"
  else
    mkdir "${root}/${payload}/bin"
    case "${component}" in
      rustc) binaries=(rustc rustdoc) ;;
      cargo) binaries=(cargo) ;;
      rustfmt) binaries=(rustfmt cargo-fmt) ;;
      clippy) binaries=(clippy-driver cargo-clippy) ;;
    esac
    for binary in "${binaries[@]}"; do
      printf '#!/bin/sh\nprintf "%s 1.2.3 (fixture)\\n"\n' "${binary}" >"${root}/${payload}/bin/${binary}"
      chmod +x "${root}/${payload}/bin/${binary}"
    done
  fi
  tar -cJf "${fixture}/archives/${stem}.tar.xz" -C "${fixture}/archives" "${stem}"
  if command -v shasum >/dev/null 2>&1; then
    digest="$(shasum -a 256 "${fixture}/archives/${stem}.tar.xz" | awk '{print $1}')"
  else
    digest="$(sha256sum "${fixture}/archives/${stem}.tar.xz" | awk '{print $1}')"
  fi
  printf '%s %s\n' "${stem}.tar.xz" "${digest}" >>"${fixture}/tools/rust_checksums.txt"
done
cat >"${fixture}/fake-bin/curl" <<'CURL'
#!/usr/bin/env bash
set -euo pipefail
while (($#)); do
  case "$1" in
    https:*) asset="${1##*/}" ;;
    -o) shift; output="$1" ;;
  esac
  shift
done
cp "${RUST_FIXTURE_ARCHIVES}/${asset}" "${output}"
CURL
chmod +x "${fixture}/fake-bin/curl"
export RUST_FIXTURE_ARCHIVES="${fixture}/archives"
export PATH="${fixture}/fake-bin:${PATH}"
"${fixture}/tools/install-rust.sh"
runtime="${fixture}/.tools/rust/${os_tag}-${arch_tag}"
for binary in rustc rustdoc cargo rustfmt cargo-fmt cargo-clippy clippy-driver; do
  [[ "$("${runtime}/bin/${binary}" --version)" == "${binary} 1.2.3 (fixture)" ]]
done
[[ "$(<"${runtime}/share/licenses/rustc/LICENSE-MIT")" == 'fixture license' ]]
[[ "$(<"${runtime}/lib/rustlib/${triple}/lib/libstd.rlib")" == 'standard library' ]]
mv "${fixture}/archives" "${fixture}/saved-archives"
"${fixture}/tools/install-rust.sh"
mv "${fixture}/saved-archives" "${fixture}/archives"
printf 'old-release\n' >"${runtime}/.code-polishy-rust-release"
printf 'corrupt archive\n' >"${fixture}/archives/cargo-1.2.3-${triple}.tar.xz"
if "${fixture}/tools/install-rust.sh" >"${fixture}/failure" 2>&1; then
  echo 'Rust installer accepted a corrupt component.' >&2
  exit 1
fi
[[ "$(<"${fixture}/failure")" == *'Checksum mismatch'* ]]
[[ "$(<"${runtime}/.code-polishy-rust-release")" == old-release ]]
[[ "$("${runtime}/bin/cargo" --version)" == 'cargo 1.2.3 (fixture)' ]]
echo 'Rust runtime installation, reuse, integrity failure and recovery passed.'
