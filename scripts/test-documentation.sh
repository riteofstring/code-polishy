#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
checker="${policy_root}/scripts/check-documentation.sh"
fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-documentation-test.XXXXXX")"

cleanup() {
  rm -rf "${fixture_root}"
}
trap cleanup EXIT

fail() {
  echo "test-documentation: $*" >&2
  exit 1
}

write_document() {
  local path="$1"
  mkdir -p "$(dirname "${fixture_root}/${path}")"
  printf '%s\n' "${@:2}" >"${fixture_root}/${path}"
}

expect_rejected() {
  local description="$1" output
  if output="$("${checker}" --root "${fixture_root}" 2>&1)"; then
    fail "${description} was accepted"
  fi
  [[ -n "${output}" ]] || fail "${description} failed without an explanation"
}

expect_accepted() {
  local description="$1" output
  if ! output="$("${checker}" --root "${fixture_root}" 2>&1)"; then
    fail "${description} was rejected: ${output}"
  fi
}

write_document docs/index.md '# Documentation index' '' '[Guide](guides/guide.md)' '' '[Uppercase target](UPPER.MD)'
write_document docs/guides/guide.md '# Guide' '' '[Index](../index.md#top)'
write_document docs/UPPER.MD '# Uppercase target'
expect_accepted "valid Markdown documentation"

write_document docs/missing.md '# Missing target' '' '[Absent](absent.md)'
expect_rejected "a missing Markdown link target"
rm "${fixture_root}/docs/missing.md"

write_document docs/reference-missing.md '# Missing reference target' '' '[Absent]:' 'absent.md'
expect_rejected "a missing Markdown reference target on a following line"
rm "${fixture_root}/docs/reference-missing.md"

write_document docs/reference-whitespace-missing.md '# Whitespace-only reference target' '' '[Absent]:   ' 'absent.md'
expect_rejected "a missing Markdown reference target after a whitespace-only definition"
rm "${fixture_root}/docs/reference-whitespace-missing.md"

write_document docs/reference-valid.md '# Valid reference target' '' '[Guide]:' 'guides/guide.md'
expect_accepted "a valid Markdown reference target on a following line"
rm "${fixture_root}/docs/reference-valid.md"

write_document docs/reference-escaped-label.md '# Escaped reference label' '' '[Absent\]]: absent.md'
expect_rejected "a missing Markdown link target in an escaped reference label"
rm "${fixture_root}/docs/reference-escaped-label.md"

write_document docs/balanced-parentheses.md '# Balanced parentheses' '' '[Absent](absent(target).md)'
expect_rejected "a missing Markdown link target with balanced parentheses"
rm "${fixture_root}/docs/balanced-parentheses.md"

write_document docs/multiline-link.md '# Multiline link' '' '[Guide](' 'guides/guide.md' ')'
expect_accepted "a valid multiline Markdown link"
rm "${fixture_root}/docs/multiline-link.md"

write_document docs/multiline-missing.md '# Multiline missing target' '' '[Absent](' 'absent.md' ')'
expect_rejected "a missing Markdown link target on a later line"
rm "${fixture_root}/docs/multiline-missing.md"

write_document docs/escape.md '# Escaping target' '' '[Outside](../../outside.md)'
expect_rejected "a repository-escaping Markdown link"
rm "${fixture_root}/docs/escape.md"

write_document docs/no-heading.md 'A document without a heading.'
expect_rejected "a document without an H1"
rm "${fixture_root}/docs/no-heading.md"

write_document docs/indented-setext-code.md 'Setext-looking code' '    ==='
expect_rejected "a four-space-indented Setext underline"
rm "${fixture_root}/docs/indented-setext-code.md"

write_document docs/closing-hash-heading.md '# #'
expect_rejected "an ATX heading containing only a closing hash sequence"
rm "${fixture_root}/docs/closing-hash-heading.md"

write_document docs/closing-hash-content.md '# Content #'
expect_accepted "an ATX heading with content before a closing hash sequence"
rm "${fixture_root}/docs/closing-hash-content.md"

write_document docs/link-like-text.md '# Link-like text' '' "\`[Example](missing.md)\`" '\[Example](missing.md)' '[Example]\(missing.md)' 'Plain ](missing.md) text.'
expect_accepted "inline-code and escaped link-like text"
rm "${fixture_root}/docs/link-like-text.md"

write_document docs/unclosed-inline-code.md '# Unclosed inline code' '' '`Code span without a closer' '' '[Absent](absent.md)'
expect_rejected "a missing Markdown link after an unclosed inline-code delimiter"
rm "${fixture_root}/docs/unclosed-inline-code.md"

ln -s guides/guide.md "${fixture_root}/docs/linked.md"
expect_rejected "a symlinked Markdown document"
rm "${fixture_root}/docs/linked.md"

write_document docs/fenced-heading.md '# Fenced heading' '' '````text' '```' '# Four-backtick code heading' '````' '' '~~~~text' '```' '# Tilde code heading' '~~~~'
expect_accepted "fenced code containing heading-like text"
rm "${fixture_root}/docs/fenced-heading.md"

write_document docs/invalid-backtick-fence.md '# First heading' '' "\`\`\`bad\`" '# Second heading'
expect_rejected "an invalid backtick fence before a second H1"
rm "${fixture_root}/docs/invalid-backtick-fence.md"

write_document docs/two-headings.md '# First heading' '' '# Second heading'
expect_rejected "a document with multiple H1 headings"

echo "test-documentation: all checks passed"
