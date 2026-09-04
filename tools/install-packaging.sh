#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python_release="$(tr -d '[:space:]' <"${policy_root}/tools/python-version.txt")"
packaging_version="$(tr -d '[:space:]' <"${policy_root}/tools/packaging-version.txt")"

if [[ ! "${python_release}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\+[0-9]{8}$ ]]; then
  echo "tools/python-version.txt must pin CPython and a python-build-standalone tag." >&2
  exit 1
fi
if [[ ! "${packaging_version}" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
  echo "tools/packaging-version.txt must pin a packaging release." >&2
  exit 1
fi
python_version="${python_release%%+*}"

case "$(uname -s)" in
  Darwin) os_tag="darwin" ;;
  Linux) os_tag="linux" ;;
  *) echo "Unsupported OS for policy-owned packaging: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch_tag="arm64" ;;
  x86_64|amd64) arch_tag="x64" ;;
  *) echo "Unsupported architecture for policy-owned packaging: $(uname -m)" >&2; exit 1 ;;
esac

runtime_root="${policy_root}/.tools/python/${os_tag}-${arch_tag}"
python_bin="${runtime_root}/python"
python_marker="${runtime_root}/.code-polishy-python-release"
packaging_marker="${runtime_root}/.code-polishy-packaging-release"
wheel="packaging-${packaging_version}-py3-none-any.whl"
checksum_inventory="${policy_root}/tools/packaging_wheel_checksums.txt"
verify_sha256="${policy_root}/tools/verify-sha256.sh"

if [[ ! -x "${python_bin}" ]]; then
  echo "The policy-owned CPython runtime is unavailable at ${python_bin}." >&2
  echo "Run ./tools/install-python.sh first." >&2
  exit 1
fi
if [[ "$("${python_bin}" -I -B -c 'import sys; print(".".join(str(value) for value in sys.version_info[:3]))' 2>/dev/null)" != "${python_version}" ]]; then
  echo "The policy-owned CPython runtime does not report ${python_version}." >&2
  exit 1
fi
if [[ ! -f "${python_marker}" ]] || [[ "$(tr -d '[:space:]' <"${python_marker}")" != "${python_release}" ]]; then
  echo "The policy-owned CPython runtime does not record ${python_release}." >&2
  echo "Run ./tools/install-python.sh first." >&2
  exit 1
fi

packaging_reported_version() {
  "$1" -I -B -c 'import importlib.metadata; print(importlib.metadata.version("packaging"))'
}

