#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"


fixture_root="$(cd "$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-install-test.XXXXXX")" && pwd -P)"
cleanup() {
  rm -rf "${fixture_root}"
}
trap cleanup EXIT

source_root="${fixture_root}/checkout"
prefix="${fixture_root}/prefix"
command_root="${fixture_root}/command bin's"
command_link="${command_root}/code-polishy"
path_profile="${fixture_root}/shell-profile"
shim_bin="${fixture_root}/shim"
git_log="${fixture_root}/git-invocations.txt"
gh_marker="${fixture_root}/gh-invoked"
release_list="${fixture_root}/releases.txt"
launcher_binary="${fixture_root}/code-polishy-launcher"
real_git="$(command -v git)"

export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null

fail() {
  echo "install test failure: $1" >&2
  exit 1
}

case "$(uname -s)" in
  Darwin) os_tag="darwin" ;;
  Linux) os_tag="linux" ;;
  *) fail "unsupported test OS $(uname -s)" ;;
esac
case "$(uname -m)" in
  arm64 | aarch64) arch_tag="arm64" ;;
  x86_64 | amd64) arch_tag="x64" ;;
  *) fail "unsupported test architecture $(uname -m)" ;;
esac
platform_tag="${os_tag}-${arch_tag}"

case "${arch_tag}" in
  arm64)
    go_platform_tag="${os_tag}-arm64"
    shellcheck_platform_tag="${os_tag}-aarch64"
    ;;
  x64)
    go_platform_tag="${os_tag}-amd64"
    shellcheck_platform_tag="${os_tag}-x86_64"
    ;;
esac
node_version="24.18.0"
pnpm_version="11.13.0"

write_file() {
  mkdir -p "$(dirname "$1")"
  cat >"$1"
}

copy_file() {
  mkdir -p "$(dirname "$2")"
  cp "$1" "$2"
}

write_stub_tool() {
  write_file "$1" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$1"
}

write_version_tool() {
  local path="$1" probe="$2" reported="$3"
  write_file "${path}" <<EOF
#!/usr/bin/env bash
if [[ "\${1:-}" == "${probe}" ]]; then
  printf '%s\n' "${reported}"
  exit 0
fi
echo "unexpected stub invocation: \$0 \$*" >&2
exit 1
EOF
  chmod +x "${path}"
}

write_python_runtime_tool() {
  local path="$1" python_version="$2" vulture_version="$3" packaging_version="$4"
  write_file "${path}" <<EOF
#!/usr/bin/env bash
if [[ "\${1:-}" == "-I" && "\${2:-}" == "-B" && "\${3:-}" == "-c" ]]; then
  case "\${4:-}" in
    *sys.version_info*) printf '%s\n' "${python_version}" ;;
    *sysconfig.get_paths*) printf '%s\n' "\$(cd "\$(dirname "\$0")" && pwd -P)/lib/python3.12/site-packages" ;;
    *'version("packaging")'*) printf '%s\n' "${packaging_version}" ;;
    *'version("vulture")'*) printf '%s\n' "${vulture_version}" ;;
    *) echo "unexpected Python probe: \$*" >&2; exit 1 ;;
  esac
  exit 0
fi
echo "unexpected Python invocation: \$*" >&2
exit 1
EOF
  chmod +x "${path}"
}


