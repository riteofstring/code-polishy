#!/usr/bin/env bash
set -euo pipefail

script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/architecture-review-handoff.sh"
fixture="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-handoff-test.XXXXXX")"
trap 'rm -rf "$fixture"' EXIT
export GIT_AUTHOR_NAME='Fixture' GIT_AUTHOR_EMAIL='fixture@example.invalid'
export GIT_COMMITTER_NAME="$GIT_AUTHOR_NAME" GIT_COMMITTER_EMAIL="$GIT_AUTHOR_EMAIL"
unset GITHUB_TOKEN
mkdir -p "$fixture/source" "$fixture/bin"
printf '#!/usr/bin/env bash\nexit 0\n' >"$fixture/bin/code-polishy"
chmod +x "$fixture/bin/code-polishy"
export PATH="$fixture/bin:$PATH"
git init -q --bare "$fixture/remote.git"
cd "$fixture/source"
git init -q
git remote add origin "$fixture/remote.git"
printf '.code-polishy-reports/\n' >.gitignore
git add .gitignore
git commit -qm base
base="$(git rev-parse HEAD)"
printf 'candidate\n' >source.txt
git add source.txt
git commit -qm candidate
candidate="$(git rev-parse HEAD)"
reference="refs/code-polishy/architecture-review/$base/$candidate"
review='review-0123456789abcdef0123456789abcdef'
evidence='.code-polishy-reports/architecture-review'
mkdir -p "$evidence/reviews/$review"
printf '{"reviewId":"%s"}\n' "$review" >"$evidence/receipt.json"
printf 'binding\n' >"$evidence/prepare.json"
printf 'source packet π\n' >"$evidence/reviews/$review/packet.json"
printf 'review result\n' >"$evidence/reviews/$review/result.json"
bash "$script" export "$base" >"$fixture/export.log"
git push -q origin "HEAD:refs/heads/main" "$reference"
git clone -q --branch main "$fixture/remote.git" "$fixture/target"
cd "$fixture/target"
bash "$script" restore "$base" >"$fixture/restore.log" 2>&1
diff -r "$fixture/source/$evidence" "$evidence"
if bash "$script" restore "$base" >"$fixture/existing.log" 2>&1; then
  echo 'Restore overwrote existing review evidence.' >&2
  exit 1
fi
rm -rf "$evidence"
valid="$(git -C "$fixture/source" rev-parse "$reference")"
for kind in missing symlink unexpected oversized wrong-candidate; do
  cd "$fixture/source"
  export GIT_INDEX_FILE="$fixture/$kind.index"
  git read-tree "$valid"
  case "$kind" in
    missing) git update-index --force-remove prepare.json ;;
    symlink)
      blob="$(printf '/tmp/outside' | git hash-object -w --stdin)"
      git update-index --add --cacheinfo "120000,$blob,prepare.json"
      ;;
    unexpected)
      blob="$(printf 'extra' | git hash-object -w --stdin)"
      git update-index --add --cacheinfo "100644,$blob,other.json"
      ;;
    oversized)
      blob="$(head -c 262145 /dev/zero | git hash-object -w --stdin)"
      git update-index --add --cacheinfo "100644,$blob,receipt.json"
      ;;
    wrong-candidate) ;;
  esac
  parent="$candidate"
  [[ "$kind" != wrong-candidate ]] || parent="$base"
  tree="$(git write-tree)"
  commit="$(printf 'invalid fixture\n' | git commit-tree "$tree" -p "$parent")"
  unset GIT_INDEX_FILE
  git update-ref "$reference" "$commit"
  git push -q --force origin "$reference"
  cd "$fixture/target"
  if bash "$script" restore "$base" >"$fixture/$kind.log" 2>&1; then
    echo "Invalid handoff restored: $kind" >&2
    exit 1
  fi
  [[ ! -e "$evidence" ]]
done
printf 'Architecture handoff roundtrip and rejection contracts passed.\n'
