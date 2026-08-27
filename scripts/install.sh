#!/usr/bin/env bash
set -euo pipefail

# Install one exact Code Polishy release from this local checkout.
#
# Usage: install.sh [--prefix DIR] [--command-dir DIR] [--add-to-path [--path-profile FILE]]
#
# Source acquisition stays outside this script. A user or agent checks out one
# exact reviewed version tag, then runs this installer from that checkout. The
# installer never chooses a version, invokes `gh`, calls a GitHub API, asks for
# a token, inspects or changes remotes, fetches, pulls, or switches revisions.
# It reads the local commit identity, requires a clean checkout, and builds what
# is already there. Maintainers may use the same path for a clean candidate
# commit before its release tag is created.
#
# Acquiring the sealed JavaScript runtime and tool bundle is a separate explicit
# step. This installer refuses to run until they are installed and verified, so
# installation itself downloads nothing.
#
# The release is staged, verified, and only then moved into place under one
# name, so an interrupted or rejected installation never leaves a release a
# target could run.

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
caller_root="$(pwd -P)"
default_prefix="${HOME}/.local/share/code-polishy"
prefix="${default_prefix}"
requested_command_root=""
requested_path_profile=""
add_to_path=false

usage() {
  echo "usage: install.sh [--prefix DIR] [--command-dir DIR] [--add-to-path [--path-profile FILE]]" >&2
  exit 2
}

require_path_argument() {
  local option="$1" value="$2"
  if [[ -z "${value}" ]]; then
    echo "${option} requires a non-empty path" >&2
    exit 2
  fi
  if [[ "${value}" != /* ]]; then
    value="${caller_root}/${value}"
  fi
  while [[ "${value}" != "/" && "${value}" == */ ]]; do
    value="${value%/}"
  done
  printf '%s\n' "${value}"
}

