#!/usr/bin/env bash
set -euo pipefail

# Prove the local facts a release of this repository is cut from.
#
# Usage: release-preflight.sh <candidate-commit-id>
#
# The candidate is the exact reviewed commit a release would be tagged and
# installed from, named with Git's canonical lowercase full commit object ID so
# the proof binds to the reviewed commit rather than to whatever a symbolic,
# abbreviated, or differently cased name currently resolves to. The preflight
# only ever reads: it never creates, moves, signs, deletes, or pushes a commit,
# branch, tag, release, lock, or manifest, and it reaches no network. A fact
# that does not hold stops the preflight and names the exact remedy, so what
# passes is the candidate that is actually present rather than the one the
# caller meant.
#
# The `v<VERSION>` tag name is derived from the candidate's own `VERSION`,
# never supplied, so the preflight cannot be asked to bless a tag that does not
# match the version the release would record. `VERSION` is read by the one
# strict reader the installer also runs, scripts/release-version.sh: one
# conventional trailing line ending is accepted, but any other whitespace is
# rejected rather than deleted into a different value.
#
# The preflight has no remote or installed-release mode. The installed
# release/lock relationship is proved by scripts/test-installed-release.sh, and
# pushing is a maintainer action the release checklist keeps explicit.

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

# The requested candidate must be the exact commit that is present, named with
# Git's canonical lowercase full commit object ID. Any other spelling -- a
# symbolic name, an abbreviated or uppercase hash, or the ID of a tag object --
# resolves to whatever happens to be present, so it would prove candidate
# identity vacuously; and a candidate that resolves elsewhere is not reviewed
# drift to tolerate, it is a different release. Every rejected spelling is
# refused with this same requirement.
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

# The drift report is pinned rather than inherited: configuration such as
# status.showUntrackedFiles=no must not hide untracked files from this proof.
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

# Tag state must be unambiguous. An existing candidate tag is a release name
# already in use, so it must be an annotated tag object that points directly at
# this exact candidate. A tag that does not exist yet is simply the next
# explicit release action, not a plausible published release.
tag_command="git tag -a ${tag_name} -m \"Code Polishy ${version}\" ${candidate}"
if git -C "${policy_root}" rev-parse --verify --quiet "refs/tags/${tag_name}" >/dev/null; then
  tag_type="$(git -C "${policy_root}" cat-file -t "refs/tags/${tag_name}")"
  if [[ "${tag_type}" != "tag" ]]; then
    fail "The existing ${tag_name} is a lightweight reference to a ${tag_type}, not an annotated tag." \
      "Delete it and create the annotated tag as its own explicit release action:" \
      "  ${tag_command}"
  fi
  # rev-parse's ^{commit} peels tags recursively, so it would also bless a tag
  # object whose direct target is another tag. The tag object's own recorded
  # target must be the candidate commit itself, so the tag names exactly one
  # reviewed commit with no intermediate object.
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