site_packages="$("${python_bin}" -I -B -c 'import sysconfig; print(sysconfig.get_paths()["purelib"])')"
case "${site_packages}" in
  "${runtime_root}"/*) ;;
  *) echo "The policy-owned CPython runtime names an external site-packages directory." >&2; exit 1 ;;
esac

packaging_metadata_is_exact() {
  local metadata count=0 exact=""
  shopt -s nullglob
  for metadata in "${site_packages}"/packaging-*.dist-info; do
    count=$((count + 1))
    exact="${metadata}"
  done
  shopt -u nullglob
  [[ "${count}" -eq 1 ]] && [[ "${exact}" == "${site_packages}/packaging-${packaging_version}.dist-info" ]]
}

if [[ -f "${packaging_marker}" ]] && [[ "$(tr -d '[:space:]' <"${packaging_marker}")" == "${packaging_version}" ]] &&
  packaging_metadata_is_exact && [[ "$(packaging_reported_version "${python_bin}" 2>/dev/null || true)" == "${packaging_version}" ]]; then
  echo "packaging ${packaging_version} is already installed in ${runtime_root}."
  exit 0
fi

for required_tool in awk curl; do
  if ! command -v "${required_tool}" >/dev/null 2>&1; then
    echo "${required_tool} is required to install policy-owned packaging." >&2
    exit 1
  fi
done

temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-packaging.XXXXXX")"
staging="${runtime_root}/.packaging-staging-$$"
backup="${runtime_root}/.packaging-backup-$$"
replacing=0
new_package_installed=0
new_metadata_installed=0
new_marker_installed=0

restore_previous_packaging() {
  local path restoration_failed=0
  [[ "${replacing}" -eq 1 ]] || return 0
  if [[ "${new_package_installed}" -eq 1 ]]; then
    rm -rf "${site_packages}/packaging" || restoration_failed=1
  fi
  if [[ "${new_metadata_installed}" -eq 1 ]]; then
    rm -rf "${site_packages}/packaging-${packaging_version}.dist-info" || restoration_failed=1
  fi
  if [[ "${new_marker_installed}" -eq 1 ]]; then
    rm -f "${packaging_marker}" || restoration_failed=1
  fi
  shopt -s nullglob
  if [[ -d "${backup}/packaging" ]]; then
    mv "${backup}/packaging" "${site_packages}/packaging" || restoration_failed=1
  fi
  for path in "${backup}"/packaging-*.dist-info; do
    mv "${path}" "${site_packages}/" || restoration_failed=1
  done
  shopt -u nullglob
  if [[ -f "${backup}/marker" ]]; then
    mv "${backup}/marker" "${packaging_marker}" || restoration_failed=1
  fi
  if [[ "${restoration_failed}" -eq 0 ]]; then
    replacing=0
  fi
  return "${restoration_failed}"
}

cleanup() {
  local status=$? cleanup_failed=0
  set +e
  if ! restore_previous_packaging; then
    echo "The previous packaging distribution could not be restored." >&2
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
url="https://files.pythonhosted.org/packages/63/34/ba1c580383c9eada3711951fef0795c80b829a078d72188184bcab9dd527/${wheel}"
echo "Downloading packaging ${packaging_version}..."
curl -fsSL "${url}" -o "${wheel_path}"
"${verify_sha256}" "${checksum_inventory}" "${wheel}" "${wheel_path}"

mkdir -p "${staging}"
"${python_bin}" -I -B -c '
import pathlib
import shutil
import sys
import zipfile

wheel = pathlib.Path(sys.argv[1])
destination = pathlib.Path(sys.argv[2])
version = sys.argv[3]
prefixes = ("packaging/", f"packaging-{version}.dist-info/")
with zipfile.ZipFile(wheel) as archive:
    for entry in archive.infolist():
        name = entry.filename
        if not name.startswith(prefixes):
            continue
        relative = pathlib.PurePosixPath(name)
        if relative.is_absolute() or ".." in relative.parts:
            raise SystemExit("packaging wheel contains an unsafe path")
        target = destination.joinpath(*relative.parts)
        if name.endswith("/"):
            target.mkdir(parents=True, exist_ok=True)
            continue
        target.parent.mkdir(parents=True, exist_ok=True)
        with archive.open(entry) as source, target.open("wb") as output:
            shutil.copyfileobj(source, output)
' "${wheel_path}" "${staging}" "${packaging_version}"

if [[ ! -d "${staging}/packaging" ]] || [[ ! -d "${staging}/packaging-${packaging_version}.dist-info" ]]; then
  echo "The packaging wheel did not contain the expected distribution." >&2
  exit 1
fi
printf '%s\n' "${packaging_version}" >"${staging}/marker"
mkdir -p "${site_packages}" "${backup}"
replacing=1
if [[ -d "${site_packages}/packaging" ]]; then
  mv "${site_packages}/packaging" "${backup}/packaging"
fi
shopt -s nullglob
for metadata in "${site_packages}"/packaging-*.dist-info; do
  mv "${metadata}" "${backup}/"
done
shopt -u nullglob
if [[ -f "${packaging_marker}" ]]; then
  mv "${packaging_marker}" "${backup}/marker"
fi
mv "${staging}/packaging" "${site_packages}/packaging"
new_package_installed=1
mv "${staging}/packaging-${packaging_version}.dist-info" "${site_packages}/packaging-${packaging_version}.dist-info"
new_metadata_installed=1
if ! packaging_metadata_is_exact || [[ "$(packaging_reported_version "${python_bin}" 2>/dev/null || true)" != "${packaging_version}" ]]; then
  echo "The installed packaging distribution does not report ${packaging_version}." >&2
  exit 1
fi
mv "${staging}/marker" "${packaging_marker}"
new_marker_installed=1
replacing=0
rm -rf "${backup}"
echo "Installed packaging ${packaging_version} in ${runtime_root}."
