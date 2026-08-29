#!/usr/bin/env bash
set -euo pipefail


























policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"

usage() {
  echo "usage: release-preflight.sh <candidate-commit-id>" >&2
  exit 2
}

if [[ "$#" -ne 1 || -z "$1" || "$1" == -* ]]; then
  usage
fi
requested="$1"

fail() {
  printf '%s\n' "$@" >&2
  exit 1
}

if ! git -C "${policy_root}" rev-parse --git-dir >/dev/null 2>&1; then
  fail "Run the release preflight from a Git checkout; ${policy_root} is not one."
fi
checkout_root="$(cd "$(git -C "${policy_root}" rev-parse --show-toplevel)" && pwd -P)"
if [[ "${checkout_root}" != "${policy_root}" ]]; then
  fail "Run the release preflight from its own checkout root, not from ${checkout_root}."
fi








candidate_requirement="Name the reviewed candidate with Git's canonical lowercase full commit object ID, exactly as \`git rev-parse HEAD\` prints it."
if [[ ! "${requested}" =~ ^[0-9a-f]{40}$ ]]; then
  fail "The requested candidate ${requested} is not a canonical lowercase full commit object ID, so it names the candidate only through resolution." \
    "${candidate_requirement}"
fi
if ! candidate="$(git -C "${policy_root}" rev-parse --verify --quiet "${requested}^{commit}")"; then
  fail "The requested candidate ${requested} does not name a commit in this checkout." \
    "${candidate_requirement}"
fi
if [[ "${requested}" != "${candidate}" ]]; then
  fail "The requested candidate ${requested} is not itself a commit object; it resolves to the commit ${candidate}." \
    "${candidate_requirement}"
fi
if ! head_commit="$(git -C "${policy_root}" rev-parse --verify --quiet HEAD)"; then
  fail "The checkout at ${policy_root} has no committed revision to release."
fi
if [[ "${candidate}" != "${head_commit}" ]]; then
  fail "The requested candidate ${candidate} is not the current commit ${head_commit}." \
    "Check out the exact candidate before running the preflight; a release is built from the commit that is present."
fi



drift="$(git -C "${policy_root}" status --porcelain=v1 --untracked-files=all)"
if [[ -n "${drift}" ]]; then
  fail "The candidate worktree at ${policy_root} has staged, unstaged, or untracked changes:" \
    "$(printf '%s\n' "${drift}" | sed 's/^/  /')" \
    "Commit or remove them; a release carries the candidate commit and nothing else."
fi

version="$("${policy_root}/scripts/release-version.sh" "${policy_root}/VERSION")"
tag_name="v${version}"

if [[ ! -f "${policy_root}/CHANGELOG.md" ]]; then
  fail "The candidate has no CHANGELOG.md to record the ${version} release in."
fi
if ! grep -Eq "^## ${version//./\\.} - [0-9]{4}-[0-9]{2}-[0-9]{2}$" "${policy_root}/CHANGELOG.md"; then
  fail "CHANGELOG.md has no released-version heading for ${version}." \
    "Move this release's entries out of \`## Unreleased\` and under an exact \`## ${version} - <YYYY-MM-DD>\` heading."
fi





tag_command="git tag -a ${tag_name} -m \"Code Polishy ${version}\" ${candidate}"
if git -C "${policy_root}" rev-parse --verify --quiet "refs/tags/${tag_name}" >/dev/null; then
  tag_type="$(git -C "${policy_root}" cat-file -t "refs/tags/${tag_name}")"
  if [[ "${tag_type}" != "tag" ]]; then
    fail "The existing ${tag_name} is a lightweight reference to a ${tag_type}, not an annotated tag." \
      "Delete it and create the annotated tag as its own explicit release action:" \
      "  ${tag_command}"
  fi




  tag_target="$(git -C "${policy_root}" for-each-ref --format='%(object)' "refs/tags/${tag_name}")"
  tag_target_type="$(git -C "${policy_root}" cat-file -t "${tag_target}" 2>/dev/null || echo "missing")"
  if [[ "${tag_target_type}" != "commit" ]]; then
    fail "The annotated tag ${tag_name} points directly at a ${tag_target_type} object, not at a commit." \
      "Delete it and create the annotated tag as its own explicit release action:" \
      "  ${tag_command}"
  fi
  if [[ "${tag_target}" != "${candidate}" ]]; then
    fail "The annotated tag ${tag_name} resolves to ${tag_target}, not to the candidate ${candidate}." \
      "A version tag names exactly one reviewed commit: releasing ${version} from this candidate requires a different version, or a deliberate reviewed retag."
  fi
  tag_state="the annotated tag ${tag_name} points directly at the exact candidate."
else
  tag_state="the tag ${tag_name} does not exist yet; creating it is the next explicit release action:
  ${tag_command}"
fi

echo "Release preflight passed for Code Polishy ${version} at ${candidate}:"
echo "- the requested candidate is the current commit and the worktree carries no drift;"
echo "- VERSION stores exactly ${version} and derives the tag name ${tag_name};"
echo "- CHANGELOG.md records the exact \`## ${version}\` released-version heading;"
echo "- ${tag_state}"
