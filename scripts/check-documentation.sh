#!/usr/bin/env bash
set -euo pipefail

# Validate the repository's Markdown documentation without a language runtime
# or network access. The future docs module runs this contract directly.

usage() {
  echo "usage: check-documentation.sh [--root PATH]" >&2
  exit 2
}

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
case "${1-}" in
  "") ;;
  --root)
    [[ $# -eq 2 && -d "$2" ]] || usage
    repository_root="$(cd "$2" && pwd -P)"
    ;;
  *) usage ;;
esac

fail() {
  echo "documentation contract: $*" >&2
  exit 1
}

heading_count() {
  awk '
    function atx_heading_has_content(line, content) {
      content = line
      sub(/^[[:space:]]{0,3}#/, "", content)
      sub(/[[:space:]]+$/, "", content)
      sub(/[[:space:]]+#+$/, "", content)
      sub(/^[[:space:]]+/, "", content)
      sub(/[[:space:]]+$/, "", content)
      return content ~ /[^[:space:]]/
    }
    function fence_opener(line, marker, marker_character, marker_length) {
      if (line !~ /^[[:space:]]{0,3}[`~][`~][`~]/) return 0
      marker = line
      sub(/^[[:space:]]{0,3}/, "", marker)
      marker_character = substr(marker, 1, 1)
      marker_length = 0
      while (substr(marker, marker_length + 1, 1) == marker_character) marker_length++
      if (marker_length < 3) return 0
      if (marker_character == "`" && substr(marker, marker_length + 1) ~ /`/) return 0
      fence_character = marker_character
      fence_length = marker_length
      return 1
    }
    function fence_closer(line, marker, marker_length) {
      marker = line
      sub(/^[[:space:]]{0,3}/, "", marker)
      if (substr(marker, 1, 1) != fence_character) return 0
      marker_length = 0
      while (substr(marker, marker_length + 1, 1) == fence_character) marker_length++
      return marker_length >= fence_length && substr(marker, marker_length + 1) ~ /^[[:space:]]*$/
    }
    fenced {
      if (fence_closer($0)) fenced = 0
      next
    }
    fence_opener($0) { fenced = 1; next }
    /^[[:space:]]{0,3}#[[:space:]]+/ {
      if (atx_heading_has_content($0)) headings++
      previous = ""
      next
    }
    /^[[:space:]]{0,3}={3,}[[:space:]]*$/ && previous ~ /[^[:space:]]/ { headings++ }
    { previous = $0 }
    END { print headings + 0 }
  ' "$1"
}

markdown_destinations() {
  awk '
    function trim_whitespace(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }
    function emit(destination, ending, position) {
      destination = trim_whitespace(destination)
      if (substr(destination, 1, 1) == "<") {
        ending = index(destination, ">")
        if (ending > 1) destination = substr(destination, 2, ending - 2)
      } else {
        for (position = 1; position <= length(destination); position++) {
          if (substr(destination, position, 1) ~ /[[:space:]]/) {
            destination = substr(destination, 1, position - 1)
            break
          }
        }
      }
      if (destination != "") print destination
    }
    function reset_inline_destination() {
      inline_active = 0
      inline_destination = ""
      inline_depth = 0
      inline_escaped = 0
    }
    function reset_inline_state() {
      reset_inline_destination()
      inline_code_length = 0
      link_label_depth = 0
    }
    function backtick_run_length(line, position, marker_length) {
      marker_length = 0
      while (substr(line, position + marker_length, 1) == "`") marker_length++
      return marker_length
    }
    function consume_inline_destination(line, cursor, character) {
      while (cursor <= length(line)) {
        character = substr(line, cursor, 1)
        if (inline_escaped) {
          inline_destination = inline_destination character
          inline_escaped = 0
        } else if (character == "\\") {
          inline_destination = inline_destination character
          inline_escaped = 1
        } else if (character == "(") {
          inline_destination = inline_destination character
          inline_depth++
        } else if (character == ")") {
          if (inline_depth == 0) {
            emit(inline_destination)
            reset_inline_destination()
            return cursor + 1
          }
          inline_destination = inline_destination character
          inline_depth--
        } else {
          inline_destination = inline_destination character
        }
        cursor++
      }
      inline_destination = inline_destination "\n"
      return 0
    }
    function fence_opener(line, marker, marker_character, marker_length) {
      if (line !~ /^[[:space:]]{0,3}[`~][`~][`~]/) return 0
      marker = line
      sub(/^[[:space:]]{0,3}/, "", marker)
      marker_character = substr(marker, 1, 1)
      marker_length = 0
      while (substr(marker, marker_length + 1, 1) == marker_character) marker_length++
      if (marker_length < 3) return 0
      if (marker_character == "`" && substr(marker, marker_length + 1) ~ /`/) return 0
      fence_character = marker_character
      fence_length = marker_length
      return 1
    }
    function fence_closer(line, marker, marker_length) {
      marker = line
      sub(/^[[:space:]]{0,3}/, "", marker)
      if (substr(marker, 1, 1) != fence_character) return 0
      marker_length = 0
      while (substr(marker, marker_length + 1, 1) == fence_character) marker_length++
      return marker_length >= fence_length && substr(marker, marker_length + 1) ~ /^[[:space:]]*$/
    }
    function inline_destinations(line, position, character, marker_length) {
      position = 1
      while (position <= length(line)) {
        if (inline_active) {
          position = consume_inline_destination(line, position)
          if (inline_active) return
          continue
        }
        character = substr(line, position, 1)
        if (inline_code_length > 0) {
          if (character == "`") {
            marker_length = backtick_run_length(line, position)
            if (marker_length == inline_code_length) inline_code_length = 0
            position += marker_length
          } else {
            position++
          }
          continue
        }
        if (character == "\\") {
          position += 2
          continue
        }
        if (character == "`") {
          inline_code_length = backtick_run_length(line, position)
          position += inline_code_length
          continue
        }
        if (character == "[") {
          link_label_depth++
          position++
          continue
        }
        if (character == "]") {
          if (link_label_depth > 0) {
            link_label_depth--
            if (substr(line, position + 1, 1) == "(") {
              inline_active = 1
              inline_destination = ""
              inline_depth = 0
              inline_escaped = 0
              position = consume_inline_destination(line, position + 2)
              if (inline_active) return
              continue
            }
          }
        }
        position++
      }
    }
    function reference_definition(line, definition, position, character, escaped) {
      reference_definition_found = 0
      reference_definition_destination = ""
      if (line !~ /^[[:space:]]{0,3}\[/) return
      definition = line
      sub(/^[[:space:]]{0,3}/, "", definition)
      escaped = 0
      for (position = 2; position <= length(definition); position++) {
        character = substr(definition, position, 1)
        if (escaped) {
          escaped = 0
        } else if (character == "\\") {
          escaped = 1
        } else if (character == "]") {
          if (position == 2 || substr(definition, position + 1, 1) != ":") return
          reference_definition_found = 1
          reference_definition_destination = substr(definition, position + 2)
          return
        }
      }
    }
    fenced {
      if (fence_closer($0)) {
        fenced = 0
        reset_inline_state()
      }
      next
    }
    fence_opener($0) { reset_inline_state(); fenced = 1; next }
    {
      line = $0
      if (reference_destination_pending) {
        emit(line)
        reference_destination_pending = 0
      } else if (inline_code_length == 0) {
        reference_definition(line)
        if (reference_definition_found) {
          if (trim_whitespace(reference_definition_destination) == "") reference_destination_pending = 1
          else emit(reference_definition_destination)
        }
      }
      inline_destinations(line)
      # An inline code span that has no closer on this line must not suppress
      # later Markdown links through end-of-file.
      inline_code_length = 0
    }
  ' "$1"
}

normalize_relative_path() {
  local path="$1" component
  local -a components normalized
  local IFS='/'
  read -r -a components <<<"${path}"
  for component in "${components[@]}"; do
    case "${component}" in
      ""|.) ;;
      ..)
        ((${#normalized[@]} > 0)) || return 1
        unset 'normalized[${#normalized[@]} - 1]'
        ;;
      *) normalized+=("${component}") ;;
    esac
  done
  (IFS=/; printf '%s' "${normalized[*]}")
}

lowercase_ascii() {
  LC_ALL=C tr '[:upper:]' '[:lower:]'
}

check_destination() {
  local source="$1" destination="$2" destination_suffix relative source_directory normalized target target_directory
  destination="${destination%%#*}"
  destination="${destination%%\?*}"
  [[ -n "${destination}" ]] || return 0
  [[ "${destination}" =~ ^[[:alpha:]][[:alnum:]+.-]*: || "${destination}" == /* || "${destination}" == \\* ]] && return 0
  destination_suffix="$(printf '%s' "${destination}" | lowercase_ascii)"
  case "${destination_suffix}" in
    *.md|*.markdown) ;;
    *) return 0 ;;
  esac

  relative="${source#"${repository_root}/"}"
  source_directory="${relative%/*}"
  if ! normalized="$(normalize_relative_path "${source_directory}/${destination}")"; then
    fail "${relative}: Markdown link escapes the repository: ${destination}"
  fi
  target="${repository_root}/${normalized}"
  [[ -f "${target}" ]] || fail "${relative}: missing Markdown link target: ${destination}"
  [[ ! -L "${target}" ]] || fail "${relative}: Markdown link escapes the repository: ${destination}"
  target_directory="$(cd -P "$(dirname "${target}")" && pwd -P)"
  case "${target_directory}" in
    "${repository_root}"|"${repository_root}"/*) ;;
    *) fail "${relative}: Markdown link escapes the repository: ${destination}" ;;
  esac
}

documents=()
if [[ -d "${repository_root}/docs" ]]; then
  while IFS= read -r -d '' document; do
    relative="${document#"${repository_root}/"}"
    fail "${relative}: documentation paths must be regular files"
  done < <(find "${repository_root}/docs" -type l -name '*.md' -print0)
  while IFS= read -r -d '' document; do
    documents+=("${document}")
  done < <(find "${repository_root}/docs" -type f -name '*.md' -print0)
fi

for document in "${documents[@]}"; do
  relative="${document#"${repository_root}/"}"
  headings="$(heading_count "${document}")"
  [[ "${headings}" -eq 1 ]] || fail "${relative}: expected exactly one non-empty H1, found ${headings}"
  destinations="$(markdown_destinations "${document}")"
  while IFS= read -r destination; do
    check_destination "${document}" "${destination}"
  done <<<"${destinations}"
done

printf 'documentation contract passed for %d Markdown documents\n' "${#documents[@]}"
