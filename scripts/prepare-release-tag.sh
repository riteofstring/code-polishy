#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"

usage() {
  echo "usage: prepare-release-tag.sh" >&2
  exit 2
}

if [[ "$#" -ne 0 ]]; then
  usage
fi

fail() {
  printf '%s\n' "$@" >&2
  exit 1
}

candidate="$(git -C "${policy_root}" rev-parse --verify HEAD)"
version="$("${policy_root}/scripts/release-version.sh" "${policy_root}/VERSION")"
tag_name="v${version}"
tag_ref="refs/tags/${tag_name}"

if git -C "${policy_root}" show-ref --verify --quiet "${tag_ref}"; then
  fail "The release tag ${tag_name} already exists in this checkout." \
    "Version tags are immutable; prepare a new patch version instead of moving or replacing it."
fi

preflight_status=0
preflight_output="$("${policy_root}/scripts/release-preflight.sh" "${candidate}" 2>&1)" || preflight_status=$?
if [[ "${preflight_status}" -ne 0 ]]; then
  printf '%s\n' "${preflight_output}" >&2
  exit "${preflight_status}"
fi

branch="$(git -C "${policy_root}" symbolic-ref --quiet --short HEAD || true)"
if [[ "${branch}" != "main" ]]; then
  fail "The release candidate is checked out on ${branch:-a detached HEAD}, not main." \
    "Release only the exact reviewed commit after it has reached main."
fi

remote="$(git -C "${policy_root}" config --get "branch.${branch}.remote" || true)"
merge_ref="$(git -C "${policy_root}" config --get "branch.${branch}.merge" || true)"
if [[ -z "${remote}" || "${merge_ref}" != "refs/heads/main" ]]; then
  fail "The local main branch has no configured main-branch upstream." \
    "Push main with an upstream before preparing its release tag."
fi

if ! remote_refs="$(git -C "${policy_root}" ls-remote --refs "${remote}" "${merge_ref}" "${tag_ref}" 2>&1)"; then
  fail "Could not inspect ${remote} for the pushed candidate and release tag." \
    "${remote_refs}"
fi

remote_candidate=""
remote_tag=""
while IFS=$'\t' read -r object_id ref_name; do
  case "${ref_name}" in
    "${merge_ref}") remote_candidate="${object_id}" ;;
    "${tag_ref}") remote_tag="${object_id}" ;;
  esac
done <<<"${remote_refs}"

if [[ "${remote_candidate}" != "${candidate}" ]]; then
  fail "The candidate ${candidate} is not the commit published at ${remote}/${merge_ref}." \
    "Push the exact candidate to main and wait for its ordinary CI before preparing the tag."
fi
if [[ -n "${remote_tag}" ]]; then
  fail "The release tag ${tag_name} already exists on ${remote}." \
    "Version tags are immutable; prepare a new patch version instead of moving or replacing it."
fi

echo "Code Polishy ${version} is ready to tag at ${candidate}."
echo "Run these commands exactly:"
echo "git tag -a ${tag_name} -m \"Code Polishy ${version}\" ${candidate}"
echo "./scripts/release-preflight.sh ${candidate}"
echo "git push ${remote} ${tag_ref}"