build_source_checkout() {
  local bundle_source_file
  rm -rf "${source_root}"
  mkdir -p "${source_root}"

  copy_file "${policy_root}/scripts/install.sh" "${source_root}/scripts/install.sh"
  copy_file "${policy_root}/scripts/release-manifest.sh" "${source_root}/scripts/release-manifest.sh"
  copy_file "${policy_root}/scripts/release-version.sh" "${source_root}/scripts/release-version.sh"
  copy_file "${policy_root}/tools/javascript-env.sh" "${source_root}/tools/javascript-env.sh"
  copy_file "${policy_root}/tools/javascript-bundle-manifest.sh" \
    "${source_root}/tools/javascript-bundle-manifest.sh"
  copy_file "${policy_root}/tools/javascript/bundle-manifest.mjs" \
    "${source_root}/tools/javascript/bundle-manifest.mjs"
  copy_file "${policy_root}/tools/javascript/source-files.txt" \
    "${source_root}/tools/javascript/source-files.txt"
  chmod +x "${source_root}/scripts/install.sh" "${source_root}/scripts/release-manifest.sh" \
    "${source_root}/scripts/release-version.sh" \
    "${source_root}/tools/javascript-bundle-manifest.sh"

  printf '9.9.9\n' >"${source_root}/VERSION"
  write_file "${source_root}/LICENSE" <<'EOF'
Apache License 2.0 fixture
EOF
  write_file "${source_root}/README.md" <<'EOF'
# Code Polishy fixture

See [installation](docs/installation.md).
EOF
  write_file "${source_root}/CHANGELOG.md" <<'EOF'
# Changelog

The former binary override is retired.
EOF
  write_file "${source_root}/docs/installation.md" <<'EOF'
# Installation

The retired `check_policy.sh` wrapper is not installed.
EOF
  write_file "${source_root}/docs/agent-workflows.md" <<'EOF'
# Agent workflows

Run the installed task-session supervisor.
EOF
  write_file "${source_root}/docs/catalog.json" <<'EOF'
{
  "version": 1,
  "topics": [
    {
      "id": "agent-workflows",
      "path": "docs/agent-workflows.md",
      "title": "Agent workflows",
      "summary": "Run the installed task-session supervisor.",
      "aliases": ["agents"],
      "public": true
    },
    {
      "id": "installation",
      "path": "docs/installation.md",
      "title": "Installation",
      "summary": "Install the native release.",
      "aliases": ["install"],
      "public": true
    }
  ]
}
EOF
  printf '1.26.6\n' >"${source_root}/scripts/go_version.txt"
  printf '%s\n' "${node_version}" >"${source_root}/tools/node-version.txt"
  printf '%s\n' "${pnpm_version}" >"${source_root}/tools/pnpm-version.txt"
  printf '0.11.0\n' >"${source_root}/tools/shellcheck-version.txt"
  printf 'v0.7.0\n' >"${source_root}/tools/staticcheck-version.txt"
  printf 'v1.3.0\n' >"${source_root}/tools/govulncheck-version.txt"
  printf 'v2.4.0\n' >"${source_root}/tools/osv-scanner-version.txt"
  printf '26.3\n' >"${source_root}/tools/packaging-version.txt"
  printf '%s\n' 'packaging-26.3-py3-none-any.whl d7193f7c8e4e93f444fde0262bf90af30e16fa0ad0ad44cb553c87339b23cd1c' \
    >"${source_root}/tools/packaging_wheel_checksums.txt"
  mkdir -p "${source_root}/internal/pythonfacts"
  printf '[project]\nname="fixture"\nversion="9.9.9"\n' >"${source_root}/internal/pythonfacts/pyproject.toml"
  printf 'version = 1\nrevision = 3\n' >"${source_root}/internal/pythonfacts/uv.lock"
  printf '3.12.13+20260728\n' >"${source_root}/tools/python-version.txt"
  printf '%s\n' 'cpython-3.12.13+20260728-x86_64-unknown-linux-gnu-install_only.tar.gz fd9d70e1e1ed3f6caccb4e2eefe570aa07589c8f86ddf0e87f68a96cd14272e1' \
    >"${source_root}/tools/python_runtime_checksums.txt"
  printf '0.16.0\n' >"${source_root}/tools/ruff-version.txt"
  printf '0.0.65\n' >"${source_root}/tools/ty-version.txt"
  printf '2.16\n' >"${source_root}/tools/vulture-version.txt"
  printf '%s\n' 'vulture-2.16-py3-none-any.whl 6e0f1c312cef1c87856957e5c2ca9608834a7c794c2180477f30bf0e4cc58eee' \
    >"${source_root}/tools/vulture_wheel_checksums.txt"
  printf '0.72.0\n' >"${source_root}/tools/trivy-version.txt"
  write_file "${source_root}/tools/ty.toml" <<'EOF'
[rules]
EOF
  printf '# disposable inventory\nexample-tool@1.2.3\tMIT\n' \
    >"${source_root}/tools/javascript_bundle_inventory.txt"
  write_file "${source_root}/.gitignore" <<'EOF'
.tools/
EOF
  write_file "${source_root}/schema/code-polishy.schema.json" <<'EOF'
{ "$schema": "https://json-schema.org/draft/2020-12/schema" }
EOF
  write_file "${source_root}/templates/AGENTS.md" <<'EOF'
# Canonical guidance
EOF
  write_file "${source_root}/templates/CLAUDE.md" <<'EOF'
@AGENTS.md
EOF
  write_file "${source_root}/templates/behavior-review.md" <<'EOF'
# Behavior review instructions
EOF
  write_file "${source_root}/skills/unpackaged-fixture/SKILL.md" <<'EOF'
# Unpackaged fixture
EOF
  write_file "${source_root}/artifact-security/scanner-policy.json" <<'EOF'
{ "scanner": "disposable" }
EOF
  write_file "${source_root}/tools/shellcheck.sh" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "${source_root}/tools/shellcheck.sh"

  write_file "${source_root}/scripts/build.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
output_dir="\${1:?build.sh requires an output directory}"
mkdir -p "\${output_dir}"
printf '#!/usr/bin/env bash\nprintf "code-polishy %s %%s\\\\n" "\$*"\n' \
  "\${CODE_POLISHY_TEST_BUILD_MARK:-first}" >"\${output_dir}/code-polishy"
chmod +x "\${output_dir}/code-polishy"
cp "${launcher_binary}" "\${output_dir}/code-polishy-launcher"
# The staged tree now holds binaries, so an installation that ends here ends
# after staging has begun. These markers end it the two ways it can end: a step
# that fails, and a signal from outside.
if [[ -e "\${CODE_POLISHY_TEST_BUILD_FAILS:-/nonexistent}" ]]; then
  echo "stub build failure" >&2
  exit 1
fi
if [[ -e "\${CODE_POLISHY_TEST_INTERRUPT:-/nonexistent}" ]]; then
  kill -TERM "\${PPID}"
fi
EOF
  chmod +x "${source_root}/scripts/build.sh"


  write_file "${source_root}/tools/javascript/package.json" <<'EOF'
{
  "name": "code-polishy-javascript",
  "private": true,
  "dependencies": {
    "example-tool": "1.2.3"
  }
}
EOF
  for bundle_source_file in pnpm-lock.yaml pnpm-workspace.yaml .npmrc runner.mjs \
    protocol.mjs audit.mjs deadcode.mjs imports.mjs gitlab.mjs licenses.mjs packages.mjs; do
    printf '// disposable %s\n' "${bundle_source_file}" \
      >"${source_root}/tools/javascript/${bundle_source_file}"
  done

  write_file "${source_root}/.tools/javascript/${platform_tag}/node/bin/node" <<EOF
#!/usr/bin/env bash
if [[ "\${1:-}" == "${source_root}/tools/javascript/bundle-manifest.mjs" ]]; then
  exec "${policy_root}/.tools/javascript/${platform_tag}/node/bin/node" "\$@"
fi
if [[ "\${1:-}" == "--version" ]]; then
  echo "v${node_version}"
  exit 0
fi
if [[ "\${1:-}" == *"/pnpm/bin/pnpm.cjs" && "\${2:-}" == "--version" ]]; then
  echo "${pnpm_version}"
  exit 0
fi
echo "unexpected stub node invocation: \$*" >&2
exit 1
EOF
  chmod +x "${source_root}/.tools/javascript/${platform_tag}/node/bin/node"
  write_file "${source_root}/.tools/javascript/${platform_tag}/pnpm/bin/pnpm.cjs" <<'EOF'
// disposable pnpm
EOF
  copy_file "${source_root}/tools/javascript/package.json" \
    "${source_root}/.tools/javascript/bundle/package.json"
  write_file "${source_root}/.tools/javascript/bundle/runner.mjs" <<'EOF'
// disposable runner
EOF
  write_file "${source_root}/.tools/javascript/bundle/node_modules/.pnpm/example-tool/index.mjs" <<'EOF'
// disposable tool
EOF
  ln -s ".pnpm/example-tool" "${source_root}/.tools/javascript/bundle/node_modules/example-tool"
  "${source_root}/tools/javascript-bundle-manifest.sh" write \
    "${source_root}/.tools/javascript/bundle"

  write_file "${source_root}/.tools/go/${go_platform_tag}/go/bin/go" <<EOF
#!/usr/bin/env bash
if [[ "\${1:-}" == "version" && "\${2:-}" == "-m" ]]; then
  case "\${3##*/}" in
    staticcheck) printf '\tmod\thonnef.co/go/tools\tv0.7.0\th1:disposable\n' ;;
    govulncheck) printf '\tmod\tgolang.org/x/vuln\tv1.3.0\th1:disposable\n' ;;
    *)
      echo "unexpected stub go module probe: \$*" >&2
      exit 1
      ;;
  esac
  exit 0
