#!/usr/bin/env bash
set -euo pipefail

# Write or verify the manifest of an installed policy-owned JavaScript tool
# bundle.
#
# Usage: javascript-bundle-manifest.sh <write|verify> <bundle-dir>
#
# The manifest records which checked-in source the bundle was installed from,
# the exact runtime and tool versions it carries, and one digest over every
# installed byte. `write` runs once, on the staged tree, before that tree
# becomes the installed one. `verify` recomputes the digest from the installed
# files, so a bundle that changed after installation is rejected rather than
# executed.

# shellcheck source=tools/javascript-env.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/javascript-env.sh"

if [[ "$#" -ne 2 ]]; then
  echo "usage: javascript-bundle-manifest.sh <write|verify> <bundle-dir>" >&2
  exit 2
fi

mode="$1"
bundle_dir="$2"
case "${mode}" in
  write | verify) ;;
  *)
    echo "usage: javascript-bundle-manifest.sh <write|verify> <bundle-dir>" >&2
    exit 2
    ;;
esac
if [[ ! -d "${bundle_dir}" ]]; then
  echo "Missing JavaScript bundle directory ${bundle_dir}." >&2
  exit 1
fi

manifest="${bundle_dir}/${javascript_bundle_manifest_name}"

# Every installed entry except the manifest, digested and ordered by path, so the
# result depends on the installed bytes and not on how the tree was walked. A
# symlink contributes its exact target: pnpm's isolated linker makes every
# top-level tool a link into .pnpm, so retargeting one would otherwise swap the
# code the runner loads without changing a single installed file.
bundle_entry_digests() {
  (
    cd "${bundle_dir}" || exit 1
    find . -type f ! -name "${javascript_bundle_manifest_name}" \
      -exec env LC_ALL=C "${javascript_digest_command[@]}" {} + |
      awk '{ print substr($0, 67) "\t" $1 }'
    find . -type l -print | while read -r link; do
      printf '%s\tsymlink %s\n' "${link}" "$(readlink "${link}")"
    done
  ) | LC_ALL=C sort
}

entry_digests="$(bundle_entry_digests)"
if [[ -z "${entry_digests}" ]]; then
  echo "The JavaScript bundle at ${bundle_dir} contains no installed entries." >&2
  exit 1
fi
bundle_digest="$(printf '%s\n' "${entry_digests}" |
  LC_ALL=C "${javascript_digest_command[@]}" | awk '{print $1}')"
entry_count="$(printf '%s\n' "${entry_digests}" | wc -l | tr -d '[:space:]')"

# The tool inventory is the bundle's own exact pins. check-javascript-bundle.sh
# reconciles those pins with the complete installed graph and its licenses; the
# runner confirms each one against the package installed beside it.
tools_json="$(awk '
  /"dependencies"/ { in_dependencies = 1; next }
  /^  }/ { in_dependencies = 0 }
  in_dependencies {
    gsub(/[",]/, "")
    sub(/:$/, "", $1)
    entries[count++] = sprintf("    \"%s\": \"%s\"", $1, $2)
  }
  END {
    for (position = 0; position < count; position++)
      printf "%s%s\n", entries[position], (position < count - 1 ? "," : "")
  }
' "${javascript_bundle_source}/package.json")"
if [[ -z "${tools_json}" ]]; then
  echo "The checked-in JavaScript bundle declares no tools." >&2
  exit 1
fi

render_manifest() {
  cat <<MANIFEST
{
  "manifestVersion": 1,
  "sourceDigest": "$(javascript_bundle_source_digest)",
  "bundleDigest": "${bundle_digest}",
  "entryCount": ${entry_count},
  "node": "$(tr -d '[:space:]' <"${javascript_policy_root}/tools/node-version.txt")",
  "pnpm": "$(tr -d '[:space:]' <"${javascript_policy_root}/tools/pnpm-version.txt")",
  "tools": {
${tools_json}
  }
}
MANIFEST
}

if [[ "${mode}" == "verify" ]]; then
  if [[ ! -f "${manifest}" ]]; then
    echo "The JavaScript bundle at ${bundle_dir} has no ${javascript_bundle_manifest_name}." >&2
    echo "Run ./tools/install-javascript-bundle.sh to install it." >&2
    exit 1
  fi
  if ! cmp -s "${manifest}" <(render_manifest); then
    echo "The JavaScript bundle at ${bundle_dir} does not exactly match its installed entries and checked-in pins." >&2
    echo "Run ./tools/install-javascript-bundle.sh to reinstall it." >&2
    exit 1
  fi
  exit 0
fi

render_manifest >"${manifest}"
