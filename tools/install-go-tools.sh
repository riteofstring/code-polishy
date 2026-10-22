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
trivy_expected+=" CGO_ENABLED=0 GOARCH=${trivy_arch} GOOS=linux -trimpath=true"
trivy_notice_layout="code-polishy-trivy-notices-v2"

trivy_identity() {
  "${repo_root}/scripts/go.sh" version -m "$1" 2>/dev/null |
    awk 'NR == 1 { go = $NF }
      $1 == "mod" { module = $2 " " $3 " " $4 }
      $1 == "build" && $2 ~ /^(CGO_ENABLED|GOARCH|GOOS|-trimpath)=/ { split($2, setting, "="); value[setting[1]] = $2 }
      END { print go " " module " " value["CGO_ENABLED"] " " value["GOARCH"] " " value["GOOS"] " " value["-trimpath"] }'
}

trivy_notice_candidates() {
  awk -v root="$1/" '{
    relative = substr($0, length(root) + 1)
    count = split(tolower(relative), part, "/")
    name = part[count]
    extension = ""
    if (match(name, /\.[^.]*$/)) extension = substr(name, RSTART)
    licensed_directory = 0
    for (index_ = 1; index_ < count; index_++) if (part[index_] == "licenses") licensed_directory = 1
    source = extension ~ /^\.(go|s|c|h|cc|cpp|proto|py|js|ts|sh|rego|ya?ml|json|xml|toml|mod|sum|pom|gemspec|jar|golden)$/
    if (licensed_directory || (name ~ /licen[cs]e|copying|notice|patents/ && !source)) {
      root_license = count == 1 && name ~ /licen[cs]e|copying/ && !source
      print (root_license ? "root" : "notice") "\t" relative
    }
  }'
}

trivy_notices() {
  local binary="$1" module_cache="$2" carrier="$3" build_info modules module version escaped source files candidates kind relative licensed
  if ! build_info="$("${repo_root}/scripts/go.sh" version -m "${binary}")"; then
    echo "The built Trivy has no readable build information." >&2
    return 1
  fi
  if awk '$1 == "=>" { found = 1 } END { exit !found }' <<<"${build_info}"; then
    echo "The built Trivy replaces a module, so its notices cannot be attributed." >&2
    return 1
  fi
  if ! awk '$1 == "mod" && $2 == "github.com/aquasecurity/trivy" { found = 1 } END { exit !found }' <<<"${build_info}"; then
    echo "The built Trivy build information names no Trivy module." >&2
    return 1
  fi
  modules="$(awk '$1 == "dep" || $1 == "mod" { print $2, $3 }' <<<"${build_info}")"
  while read -r module version; do
    if [[ -z "${module}" || -z "${version}" ]]; then
      echo "The built Trivy build information has a malformed module record." >&2
      return 1
    fi
    escaped="$(printf '%s\n' "${module}" | awk '{ out = ""; for (i = 1; i <= length($0); i++) { c = substr($0, i, 1); out = out (c ~ /[A-Z]/ ? "!" tolower(c) : c) } print out }')"
    source="${module_cache}/${escaped}@${version}"
    if [[ ! -d "${source}" ]] || ! files="$(find "${source}" -type f -print)"; then
      echo "Trivy module ${module}@${version} has no readable source in the module cache." >&2
      return 1
    fi
    candidates="$(trivy_notice_candidates "${source}" <<<"${files}")"
    licensed=false
    while IFS=$'\t' read -r kind relative; do
      [[ -n "${relative}" ]] || continue
      [[ "${kind}" != root ]] || licensed=true
      mkdir -p "$(dirname "${carrier}/licenses/${escaped}@${version}/${relative}")"
      cp "${source}/${relative}" "${carrier}/licenses/${escaped}@${version}/${relative}"
      chmod 0644 "${carrier}/licenses/${escaped}@${version}/${relative}"
    done <<<"${candidates}"
    if [[ "${licensed}" != true ]]; then
      echo "Trivy module ${module}@${version} carries no license file at its root." >&2
      return 1
    fi
  done <<<"${modules}"
  printf '%s\n' "${trivy_notice_layout}" >"${carrier}/notices-layout"
}

trivy_carrier_complete() {
  [[ -f "$1/trivy" && -d "$1/licenses" && "$(trivy_identity "$1/trivy")" == "${trivy_expected}" &&
    "$(cat "$1/notices-layout" 2>/dev/null || true)" == "${trivy_notice_layout}" ]]
}

if trivy_carrier_complete "${trivy_dir}"; then
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
  chmod 0755 "${trivy_scratch}/stage/trivy"
  trivy_notices "${built}" "${trivy_scratch}/gopath/pkg/mod" "${trivy_scratch}/stage"
  mkdir -p "$(dirname "${trivy_dir}")"
  rm -rf "${trivy_dir}"
  mv "${trivy_scratch}/stage" "${trivy_dir}"
fi

"${repo_root}/.tools/bin/staticcheck" -version
"${repo_root}/.tools/bin/govulncheck" -version
"${repo_root}/scripts/go.sh" version -m "${repo_root}/.tools/bin/deadcode"
echo "trivy $(trivy_identity "${trivy_dir}/trivy")"