fi
if [[ "\${1:-}" == "version" ]]; then
  echo "go version go1.26.6 ${os_tag}/${go_platform_tag##*-}"
  exit 0
fi
echo "unexpected stub go invocation: \$*" >&2
exit 1
EOF
  chmod +x "${source_root}/.tools/go/${go_platform_tag}/go/bin/go"
  write_stub_tool "${source_root}/.tools/go/${go_platform_tag}/go/bin/gofmt"
  write_version_tool "${source_root}/.tools/shellcheck/${shellcheck_platform_tag}/shellcheck" \
    --version "version: 0.11.0"
  write_version_tool "${source_root}/.tools/bin/osv-scanner" --version "osv-scanner version: 2.4.0"
  write_version_tool "${source_root}/.tools/bin/ruff" --version "ruff 0.16.0"
  write_version_tool "${source_root}/.tools/bin/ty" --version "ty 0.0.65 (87de836df 2026-07-29)"
  write_python_runtime_tool "${source_root}/.tools/python/${platform_tag}/python" "3.12.13" "2.16" "26.3"
  printf '3.12.13+20260728\n' >"${source_root}/.tools/python/${platform_tag}/.code-polishy-python-release"
  printf '26.3\n' >"${source_root}/.tools/python/${platform_tag}/.code-polishy-packaging-release"
  printf '2.16\n' >"${source_root}/.tools/python/${platform_tag}/.code-polishy-vulture-release"
  mkdir -p "${source_root}/.tools/python/${platform_tag}/lib/python3.12/site-packages/packaging-26.3.dist-info"
  mkdir -p "${source_root}/.tools/python/${platform_tag}/lib/python3.12/site-packages/vulture-2.16.dist-info"

  write_stub_tool "${source_root}/.tools/bin/staticcheck"
  write_stub_tool "${source_root}/.tools/bin/govulncheck"

  "${real_git}" -C "${source_root}" init --quiet
  "${real_git}" -C "${source_root}" config user.email "install-test@example.invalid"
  "${real_git}" -C "${source_root}" config user.name "Install Test"
  "${real_git}" -C "${source_root}" add -A
  "${real_git}" -C "${source_root}" commit --quiet -m "disposable checkout"
}

build_command_shims() {
  mkdir -p "${shim_bin}"
  cat >"${shim_bin}/git" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >>"${git_log}"
exec "${real_git}" "\$@"
EOF
  cat >"${shim_bin}/gh" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >>"${gh_marker}"
exit 1
EOF
  chmod +x "${shim_bin}/git" "${shim_bin}/gh"
}

install_release() {
  PATH="${shim_bin}:${PATH}" "${source_root}/scripts/install.sh" \
    --prefix "${prefix}" --command-dir "${command_root}" "$@"
}

path_after_action() {
  env ACTION_FILE="$1" PATH=/usr/bin:/bin \
    bash --noprofile --norc <<'EOF'
source "${ACTION_FILE}"
printf '%s\n' "${PATH}"
EOF
}

record_installed_releases() {
  : >"${release_list}"
  if [[ -d "${prefix}/releases" ]]; then
    find "${prefix}/releases" -mindepth 1 -maxdepth 1 -type d -name '9.9.9-*' |
      LC_ALL=C sort >"${release_list}"
  fi
}

installed_release_count() {
  record_installed_releases
  wc -l <"${release_list}" | tr -d '[:space:]'
}

installed_release() {
  record_installed_releases
  awk -v wanted="$1" 'NR == wanted { print; exit }' "${release_list}"
}

manifest_field() {
  awk -v key="\"$2\":" '$1 == key { value = $2; gsub(/[",]/, "", value); print value; exit }' "$1"
}

write_target_lock() {
  local target="$1"
  local manifest="$2"
  write_file "${target}/.code-polishy.lock.json" <<EOF
{
  "lockVersion": 1,
  "codePolishyVersion": "$(manifest_field "${manifest}" codePolishyVersion)",
  "releaseDigest": "$(manifest_field "${manifest}" releaseDigest)",
  "features": ["javascript-bundle"]
}
EOF
}

(cd "${policy_root}" && "${policy_root}/scripts/go.sh" build -trimpath \
  -o "${launcher_binary}" ./cmd/code-polishy-launcher)

build_source_checkout
build_command_shims

mkdir -p "${command_root}"
ln -s "${fixture_root}/unrelated-command" "${command_link}"
if install_release >"${fixture_root}/command-collision.log" 2>&1; then
  fail "the installer replaced an unrelated command link"
fi
grep -q "Refusing to replace the unrelated command link" \
  "${fixture_root}/command-collision.log" ||
  fail "the unrelated command link was refused without naming the collision"
