#!/usr/bin/env bash
set -euo pipefail









policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
fixture_root="$(cd "$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-preflight-test.XXXXXX")" && pwd -P)"
cleanup() {
  rm -rf "${fixture_root}"
}
trap cleanup EXIT

source_root="${fixture_root}/checkout"
shim_bin="${fixture_root}/shim"
git_log="${fixture_root}/git-invocations.txt"
output="${fixture_root}/output.txt"
real_git="$(command -v git)"





export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null

fail() {
  echo "release preflight test failure: $1" >&2
  exit 1
}

write_file() {
  mkdir -p "$(dirname "$1")"
  cat >"$1"
}





build_source_checkout() {
  rm -rf "${source_root}"
  mkdir -p "${source_root}/scripts"
  cp "${policy_root}/scripts/release-preflight.sh" "${source_root}/scripts/release-preflight.sh"
  cp "${policy_root}/scripts/release-version.sh" "${source_root}/scripts/release-version.sh"
  chmod +x "${source_root}/scripts/release-preflight.sh" \
    "${source_root}/scripts/release-version.sh"
  printf '9.9.9\n' >"${source_root}/VERSION"
  write_file "${source_root}/CHANGELOG.md" <<'EOF'
# Changelog

## Unreleased

## 9.9.9 - 2026-08-20

- A disposable release entry.
EOF
  "${real_git}" -C "${source_root}" init --quiet
  "${real_git}" -C "${source_root}" config user.email "preflight-test@example.invalid"
  "${real_git}" -C "${source_root}" config user.name "Preflight Test"
  "${real_git}" -C "${source_root}" config core.autocrlf false
  commit_checkout "disposable candidate"
}

commit_checkout() {
  "${real_git}" -C "${source_root}" add -A
  "${real_git}" -C "${source_root}" commit --quiet -m "$1"
}



build_command_shims() {
  mkdir -p "${shim_bin}"
  cat >"${shim_bin}/git" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >>"${git_log}"
exec "${real_git}" "\$@"
EOF
  chmod +x "${shim_bin}/git"
}

repository_state() {
  "${real_git}" -C "${source_root}" for-each-ref
  "${real_git}" -C "${source_root}" rev-parse HEAD
  "${real_git}" -C "${source_root}" status --porcelain=v1 --untracked-files=all
}



run_preflight() {
  local before after status=0
  before="$(repository_state)"
  PATH="${shim_bin}:${PATH}" "${source_root}/scripts/release-preflight.sh" "$@" \
    >"${output}" 2>&1 || status=$?
  after="$(repository_state)"
  [[ "${before}" == "${after}" ]] ||
    fail "the preflight changed repository state: ${*:-<no arguments>}"
  return "${status}"
}

expect_pass() {
  local description="$1"
  shift
  run_preflight "$@" ||
    fail "${description}: the preflight failed: $(head -10 "${output}")"
  grep -q "Release preflight passed" "${output}" ||
    fail "${description}: the preflight did not report what it proved: $(head -10 "${output}")"
}

expect_fail() {
  local description="$1" needle="$2"
  shift 2
  if run_preflight "$@"; then
    fail "${description}: the preflight passed: $(head -10 "${output}")"
  fi
  grep -qF "${needle}" "${output}" ||
    fail "${description} was refused without \`${needle}\`: $(head -10 "${output}")"
}

build_source_checkout
build_command_shims
candidate="$("${real_git}" -C "${source_root}" rev-parse HEAD)"


usage_status=0
run_preflight "${candidate}" "v9.9.9" || usage_status=$?
[[ "${usage_status}" == "2" ]] ||
  fail "an independently supplied tag name was not refused as a usage error"
grep -q "usage: release-preflight.sh <candidate-commit-id>" "${output}" ||
  fail "the usage error does not name the one candidate argument"
usage_status=0
run_preflight || usage_status=$?
[[ "${usage_status}" == "2" ]] || fail "a missing candidate was not refused as a usage error"






requirement="canonical lowercase full commit object ID"
expect_fail "a candidate that names no commit" "does not name a commit" \
  0000000000000000000000000000000000000000
grep -qF "${requirement}" "${output}" ||
  fail "an unknown candidate ID was refused without the exact naming requirement"
expect_fail "a symbolic candidate name" "${requirement}" HEAD
expect_fail "an abbreviated candidate hash" "${requirement}" "${candidate:0:12}"
expect_fail "an uppercase candidate spelling" "${requirement}" \
  "$(printf '%s' "${candidate}" | tr '[:lower:]' '[:upper:]')"
printf 'drifted\n' >"${source_root}/later.txt"
commit_checkout "disposable later commit"
expect_fail "a candidate behind the current commit" "is not the current commit" "${candidate}"
grep -q "Check out the exact candidate" "${output}" ||
  fail "a stale candidate was refused without the checkout remedy"
candidate="$("${real_git}" -C "${source_root}" rev-parse HEAD)"


printf 'untracked\n' >"${source_root}/scratch.txt"
expect_fail "an untracked file" "untracked changes" "${candidate}"
grep -q "scratch.txt" "${output}" || fail "untracked drift was refused without naming it"


"${real_git}" -C "${source_root}" config status.showUntrackedFiles no
expect_fail "an untracked file hidden by status.showUntrackedFiles=no" \
  "untracked changes" "${candidate}"
grep -q "scratch.txt" "${output}" ||
  fail "config-hidden untracked drift was refused without naming it"