while (($#)); do
  case "$1" in
    --prefix)
      if (($# < 2)); then
        usage
      fi
      prefix="$(require_path_argument --prefix "$2")"
      shift 2
      ;;
    --prefix=*)
      prefix="$(require_path_argument --prefix "${1#*=}")"
      shift
      ;;
    --command-dir)
      if (($# < 2)); then
        usage
      fi
      requested_command_root="$(require_path_argument --command-dir "$2")"
      shift 2
      ;;
    --command-dir=*)
      requested_command_root="$(require_path_argument --command-dir "${1#*=}")"
      shift
      ;;
    --add-to-path)
      add_to_path=true
      shift
      ;;
    --path-profile)
      if (($# < 2)); then
        usage
      fi
      requested_path_profile="$(require_path_argument --path-profile "$2")"
      shift 2
      ;;
    --path-profile=*)
      requested_path_profile="$(require_path_argument --path-profile "${1#*=}")"
      shift
      ;;
    *) usage ;;
  esac
done
if [[ -n "${requested_path_profile}" && "${add_to_path}" != true ]]; then
  echo "--path-profile requires --add-to-path" >&2
  exit 2
fi

launcher_root="${prefix}/bin"
launcher_path="${launcher_root}/code-polishy"
command_root="${launcher_root}"
command_link=""
if [[ -n "${requested_command_root}" ]]; then
  command_root="${requested_command_root}"
  if [[ "${command_root}" != "${launcher_root}" ]]; then
    command_link="${command_root}/code-polishy"
  fi
elif [[ "${prefix}" == "${default_prefix}" ]]; then
  command_root="${HOME}/.local/bin"
  command_link="${command_root}/code-polishy"
fi
path_profile=""
path_profile_line=""
path_profile_state="not-requested"
path_profile_staging=""
if [[ "${command_root}" == *$'\n'* ]]; then
  echo "The installation command directory cannot contain a newline." >&2
  exit 1
fi

shell_quote() {
  printf "'"
  printf '%s' "$1" | sed "s/'/'\\\\''/g"
  printf "'"
}

validate_command_link() {
  if [[ -z "${command_link}" ]]; then
    return
  fi
  if [[ -e "${command_root}" && ! -d "${command_root}" ]]; then
    echo "The command directory is not a directory: ${command_root}" >&2
    exit 1
  fi
  if [[ -L "${command_link}" ]]; then
    local target
    target="$(readlink "${command_link}")"
    if [[ "${target}" != "${launcher_path}" ]]; then
      echo "Refusing to replace the unrelated command link at ${command_link}; it points to ${target}." >&2
      exit 1
    fi
    return
  fi
  if [[ -e "${command_link}" ]]; then
    echo "Refusing to replace the unrelated path at ${command_link}." >&2
    exit 1
  fi
}

plan_path_persistence() {
  if [[ "${add_to_path}" != true ]]; then
    return
  fi
  local shell_name quoted_root profile_root marker
  shell_name="$(basename "${SHELL:-sh}")"
  quoted_root="$(shell_quote "${command_root}")"
  marker="# Code Polishy PATH"
  case "${shell_name}" in
    zsh)
      profile_root="${ZDOTDIR:-${HOME}}"
      path_profile="${profile_root}/.zprofile"
      path_profile_line="export PATH=${quoted_root}:\"\$PATH\" ${marker}"
      ;;
    bash)
      if [[ -e "${HOME}/.bash_profile" || -L "${HOME}/.bash_profile" ]]; then
        path_profile="${HOME}/.bash_profile"
      else
        path_profile="${HOME}/.profile"
      fi
      path_profile_line="export PATH=${quoted_root}:\"\$PATH\" ${marker}"
      ;;
    sh | dash | ksh)
      path_profile="${HOME}/.profile"
      path_profile_line="export PATH=${quoted_root}:\"\$PATH\" ${marker}"
      ;;
    fish)
      profile_root="${XDG_CONFIG_HOME:-${HOME}/.config}"
      path_profile="${profile_root}/fish/conf.d/code-polishy.fish"
      path_profile_line="fish_add_path ${quoted_root} ${marker}"
      ;;
    *)
      echo "--add-to-path does not support ${SHELL:-the current shell}; add ${command_root} manually." >&2
      exit 1
      ;;
  esac
  if [[ -n "${requested_path_profile}" ]]; then
    path_profile="${requested_path_profile}"
  fi
  if [[ "${path_profile}" != /* ]]; then
    echo "The shell configuration directory for --add-to-path must be absolute: ${path_profile}" >&2
    exit 1
  fi
  if [[ -L "${path_profile}" || ( -e "${path_profile}" && ! -f "${path_profile}" ) ]]; then
    echo "Refusing to edit the non-regular shell startup path at ${path_profile}." >&2
    exit 1
  fi
  if [[ -f "${path_profile}" ]]; then
    if grep -Fqx "${path_profile_line}" "${path_profile}"; then
      path_profile_state="current"
      return
    fi
    if grep -Fq "${marker}" "${path_profile}"; then
      echo "Refusing to replace the existing Code Polishy PATH entry in ${path_profile}." >&2
      exit 1
    fi
  fi
  path_profile_state="planned"
}

publish_path_persistence() {
  if [[ "${path_profile_state}" == "not-requested" ]]; then
    return
  fi
  if [[ "${path_profile_state}" == "current" ]]; then
    echo "Persistent PATH: ${path_profile} already includes ${command_root}."
    return
  fi
  local profile_directory
  profile_directory="$(dirname "${path_profile}")"
  mkdir -p "${profile_directory}"
  path_profile_staging="$(mktemp "${profile_directory}/.code-polishy-path.XXXXXX")"
  if [[ -f "${path_profile}" ]]; then
    cp -p "${path_profile}" "${path_profile_staging}"
    printf '\n' >>"${path_profile_staging}"
  else
    chmod 600 "${path_profile_staging}"
  fi
  printf '%s\n' "${path_profile_line}" >>"${path_profile_staging}"
  mv -f "${path_profile_staging}" "${path_profile}"
  path_profile_staging=""
  path_profile_state="current"
  echo "Persistent PATH: added ${command_root} in ${path_profile}; open a new shell to use it."
}

# shellcheck source=tools/javascript-env.sh
source "${policy_root}/tools/javascript-env.sh"

release_manifest="${policy_root}/scripts/release-manifest.sh"
bundle_manifest="${policy_root}/tools/javascript-bundle-manifest.sh"

# Everything a release carries besides the built binary and its own manifest:
# the version-matched documentation, schema, templates, and canonical guidance
# a target is given; the pinned versions and workflow scripts the engine reads
# at runtime; the sealed JavaScript runtime and tool bundle; and the bundle's
# dependency and license inventory. Each path is required; a missing one fails
# the installation rather than producing a release with a hole in it.
release_contents=(
  "VERSION"
  "LICENSE"
  "README.md"
  "CHANGELOG.md"
  "docs"
  "schema"
  "templates"
  "skills"
  "artifact-security"
  "scripts/go_version.txt"
  "scripts/release-manifest.sh"
  "tools/shellcheck.sh"
  "tools/shellcheck-version.txt"
  "tools/node-version.txt"
  "tools/pnpm-version.txt"
  "tools/staticcheck-version.txt"
  "tools/govulncheck-version.txt"
  "tools/osv-scanner-version.txt"
  "tools/ruff-version.txt"
  "tools/trivy-version.txt"
  "tools/javascript_bundle_inventory.txt"
)

# The pinned tools the engine runs from its own policy root. A release is the
# whole Code Polishy a target gets, so it carries them: a target installs no
# policy tooling, and nothing is taken from an ambient PATH or a host
# installation. Acquiring them is the same separate explicit step the sealed
# runtime uses, so installation itself still downloads nothing.
case "${javascript_arch_tag}" in
  arm64)
    go_arch_tag="arm64"
    shellcheck_arch_tag="aarch64"
    ;;
  x64)
    go_arch_tag="amd64"
    shellcheck_arch_tag="x86_64"
    ;;
  *)
    echo "Unsupported architecture for the pinned Code Polishy tools: ${javascript_arch_tag}" >&2
    exit 1
    ;;
esac
go_tool_dir=".tools/go/${javascript_os_tag}-${go_arch_tag}"
shellcheck_tool_dir=".tools/shellcheck/${javascript_os_tag}-${shellcheck_arch_tag}"
policy_tools=(
  "${go_tool_dir}"
  "${shellcheck_tool_dir}"
  ".tools/bin/staticcheck"
  ".tools/bin/govulncheck"
  ".tools/bin/osv-scanner"
  ".tools/bin/ruff"
)

# Each acquired executable a release carries, beside the checked-in file that
# pins its version. A release records these versions and a target trusts them,
# so the installer asks every one of them what it is before it stages anything:
# a presence check and the byte inventory cannot show that an ignored local tool
# cache holds the version the manifest would claim.
#
# The engine and the launcher are built here from the reviewed commit rather
# than acquired, so what they are is the source revision the manifest already
# records. The bundle's analyzers are governed by its frozen lock, inventory,
# and manifest, and the only executables that run them are the Node and pnpm
# probed here.
carried_tools=(
  "go:scripts/go_version.txt"
  "node:tools/node-version.txt"
  "pnpm:tools/pnpm-version.txt"
  "shellcheck:tools/shellcheck-version.txt"
  "staticcheck:tools/staticcheck-version.txt"
  "govulncheck:tools/govulncheck-version.txt"
  "osv-scanner:tools/osv-scanner-version.txt"
  "ruff:tools/ruff-version.txt"
)

# The version a checked-in pin names, in the one form every probe reports: the
# distributions disagree about a leading `v`, and a release records one form.
pinned_version() {
  local value
  value="$(tr -d '[:space:]' <"${policy_root}/$1")"
  printf '%s\n' "${value#v}"
}

# The version the local executable reports, asked of it the way its own
# distribution answers.
probed_version() {
  case "$1" in
    go)
      javascript_sealed_run "${policy_root}/${go_tool_dir}/go/bin/go" version |
        awk '{ sub(/^go/, "", $3); print $3 }'
      ;;
    node)
      javascript_sealed_run "${javascript_node}" --version | sed 's/^v//'
      ;;
    pnpm)
      javascript_sealed_run "${javascript_node}" "${javascript_pnpm}" --version
      ;;
    shellcheck)
      javascript_sealed_run "${policy_root}/${shellcheck_tool_dir}/shellcheck" --version |
        awk '/^version:/ { print $2 }'
      ;;
    staticcheck | govulncheck)
      # Read out of the binary with the pinned toolchain rather than asked of
      # the binary: `govulncheck -version` contacts the vulnerability database,
      # and which version a release carries is an offline question.
      javascript_sealed_run "${policy_root}/${go_tool_dir}/go/bin/go" version -m \
        "${policy_root}/.tools/bin/$1" |
        awk '$1 == "mod" { sub(/^v/, "", $3); print $3; exit }'
      ;;
    osv-scanner)
      javascript_sealed_run "${policy_root}/.tools/bin/osv-scanner" --version |
        awk '/^osv-scanner version:/ { print $3 }'
      ;;
    ruff)
      javascript_sealed_run "${policy_root}/.tools/bin/ruff" --version | awk '{ print $2 }'
      ;;
    *)
      echo "There is no version probe for the carried ${1}." >&2
      exit 1
      ;;
  esac
}

for tool in git find cp mv grep ln readlink mktemp sed; do
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "${tool} is required to install Code Polishy." >&2
    exit 1
  fi
done
validate_command_link
plan_path_persistence

# A release names the exact reviewed commit it was built from, so the checkout
# must be a repository whose working tree carries nothing that commit does not.
if ! git -C "${policy_root}" rev-parse --git-dir >/dev/null 2>&1; then
  echo "Install Code Polishy from a Git checkout; ${policy_root} is not one." >&2
  exit 1
fi
checkout_root="$(cd "$(git -C "${policy_root}" rev-parse --show-toplevel)" && pwd -P)"
if [[ "${checkout_root}" != "${policy_root}" ]]; then
  echo "Install Code Polishy from its own checkout root, not from ${checkout_root}." >&2
  exit 1
fi
# The drift report is pinned rather than inherited: configuration such as
# status.showUntrackedFiles=no must not hide untracked files from this gate.
if [[ -n "$(git -C "${policy_root}" status --porcelain=v1 --untracked-files=all)" ]]; then
  echo "The Code Polishy checkout at ${policy_root} is not clean." >&2
  echo "Check out the exact reviewed commit before installing a release." >&2
  exit 1
fi
source_revision="$(git -C "${policy_root}" rev-parse HEAD)"
if [[ ! "${source_revision}" =~ ^[0-9a-f]{40}$ ]]; then
  echo "The Code Polishy checkout at ${policy_root} has no committed revision." >&2
  exit 1
fi

for relative in "${release_contents[@]}"; do
  if [[ ! -e "${policy_root}/${relative}" ]]; then
    echo "The Code Polishy checkout at ${policy_root} is missing ${relative}." >&2
    exit 1
  fi
done

# The version names the installed release, so it is read here, before anything
# is staged, by the one strict reader the release preflight also runs:
# whitespace is refused with its exact remedy rather than deleted into a
# different release name.
code_polishy_version="$("${policy_root}/scripts/release-version.sh" "${policy_root}/VERSION")"

# The sealed runtime and bundle are installed by their own explicit acquisition
# step. Confirming them here keeps installation offline and keeps a release from
# carrying a bundle that no longer matches its checked-in pins.
if [[ ! -x "${javascript_node}" || ! -f "${javascript_pnpm}" ]]; then
  echo "The policy-owned JavaScript runtime is not installed at ${javascript_runtime_dir}." >&2
  echo "Run ./tools/install-javascript-runtime.sh before installing a release." >&2
  exit 1
fi
if ! "${bundle_manifest}" verify "${javascript_bundle_dir}"; then
  echo "Run ./tools/install-javascript-bundle.sh before installing a release." >&2
  exit 1
fi
for relative in "${policy_tools[@]}"; do
  if [[ ! -e "${policy_root}/${relative}" ]]; then
    echo "The Code Polishy checkout at ${policy_root} carries no pinned ${relative}." >&2
    echo "Run ./tools/install-policy-tools.sh before installing a release." >&2
    exit 1
  fi
done

staging_root="${prefix}/releases"
staging="${staging_root}/.staging-$$"
# Where a release that no longer matches its own manifest waits while its
# replacement takes the installed name.
superseded="${staging_root}/.superseded-$$"
launcher_staging="${launcher_root}/.code-polishy-$$"
scratch="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-install.XXXXXX")"
cleanup() {
  rm -rf "${staging}" "${superseded}" "${launcher_staging}" "${scratch}"
  if [[ -n "${path_profile_staging}" ]]; then
    rm -f "${path_profile_staging}"
  fi
}
# An installation interrupted after staging has begun removes the trees it made
# itself rather than leaving them for the next one to find. Neither one is a
# release a target could run -- no lock can name either -- but a release store
# holds only complete releases.
interrupted() {
  cleanup
  echo "The Code Polishy installation was interrupted; nothing was installed." >&2
  exit 1
}
trap cleanup EXIT
trap interrupted INT TERM HUP

javascript_scratch_home="${scratch}/home"
echo "Probing the exact version of every tool this release carries..."
for carried in "${carried_tools[@]}"; do
  carried_tool="${carried%%:*}"
  carried_pin="${carried#*:}"
  expected_version="$(pinned_version "${carried_pin}")"
  if ! reported_version="$(probed_version "${carried_tool}" 2>/dev/null)"; then
    reported_version=""
  fi
  if [[ "${reported_version}" != "${expected_version}" ]]; then
    echo "The ${carried_tool} this checkout carries reports ${reported_version:-no version}, and ${carried_pin} pins ${expected_version}." >&2
    echo "Run ./tools/install-policy-tools.sh before installing a release." >&2
    exit 1
  fi
done

mkdir -p "${staging_root}"
mkdir -p "${staging}/bin"

echo "Building the Code Polishy binaries from ${source_revision}..."
"${policy_root}/scripts/build.sh" "${staging}/bin"

echo "Staging the release..."
for relative in "${release_contents[@]}"; do
  mkdir -p "$(dirname "${staging}/${relative}")"
  cp -RPp "${policy_root}/${relative}" "${staging}/${relative}"
done

# The sealed runtime is the one platform-specific tree in a release, and the
# bundle is shared by every supported host. Both keep the layout the engine
# already resolves, so an installed release is simply a complete policy root.
staged_tools="${staging}/.tools"
staged_javascript="${staged_tools}/javascript"
mkdir -p "${staged_javascript}"
cp -RPp "${javascript_runtime_dir}" "${staged_javascript}/${javascript_platform_tag}"
cp -RPp "${javascript_bundle_dir}" "${staged_javascript}/bundle"
for relative in "${policy_tools[@]}"; do
  mkdir -p "$(dirname "${staging}/${relative}")"
  cp -RPp "${policy_root}/${relative}" "${staging}/${relative}"
done

# A release is the whole Code Polishy interface a target gets, so the submodule
# and wrapper control plane the direct cutover removed must not reach one
# through a template, canonical guidance, a skill, or a workflow script. Search
# the staged tree for the paths that plane was made of and every policy-owned
# runtime or instruction surface for the commands that drove it. The changelog
# and permanent guides may explain why a retired command is invalid;
# explanation is not an executable or canonical instruction. The acquired tools
# under .tools are third-party bytes governed by their own checksums, inventory,
# and manifest and are not searched for Code Polishy's own retired commands.
echo "Searching the staged release for retired paths and commands..."
retired_paths="$(find "${staging}" -path "${staged_tools}" -prune -o \
  \( -name ".gitmodules" -o -name ".git" \
  -o -name "check_policy.sh" -o -name "code-task-session" \
  -o -name "code-plan-loop" \) -print)"
if [[ -n "${retired_paths}" ]]; then
  echo "The staged release carries retired paths:" >&2
  printf '%s\n' "${retired_paths}" | sed "s#^${staging}/#  #" >&2
  exit 1
fi
staged_text=()
for relative in "${release_contents[@]}"; do
  case "${relative}" in
    "CHANGELOG.md" | "docs") continue ;;
    *) staged_text+=("${staging}/${relative}") ;;
  esac
done
for retired in "check_policy.sh" "git submodule"; do
  # grep reports 0 when it matched and 1 when it did not; anything above that is
  # a search that failed, which is not a release that carries nothing.
  search_status=0
  retired_text="$(grep -rIl -F -e "${retired}" -- "${staged_text[@]}")" || search_status=$?
  if ((search_status > 1)); then
    echo "Searching the staged release for \`${retired}\` failed." >&2
    exit 1
  fi
  if [[ -n "${retired_text}" ]]; then
    echo "The staged release still names the retired \`${retired}\` in:" >&2
    printf '%s\n' "${retired_text}" | sed "s#^${staging}/#  #" >&2
    exit 1
  fi
done

release_digest="$("${release_manifest}" write "${staging}" "${source_revision}")"
"${release_manifest}" verify "${staging}"

if [[ ! "${release_digest}" =~ ^[0-9a-f]{64}$ ]]; then
  echo "The staged release recorded no usable release digest." >&2
  exit 1
fi
release_id="${code_polishy_version}-${release_digest}"
destination="${staging_root}/${release_id}"

installed_message="Installed Code Polishy ${code_polishy_version} at ${destination}."
if [[ -e "${destination}" ]]; then
  if "${release_manifest}" verify "${destination}" >/dev/null 2>&1; then
    installed_message="Code Polishy ${code_polishy_version} is already installed at ${destination}."
  else
    # A release that no longer matches its own manifest is replaced whole. The
    # rejected bytes move aside first so the installed name is never a partial
    # tree, and they are removed only once the verified one is in place.
    rm -rf "${superseded}"
    mv "${destination}" "${superseded}"
    mv "${staging}" "${destination}"
    rm -rf "${superseded}"
  fi
else
  mv "${staging}" "${destination}"
fi

# The launcher is the one stable path a target runs. It is taken from the
# release that was just verified rather than built again, and it is moved into
# place within its own directory so the installed name is never a partial file.
# One launcher selects every installed release that records itself the way this
# one does, so refreshing it does not change how another of those is selected. A
# release recorded under an earlier manifest version is not one of them: the
# launcher reads one exact manifest version, so a release installed before the
# manifest changed is reinstalled from its commit rather than reinterpreted.
mkdir -p "${launcher_root}"
cp -p "${destination}/bin/code-polishy-launcher" "${launcher_staging}"
mv -f "${launcher_staging}" "${launcher_path}"

if [[ -n "${command_link}" ]]; then
  mkdir -p "${command_root}"
  if [[ ! -L "${command_link}" ]]; then
    ln -s "${launcher_path}" "${command_link}"
  fi
fi
publish_path_persistence

echo "${installed_message}"
echo "Release digest: ${release_digest}"
echo "Launcher: ${launcher_path}"
if [[ -n "${command_link}" ]]; then
  echo "Command: ${command_link}"
fi
discovered_launcher="$(command -v code-polishy 2>/dev/null || true)"
if [[ "${discovered_launcher}" == "${launcher_path}" || \
  ( -n "${command_link}" && "${discovered_launcher}" == "${command_link}" ) ]]; then
  echo "Command discovery: code-polishy already resolves to the installed launcher."
else
  if [[ -n "${discovered_launcher}" ]]; then
    echo "Command discovery: this shell currently resolves code-polishy to ${discovered_launcher}, not the installed launcher."
  else
    echo "Command discovery: the installed launcher is not on this shell's PATH."
  fi
  if [[ "$(basename "${SHELL:-sh}")" == "fish" ]]; then
    printf '  fish_add_path %s\n' "$(shell_quote "${command_root}")"
  else
    printf '  export PATH=%s:"%s"\n' "$(shell_quote "${command_root}")" "\$PATH"
  fi
fi
echo "Require this release in a target repository with:"
echo "  ${destination}/bin/code-polishy lock"