[[ ! -e "${prefix}" ]] || fail "a command-link collision changed the install prefix"
rm "${command_link}"


install_release >"${fixture_root}/install.log"
grep -Fq 'Command discovery:' "${fixture_root}/install.log" ||
  fail "installer did not report stable launcher discovery"
if grep -Fq 'already resolves to the installed launcher' "${fixture_root}/install.log"; then
  fail "installer incorrectly reported the new stable launcher as already selected"
fi
sed -n '/^  export PATH=/s/^  //p' "${fixture_root}/install.log" \
  >"${fixture_root}/session-path-action"
[[ "$(wc -l <"${fixture_root}/session-path-action" | tr -d '[:space:]')" == "1" ]] ||
  fail "installer did not print one stable PATH action"
session_path="$(path_after_action "${fixture_root}/session-path-action")"
[[ "${session_path%%:*}" == "${command_root}" ]] ||
  fail "the printed PATH action did not preserve the command directory"
[[ -L "${command_link}" ]] || fail "installer did not create the managed command link"
[[ "$(readlink "${command_link}")" == "${prefix}/bin/code-polishy" ]] ||
  fail "the managed command link does not target the stable launcher"
[[ ! -e "${path_profile}" ]] || fail "installer edited a shell profile without permission"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a clean checkout did not install exactly one release"
release="$(installed_release 1)"
manifest="${release}/release-manifest.json"
expected_revision="$("${real_git}" -C "${source_root}" rev-parse HEAD)"
[[ "$(manifest_field "${manifest}" sourceRevision)" == "${expected_revision}" ]] ||
  fail "the release does not record the exact source commit"
[[ "$(manifest_field "${manifest}" host)" == "${platform_tag}" ]] ||
  fail "the release does not record this host"
[[ "$(manifest_field "${manifest}" codePolishyVersion)" == "9.9.9" ]] ||
  fail "the release does not record the checkout version"

for carried in go:1.26.6 node:24.18.0 pnpm:11.13.0 packaging:26.3 shellcheck:0.11.0 \
  staticcheck:0.7.0 govulncheck:1.3.0 osv-scanner:2.4.0 python:3.12.13+20260728 \
  ruff:0.16.0 ty:0.0.65 vulture:2.16; do
  [[ "$(manifest_field "${manifest}" "${carried%%:*}")" == "${carried##*:}" ]] ||
    fail "the release does not record which ${carried%%:*} it carries"
done
release_digest="$(manifest_field "${manifest}" releaseDigest)"
content_digest="$(manifest_field "${manifest}" contentDigest)"
[[ "${release}" == *"/9.9.9-${release_digest}" ]] ||
  fail "the release directory is not named by its version and release digest"
[[ "${release_digest}" != "${content_digest}" ]] ||
  fail "the host-independent digest is the installed-bytes digest"

for required in bin/code-polishy bin/code-polishy-launcher VERSION LICENSE README.md CHANGELOG.md \
  docs/installation.md docs/agent-workflows.md docs/catalog.json schema/code-polishy.schema.json \
  templates/AGENTS.md templates/CLAUDE.md templates/behavior-review.md \
  artifact-security/scanner-policy.json \
  scripts/go_version.txt scripts/release-manifest.sh tools/shellcheck.sh \
  tools/shellcheck-version.txt tools/node-version.txt tools/pnpm-version.txt \
  tools/staticcheck-version.txt tools/govulncheck-version.txt \
  tools/osv-scanner-version.txt tools/python-version.txt tools/python_runtime_checksums.txt \
  internal/pythonfacts/pyproject.toml internal/pythonfacts/uv.lock \
  tools/packaging-version.txt tools/packaging_wheel_checksums.txt \
  tools/ruff-version.txt tools/ty-version.txt tools/ty.toml tools/vulture-version.txt tools/vulture_wheel_checksums.txt \
  tools/trivy-version.txt tools/javascript_bundle_inventory.txt \
  ".tools/javascript/${platform_tag}/node/bin/node" \
  ".tools/javascript/${platform_tag}/pnpm/bin/pnpm.cjs" \
  ".tools/python/${platform_tag}/python" \
  ".tools/python/${platform_tag}/.code-polishy-packaging-release" \
  ".tools/python/${platform_tag}/.code-polishy-python-release" \
  ".tools/python/${platform_tag}/.code-polishy-vulture-release" \
  ".tools/python/${platform_tag}/lib/python3.12/site-packages/packaging-26.3.dist-info" \
  .tools/javascript/bundle/runner.mjs \
  ".tools/go/${go_platform_tag}/go/bin/go" \
  ".tools/go/${go_platform_tag}/go/bin/gofmt" \
  ".tools/shellcheck/${shellcheck_platform_tag}/shellcheck" \
  .tools/bin/staticcheck .tools/bin/govulncheck .tools/bin/osv-scanner .tools/bin/ruff .tools/bin/ty; do
  [[ -e "${release}/${required}" ]] || fail "the release is missing ${required}"
done
[[ ! -e "${release}/scripts/automation" ]] ||
  fail "the installed release carried retired shell workflow supervisors"
for documentation in README.md CHANGELOG.md docs/installation.md docs/agent-workflows.md docs/catalog.json; do
  cmp -s "${source_root}/${documentation}" "${release}/${documentation}" ||
    fail "the release did not carry the exact ${documentation} documentation"
done
[[ ! -e "${release}/skills" ]] ||
  fail "the installed release carried a source-only skills directory"
cmp -s "${source_root}/templates/CLAUDE.md" "${release}/templates/CLAUDE.md" ||
  fail "the release did not carry the exact canonical CLAUDE.md import"
cmp -s "${source_root}/templates/behavior-review.md" \
  "${release}/templates/behavior-review.md" ||
  fail "the release did not carry the exact behavior review instructions"
[[ -x "${release}/bin/code-polishy" ]] || fail "the release binary is not executable"
[[ -L "${release}/.tools/javascript/bundle/node_modules/example-tool" ]] ||
  fail "the release did not keep the bundle's links as links"
