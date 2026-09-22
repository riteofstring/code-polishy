#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if (($# != 1)) || [[ "$1" != /* ]]; then
  echo "usage: build-shell-provider.sh ABSOLUTE_OUTPUT_DIRECTORY" >&2
  exit 2
fi
output_root="$1"
if [[ -e "${output_root}" ]]; then
  echo "output already exists: ${output_root}" >&2
  exit 1
fi
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-shell-pack.XXXXXX")"
trap 'rm -r "${scratch_root}"' EXIT
candidate_root="${scratch_root}/pack"
mkdir -p "${candidate_root}/bin"
cp -R "${repository_root}/providers/shell/pack/." "${candidate_root}/"
while IFS= read -r -d '' fixture_template; do
  fixture="${fixture_template%.txt}"
  tail -n +2 "${fixture_template}" >"${fixture}"
  rm "${fixture_template}"
done < <(find "${candidate_root}/fixtures" -type f -name '*.sh.txt' -print0)
targets=(
  darwin:amd64
  darwin:arm64
  linux:amd64
  linux:arm64
  windows:amd64
)
for target in "${targets[@]}"; do
  target_os="${target%%:*}"
  target_arch="${target##*:}"
  executable="${candidate_root}/bin/adapter-${target_os}-${target_arch}"
  if [[ "${target_os}" == windows ]]; then
    executable="${executable}.exe"
  fi
  CGO_ENABLED=0 GOOS="${target_os}" GOARCH="${target_arch}" \
    "${repository_root}/scripts/go.sh" build -trimpath -ldflags="-s -w" \
    -o "${executable}" ./providers/shell
done
mkdir -p "$(dirname "${output_root}")"
mv "${candidate_root}" "${output_root}"
