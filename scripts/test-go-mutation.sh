#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
fixture_root="$(cd "$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-mutation-test.XXXXXX")" && pwd -P)"
cleanup() {
  rm -rf "${fixture_root}"
}
trap cleanup EXIT

fail() {
  echo "go mutation test failure: $1" >&2
  exit 1
}

case "$(uname -s)" in
  Darwin) mutation_os="darwin" ;;
  Linux) mutation_os="linux" ;;
  *) fail "unsupported test OS $(uname -s)" ;;
esac
case "$(uname -m)" in
  arm64 | aarch64) mutation_arch="arm64" ;;
  x86_64 | amd64) mutation_arch="amd64" ;;
  *) fail "unsupported test architecture $(uname -m)" ;;
esac

fixture_repo="${fixture_root}/repository"
marker="${fixture_root}/gremlins-invoked"
output="${fixture_root}/output"
mkdir -p "${fixture_repo}/scripts" \
  "${fixture_repo}/.tools/bin" \
  "${fixture_repo}/.tools/go/${mutation_os}-${mutation_arch}/go/bin"
cp "${policy_root}/scripts/go-mutation.sh" "${fixture_repo}/scripts/go-mutation.sh"
chmod +x "${fixture_repo}/scripts/go-mutation.sh"

cat >"${fixture_repo}/.tools/go/${mutation_os}-${mutation_arch}/go/bin/go" <<'EOF'
#!/usr/bin/env bash
if [[ "${1:-}" == "list" && "${2:-}" == "-m" ]]; then
  pwd -P
elif [[ "${1:-}" == "list" ]]; then
  root="$(pwd -P)"
  printf '%s\n' "${root}/internal/sample/inactive_other.go" "${root}/internal/sample/inactive_other_test.go"
fi
exit 0
EOF
chmod +x "${fixture_repo}/.tools/go/${mutation_os}-${mutation_arch}/go/bin/go"

cat >"${fixture_repo}/.tools/bin/gremlins" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" == "4" ]]
[[ "$1" == "--config" ]]
[[ "$3" == "unleash" ]]
[[ "$4" == "./internal/sample" ]]
grep -qx 'unleash:' "$2"
grep -qx '  workers: 2' "$2"
grep -qx '  threshold:' "$2"
grep -qx '    efficacy: 80' "$2"
grep -qx '    mutant-coverage: 80' "$2"
grep -qx '  exclude-files:' "$2"
grep -Fqx "    - '(^|/)inactive_other[.]go$'" "$2"
printf '%s\n' invoked >"${GREMLINS_TEST_MARKER}"
exit 10
EOF
chmod +x "${fixture_repo}/.tools/bin/gremlins"

git -C "${fixture_repo}" init --quiet
git -C "${fixture_repo}" config user.email "mutation-test@example.invalid"
git -C "${fixture_repo}" config user.name "Mutation Test"
git -C "${fixture_repo}" add scripts/go-mutation.sh
git -C "${fixture_repo}" commit --quiet -m fixture

status=0
GREMLINS_TEST_MARKER="${marker}" \
  "${fixture_repo}/scripts/go-mutation.sh" ./internal/sample >"${output}" 2>&1 || status=$?
[[ "${status}" == "10" ]] ||
  fail "threshold failure exited ${status}, expected 10: $(sed -n '1,10p' "${output}")"
[[ -f "${marker}" ]] || fail "the configured Gremlins command did not run"

echo "go mutation tests passed"
