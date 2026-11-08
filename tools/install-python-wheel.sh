#!/usr/bin/env bash
set -euo pipefail

package_name="$1"
package_label="$2"
download_prefix="$3"
python_flags=(-I)
if [[ "${4:-}" == "-B" ]]; then
  python_flags+=(-B)
fi
if [[ ! "${package_name}" =~ ^[a-z][a-z0-9_]*$ ]]; then
  echo "Invalid Python wheel package name." >&2
  exit 1
fi

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python_release="$(tr -d '[:space:]' <"${policy_root}/tools/python-version.txt")"
package_version="$(tr -d '[:space:]' <"${policy_root}/tools/${package_name}-version.txt")"

if [[ ! "${python_release}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\+[0-9]{8}$ ]]; then
  echo "tools/python-version.txt must pin CPython and a python-build-standalone tag." >&2
  exit 1
fi
if [[ ! "${package_version}" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
  echo "tools/${package_name}-version.txt must pin a ${package_name} release." >&2
  exit 1
fi
python_version="${python_release%%+*}"

case "$(uname -s)" in
  Darwin) os_tag="darwin" ;;
  Linux) os_tag="linux" ;;
  *) echo "Unsupported OS for policy-owned ${package_name}: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch_tag="arm64" ;;
  x86_64|amd64) arch_tag="x64" ;;
  *) echo "Unsupported architecture for policy-owned ${package_name}: $(uname -m)" >&2; exit 1 ;;
esac

runtime_root="${policy_root}/.tools/python/${os_tag}-${arch_tag}"
python_bin="${runtime_root}/python"
python_marker="${runtime_root}/.code-polishy-python-release"
package_marker="${runtime_root}/.code-polishy-${package_name}-release"
wheel="${package_name}-${package_version}-py3-none-any.whl"
checksum_inventory="${policy_root}/tools/${package_name}_wheel_checksums.txt"
verify_sha256="${policy_root}/tools/verify-sha256.sh"

if [[ ! -x "${python_bin}" ]]; then
  echo "The policy-owned CPython runtime is unavailable at ${python_bin}." >&2
  echo "Run ./tools/install-python.sh first." >&2
  exit 1
fi
if [[ "$("${python_bin}" "${python_flags[@]}" -c 'import sys; print(".".join(str(value) for value in sys.version_info[:3]))' 2>/dev/null)" != "${python_version}" ]]; then
  echo "The policy-owned CPython runtime does not report ${python_version}." >&2
  exit 1
fi
if [[ ! -f "${python_marker}" ]] || [[ "$(tr -d '[:space:]' <"${python_marker}")" != "${python_release}" ]]; then
  echo "The policy-owned CPython runtime does not record ${python_release}." >&2
  echo "Run ./tools/install-python.sh first." >&2
  exit 1
fi

package_reported_version() {
  "$1" "${python_flags[@]}" -c 'import importlib.metadata, sys; print(importlib.metadata.version(sys.argv[1]))' "${package_name}"
}