"${release}/scripts/release-manifest.sh" verify "${release}" ||
  fail "a freshly installed release does not verify"


if [[ -e "${gh_marker}" ]]; then
  fail "the installer invoked gh"
fi
while read -r invocation; do
  case "${invocation}" in
    "-C "*" rev-parse "* | "-C "*" status --porcelain=v1 --untracked-files=all") ;;
    *) fail "the installer ran an unexpected git command: git ${invocation}" ;;
  esac
done <"${git_log}"
grep -q "rev-parse HEAD" "${git_log}" || fail "the installer did not read the local commit"


CODE_POLISHY_TEST_BUILD_MARK=second install_release >"${fixture_root}/reinstall.log"
grep -q "already installed" "${fixture_root}/reinstall.log" ||
  fail "reinstalling the same commit did not recognize the installed release"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "reinstalling the same commit added a release"
[[ "$(manifest_field "${manifest}" contentDigest)" == "${content_digest}" ]] ||
  fail "reinstalling replaced the verified installed bytes"
PATH="${command_root}:${PATH}" install_release >"${fixture_root}/discoverable-reinstall.log"
grep -Fq 'Command discovery: code-polishy already resolves to the installed launcher.' \
  "${fixture_root}/discoverable-reinstall.log" ||
  fail "installer did not recognize the stable launcher already on PATH"

install_release --add-to-path --path-profile "${path_profile}" \
  >"${fixture_root}/add-to-path.log"
grep -Fq '# Code Polishy PATH' "${path_profile}" ||
  fail "--add-to-path did not mark its owned profile entry"
persisted_path="$(path_after_action "${path_profile}")"
[[ "${persisted_path%%:*}" == "${command_root}" ]] ||
  fail "the persistent PATH entry did not preserve the command directory"
install_release --add-to-path --path-profile "${path_profile}" \
  >"${fixture_root}/add-to-path-again.log"
[[ "$(grep -Fc '# Code Polishy PATH' "${path_profile}")" == "1" ]] ||
  fail "--add-to-path duplicated its profile entry"

cp "${path_profile}" "${fixture_root}/expected-shell-profile"
printf 'export PATH=/unrelated:"%s" # Code Polishy PATH\n' "\$PATH" >"${path_profile}"
if install_release --add-to-path --path-profile "${path_profile}" \
  >"${fixture_root}/path-profile-collision.log" 2>&1; then
  fail "--add-to-path replaced a changed profile entry"
fi
grep -q "Refusing to replace the existing Code Polishy PATH entry" \
  "${fixture_root}/path-profile-collision.log" ||
  fail "a changed PATH entry was refused without naming the conflict"
mv "${fixture_root}/expected-shell-profile" "${path_profile}"

printf 'tampered\n' >>"${release}/bin/code-polishy"
install_release >"${fixture_root}/replace.log"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "replacing a changed release added a release"
"${release}/scripts/release-manifest.sh" verify "${release}" ||
  fail "a replaced release does not verify"
[[ "$(manifest_field "${manifest}" contentDigest)" == "${content_digest}" ]] ||
  fail "replacing a changed release did not restore the recorded bytes"
if [[ -n "$(find "${prefix}/releases" -mindepth 1 -maxdepth 1 -name '.superseded-*')" ]]; then
  fail "replacing a changed release left the rejected tree in the store"
fi


printf 'uncommitted\n' >"${source_root}/scratch.txt"
if install_release >"${fixture_root}/dirty.log" 2>&1; then
  fail "a dirty checkout installed a release"
fi
grep -q "is not clean" "${fixture_root}/dirty.log" ||
  fail "a dirty checkout was rejected for the wrong reason"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a dirty checkout changed what is installed"

"${real_git}" -C "${source_root}" config status.showUntrackedFiles no
if install_release >"${fixture_root}/hidden-dirty.log" 2>&1; then
  fail "a dirty checkout with status.showUntrackedFiles=no installed a release"
fi
grep -q "is not clean" "${fixture_root}/hidden-dirty.log" ||
  fail "config-hidden untracked drift was rejected for the wrong reason"
"${real_git}" -C "${source_root}" config --unset status.showUntrackedFiles
rm "${source_root}/scratch.txt"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a config-hidden dirty checkout changed what is installed"

mv "${source_root}/.tools/bin/osv-scanner" "${fixture_root}/held-osv-scanner"
if install_release >"${fixture_root}/missing-tool.log" 2>&1; then
  fail "a checkout missing a pinned tool installed a release"
fi
grep -q ".tools/bin/osv-scanner" "${fixture_root}/missing-tool.log" ||
  fail "the missing pinned tool was refused without naming it"
mv "${fixture_root}/held-osv-scanner" "${source_root}/.tools/bin/osv-scanner"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a checkout missing a pinned tool changed what is installed"

write_version_tool "${source_root}/.tools/bin/ruff" --version "ruff 0.99.0"
if install_release >"${fixture_root}/tool-version.log" 2>&1; then
  fail "a checkout whose Ruff is not the pinned version installed a release"
fi
if ! grep -q "ruff" "${fixture_root}/tool-version.log" ||
  ! grep -q "0.99.0" "${fixture_root}/tool-version.log"; then
  fail "the unpinned tool was refused without naming it and what it reports"
fi
write_version_tool "${source_root}/.tools/bin/ruff" --version "ruff 0.16.0"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a checkout whose Ruff is not the pinned version changed what is installed"

write_version_tool "${source_root}/.tools/bin/ty" --version "ty 0.0.99 (disposable)"
if install_release >"${fixture_root}/ty-version.log" 2>&1; then
  fail "a checkout whose ty is not the pinned version installed a release"
fi
if ! grep -q "ty" "${fixture_root}/ty-version.log" ||
  ! grep -q "0.0.99" "${fixture_root}/ty-version.log"; then
  fail "the unpinned ty was refused without naming it and what it reports"
