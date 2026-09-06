#!/usr/bin/env bash
set -euo pipefail





















policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
caller_root="$(pwd -P)"
default_prefix="${HOME}/.local/share/code-polishy"
prefix="${default_prefix}"
requested_command_root=""
requested_path_profile=""
publication_dir=""
add_to_path=false
build_only=false

usage() {
  echo "usage: install.sh [--prefix DIR] [--command-dir DIR] [--add-to-path [--path-profile FILE]] [--publication-dir DIR --build-only]" >&2
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
    --publication-dir)
      if (($# < 2)); then
        usage
      fi
      publication_dir="$(require_path_argument --publication-dir "$2")"
      shift 2
      ;;
    --publication-dir=*)
      publication_dir="$(require_path_argument --publication-dir "${1#*=}")"
      shift
      ;;
    --build-only)
      build_only=true
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
if [[ "${build_only}" == true && -z "${publication_dir}" ]]; then
  echo "--build-only requires --publication-dir" >&2
  exit 2
fi
if [[ -n "${publication_dir}" && -e "${publication_dir}" ]]; then
  echo "The publication directory already exists: ${publication_dir}" >&2
  exit 1
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







release_contents=(
  "VERSION"
  "LICENSE"
  "README.md"
  "CHANGELOG.md"
  "docs"
  "schema"
  "templates"
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
  "internal/pythonfacts/pyproject.toml"
  "internal/pythonfacts/uv.lock"
  "tools/astroid-version.txt"
  "tools/astroid_checksums.txt"
  "tools/packaging-version.txt"
  "tools/packaging_wheel_checksums.txt"
  "tools/python-version.txt"
  "tools/python_runtime_checksums.txt"
  "tools/ruff-version.txt"
  "tools/ty-version.txt"
  "tools/ty.toml"
  "tools/trivy-version.txt"
  "tools/vulture-version.txt"
  "tools/vulture_wheel_checksums.txt"
  "tools/javascript_bundle_inventory.txt"
)






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
python_tool_dir=".tools/python/${javascript_platform_tag}"
policy_tools=(
  "${go_tool_dir}"
  "${shellcheck_tool_dir}"
  "${python_tool_dir}"
  ".tools/bin/staticcheck"
  ".tools/bin/govulncheck"
  ".tools/bin/osv-scanner"
  ".tools/bin/ruff"
  ".tools/bin/ty"
)












carried_tools=(
  "go:scripts/go_version.txt"
  "node:tools/node-version.txt"
  "pnpm:tools/pnpm-version.txt"
  "shellcheck:tools/shellcheck-version.txt"
  "staticcheck:tools/staticcheck-version.txt"
  "govulncheck:tools/govulncheck-version.txt"
  "osv-scanner:tools/osv-scanner-version.txt"
  "astroid:tools/astroid-version.txt"
  "packaging:tools/packaging-version.txt"
  "python:tools/python-version.txt"
  "ruff:tools/ruff-version.txt"
  "ty:tools/ty-version.txt"
  "vulture:tools/vulture-version.txt"
)

carried_markers=(
  "astroid:${python_tool_dir}/.code-polishy-astroid-release:tools/astroid-version.txt"
  "packaging:${python_tool_dir}/.code-polishy-packaging-release:tools/packaging-version.txt"
  "python:${python_tool_dir}/.code-polishy-python-release:tools/python-version.txt"
  "vulture:${python_tool_dir}/.code-polishy-vulture-release:tools/vulture-version.txt"
)



pinned_version() {
  local value
  value="$(tr -d '[:space:]' <"${policy_root}/$1")"
  if [[ "$1" == "tools/python-version.txt" ]]; then
    value="${value%%+*}"
  fi
  printf '%s\n' "${value#v}"
}

pinned_identity() {
  local value
  value="$(tr -d '[:space:]' <"${policy_root}/$1")"
  printf '%s\n' "${value#v}"
}



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



      javascript_sealed_run "${policy_root}/${go_tool_dir}/go/bin/go" version -m \
        "${policy_root}/.tools/bin/$1" |
        awk '$1 == "mod" { sub(/^v/, "", $3); print $3; exit }'
      ;;
    osv-scanner)
      javascript_sealed_run "${policy_root}/.tools/bin/osv-scanner" --version |
        awk '/^osv-scanner version:/ { print $3 }'
      ;;
    astroid)
      javascript_sealed_run "${policy_root}/${python_tool_dir}/python" -I -B -c 'import importlib.metadata; print(importlib.metadata.version("astroid"))'
      ;;
    packaging)
      javascript_sealed_run "${policy_root}/${python_tool_dir}/python" -I -B -c \
        'import importlib.metadata; print(importlib.metadata.version("packaging"))'
      ;;
    python)
      javascript_sealed_run "${policy_root}/${python_tool_dir}/python" -I -B -c \
        'import sys; print(".".join(str(value) for value in sys.version_info[:3]))'
      ;;
    ruff)
      javascript_sealed_run "${policy_root}/.tools/bin/ruff" --version | awk '{ print $2 }'
      ;;
    ty)
      javascript_sealed_run "${policy_root}/.tools/bin/ty" --version | awk '{ print $2 }'
      ;;
    vulture)
      javascript_sealed_run "${policy_root}/${python_tool_dir}/python" -I -B -c \
        'import importlib.metadata; print(importlib.metadata.version("vulture"))'
      ;;
    *)
      echo "There is no version probe for the carried ${1}." >&2
      exit 1
      ;;
  esac
}

