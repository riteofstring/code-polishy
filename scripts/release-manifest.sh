#!/usr/bin/env bash
set -euo pipefail




















release_manifest_name="release-manifest.json"
release_manifest_version=3



release_features=(javascript-bundle)






release_pin_files=(
  "VERSION"
  "scripts/go_version.txt"
  "tools/govulncheck-version.txt"
  "tools/node-version.txt"
  "tools/osv-scanner-version.txt"
  "tools/pnpm-version.txt"
  "tools/ruff-version.txt"
  "tools/shellcheck-version.txt"
  "tools/staticcheck-version.txt"
  "tools/ty-version.txt"
)

case "$(uname -s)" in
  Darwin) release_os_tag="darwin" ;;
  Linux) release_os_tag="linux" ;;
  *)
    echo "Unsupported OS for a Code Polishy release: $(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  arm64 | aarch64) release_arch_tag="arm64" ;;
  x86_64 | amd64) release_arch_tag="x64" ;;
  *)
    echo "Unsupported architecture for a Code Polishy release: $(uname -m)" >&2
    exit 1
    ;;
esac

host_tuple="${release_os_tag}-${release_arch_tag}"

if command -v shasum >/dev/null 2>&1; then
  release_digest_command=(shasum -a 256)
elif command -v sha256sum >/dev/null 2>&1; then
  release_digest_command=(sha256sum)
else
  echo "A SHA-256 checksum tool (shasum or sha256sum) is required." >&2
  exit 1
fi

if [[ "$#" -lt 2 ]]; then
  echo "usage: release-manifest.sh <write|verify> <release-dir> [source-revision]" >&2
  exit 2
fi

mode="$1"
release_dir="$2"
case "${mode}" in
  write)
    if [[ "$#" -ne 3 ]]; then
      echo "usage: release-manifest.sh write <release-dir> <source-revision>" >&2
      exit 2
    fi
    ;;
  verify)
    if [[ "$#" -ne 2 ]]; then
      echo "usage: release-manifest.sh verify <release-dir>" >&2
      exit 2
    fi
    ;;
  *)
    echo "usage: release-manifest.sh <write|verify> <release-dir> [source-revision]" >&2
    exit 2
    ;;
esac
if [[ ! -d "${release_dir}" ]]; then
  echo "Missing Code Polishy release directory ${release_dir}." >&2
  exit 1
fi

manifest="${release_dir}/${release_manifest_name}"

read_pin() {
  local relative="$1"
  local file="${release_dir}/${relative}"
  local value
  if [[ ! -f "${file}" ]]; then
    echo "The Code Polishy release at ${release_dir} is missing ${relative}." >&2
    exit 1
  fi
  value="$(tr -d '[:space:]' <"${file}")"
  if [[ ! "${value}" =~ ^[0-9A-Za-z._+-]+$ ]]; then
    echo "The Code Polishy release at ${release_dir} records an unusable ${relative}." >&2
    exit 1
  fi
  printf '%s\n' "${value}"
}





read_tool_pin() {
  local value
  value="$(read_pin "$1")"
  printf '%s\n' "${value#v}"
}

code_polishy_version="$(read_pin "${release_pin_files[0]}")"
go_version="$(read_tool_pin "${release_pin_files[1]}")"
govulncheck_version="$(read_tool_pin "${release_pin_files[2]}")"
node_version="$(read_tool_pin "${release_pin_files[3]}")"
osv_scanner_version="$(read_tool_pin "${release_pin_files[4]}")"
pnpm_version="$(read_tool_pin "${release_pin_files[5]}")"
ruff_version="$(read_tool_pin "${release_pin_files[6]}")"
shellcheck_version="$(read_tool_pin "${release_pin_files[7]}")"
staticcheck_version="$(read_tool_pin "${release_pin_files[8]}")"
ty_version="$(read_tool_pin "${release_pin_files[9]}")"