fi
write_version_tool "${source_root}/.tools/bin/ty" --version "ty 0.0.65 (87de836df 2026-07-29)"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a checkout whose ty is not the pinned version changed what is installed"

write_python_runtime_tool "${source_root}/.tools/python/${platform_tag}/python" "3.12.99" "2.16" "26.3"
if install_release >"${fixture_root}/python-version.log" 2>&1; then
  fail "a checkout whose CPython is not the pinned version installed a release"
fi
if ! grep -q "python" "${fixture_root}/python-version.log" ||
  ! grep -q "3.12.99" "${fixture_root}/python-version.log"; then
  fail "the unpinned CPython was refused without naming it and what it reports"
fi
write_python_runtime_tool "${source_root}/.tools/python/${platform_tag}/python" "3.12.13" "2.16" "26.3"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a checkout whose CPython is not the pinned version changed what is installed"

write_python_runtime_tool "${source_root}/.tools/python/${platform_tag}/python" "3.12.13" "2.99" "26.3"
if install_release >"${fixture_root}/vulture-version.log" 2>&1; then
  fail "a checkout whose Vulture is not the pinned version installed a release"
fi
if ! grep -q "vulture" "${fixture_root}/vulture-version.log" ||
  ! grep -q "2.99" "${fixture_root}/vulture-version.log"; then
  fail "the unpinned Vulture was refused without naming it and what it reports"
fi
write_python_runtime_tool "${source_root}/.tools/python/${platform_tag}/python" "3.12.13" "2.16" "26.3"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a checkout whose Vulture is not the pinned version changed what is installed"

write_python_runtime_tool "${source_root}/.tools/python/${platform_tag}/python" "3.12.13" "2.16" "26.99"
if install_release >"${fixture_root}/packaging-version.log" 2>&1; then
  fail "a checkout whose packaging is not the pinned version installed a release"
fi
if ! grep -q "packaging" "${fixture_root}/packaging-version.log" ||
  ! grep -q "26.99" "${fixture_root}/packaging-version.log"; then
  fail "the unpinned packaging distribution was refused without naming it and what it reports"
fi
write_python_runtime_tool "${source_root}/.tools/python/${platform_tag}/python" "3.12.13" "2.16" "26.3"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a checkout whose packaging is not the pinned version changed what is installed"

printf '3.12.13+20260727\n' >"${source_root}/.tools/python/${platform_tag}/.code-polishy-python-release"
if install_release >"${fixture_root}/python-marker.log" 2>&1; then
  fail "a checkout whose CPython carrier marker is not the pinned build installed a release"
fi
if ! grep -q "carrier marker" "${fixture_root}/python-marker.log" ||
  ! grep -q "3.12.13+20260727" "${fixture_root}/python-marker.log"; then
  fail "the unpinned CPython carrier marker was refused without naming it and what it reports"
fi
printf '3.12.13+20260728\n' >"${source_root}/.tools/python/${platform_tag}/.code-polishy-python-release"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a checkout whose CPython carrier marker is not the pinned build changed what is installed"

printf '2.15\n' >"${source_root}/.tools/python/${platform_tag}/.code-polishy-vulture-release"
if install_release >"${fixture_root}/vulture-marker.log" 2>&1; then
  fail "a checkout whose Vulture carrier marker is not the pinned release installed a release"
fi
if ! grep -q "carrier marker" "${fixture_root}/vulture-marker.log" ||
  ! grep -q "2.15" "${fixture_root}/vulture-marker.log"; then
  fail "the unpinned Vulture carrier marker was refused without naming it and what it reports"
fi
printf '2.16\n' >"${source_root}/.tools/python/${platform_tag}/.code-polishy-vulture-release"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a checkout whose Vulture carrier marker is not the pinned release changed what is installed"

printf '26.2\n' >"${source_root}/.tools/python/${platform_tag}/.code-polishy-packaging-release"
if install_release >"${fixture_root}/packaging-marker.log" 2>&1; then
  fail "a checkout whose packaging carrier marker is not the pinned release installed a release"
fi
if ! grep -q "carrier marker" "${fixture_root}/packaging-marker.log" ||
  ! grep -q "26.2" "${fixture_root}/packaging-marker.log"; then
  fail "the unpinned packaging carrier marker was refused without naming it and what it reports"
fi
printf '26.3\n' >"${source_root}/.tools/python/${platform_tag}/.code-polishy-packaging-release"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a checkout whose packaging carrier marker is not the pinned release changed what is installed"

vulture_metadata_root="${source_root}/.tools/python/${platform_tag}/lib/python3.12/site-packages"
rmdir "${vulture_metadata_root}/vulture-2.16.dist-info"
if install_release >"${fixture_root}/vulture-metadata-missing.log" 2>&1; then
  fail "a checkout whose Vulture carrier has no metadata installed a release"
fi
if ! grep -q "Vulture carrier" "${fixture_root}/vulture-metadata-missing.log"; then
  fail "missing Vulture carrier metadata was refused without naming the carrier"
fi
mkdir -p "${vulture_metadata_root}/vulture-2.16.dist-info"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a checkout whose Vulture carrier has no metadata changed what is installed"

mkdir -p "${vulture_metadata_root}/vulture-2.15.dist-info"
if install_release >"${fixture_root}/vulture-metadata.log" 2>&1; then
  fail "a checkout whose Vulture carrier has stale metadata installed a release"
fi
if ! grep -q "Vulture carrier" "${fixture_root}/vulture-metadata.log"; then
  fail "stale Vulture carrier metadata was refused without naming the carrier"
fi
rmdir "${vulture_metadata_root}/vulture-2.15.dist-info"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a checkout whose Vulture carrier has stale metadata changed what is installed"

rmdir "${vulture_metadata_root}/packaging-26.3.dist-info"
if install_release >"${fixture_root}/packaging-metadata-missing.log" 2>&1; then
  fail "a checkout whose packaging carrier has no metadata installed a release"
