#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gremlins="${repo_root}/.tools/bin/gremlins"
if [[ ! -x "${gremlins}" ]]; then
  echo "Pinned Gremlins is unavailable; run ./tools/install-gremlins.sh." >&2
  exit 1
fi

case "$(uname -s)" in
  Darwin) mutation_os="darwin" ;;
  Linux) mutation_os="linux" ;;
  *) mutation_os="" ;;
esac
case "$(uname -m)" in
  arm64|aarch64) mutation_arch="arm64" ;;
  x86_64|amd64) mutation_arch="amd64" ;;
  *) mutation_arch="" ;;
esac
mutation_go=""
if [[ -n "${mutation_os}" && -n "${mutation_arch}" ]]; then
  mutation_go="${repo_root}/.tools/go/${mutation_os}-${mutation_arch}/go/bin/go"
fi
if [[ -z "${mutation_go}" || ! -x "${mutation_go}" ]]; then
  echo "Pinned Go is unavailable; run ./tools/install-go.sh." >&2
  exit 1
fi
mutation_go_directory="$(dirname "${mutation_go}")"
export PATH="${mutation_go_directory}:${PATH}"

if ! git -C "${repo_root}" rev-parse --verify HEAD >/dev/null 2>&1; then
  echo "Go mutation requires a Git repository with at least one commit." >&2
  exit 1
fi

temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-mutation.XXXXXX")"
worktree="${temporary_dir}/worktree"
gremlins_config="${temporary_dir}/gremlins.yaml"
cleanup() {
  git -C "${repo_root}" worktree remove --force "${worktree}" >/dev/null 2>&1 || true
  rm -rf "${temporary_dir}"
}
trap cleanup EXIT

git -C "${repo_root}" worktree add --quiet --detach "${worktree}" HEAD
patch_file="${temporary_dir}/working-tree.patch"
git -C "${repo_root}" diff --binary HEAD -- . >"${patch_file}"
if [[ -s "${patch_file}" ]]; then
  git -C "${worktree}" apply --binary "${patch_file}"
fi
while IFS= read -r -d '' path; do
  mkdir -p "${worktree}/$(dirname "${path}")"
  cp -p "${repo_root}/${path}" "${worktree}/${path}"
done < <(git -C "${repo_root}" ls-files --others --exclude-standard -z)

printf '%s\n' \
  'unleash:' \
  '  workers: 2' \
  '  threshold:' \
  '    efficacy: 80' \
  '    mutant-coverage: 80' >"${gremlins_config}"

cd "${worktree}"
inactive_patterns=()
mutation_target="${1:-}"
if [[ -n "${mutation_target}" && "${mutation_target}" != -* ]]; then
  module_directory="$("${mutation_go}" list -m -f '{{ .Dir }}')"
  go_list_template="{{ \$directory := .Dir }}{{ range .IgnoredGoFiles }}{{ printf \"%s/%s\\n\" \$directory . }}{{ end }}"
  ignored_sources="$("${mutation_go}" list -f "${go_list_template}" "${mutation_target}")"
  while IFS= read -r ignored_source; do
    [[ -n "${ignored_source}" && "${ignored_source}" == "${module_directory}/"* && "${ignored_source}" != *_test.go ]] || continue
    inactive_source="${ignored_source##*/}"
    inactive_patterns+=("(^|/)${inactive_source//./[.]}$")
  done <<<"${ignored_sources}"
fi
if [[ "${#inactive_patterns[@]}" -gt 0 ]]; then
  printf '%s\n' '  exclude-files:' >>"${gremlins_config}"
  for inactive_pattern in "${inactive_patterns[@]}"; do
    printf "    - '%s'\n" "${inactive_pattern}" >>"${gremlins_config}"
  done
fi
"${gremlins}" --config "${gremlins_config}" unleash "$@"
