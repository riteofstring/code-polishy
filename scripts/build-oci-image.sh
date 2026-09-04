#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
caller_root="$(pwd -P)"
publication_dir=""
image_ref=""
output=""
push=false

usage() {
  echo "usage: build-oci-image.sh --publication-dir DIR --image REF (--push | --output OCI-ARCHIVE)" >&2
  exit 2
}

absolute_path() {
  if [[ "$1" == /* ]]; then
    printf '%s\n' "$1"
  else
    printf '%s/%s\n' "${caller_root}" "$1"
  fi
}

while (($#)); do
  case "$1" in
    --publication-dir)
      if (($# < 2)); then usage; fi
      publication_dir="$(absolute_path "$2")"
      shift 2
      ;;
    --publication-dir=*)
      publication_dir="$(absolute_path "${1#*=}")"
      shift
      ;;
    --image)
      if (($# < 2)); then usage; fi
      image_ref="$2"
      shift 2
      ;;
    --image=*)
      image_ref="${1#*=}"
      shift
      ;;
    --output)
      if (($# < 2)); then usage; fi
      output="$(absolute_path "$2")"
      shift 2
      ;;
    --output=*)
      output="$(absolute_path "${1#*=}")"
      shift
      ;;
    --push)
      push=true
      shift
      ;;
    *) usage ;;
  esac
done

if [[ "$(uname -s)" != "Linux" || -z "${publication_dir}" || -z "${image_ref}" ]]; then
  usage
fi
if [[ "${image_ref}" == *[[:space:]]* || "${image_ref}" == *@* ]]; then
  echo "The OCI build image reference must be an unpinned discovery tag; consumers use the reported digest." >&2
  exit 2
fi
if [[ "${push}" == true && -n "${output}" ]] || [[ "${push}" != true && -z "${output}" ]]; then
  usage
fi
if [[ -n "${output}" && -e "${output}" ]]; then
  echo "The OCI archive output already exists: ${output}" >&2
  exit 1
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "docker with buildx is required to build the OCI image." >&2
  exit 1
fi

shopt -s nullglob
descriptors=("${publication_dir}"/*.release.json)
shopt -u nullglob
if ((${#descriptors[@]} != 1)); then
  echo "The publication directory must contain exactly one release descriptor." >&2
  exit 1
fi

engine="${policy_root}/.tools/bin/code-polishy"
if [[ ! -x "${engine}" ]]; then
  "${policy_root}/scripts/build.sh"
fi

scratch="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-oci-build.XXXXXX")"
cleanup() {
  rm -rf "${scratch}"
}
trap cleanup EXIT INT TERM HUP
context="${scratch}/context"
"${engine}" release-manifest oci-context \
  --descriptor "${descriptors[0]}" \
  --template "${policy_root}/release/oci.Containerfile.template" \
  --destination "${context}"

version=""
source_revision=""
release_digest=""
bundle_sha256=""
platform=""
while IFS='=' read -r name value; do
  case "${name}" in
    CODE_POLISHY_VERSION) version="${value}" ;;
    CODE_POLISHY_SOURCE_REVISION) source_revision="${value}" ;;
    CODE_POLISHY_RELEASE_DIGEST) release_digest="${value}" ;;
    CODE_POLISHY_BUNDLE_SHA256) bundle_sha256="${value}" ;;
    CODE_POLISHY_PLATFORM) platform="${value}" ;;
    *) echo "The OCI context returned an unknown build argument: ${name}" >&2; exit 1 ;;
  esac
done <"${context}/build-args.env"
if [[ -z "${version}" || ! "${source_revision}" =~ ^[0-9a-f]{40}$ || ! "${release_digest}" =~ ^[0-9a-f]{64}$ ||
  ! "${bundle_sha256}" =~ ^[0-9a-f]{64}$ || ! "${platform}" =~ ^linux/(amd64|arm64)$ ]]; then
  echo "The OCI context returned incomplete build identity." >&2
  exit 1
fi

arguments=(
  buildx build
  --platform "${platform}"
  --file "${context}/Containerfile"
  --build-arg "CODE_POLISHY_VERSION=${version}"
  --build-arg "CODE_POLISHY_SOURCE_REVISION=${source_revision}"
  --build-arg "CODE_POLISHY_RELEASE_DIGEST=${release_digest}"
  --build-arg "CODE_POLISHY_BUNDLE_SHA256=${bundle_sha256}"
  --provenance=mode=max
  --sbom=true
  --tag "${image_ref}"
)
if [[ "${push}" == true ]]; then
  arguments+=(--push)
else
  mkdir -p "$(dirname "${output}")"
  arguments+=(--output "type=oci,dest=${output}")
fi
arguments+=("${context}")
docker "${arguments[@]}"

if [[ "${push}" == true ]]; then
  image_digest="$(docker buildx imagetools inspect "${image_ref}" --format '{{.Manifest.Digest}}')"
  if [[ ! "${image_digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    echo "The registry returned no exact OCI digest for ${image_ref}." >&2
    exit 1
  fi
  echo "image=${image_ref}@${image_digest}"
else
  echo "ociArchive=${output}"
  echo "ociArchiveSHA256=$(sha256sum "${output}" | awk '{print $1}')"
fi