fi
if ! grep -q "packaging carrier" "${fixture_root}/packaging-metadata-missing.log"; then
  fail "missing packaging carrier metadata was refused without naming the carrier"
fi
mkdir -p "${vulture_metadata_root}/packaging-26.3.dist-info"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a checkout whose packaging carrier has no metadata changed what is installed"

mkdir -p "${vulture_metadata_root}/packaging-26.2.dist-info"
if install_release >"${fixture_root}/packaging-metadata.log" 2>&1; then
  fail "a checkout whose packaging carrier has stale metadata installed a release"
fi
if ! grep -q "packaging carrier" "${fixture_root}/packaging-metadata.log"; then
  fail "stale packaging carrier metadata was refused without naming the carrier"
fi
rmdir "${vulture_metadata_root}/packaging-26.2.dist-info"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a checkout whose packaging carrier has stale metadata changed what is installed"

sed 's#golang.org/x/vuln\\tv1.3.0#golang.org/x/vuln\\tv1.3.1#' \
  "${source_root}/.tools/go/${go_platform_tag}/go/bin/go" >"${fixture_root}/other-go"
cp "${source_root}/.tools/go/${go_platform_tag}/go/bin/go" "${fixture_root}/held-go"
cp "${fixture_root}/other-go" "${source_root}/.tools/go/${go_platform_tag}/go/bin/go"
if install_release >"${fixture_root}/module-version.log" 2>&1; then
  fail "a checkout whose govulncheck is not the pinned version installed a release"
fi
grep -q "govulncheck" "${fixture_root}/module-version.log" ||
  fail "the unpinned Go analyzer was refused without naming it"
cp "${fixture_root}/held-go" "${source_root}/.tools/go/${go_platform_tag}/go/bin/go"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a checkout whose govulncheck is not the pinned version changed what is installed"

commit_checkout() {
  "${real_git}" -C "${source_root}" add -A
  "${real_git}" -C "${source_root}" commit --quiet -m "$1"
}

write_file "${source_root}/templates/check_policy.sh" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
commit_checkout "disposable retired wrapper"
if install_release >"${fixture_root}/retired-path.log" 2>&1; then
  fail "a checkout carrying the retired wrapper installed a release"
fi
grep -q "templates/check_policy.sh" "${fixture_root}/retired-path.log" ||
  fail "the retired wrapper was refused without naming the path that carries it"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a release carrying a retired path was installed anyway"
rm "${source_root}/templates/check_policy.sh"
commit_checkout "disposable retired wrapper removed"

write_file "${source_root}/templates/AGENTS.md" <<'EOF'
# Canonical guidance

Run `git submodule update --init`.
EOF
commit_checkout "disposable retired command"
if install_release >"${fixture_root}/retired-command.log" 2>&1; then
  fail "a checkout naming a retired command installed a release"
fi
grep -q "templates/AGENTS.md" "${fixture_root}/retired-command.log" ||
  fail "the retired command was refused without naming the file that carries it"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a release naming a retired command was installed anyway"
write_file "${source_root}/templates/AGENTS.md" <<'EOF'
# Canonical guidance
EOF
commit_checkout "disposable retired command removed"
if [[ -n "$(find "${prefix}/releases" -mindepth 1 -maxdepth 1 -name '.staging-*')" ]]; then
  fail "a refused release left a staging tree behind"
fi

printf '9. 9.9\n' >"${source_root}/VERSION"
commit_checkout "disposable whitespace version"
if install_release >"${fixture_root}/version.log" 2>&1; then
  fail "a checkout whose VERSION carries interior whitespace installed a release"
fi
grep -q "carries whitespace" "${fixture_root}/version.log" ||
  fail "the whitespace-carrying VERSION was refused without naming the contract it breaks"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a whitespace-carrying VERSION changed what is installed"
printf '9.9.9\n' >"${source_root}/VERSION"
commit_checkout "disposable version restored"

build_marker="${fixture_root}/fail-the-build"
: >"${build_marker}"
if CODE_POLISHY_TEST_BUILD_FAILS="${build_marker}" install_release \
  >"${fixture_root}/failed.log" 2>&1; then
  fail "a failed build still installed a release"
fi
rm "${build_marker}"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "a failed installation left a release behind"
if [[ -n "$(find "${prefix}/releases" -mindepth 1 -maxdepth 1 -name '.staging-*')" ]]; then
  fail "a failed installation left a staging tree behind"
fi


interrupt_marker="${fixture_root}/interrupt-the-install"
: >"${interrupt_marker}"
if CODE_POLISHY_TEST_INTERRUPT="${interrupt_marker}" install_release \
  >"${fixture_root}/interrupted.log" 2>&1; then
  fail "an interrupted installation still installed a release"
fi
rm "${interrupt_marker}"
grep -q "interrupted" "${fixture_root}/interrupted.log" ||
  fail "an interrupted installation did not report that it installed nothing"
[[ "$(installed_release_count)" == "1" ]] ||
  fail "an interrupted installation left a release behind"
if [[ -n "$(find "${prefix}/releases" -mindepth 1 -maxdepth 1 -name '.staging-*')" ]]; then
  fail "an interrupted installation left a staging tree behind"
fi


changed="${fixture_root}/changed"
cp -RPp "${release}" "${changed}"
printf 'tampered\n' >>"${changed}/bin/code-polishy"
if "${changed}/scripts/release-manifest.sh" verify "${changed}" >/dev/null 2>&1; then
  fail "a changed release file verified"
fi
rm -rf "${changed}"


retargeted="${fixture_root}/retargeted"
cp -RPp "${release}" "${retargeted}"
mkdir -p "${retargeted}/.tools/javascript/bundle/node_modules/.pnpm/other-tool"
printf '// other\n' >"${retargeted}/.tools/javascript/bundle/node_modules/.pnpm/other-tool/index.mjs"
ln -sf ".pnpm/other-tool" "${retargeted}/.tools/javascript/bundle/node_modules/example-tool"
if "${retargeted}/scripts/release-manifest.sh" verify "${retargeted}" >/dev/null 2>&1; then
  fail "a retargeted bundle link verified"