manifest_field() {
  local key="$1"
  awk -v key="\"${key}\":" '
    $1 == key {
      value = $2
      gsub(/[",]/, "", value)
      print value
      found = 1
      exit
    }
    END { if (!found) exit 1 }
  ' "${manifest}"
}

if [[ "${mode}" == "write" ]]; then
  source_revision="$3"
else
  if [[ ! -f "${manifest}" ]]; then
    echo "The Code Polishy release at ${release_dir} has no ${release_manifest_name}." >&2
    echo "Reinstall it with ./scripts/install.sh from the matching checkout." >&2
    exit 1
  fi
  if ! source_revision="$(manifest_field sourceRevision)"; then
    echo "The Code Polishy release at ${release_dir} records no source revision." >&2
    exit 1
  fi
fi
if [[ ! "${source_revision}" =~ ^[0-9a-f]{40}$ ]]; then
  echo "A Code Polishy release records an exact source commit, not ${source_revision}." >&2
  exit 1
fi







release_entries() {
  (
    cd "${release_dir}" || exit 1
    find . -type f ! -path "./${release_manifest_name}" \
      -exec env LC_ALL=C "${release_digest_command[@]}" {} + |
      awk '{ print substr($0, 67) "\t" $1 }'
    find . -type l -print | while read -r link; do
      printf '%s\tsymlink %s\n' "${link}" "$(readlink "${link}")"
    done
  ) | LC_ALL=C sort
}

entries="$(release_entries)"
if [[ -z "${entries}" ]]; then
  echo "The Code Polishy release at ${release_dir} contains no installed entries." >&2
  exit 1
fi
content_digest="$(printf '%s\n' "${entries}" |
  LC_ALL=C "${release_digest_command[@]}" | awk '{print $1}')"
entry_count="$(printf '%s\n' "${entries}" | wc -l | tr -d '[:space:]')"




entry_json="$(printf '%s\n' "${entries}" | awk -F '\t' '
  {
    path = $1
    sub(/^\.\//, "", path)
    value = $2
    target = ""
    if (index(value, "symlink ") == 1)
      target = substr(value, 9)
    if (path ~ /["\\]/ || path ~ /[[:cntrl:]]/ ||
        target ~ /["\\]/ || target ~ /[[:cntrl:]]/) {
      printf "A Code Polishy release cannot record the entry %s.\n", path > "/dev/stderr"
      exit 1
    }
    if (target != "")
      entries[count++] = sprintf("    { \"path\": \"%s\", \"symlink\": \"%s\" }", path, target)
    else
      entries[count++] = sprintf("    { \"path\": \"%s\", \"sha256\": \"%s\" }", path, value)
  }
  END {
    for (position = 0; position < count; position++)
      printf "%s%s\n", entries[position], (position < count - 1 ? "," : "")
  }
')"



release_identity() {
  local feature
  printf 'manifestVersion=%s\n' "${release_manifest_version}"
  printf 'codePolishyVersion=%s\n' "${code_polishy_version}"
  printf 'sourceRevision=%s\n' "${source_revision}"
  for feature in "${release_features[@]}"; do
    printf 'feature=%s\n' "${feature}"
  done
  printf 'tool.go=%s\n' "${go_version}"
  printf 'tool.govulncheck=%s\n' "${govulncheck_version}"
  printf 'tool.node=%s\n' "${node_version}"
  printf 'tool.osv-scanner=%s\n' "${osv_scanner_version}"
  printf 'tool.pnpm=%s\n' "${pnpm_version}"
  printf 'tool.ruff=%s\n' "${ruff_version}"
  printf 'tool.shellcheck=%s\n' "${shellcheck_version}"
  printf 'tool.staticcheck=%s\n' "${staticcheck_version}"
  printf 'tool.ty=%s\n' "${ty_version}"
}

release_digest="$(release_identity |
  LC_ALL=C "${release_digest_command[@]}" | awk '{print $1}')"

render_features() {
  local position separator
  for position in "${!release_features[@]}"; do
    separator=","
    if ((position == ${#release_features[@]} - 1)); then
      separator=""
    fi
    printf '    "%s"%s\n' "${release_features[position]}" "${separator}"
  done
}

render_manifest() {
  cat <<MANIFEST
{
  "manifestVersion": ${release_manifest_version},
  "codePolishyVersion": "${code_polishy_version}",
  "sourceRevision": "${source_revision}",
  "host": "${host_tuple}",
  "features": [
$(render_features)
  ],
  "tools": {
    "go": "${go_version}",
    "govulncheck": "${govulncheck_version}",
    "node": "${node_version}",
    "osv-scanner": "${osv_scanner_version}",
    "pnpm": "${pnpm_version}",
    "ruff": "${ruff_version}",
    "shellcheck": "${shellcheck_version}",
    "staticcheck": "${staticcheck_version}",
    "ty": "${ty_version}"
  },
  "releaseDigest": "${release_digest}",
  "contentDigest": "${content_digest}",
  "entryCount": ${entry_count},
  "entries": [
${entry_json}
  ]
}
MANIFEST
}

if [[ "${mode}" == "verify" ]]; then
  if ! cmp -s "${manifest}" <(render_manifest); then
    echo "The Code Polishy release at ${release_dir} does not exactly match its ${release_manifest_name}." >&2
    echo "Reinstall it with ./scripts/install.sh from the matching checkout." >&2
    exit 1
  fi
  exit 0
fi

render_manifest >"${manifest}"


printf '%s\n' "${release_digest}"
