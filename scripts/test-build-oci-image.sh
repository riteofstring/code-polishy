#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-oci-contract.XXXXXX")"
cleanup() {
  rm -rf "${scratch}"
}
trap cleanup EXIT INT TERM HUP

fixture_root="${scratch}/fixture"
publication="${scratch}/publication"
shim_bin="${scratch}/bin"
docker_log="${scratch}/docker.log"
mkdir -p "${fixture_root}/scripts" "${fixture_root}/release" "${fixture_root}/.tools/bin" "${publication}" "${shim_bin}"
cp "${policy_root}/scripts/build-oci-image.sh" "${fixture_root}/scripts/build-oci-image.sh"
cp "${policy_root}/release/oci.Containerfile.template" "${fixture_root}/release/oci.Containerfile.template"
printf '{}\n' >"${publication}/fixture.release.json"

cat >"${fixture_root}/.tools/bin/code-polishy" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
destination=""
while (($#)); do
  case "$1" in
    --destination) destination="$2"; shift 2 ;;
    *) shift ;;
  esac
done
mkdir -p "${destination}"
printf 'FROM scratch\n' >"${destination}/Containerfile"
cat >"${destination}/build-args.env" <<'ARGS'
CODE_POLISHY_VERSION=9.9.9
CODE_POLISHY_SOURCE_REVISION=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
CODE_POLISHY_RELEASE_DIGEST=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
CODE_POLISHY_BUNDLE_SHA256=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
CODE_POLISHY_PLATFORM=linux/amd64
ARGS
EOF
chmod +x "${fixture_root}/.tools/bin/code-polishy"

cat >"${shim_bin}/uname" <<'EOF'
#!/usr/bin/env bash
if [[ "${1:-}" == "-s" ]]; then
  printf 'Linux\n'
  exit 0
fi
exec /usr/bin/uname "$@"
EOF
chmod +x "${shim_bin}/uname"

raw_manifest='{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}'
cat >"${shim_bin}/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%q ' "$@" >>"${DOCKER_LOG}"
printf '\n' >>"${DOCKER_LOG}"
case "${1:-}" in
  buildx)
    if [[ "${2:-}" == "build" ]]; then
      exit 0
    fi
    if [[ "${2:-}" == "imagetools" && "${3:-}" == "inspect" && "${5:-}" == "--raw" ]]; then
      printf '%s' "${OCI_RAW_MANIFEST}"
      exit 0
    fi
    ;;
  pull) exit 0 ;;
  run)
    printf 'code-polishy 9.9.9\n'
    exit 0
    ;;
esac
printf 'unexpected docker invocation: %s\n' "$*" >&2
exit 1
EOF
chmod +x "${shim_bin}/docker"

expected_digest="sha256:dff9de10919148711140d349bf03f1a99eb06f94b03e51715ccebfa7cdc518e2"
output="$(
  DOCKER_LOG="${docker_log}" OCI_RAW_MANIFEST="${raw_manifest}" PATH="${shim_bin}:${PATH}" \
    "${fixture_root}/scripts/build-oci-image.sh" \
    --publication-dir "${publication}" \
    --image registry.example/code-polishy:v9.9.9 \
    --push
)"

expected_output="image=registry.example/code-polishy:v9.9.9@${expected_digest}"
if [[ "${output}" != "${expected_output}" ]]; then
  printf 'OCI build output=%q want=%q\n' "${output}" "${expected_output}" >&2
  exit 1
fi
if ! grep -Fq 'buildx build ' "${docker_log}" ||
  ! grep -Fq -- '--provenance=mode=max' "${docker_log}" ||
  ! grep -Fq -- '--sbom=true' "${docker_log}" ||
  ! grep -Fq -- '--push' "${docker_log}"; then
  printf 'OCI build omitted required Buildx arguments\n' >&2
  exit 1
fi
if ! grep -Fq "buildx imagetools inspect registry.example/code-polishy:v9.9.9 --raw" "${docker_log}"; then
  printf 'OCI build did not inspect the raw registry manifest\n' >&2
  exit 1
fi
if ! grep -Fq "pull --platform linux/amd64 registry.example/code-polishy:v9.9.9@${expected_digest}" "${docker_log}"; then
  printf 'OCI build did not pull the digest-pinned image\n' >&2
  exit 1
fi
run_line="$(grep '^run ' "${docker_log}")"
if [[ "${run_line}" != *"--workdir /tmp"* || "${run_line}" != *"--entrypoint /bin/sh"* ||
  "${run_line}" != *"registry.example/code-polishy:v9.9.9@${expected_digest}"* ]]; then
  printf 'OCI build did not exercise the digest-pinned launcher\n' >&2
  exit 1
fi
if [[ "${run_line}" == *"--user"* ]]; then
  printf 'OCI runtime verification overrode the image user\n' >&2
  exit 1
fi

printf 'OCI image contract passed\n'
