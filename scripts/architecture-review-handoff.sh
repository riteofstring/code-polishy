#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "$*" >&2
  exit 1
}

[[ "$#" -eq 2 && "$1" =~ ^(export|restore)$ ]] || fail 'usage: architecture-review-handoff.sh export|restore BASE'
operation="$1"
base="$(git rev-parse --verify "${2}^{commit}")"
candidate="$(git rev-parse --verify HEAD)"
[[ "$base" =~ ^[0-9a-f]{40}$ && "$candidate" =~ ^[0-9a-f]{40}$ ]] || fail 'Expected SHA-1 commit identities.'
cd "$(git rev-parse --show-toplevel)"
reference="refs/code-polishy/architecture-review/${base}/${candidate}"
evidence='.code-polishy-reports/architecture-review'
[[ ! -L .code-polishy-reports ]] || fail 'The reports directory must not be a symbolic link.'
mkdir -p .code-polishy-reports
temporary="$(mktemp -d '.code-polishy-reports/.architecture-handoff.XXXXXX')"
temporary="$(cd "$temporary" && pwd)"
trap 'rm -rf "$temporary"' EXIT

if [[ "$operation" == export ]]; then
  code-polishy architecture-review status --base "$base"
  [[ -f "$evidence/receipt.json" && ! -L "$evidence/receipt.json" ]] || fail 'No regular accepted review receipt.'
  receipt="$(cat "$evidence/receipt.json")"
  [[ "$receipt" =~ \"reviewId\":\"(review-[0-9a-f]{32})\" ]] || fail 'Invalid accepted review identifier.'
  review="${BASH_REMATCH[1]}"
  for path in prepare.json receipt.json "reviews/$review/packet.json" "reviews/$review/result.json"; do
    [[ -f "$evidence/$path" && ! -L "$evidence/$path" ]] || fail "Missing regular review evidence: $path"
    blob="$(git hash-object -w -- "$evidence/$path")"
    GIT_INDEX_FILE="$temporary/index" git update-index --add --cacheinfo "100644,$blob,$path"
  done
  tree="$(GIT_INDEX_FILE="$temporary/index" git write-tree)"
  commit="$(printf 'Accepted architecture evidence for %s against %s\n' "$candidate" "$base" | git commit-tree "$tree" -p "$candidate")"
  git update-ref "$reference" "$commit"
  printf 'Review handoff prepared locally. Publish with explicit authorization:\ngit push origin %s\n' "$reference"
  exit
fi

[[ ! -e "$evidence" && ! -L "$evidence" ]] || fail 'Restore requires an absent architecture-review directory; preserve existing evidence first.'
authorization=''
if [[ -n "${GITHUB_TOKEN:-}" ]]; then
  authorization="$(printf 'x-access-token:%s' "$GITHUB_TOKEN" | base64 | tr -d '\r\n')"
  printf '::add-mask::%s\n' "$authorization"
fi
if ! git -c "http.extraheader=${authorization:+AUTHORIZATION: basic $authorization}" fetch --no-tags origin "$reference"; then
  fail "No published review handoff for $candidate against $base. Export and publish $reference before retrying CI."
fi
unset authorization
commit="$(git rev-parse --verify 'FETCH_HEAD^{commit}')"
[[ "$(git show -s --format=%P "$commit")" == "$candidate" ]] || fail 'Review handoff is not bound to the requested candidate.'
git ls-tree -r --full-tree "$commit" >"$temporary/tree"
count=0
review=''
while IFS=$'\t' read -r metadata path; do
  [[ "$metadata" =~ ^100644\ blob\ ([0-9a-f]{40})$ ]] || fail 'Review handoff contains a non-regular artifact.'
  blob="${BASH_REMATCH[1]}"
  limit=262144
  case "$path" in
    prepare.json|receipt.json) ;;
    *)
      [[ "$path" =~ ^reviews/(review-[0-9a-f]{32})/(packet|result)\.json$ ]] || fail 'Review handoff contains an unexpected path.'
      identifier="${BASH_REMATCH[1]}"
      [[ -z "$review" || "$review" == "$identifier" ]] || fail 'Review handoff contains multiple reviews.'
      review="$identifier"
      [[ "${BASH_REMATCH[2]:-}" != packet ]] || limit=134217728
      ;;
  esac
  size="$(git cat-file -s "$blob")"
  [[ "$size" -gt 0 && "$size" -le "$limit" ]] || fail "Review artifact exceeds its byte limit: $path"
  mkdir -p "$temporary/evidence/$(dirname "$path")"
  git cat-file blob "$blob" >"$temporary/evidence/$path"
  count=$((count + 1))
done <"$temporary/tree"
[[ "$count" -eq 4 && -n "$review" ]] || fail 'Review handoff must contain exactly four artifacts.'
for path in prepare.json receipt.json "reviews/$review/packet.json" "reviews/$review/result.json"; do
  [[ -f "$temporary/evidence/$path" ]] || fail "Review handoff is incomplete: $path"
done
mv "$temporary/evidence" "$evidence"
printf 'Restored review evidence; the merge gate must validate its acceptance and current architecture.\n'