fi
rm -rf "${retargeted}"


foreign="${fixture_root}/foreign"
other_host="linux-x64"
if [[ "${platform_tag}" == "${other_host}" ]]; then
  other_host="darwin-arm64"
fi
cp -RPp "${release}" "${foreign}"
sed "s/\"host\": \"${platform_tag}\"/\"host\": \"${other_host}\"/" \
  "${foreign}/release-manifest.json" >"${foreign}/foreign-manifest.json"
mv "${foreign}/foreign-manifest.json" "${foreign}/release-manifest.json"
if "${foreign}/scripts/release-manifest.sh" verify "${foreign}" >/dev/null 2>&1; then
  fail "a release recorded for another host verified"
fi
rm -rf "${foreign}"


unmanaged="${fixture_root}/unmanaged"
cp -RPp "${release}" "${unmanaged}"
rm "${unmanaged}/release-manifest.json"
if "${policy_root}/scripts/release-manifest.sh" verify "${unmanaged}" >/dev/null 2>&1; then
  fail "a release without a manifest verified"
fi
rm -rf "${unmanaged}"


printf '# Canonical guidance, revised\n' >"${source_root}/templates/AGENTS.md"
"${real_git}" -C "${source_root}" add -A
"${real_git}" -C "${source_root}" commit --quiet -m "second disposable commit"
CODE_POLISHY_TEST_BUILD_MARK=second install_release >"${fixture_root}/second-install.log"
[[ "$(installed_release_count)" == "2" ]] ||
  fail "a second reviewed commit did not install beside the first"
first_release="$(installed_release 1)"
second_release="$(installed_release 2)"
"${first_release}/scripts/release-manifest.sh" verify "${first_release}" ||
  fail "the first coexisting release does not verify"
"${second_release}/scripts/release-manifest.sh" verify "${second_release}" ||
  fail "the second coexisting release does not verify"
[[ "$(manifest_field "${first_release}/release-manifest.json" releaseDigest)" != \
  "$(manifest_field "${second_release}/release-manifest.json" releaseDigest)" ]] ||
  fail "two reviewed commits share one release digest"

[[ -x "${prefix}/bin/code-polishy" ]] || fail "the installer did not install the launcher"
launcher="${command_link}"
[[ -x "${launcher}" ]] || fail "the managed command link is not executable"


target="${fixture_root}/target"
mkdir -p "${target}"
if (cd "${target}" && "${launcher}" check) >"${fixture_root}/unlocked.log" 2>&1; then
  fail "a target with no lock ran a release"
fi
grep -q ".code-polishy.lock.json" "${fixture_root}/unlocked.log" ||
  fail "a target with no lock was not told which file to write"

newer_release="${first_release}"
if [[ "${newer_release}" == "${release}" ]]; then
  newer_release="${second_release}"
fi
for selected in "${release}:first" "${newer_release}:second"; do
  installed="${selected%:*}"
  expected_mark="${selected##*:}"
  write_target_lock "${target}" "${installed}/release-manifest.json"
  (cd "${target}" && "${launcher}" check --all) >"${fixture_root}/locked.log" 2>&1 ||
    fail "the launcher did not run the release ${installed}"
  grep -q "code-polishy ${expected_mark} " "${fixture_root}/locked.log" ||
    fail "the launcher ran a release other than ${installed}"
  grep -q -- "--policy-root ${installed} --repo-root ${target} check --all" \
    "${fixture_root}/locked.log" ||
    fail "the launcher did not hand ${installed} the target and the caller's arguments"
done


missing_digest="0000000000000000000000000000000000000000000000000000000000000000"
sed "s/\"releaseDigest\": \".*\"/\"releaseDigest\": \"${missing_digest}\"/" \
  "${target}/.code-polishy.lock.json" >"${fixture_root}/missing-lock.json"
cp "${fixture_root}/missing-lock.json" "${target}/.code-polishy.lock.json"
if (cd "${target}" && "${launcher}" check) >"${fixture_root}/missing.log" 2>&1; then
  fail "a lock naming an uninstalled release ran something"
fi
grep -q "${missing_digest}" "${fixture_root}/missing.log" ||
  fail "a missing release did not name the digest the target requires"
grep -q "./scripts/install.sh" "${fixture_root}/missing.log" ||
  fail "a missing release did not name the local installation step"

write_target_lock "${target}" "${release}/release-manifest.json"
added="${release}/.tools/javascript/bundle/added.mjs"
printf '// added\n' >"${added}"
if (cd "${target}" && "${launcher}" check) >"${fixture_root}/added.log" 2>&1; then
  fail "the launcher ran a release carrying a file it does not record"
fi
grep -q "does not record it" "${fixture_root}/added.log" ||
  fail "an unrecorded release file was refused for another reason"
rm "${added}"

removed="${release}/.tools/javascript/bundle/runner.mjs"
mv "${removed}" "${fixture_root}/removed-runner.mjs"
if (cd "${target}" && "${launcher}" check) >"${fixture_root}/removed.log" 2>&1; then
  fail "the launcher ran a release that lost a file it records"
fi
grep -q "is missing" "${fixture_root}/removed.log" ||
  fail "a missing release file was refused for another reason"
mv "${fixture_root}/removed-runner.mjs" "${removed}"
(cd "${target}" && "${launcher}" check --all) >"${fixture_root}/restored.log" 2>&1 ||
  fail "the release did not run once it was whole again"


write_target_lock "${target}" "${newer_release}/release-manifest.json"
printf '\n# tampered\n' >>"${newer_release}/bin/code-polishy"
if (cd "${target}" && "${launcher}" check) >"${fixture_root}/tampered.log" 2>&1; then
  fail "the launcher ran a release binary that changed after installation"
fi
grep -q "install.sh" "${fixture_root}/tampered.log" ||
  fail "a changed release binary was not rejected with an installation instruction"

printf 'install tests passed\n'