"${real_git}" -C "${source_root}" config --unset status.showUntrackedFiles
rm "${source_root}/scratch.txt"
printf 'staged\n' >"${source_root}/staged.txt"
"${real_git}" -C "${source_root}" add staged.txt
expect_fail "a staged file" "staged.txt" "${candidate}"
"${real_git}" -C "${source_root}" rm --cached --quiet staged.txt
rm "${source_root}/staged.txt"
printf 'unstaged\n' >>"${source_root}/later.txt"
expect_fail "an unstaged modification" "later.txt" "${candidate}"
"${real_git}" -C "${source_root}" checkout -- later.txt




expect_pass "a clean untagged candidate" "${candidate}"
grep -q "the tag v9.9.9 does not exist yet" "${output}" ||
  fail "the missing tag state was not reported"
grep -q "next explicit release action" "${output}" ||
  fail "the missing tag was not reported as the next explicit release action"
grep -qF "git tag -a v9.9.9 -m \"Code Polishy 9.9.9\" ${candidate}" "${output}" ||
  fail "the derived tag command was not reported exactly"


printf '9.9\n' >"${source_root}/VERSION"
commit_checkout "disposable malformed version"
candidate="$("${real_git}" -C "${source_root}" rev-parse HEAD)"
expect_fail "a malformed VERSION" "not a strict MAJOR.MINOR.PATCH semantic version" "${candidate}"
grep -qF "\`9.9\`" "${output}" || fail "the malformed version was refused without naming it"



printf '9. 9.9\n' >"${source_root}/VERSION"
commit_checkout "disposable interior whitespace version"
candidate="$("${real_git}" -C "${source_root}" rev-parse HEAD)"
expect_fail "interior whitespace in VERSION" "carries whitespace" "${candidate}"
grep -qF "\`9.9.9\`" "${output}" ||
  fail "the interior whitespace refusal does not name the value trimming would have invented"
printf '9.9.9\n\n' >"${source_root}/VERSION"
commit_checkout "disposable trailing blank line version"
candidate="$("${real_git}" -C "${source_root}" rev-parse HEAD)"
expect_fail "a trailing blank line in VERSION" "beyond one trailing line ending" "${candidate}"
printf '9.9.9\r\n' >"${source_root}/VERSION"
commit_checkout "disposable CRLF version"
candidate="$("${real_git}" -C "${source_root}" rev-parse HEAD)"
expect_fail "a CRLF line ending in VERSION" "carries whitespace" "${candidate}"


printf '9.9.9' >"${source_root}/VERSION"
commit_checkout "disposable version without newline"
candidate="$("${real_git}" -C "${source_root}" rev-parse HEAD)"
expect_pass "a version stored without a trailing newline" "${candidate}"
printf '9.9.9\n' >"${source_root}/VERSION"
commit_checkout "disposable version restored"
candidate="$("${real_git}" -C "${source_root}" rev-parse HEAD)"



write_file "${source_root}/CHANGELOG.md" <<'EOF'
# Changelog

## Unreleased

- 9.9.9 will ship a disposable release entry.
EOF
commit_checkout "disposable unreleased changelog"
candidate="$("${real_git}" -C "${source_root}" rev-parse HEAD)"
expect_fail "a changelog without the released-version heading" \
  "no released-version heading for 9.9.9" "${candidate}"
grep -qF '## 9.9.9 - <YYYY-MM-DD>' "${output}" ||
  fail "the changelog was refused without the exact heading to add"
build_source_checkout
candidate="$("${real_git}" -C "${source_root}" rev-parse HEAD)"



"${real_git}" -C "${source_root}" tag v9.9.9
expect_fail "a lightweight candidate tag" "not an annotated tag" "${candidate}"
"${real_git}" -C "${source_root}" tag -d v9.9.9 >/dev/null



"${real_git}" -C "${source_root}" tag -a nested-inner -m "inner tag" "${candidate}"
"${real_git}" -C "${source_root}" -c advice.nestedTag=false tag -a v9.9.9 \
  -m "Code Polishy 9.9.9" nested-inner
expect_fail "a nested annotated tag" \
  "points directly at a tag object, not at a commit" "${candidate}"
"${real_git}" -C "${source_root}" tag -d v9.9.9 nested-inner >/dev/null
printf 'drifted\n' >"${source_root}/later.txt"
commit_checkout "disposable tagged-behind commit"
"${real_git}" -C "${source_root}" tag -a v9.9.9 -m "Code Polishy 9.9.9" "${candidate}"
misdirected="${candidate}"
candidate="$("${real_git}" -C "${source_root}" rev-parse HEAD)"
expect_fail "a misdirected annotated tag" \
  "resolves to ${misdirected}, not to the candidate ${candidate}" "${candidate}"
"${real_git}" -C "${source_root}" tag -d v9.9.9 >/dev/null
"${real_git}" -C "${source_root}" tag -a v9.9.9 -m "Code Polishy 9.9.9" "${candidate}"
expect_pass "an annotated tag at the exact candidate" "${candidate}"
grep -q "the annotated tag v9.9.9 points directly at the exact candidate" "${output}" ||
  fail "the resolved tag state was not reported"




tag_object="$("${real_git}" -C "${source_root}" rev-parse refs/tags/v9.9.9)"
expect_fail "a full object ID naming a tag object" \
  "not itself a commit object" "${tag_object}"
grep -qF "${requirement}" "${output}" ||
  fail "a tag object ID was refused without the exact naming requirement"



[[ -s "${git_log}" ]] || fail "the preflight was never asked for repository state"
while read -r invocation; do
  case "${invocation}" in
    "-C "*" rev-parse "* | "-C "*" status --porcelain=v1 --untracked-files=all" \
      | "-C "*" cat-file -t "* | "-C "*" for-each-ref --format=%(object) refs/tags/"*) ;;
    *) fail "the preflight ran an unexpected git command: git ${invocation}" ;;
  esac
done <"${git_log}"

printf 'release preflight tests passed\n'
