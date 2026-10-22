#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
staticcheck_version="$(tr -d '[:space:]' <"${repo_root}/tools/staticcheck-version.txt")"
govulncheck_version="$(tr -d '[:space:]' <"${repo_root}/tools/govulncheck-version.txt")"
deadcode_version="$(tr -d '[:space:]' <"${repo_root}/tools/deadcode-version.txt")"
trivy_version="$(tr -d '[:space:]' <"${repo_root}/tools/trivy-version.txt")"
trivy_module_sum="$(tr -d '[:space:]' <"${repo_root}/tools/trivy_module_sum.txt")"
go_version="$(tr -d '[:space:]' <"${repo_root}/scripts/go_version.txt")"

mkdir -p "${repo_root}/.tools/bin"
export GOBIN="${repo_root}/.tools/bin"
"${repo_root}/scripts/go.sh" install "honnef.co/go/tools/cmd/staticcheck@${staticcheck_version}"
"${repo_root}/scripts/go.sh" install "golang.org/x/vuln/cmd/govulncheck@${govulncheck_version}"
"${repo_root}/scripts/go.sh" install "golang.org/x/tools/cmd/deadcode@${deadcode_version}"
unset GOBIN

case "$(uname -m)" in
  arm64|aarch64) trivy_arch="arm64" ;;
  x86_64|amd64) trivy_arch="amd64" ;;
  *) echo "Unsupported architecture for the carried Trivy scanner: $(uname -m)" >&2; exit 1 ;;
esac
trivy_dir="${repo_root}/.tools/trivy/linux-${trivy_arch}"
trivy_expected="go${go_version#go}-X:jsonv2 github.com/aquasecurity/trivy v${trivy_version} ${trivy_module_sum}"

trivy_identity() {
  "${repo_root}/scripts/go.sh" version -m "$1" 2>/dev/null |
    awk 'NR == 1 { identity = $NF } $1 == "mod" { identity = identity " " $2 " " $3 " " $4 } END { print identity }'
}

if [[ -f "${trivy_dir}/trivy" && -f "${trivy_dir}/LICENSE" && "$(trivy_identity "${trivy_dir}/trivy")" == "${trivy_expected}" ]]; then
  echo "Trivy ${trivy_version} for linux/${trivy_arch} is already built at ${trivy_dir}."
else
  trivy_scratch="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-trivy.XXXXXX")"
  trap 'rm -rf "${trivy_scratch}"' EXIT
  echo "Building Trivy ${trivy_version} for linux/${trivy_arch} from its Go module..."
  env -u GONOSUMDB -u GONOSUMCHECK -u GOPRIVATE -u GONOPROXY -u GOINSECURE -u GOWORK \
    GOENV=off GOPATH="${trivy_scratch}/gopath" GOMODCACHE="${trivy_scratch}/gopath/pkg/mod" GOCACHE="${trivy_scratch}/gocache" \
    GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org GOFLAGS=-modcacherw \
    GOOS=linux GOARCH="${trivy_arch}" CGO_ENABLED=0 GOEXPERIMENT=jsonv2 \
    "${repo_root}/scripts/go.sh" install -trimpath \
    -ldflags="-s -w -extldflags '-static' -X github.com/aquasecurity/trivy/pkg/version/app.ver=${trivy_version}" \
    "github.com/aquasecurity/trivy/cmd/trivy@v${trivy_version}"
  built="${trivy_scratch}/gopath/bin/linux_${trivy_arch}/trivy"
  [[ -f "${built}" ]] || built="${trivy_scratch}/gopath/bin/trivy"
  if [[ "$(trivy_identity "${built}")" != "${trivy_expected}" ]]; then
    echo "The built Trivy does not match ${trivy_expected}." >&2
    exit 1
  fi
  mkdir -p "${trivy_scratch}/stage"
  cp "${built}" "${trivy_scratch}/stage/trivy"
  cp "${trivy_scratch}/gopath/pkg/mod/github.com/aquasecurity/trivy@v${trivy_version}/LICENSE" "${trivy_scratch}/stage/LICENSE"
  chmod 0755 "${trivy_scratch}/stage/trivy"
  chmod 0644 "${trivy_scratch}/stage/LICENSE"
  mkdir -p "$(dirname "${trivy_dir}")"
  rm -rf "${trivy_dir}"
  mv "${trivy_scratch}/stage" "${trivy_dir}"
fi

"${repo_root}/.tools/bin/staticcheck" -version
"${repo_root}/.tools/bin/govulncheck" -version
"${repo_root}/scripts/go.sh" version -m "${repo_root}/.tools/bin/deadcode"
echo "trivy $(trivy_identity "${trivy_dir}/trivy")"
