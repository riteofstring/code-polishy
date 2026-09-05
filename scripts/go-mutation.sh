#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fast_tests=""
if [[ "${1:-}" == "--fast-tests" ]]; then
  if [[ -z "${2:-}" ]]; then
    echo "--fast-tests requires a nonempty Go test expression." >&2
    exit 1
  fi
  fast_tests="$2"
  shift 2
fi
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
export GOFLAGS="${GOFLAGS:+${GOFLAGS} }-count=1"

if ! git -C "${repo_root}" rev-parse --verify HEAD >/dev/null 2>&1; then
  echo "Go mutation requires a Git repository with at least one commit." >&2
  exit 1
fi

temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-mutation.XXXXXX")"
worktree="${temporary_dir}/worktree"
coverage_config="${temporary_dir}/gremlins-coverage.yaml"
mutation_config="${temporary_dir}/gremlins-mutation.yaml"
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
  if [[ "${path}" == ".tools" || "${path}" == .tools/* ]]; then
    continue
  fi
  mkdir -p "${worktree}/$(dirname "${path}")"
  cp -p "${repo_root}/${path}" "${worktree}/${path}"
done < <(git -C "${repo_root}" ls-files --others --exclude-standard -z)
if [[ -e "${worktree}/.tools" || -L "${worktree}/.tools" ]]; then
  echo "Go mutation worktree unexpectedly contains .tools." >&2
  exit 1
fi
ln -s "${repo_root}/.tools" "${worktree}/.tools"

mkdir -p "${temporary_dir}/bin"
cat >"${temporary_dir}/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
module_file="$("${CODE_POLISHY_MUTATION_GO}" env GOMOD)"
module_root="${module_file%/go.mod}"
if [[ -f "${module_file}" ]]; then
  module_root="$(cd "${module_root}" && pwd -P)"
fi
if [[ "${module_root}" == "${CODE_POLISHY_MUTATION_ROOT}/"* ]]; then
  if [[ ! -e "${module_root}/.tools" && ! -L "${module_root}/.tools" ]]; then
    ln -s "${CODE_POLISHY_MUTATION_TOOLS}" "${module_root}/.tools" 2>/dev/null ||
      [[ -L "${module_root}/.tools" ]]
  fi
  if [[ "$(readlink "${module_root}/.tools")" != "${CODE_POLISHY_MUTATION_TOOLS}" ]]; then
    echo "Mutation copy has an unexpected tool directory." >&2
    exit 1
  fi
fi
if [[ "${1:-}" == test && -n "${CODE_POLISHY_MUTATION_FAST_TESTS}" ]]; then
  for argument in "$@"; do
    if [[ "${argument}" == -failfast ]]; then
      "${CODE_POLISHY_MUTATION_GO}" "$@" -run "${CODE_POLISHY_MUTATION_FAST_TESTS}"
      break
    fi
  done
fi
exec "${CODE_POLISHY_MUTATION_GO}" "$@"
EOF
chmod +x "${temporary_dir}/bin/go"
export CODE_POLISHY_MUTATION_GO="${mutation_go}"
export CODE_POLISHY_MUTATION_TOOLS="${repo_root}/.tools"
export CODE_POLISHY_MUTATION_FAST_TESTS="${fast_tests}"
CODE_POLISHY_MUTATION_ROOT="$(cd "${temporary_dir}" && pwd -P)"
export CODE_POLISHY_MUTATION_ROOT
export TMPDIR="${CODE_POLISHY_MUTATION_ROOT}"
export PATH="${temporary_dir}/bin:${PATH}"

cd "${worktree}"
inactive_patterns=()
mutation_target="${1:-}"
if [[ -n "${fast_tests}" ]]; then
  if [[ -z "${mutation_target}" || "${mutation_target}" == -* ]]; then
    echo "--fast-tests requires an explicit package target." >&2
    exit 1
  fi
  listed_tests="$("${mutation_go}" test -list "${fast_tests}" "${mutation_target}")"
  if [[ "${listed_tests}" != Test* && "${listed_tests}" != *$'\n'Test* ]]; then
    echo "--fast-tests matches no tests." >&2
    exit 1
  fi
  "${mutation_go}" test -run "${fast_tests}" "${mutation_target}"
fi
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
write_gremlins_config() {
  local path="$1"
  local efficacy="$2"
  local mutant_coverage="$3"
  printf '%s\n' \
    'unleash:' \
    '  workers: 2' \
    '  threshold:' \
    "    efficacy: ${efficacy}" \
    "    mutant-coverage: ${mutant_coverage}" >"${path}"
  if [[ "${#inactive_patterns[@]}" -eq 0 ]]; then
    return
  fi
  printf '%s\n' '  exclude-files:' >>"${path}"
  for inactive_pattern in "${inactive_patterns[@]}"; do
    printf "    - '%s'\n" "${inactive_pattern}" >>"${path}"
  done
}

write_gremlins_config "${coverage_config}" 0 80
"${gremlins}" --config "${coverage_config}" unleash --dry-run "$@"
write_gremlins_config "${mutation_config}" 80 0
"${gremlins}" --config "${mutation_config}" unleash "$@"