site_packages="$("${python_bin}" "${python_flags[@]}" -c 'import sysconfig; print(sysconfig.get_paths()["purelib"])')"
case "${site_packages}" in
  "${runtime_root}"/*) ;;
  *) echo "The policy-owned CPython runtime names an external site-packages directory." >&2; exit 1 ;;
esac

package_metadata_is_exact() {
  local metadata count=0 exact=""
  shopt -s nullglob
  for metadata in "${site_packages}/${package_name}"-*.dist-info; do
    count=$((count + 1))
    exact="${metadata}"
  done
  shopt -u nullglob
  [[ "${count}" -eq 1 ]] && [[ "${exact}" == "${site_packages}/${package_name}-${package_version}.dist-info" ]]
}

if [[ -f "${package_marker}" ]] && [[ "$(tr -d '[:space:]' <"${package_marker}")" == "${package_version}" ]] &&
  package_metadata_is_exact && [[ "$(package_reported_version "${python_bin}" 2>/dev/null || true)" == "${package_version}" ]]; then
  echo "${package_label} ${package_version} is already installed in ${runtime_root}."
  exit 0
fi

for required_tool in awk curl; do
  if ! command -v "${required_tool}" >/dev/null 2>&1; then
    echo "${required_tool} is required to install policy-owned ${package_name}." >&2
    exit 1
  fi
done

temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-${package_name}.XXXXXX")"
staging="${runtime_root}/.${package_name}-staging-$$"
backup="${runtime_root}/.${package_name}-backup-$$"
replacing=0
new_package_installed=0
new_metadata_installed=0
new_marker_installed=0

restore_previous_package() {
  local path restoration_failed=0
  [[ "${replacing}" -eq 1 ]] || return 0
  if [[ "${new_package_installed}" -eq 1 ]]; then
    rm -rf "${site_packages:?}/${package_name:?}" || restoration_failed=1
  fi
  if [[ "${new_metadata_installed}" -eq 1 ]]; then
    rm -rf "${site_packages}/${package_name}-${package_version}.dist-info" || restoration_failed=1
  fi
  if [[ "${new_marker_installed}" -eq 1 ]]; then
    rm -f "${package_marker}" || restoration_failed=1
  fi
  shopt -s nullglob
  if [[ -d "${backup}/${package_name}" ]]; then
    mv "${backup}/${package_name}" "${site_packages}/${package_name}" || restoration_failed=1
  fi
  for path in "${backup}/${package_name}"-*.dist-info; do
    mv "${path}" "${site_packages}/" || restoration_failed=1
  done
  shopt -u nullglob
  if [[ -f "${backup}/marker" ]]; then
    mv "${backup}/marker" "${package_marker}" || restoration_failed=1
  fi
  if [[ "${restoration_failed}" -eq 0 ]]; then
    replacing=0
  fi
  return "${restoration_failed}"
}

cleanup() {
  local status=$? cleanup_failed=0
  set +e
  if ! restore_previous_package; then
    echo "The previous ${package_name} distribution could not be restored." >&2
    cleanup_failed=1
  fi
  rm -rf "${temporary_dir}" "${staging}"
  if [[ "${replacing}" -eq 0 ]]; then
    rm -rf "${backup}"
  fi
  if [[ "${cleanup_failed}" -eq 1 ]]; then
    status=1
  fi
  exit "${status}"
}
trap cleanup EXIT

wheel_path="${temporary_dir}/${wheel}"
url="${download_prefix}/${wheel}"
echo "Downloading ${package_label} ${package_version}..."
curl -fsSL "${url}" -o "${wheel_path}"
"${verify_sha256}" "${checksum_inventory}" "${wheel}" "${wheel_path}"

mkdir -p "${staging}"
"${python_bin}" "${python_flags[@]}" -c '
import pathlib
import shutil
import sys
import zipfile

wheel = pathlib.Path(sys.argv[1])
destination = pathlib.Path(sys.argv[2])
version = sys.argv[3]
package = sys.argv[4]
prefixes = (f"{package}/", f"{package}-{version}.dist-info/")
with zipfile.ZipFile(wheel) as archive:
    for entry in archive.infolist():
        name = entry.filename
        if not name.startswith(prefixes):
            continue
        relative = pathlib.PurePosixPath(name)
        if relative.is_absolute() or ".." in relative.parts:
            raise SystemExit(f"{package} wheel contains an unsafe path")
        target = destination.joinpath(*relative.parts)
        if name.endswith("/"):
            target.mkdir(parents=True, exist_ok=True)
            continue
        target.parent.mkdir(parents=True, exist_ok=True)
        with archive.open(entry) as source, target.open("wb") as output:
            shutil.copyfileobj(source, output)
' "${wheel_path}" "${staging}" "${package_version}" "${package_name}"

if [[ ! -d "${staging}/${package_name}" ]] || [[ ! -d "${staging}/${package_name}-${package_version}.dist-info" ]]; then
  echo "The ${package_name} wheel did not contain the expected distribution." >&2
  exit 1
fi
printf '%s\n' "${package_version}" >"${staging}/marker"
mkdir -p "${site_packages}" "${backup}"
replacing=1
if [[ -d "${site_packages}/${package_name}" ]]; then
  mv "${site_packages}/${package_name}" "${backup}/${package_name}"
fi
shopt -s nullglob
for metadata in "${site_packages}/${package_name}"-*.dist-info; do
  mv "${metadata}" "${backup}/"
done
shopt -u nullglob
if [[ -f "${package_marker}" ]]; then
  mv "${package_marker}" "${backup}/marker"
fi
mv "${staging}/${package_name}" "${site_packages}/${package_name}"
new_package_installed=1
mv "${staging}/${package_name}-${package_version}.dist-info" "${site_packages}/${package_name}-${package_version}.dist-info"
new_metadata_installed=1
if ! package_metadata_is_exact || [[ "$(package_reported_version "${python_bin}" 2>/dev/null || true)" != "${package_version}" ]]; then
  echo "The installed ${package_name} distribution does not report ${package_version}." >&2
  exit 1
fi
mv "${staging}/marker" "${package_marker}"
new_marker_installed=1
replacing=0
rm -rf "${backup}"
echo "Installed ${package_label} ${package_version} in ${runtime_root}."