vulture_metadata_is_exact() {
  local site_packages metadata expected count=0 malformed=0 exact=""
  if ! site_packages="$(javascript_sealed_run "${policy_root}/${python_tool_dir}/python" -I -B -c \
    'import sysconfig; print(sysconfig.get_paths()["purelib"])' 2>/dev/null)"; then
    return 1
  fi
  if [[ "${site_packages}" == *$'\n'* ]]; then
    return 1
  fi
  case "${site_packages}" in
    "${policy_root}/${python_tool_dir}"/*) ;;
    *) return 1 ;;
  esac
  expected="${site_packages}/vulture-$(pinned_version "tools/vulture-version.txt").dist-info"
  shopt -s nullglob
  for metadata in "${site_packages}"/vulture-*.dist-info; do
    if [[ ! -d "${metadata}" ]]; then
      malformed=1
    fi
    count=$((count + 1))
    exact="${metadata}"
  done
  shopt -u nullglob
  [[ "${malformed}" -eq 0 ]] && [[ "${count}" -eq 1 ]] && [[ "${exact}" == "${expected}" ]]
}

packaging_metadata_is_exact() {
  local site_packages metadata expected count=0 malformed=0 exact=""
  if ! site_packages="$(javascript_sealed_run "${policy_root}/${python_tool_dir}/python" -I -B -c \
    'import sysconfig; print(sysconfig.get_paths()["purelib"])' 2>/dev/null)"; then
    return 1
  fi
  if [[ "${site_packages}" == *$'\n'* ]]; then
    return 1
  fi
  case "${site_packages}" in
    "${policy_root}/${python_tool_dir}"/*) ;;
    *) return 1 ;;
  esac
  expected="${site_packages}/packaging-$(pinned_version "tools/packaging-version.txt").dist-info"
  shopt -s nullglob
  for metadata in "${site_packages}"/packaging-*.dist-info; do
    if [[ ! -d "${metadata}" ]]; then
      malformed=1
    fi
    count=$((count + 1))
    exact="${metadata}"
  done
  shopt -u nullglob
  [[ "${malformed}" -eq 0 ]] && [[ "${count}" -eq 1 ]] && [[ "${exact}" == "${expected}" ]]
}

astroid_metadata_is_exact() {
  local site_packages metadata expected count=0 malformed=0 exact=""
  if ! site_packages="$(javascript_sealed_run "${policy_root}/${python_tool_dir}/python" -I -B -c \
    'import sysconfig; print(sysconfig.get_paths()["purelib"])' 2>/dev/null)"; then
    return 1
  fi
  if [[ "${site_packages}" == *$'\n'* ]]; then
    return 1
  fi
  case "${site_packages}" in
    "${policy_root}/${python_tool_dir}"/*) ;;
    *) return 1 ;;
  esac
  expected="${site_packages}/astroid-$(pinned_version "tools/astroid-version.txt").dist-info"
  shopt -s nullglob
  for metadata in "${site_packages}"/astroid-*.dist-info; do
    if [[ ! -d "${metadata}" ]]; then
      malformed=1
    fi
    count=$((count + 1))
    exact="${metadata}"
  done
  shopt -u nullglob
  [[ "${malformed}" -eq 0 ]] && [[ "${count}" -eq 1 ]] && [[ "${exact}" == "${expected}" ]]
}

for tool in git find cp mv grep ln readlink mktemp sed; do
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "${tool} is required to install Code Polishy." >&2
    exit 1
  fi
done
validate_command_link
plan_path_persistence



if ! git -C "${policy_root}" rev-parse --git-dir >/dev/null 2>&1; then
  echo "Install Code Polishy from a Git checkout; ${policy_root} is not one." >&2
  exit 1
fi
checkout_root="$(cd "$(git -C "${policy_root}" rev-parse --show-toplevel)" && pwd -P)"
if [[ "${checkout_root}" != "${policy_root}" ]]; then
  echo "Install Code Polishy from its own checkout root, not from ${checkout_root}." >&2
  exit 1
fi


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





code_polishy_version="$("${policy_root}/scripts/release-version.sh" "${policy_root}/VERSION")"




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

for carried in "${carried_markers[@]}"; do
  carried_tool="${carried%%:*}"
  carried_remainder="${carried#*:}"
  marker_path="${carried_remainder%%:*}"
  marker_pin="${carried_remainder#*:}"
  expected_identity="$(pinned_identity "${marker_pin}")"
  if ! reported_identity="$(tr -d '[:space:]' <"${policy_root}/${marker_path}" 2>/dev/null)"; then
    reported_identity=""
  fi
  if [[ "${reported_identity}" != "${expected_identity}" ]]; then
    echo "The ${carried_tool} carrier marker ${marker_path} reports ${reported_identity:-no version}, and ${marker_pin} pins ${expected_identity}." >&2
    echo "Run ./tools/install-policy-tools.sh before installing a release." >&2
    exit 1
  fi
done

if ! vulture_metadata_is_exact; then
  echo "The Vulture carrier at ${python_tool_dir} has missing or stale Vulture metadata." >&2
  echo "Run ./tools/install-policy-tools.sh before installing a release." >&2
  exit 1
fi

if ! astroid_metadata_is_exact || [[ ! -f "${policy_root}/${python_tool_dir}/astroid-source.tar.gz" ]]; then
  echo "The Astroid carrier omits its exact metadata or corresponding source." >&2
  exit 1
fi
if ! packaging_metadata_is_exact; then
  echo "The packaging carrier at ${python_tool_dir} has missing or stale packaging metadata." >&2
  echo "Run ./tools/install-policy-tools.sh before installing a release." >&2
  exit 1
fi

for unshipped_python_tool in \
  "${python_tool_dir}/bin/pip" "${python_tool_dir}/bin/pip3" "${python_tool_dir}/bin/pip3.12" \
  "${python_tool_dir}/Scripts/pip.exe" "${python_tool_dir}/Scripts/pip3.exe" "${python_tool_dir}/Scripts/pip3.12.exe" \
  "${python_tool_dir}/lib/python3.12/ensurepip" "${python_tool_dir}/Lib/ensurepip" \
  "${python_tool_dir}/lib/python3.12/site-packages/pip" "${python_tool_dir}/Lib/site-packages/pip"; do
  if [[ -e "${policy_root}/${unshipped_python_tool}" ]]; then
    echo "The CPython carrier contains the ungoverned installer ${unshipped_python_tool}." >&2
    echo "Run ./tools/install-python.sh before installing a release." >&2
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




staged_tools="${staging}/.tools"
staged_javascript="${staged_tools}/javascript"
mkdir -p "${staged_javascript}"
cp -RPp "${javascript_runtime_dir}" "${staged_javascript}/${javascript_platform_tag}"
cp -RPp "${javascript_bundle_dir}" "${staged_javascript}/bundle"
for relative in "${policy_tools[@]}"; do
  mkdir -p "$(dirname "${staging}/${relative}")"
  cp -RPp "${policy_root}/${relative}" "${staging}/${relative}"
done










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
if [[ -n "${publication_dir}" ]]; then
  release_archive="${scratch}/release.zip"
  "${staging}/bin/code-polishy" --policy-root "${staging}" release-manifest archive \
    --root "${staging}" --output "${release_archive}"
  "${staging}/bin/code-polishy" --policy-root "${staging}" release-manifest publish \
    --archive "${release_archive}" --destination "${publication_dir}"
fi
if [[ "${build_only}" == true ]]; then
  echo "Built the portable Code Polishy publication at ${publication_dir}."
  exit 0
fi
release_id="${code_polishy_version}-${release_digest}"
destination="${staging_root}/${release_id}"

installed_message="Installed Code Polishy ${code_polishy_version} at ${destination}."
if [[ -e "${destination}" ]]; then
  if "${release_manifest}" verify "${destination}" >/dev/null 2>&1; then
    installed_message="Code Polishy ${code_polishy_version} is already installed at ${destination}."
  else



    rm -rf "${superseded}"
    mv "${destination}" "${superseded}"
    mv "${staging}" "${destination}"
    rm -rf "${superseded}"
  fi
else
  mv "${staging}" "${destination}"
fi









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
