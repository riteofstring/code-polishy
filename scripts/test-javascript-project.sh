#!/usr/bin/env bash
set -euo pipefail

# Offline contract tests for the pnpm-project operations of the sealed,
# policy-owned JavaScript tool bundle.
#
# A pnpm project is asked about as a whole rather than as a selection of files:
# its lockfile, its workspace settings, the licenses its installed dependencies
# declare, and the advisories its native audit returns. Each is exercised here
# against the installed bundle. The operations that read selected source, and
# the protocol and launch contract every operation answers under, are covered by
# scripts/test-javascript-runner.sh.

javascript_test_name="test-javascript-project"
# shellcheck source=scripts/javascript-runner-test-env.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/javascript-runner-test-env.sh"

# pnpm workspace and lock facts are read from the lockfile and the manifests it
# resolves. The operation reports what each side declared and what every
# resolution came from; whether that is drift or an inadmissible source is a Go
# decision this runner knows nothing about.
packages_target="${fixture_root}/packages"
host_os="${javascript_platform_tag%%-*}"
host_cpu="${javascript_platform_tag#*-}"
mkdir -p "${packages_target}/packages/app" "${packages_target}/packages/lib"
printf '{"name":"root","packageManager":"pnpm@%s","devDependencies":{"typescript":"6.0.3"},"optionalDependencies":{"foreign-optional":"1.0.0","host-compatible-optional":"1.0.0"}}\n' \
  "${pnpm_version}" >"${packages_target}/package.json"
cat >"${packages_target}/packages/app/package.json" <<'JSON'
{
  "name": "app",
  "dependencies": { "aliased": "npm:left-pad@1.3.0", "lib": "workspace:*", "react": "19.0.0" },
  "peerDependencies": { "vue": "3.0.0" }
}
JSON
printf '{"name":"lib"}\n' >"${packages_target}/packages/lib/package.json"
cat >"${packages_target}/pnpm-lock.yaml" <<YAML
lockfileVersion: '9.0'

settings:
  autoInstallPeers: false

importers:

  .:
    devDependencies:
      typescript:
        specifier: 6.0.3
        version: 6.0.3
    optionalDependencies:
      foreign-optional:
        specifier: 1.0.0
        version: 1.0.0
      host-compatible-optional:
        specifier: 1.0.0
        version: 1.0.0

  packages/app:
    dependencies:
      aliased:
        specifier: npm:left-pad@1.3.0
        version: left-pad@1.3.0
      lib:
        specifier: workspace:*
        version: link:../lib
      react:
        specifier: 18.3.1
        version: 18.3.1

  packages/lib:
    dependencies:
      undeclared:
        specifier: 1.0.0
        version: 1.0.0

packages:

  left-pad@1.3.0:
    resolution: {integrity: sha512-aaa==}

  react@18.3.1:
    resolution: {integrity: sha512-bbb==}

  typescript@6.0.3:
    resolution: {integrity: sha512-ccc==}
    hasBin: true

  '@scope/peered@2.0.0':
    resolution: {integrity: sha512-ddd==}

  forked@1.0.0:
    resolution: {tarball: https://example.invalid/forked.tgz}

  signed-fork@1.0.0:
    resolution: {tarball: https://example.invalid/signed.tgz, integrity: sha512-eee==}

  vendored@0.0.0:
    resolution: {type: directory, directory: vendor/local}

  foreign-optional@1.0.0:
    resolution: {integrity: sha512-fff==}
    os: [win32]

  host-compatible-optional@1.0.0:
    resolution: {integrity: sha512-ggg==}
    os: [${host_os}]
    cpu: [${host_cpu}]

  foreign-optional-child@1.0.0:
    resolution: {integrity: sha512-child==}

  foreign-optional-grandchild@1.0.0:
    resolution: {integrity: sha512-grandchild==}

  shared-optional-child@1.0.0:
    resolution: {integrity: sha512-shared==}

  libc-compatible-optional@1.0.0:
    resolution: {integrity: sha512-hhh==}
    os: [linux]
    cpu: [${host_cpu}]
    libc: [glibc, musl]

  libc-excluded-optional@1.0.0:
    resolution: {integrity: sha512-iii==}
    os: [linux]
    cpu: [${host_cpu}]
    libc: ['!glibc', '!musl']

  mixed-context@1.0.0:
    resolution: {integrity: sha512-jjj==}
    os: [win32]

snapshots:
  foreign-optional@1.0.0:
    dependencies:
      foreign-optional-child: 1.0.0
      shared-optional-child: 1.0.0
    optional: true
  host-compatible-optional@1.0.0:
    dependencies:
      shared-optional-child: 1.0.0
    optional: true
  foreign-optional-child@1.0.0:
    dependencies:
      foreign-optional-grandchild: 1.0.0
    optional: true
  foreign-optional-grandchild@1.0.0:
    optional: true
  shared-optional-child@1.0.0:
    optional: true
  libc-compatible-optional@1.0.0:
    optional: true
  libc-excluded-optional@1.0.0:
    optional: true
  mixed-context@1.0.0:
    optional: true
  mixed-context@1.0.0(peer@2.0.0): {}
YAML

packages_request() {
  printf '{"protocolVersion":2,"operation":"packages","root":"%s","directory":"%s"}' \
    "$1" "${2:-.}"
}
packages_response="${fixture_root}/packages.json"
packages_request "${packages_target}" | run_runner >"${packages_response}" ||
  fail "the runner rejected a well-formed packages request"

# The lock is also the workspace inventory: pnpm writes one importer per
# workspace package, so a member it omits is one the target never installed.
for importer in '"path":".","manifest":"package.json"' \
  '"path":"packages/app","manifest":"packages/app/package.json"' \
  '"path":"packages/lib","manifest":"packages/lib/package.json"'; do
  if ! grep -qF "${importer}" "${packages_response}"; then
    fail "packages did not report importer ${importer}: $(cat "${packages_response}")"
  fi
done

expect_dependency() {
  if ! grep -qF "$1" "${packages_response}"; then
    fail "packages did not report $2: $(cat "${packages_response}")"
  fi
}
# What the manifest wrote and what the lock recorded are reported side by side,
# so the same entry carries the drift and Go decides it.
expect_dependency '"name":"react","scope":"runtime","declared":"19.0.0","specifier":"18.3.1"' \
  "a declaration the lock resolves differently"
expect_dependency '"name":"undeclared","scope":"runtime","declared":"","specifier":"1.0.0"' \
  "a resolution no manifest declares"
# An alias resolves under the name it really installs, and a workspace
# dependency resolves to a sibling package rather than to a release.
expect_dependency '"resolvedName":"left-pad","resolvedVersion":"1.3.0"' "an aliased dependency"
expect_dependency '"name":"lib","scope":"runtime","declared":"workspace:*","specifier":"workspace:*","resolvedName":"","resolvedVersion":"","link":"packages/lib"' \
  "a workspace link"
# A peer declaration is not resolved into a lock, so comparing one against it
# would report every peer as drift. It is a manifest fact Go already reads.
if grep -qF '"name":"vue"' "${packages_response}"; then
  fail "packages reported a peer declaration: $(cat "${packages_response}")"
fi
# Every resolution carries the kind of source it came from, decided by the
# source the lock names rather than by the fields it carries: a download the
# target chose is a tarball whether or not pnpm also recorded integrity over
# those bytes, because integrity says they arrived unchanged and not that a
# registry published them.
for resolved in '"name":"react","version":"18.3.1","source":"registry"' \
  '"name":"@scope/peered","version":"2.0.0","source":"registry"' \
  '"name":"forked","version":"1.0.0","source":"tarball"' \
  '"name":"signed-fork","version":"1.0.0","source":"tarball"' \
  '"name":"vendored","version":"0.0.0","source":"directory"'; do
  if ! grep -qF "${resolved}" "${packages_response}"; then
    fail "packages did not report ${resolved}: $(cat "${packages_response}")"
  fi
done
# A normal frozen install on Darwin or Linux omits this optional Windows-only
# release. The package fact must say that its missing installed license metadata
# is expected, rather than making the Go policy report false coverage.
expect_dependency '"name":"foreign-optional","version":"1.0.0","source":"registry","licenseMetadata":"platform-excluded"' \
  "a foreign-platform optional release whose normal install is absent"
expect_dependency '"name":"foreign-optional-child","version":"1.0.0","source":"registry","licenseMetadata":"platform-excluded"' \
  "a universal child reached only through an excluded optional release"
expect_dependency '"name":"foreign-optional-grandchild","version":"1.0.0","source":"registry","licenseMetadata":"platform-excluded"' \
  "a universal descendant reached only through an excluded optional release"
expect_dependency '"name":"shared-optional-child","version":"1.0.0","source":"registry","licenseMetadata":"required"' \
  "an optional child also reached through a compatible path"
expect_dependency '"name":"left-pad","version":"1.3.0","source":"registry","licenseMetadata":"required"' \
  "a universal release whose metadata remains required"
expect_dependency '"name":"host-compatible-optional","version":"1.0.0","source":"registry","licenseMetadata":"required"' \
  "a host-compatible optional release whose metadata remains required"
expect_dependency '"name":"mixed-context","version":"1.0.0","source":"registry","licenseMetadata":"required"' \
  "a release reached through a required snapshot context"
# libc is decided only by the policy-owned Node host. On Darwin both Linux-only
# releases are excluded by os; on Linux a known libc makes the broad allowlist
# required and its all-negative complement excluded, while an unavailable
# runtime fact leaves both unknown instead of guessing.
if [[ "${host_os}" == darwin ]]; then
  expect_dependency '"name":"libc-compatible-optional","version":"1.0.0","source":"registry","licenseMetadata":"platform-excluded"' \
    "a Linux libc-specific optional release on Darwin"
  expect_dependency '"name":"libc-excluded-optional","version":"1.0.0","source":"registry","licenseMetadata":"platform-excluded"' \
    "a Linux libc-specific exclusion on Darwin"
else
  if grep -qF '"name":"libc-compatible-optional","version":"1.0.0","source":"registry","licenseMetadata":"unknown"' "${packages_response}"; then
    expect_dependency '"name":"libc-excluded-optional","version":"1.0.0","source":"registry","licenseMetadata":"unknown"' \
      "an unknown libc fact shared by every libc selector"
  else
    expect_dependency '"name":"libc-compatible-optional","version":"1.0.0","source":"registry","licenseMetadata":"required"' \
      "a known compatible libc selector"
    expect_dependency '"name":"libc-excluded-optional","version":"1.0.0","source":"registry","licenseMetadata":"platform-excluded"' \
      "a known incompatible libc selector"
  fi
fi
if ! grep -qF '"unsupported":[]' "${packages_response}"; then
  fail "a well-formed lockfile reported missing coverage: $(cat "${packages_response}")"
fi
if grep -qF "${fixture_root}" "${packages_response}"; then
  fail "a packages result leaked a host path: $(cat "${packages_response}")"
fi
if [[ -e "${packages_target}/node_modules" ]]; then
  fail "reading pnpm facts wrote into the target tree"
fi

# A lock this operation cannot read is missing coverage rather than a project
# that resolves nothing. Each reason is specific, and none of them is a guess at
# what another lockfile format would have meant.
expect_lock_unsupported() {
  local description="$1" contents="$2" reason="$3"
  printf '%s' "${contents}" >"${fixture_root}/packages/unread/pnpm-lock.yaml"
  packages_request "${packages_target}" unread | run_runner >"${fixture_root}/out" ||
    fail "${description} was not reported"
  if ! grep -qF "${reason}" "${fixture_root}/out"; then
    fail "${description} was not reported as ${reason}: $(cat "${fixture_root}/out")"
  fi
}
mkdir -p "${packages_target}/unread"
expect_lock_unsupported "another lockfile format" "lockfileVersion: '6.0'
" "the lockfile declares version '6.0', not 9.0"
expect_lock_unsupported "a malformed lockfile" "importers:
  .: [
" "unexpected end of the stream within a flow collection"
expect_lock_unsupported "a lockfile that is not a map" "- one
- two
" "the lockfile is not a YAML map"
expect_lock_unsupported "a lockfile with no importers" "lockfileVersion: '9.0'
" "the lockfile declares no importers"
expect_lock_unsupported "an importer outside the repository" "lockfileVersion: '9.0'
importers:
  ../../elsewhere: {}
" "is outside the repository"
expect_lock_unsupported "a resolved key with no version" "lockfileVersion: '9.0'
importers:
  .: {}
packages:
  bare:
    resolution: {integrity: sha512-eee==}
" "resolved package key 'bare' has no version"
rm -rf "${packages_target}/unread"
packages_request "${packages_target}" absent | run_runner >"${fixture_root}/out" ||
  fail "an absent lockfile was not reported"
if ! grep -qF '"path":"absent/pnpm-lock.yaml","reason":"the file is unreadable' "${fixture_root}/out"; then
  fail "an absent lockfile was not reported as unreadable: $(cat "${fixture_root}/out")"
fi

# A scalar platform selector is not coerced into a one-element list. The
# package fact remains explicitly unknown and the malformed lock declaration is
# reported, so absent metadata cannot become a false platform exclusion.
malformed_platform_target="${fixture_root}/malformed-platform"
mkdir -p "${malformed_platform_target}"
printf '{"name":"malformed","packageManager":"pnpm@%s"}\n' "${pnpm_version}" \
  >"${malformed_platform_target}/package.json"
cat >"${malformed_platform_target}/pnpm-lock.yaml" <<'YAML'
lockfileVersion: '9.0'
importers:
  .: {}
packages:
  malformed-optional@1.0.0:
    resolution: {integrity: sha512-kkk==}
    os: win32
  required-wins@1.0.0:
    resolution: {integrity: sha512-lll==}
    os: [win32]
  unknown-wins@1.0.0:
    resolution: {integrity: sha512-mmm==}
    os: [win32]
snapshots:
  malformed-optional@1.0.0:
    optional: true
  required-wins@1.0.0: {}
  required-wins@1.0.0(peer@2.0.0):
    optional: "true"
  unknown-wins@1.0.0:
    optional: true
  unknown-wins@1.0.0(peer@2.0.0):
    optional: "true"
YAML
packages_request "${malformed_platform_target}" | run_runner >"${fixture_root}/out" ||
  fail "a malformed platform selector was not reported"
if ! grep -qF '"name":"malformed-optional","version":"1.0.0","source":"registry","licenseMetadata":"unknown"' "${fixture_root}/out"; then
  fail "a malformed platform selector was classified as excluded: $(cat "${fixture_root}/out")"
fi
if ! grep -qF '"name":"required-wins","version":"1.0.0","source":"registry","licenseMetadata":"required"' "${fixture_root}/out"; then
  fail "a required snapshot context did not dominate unknown metadata: $(cat "${fixture_root}/out")"
fi
if ! grep -qF '"name":"unknown-wins","version":"1.0.0","source":"registry","licenseMetadata":"unknown"' "${fixture_root}/out"; then
  fail "an unknown snapshot context did not dominate platform exclusion: $(cat "${fixture_root}/out")"
fi
if ! grep -qF 'declares malformed os selectors' "${fixture_root}/out"; then
  fail "a malformed platform selector was not rejected: $(cat "${fixture_root}/out")"
fi
if grep -qF "${fixture_root}" "${fixture_root}/out"; then
  fail "a malformed platform result leaked a host path: $(cat "${fixture_root}/out")"
fi

# pnpm resolves under the settings its workspace file declares, so those are
# read with the same parser too. Every setting crosses as the text it was
# written as; which ones a pinned pnpm must carry, and what each has to be, is a
# Go decision this runner knows nothing about.
workspace_target="${fixture_root}/workspace"
mkdir -p "${workspace_target}/nested"
cat >"${workspace_target}/pnpm-workspace.yaml" <<'YAML'
# A comment declares nothing.
packages:
  - packages/*
minimumReleaseAge: 43200
minimumReleaseAgeStrict: true
trustPolicy: no-downgrade
onlyBuiltDependencies: []
minimumReleaseAgeExclude:
  - example@1.2.3
  - 'other@2.0.0'
catalog:
  react: 19.0.0
YAML
workspace_request() {
  printf '{"protocolVersion":2,"operation":"workspace","root":"%s","paths":["%s"]}' \
    "$1" "${2:-pnpm-workspace.yaml}"
}
workspace_response="${fixture_root}/workspace.json"
workspace_request "${workspace_target}" | run_runner >"${workspace_response}" ||
  fail "the runner rejected a well-formed workspace request"
for setting in '"path":"pnpm-workspace.yaml"' \
  '{"name":"packages","values":["packages/*"]}' \
  '{"name":"minimumReleaseAge","values":["43200"]}' \
  '{"name":"minimumReleaseAgeStrict","values":["true"]}' \
  '{"name":"trustPolicy","values":["no-downgrade"]}' \
  '{"name":"onlyBuiltDependencies","values":[]}' \
  '{"name":"minimumReleaseAgeExclude","values":["example@1.2.3","other@2.0.0"]}' \
  '{"name":"catalog","values":["19.0.0"]}'; do
  if ! grep -qF "${setting}" "${workspace_response}"; then
    fail "workspace did not report ${setting}: $(cat "${workspace_response}")"
  fi
done
if ! grep -qF '"unsupported":[]' "${workspace_response}"; then
  fail "a well-formed workspace file reported missing coverage: $(cat "${workspace_response}")"
fi

# A settings file this operation cannot read is missing coverage rather than a
# workspace that declared nothing, and a file declaring nothing at all is
# reported as exactly that.
expect_workspace_unsupported() {
  local description="$1" contents="$2" reason="$3"
  printf '%s' "${contents}" >"${workspace_target}/nested/pnpm-workspace.yaml"
  workspace_request "${workspace_target}" nested/pnpm-workspace.yaml |
    run_runner >"${fixture_root}/out" || fail "${description} was not reported"
  if ! grep -qF "${reason}" "${fixture_root}/out"; then
    fail "${description} was not reported as ${reason}: $(cat "${fixture_root}/out")"
  fi
}
expect_workspace_unsupported "a malformed workspace file" "packages: [
" "unexpected end of the stream within a flow collection"
expect_workspace_unsupported "a workspace file that is not a map" "- one
" "the workspace file is not a YAML map"
expect_workspace_unsupported "an empty workspace file" "" '"settings":[]'
rm -rf "${workspace_target}/nested"
workspace_request "${workspace_target}" nested/pnpm-workspace.yaml |
  run_runner >"${fixture_root}/out" || fail "an absent workspace file was not reported"
if ! grep -qF '"path":"nested/pnpm-workspace.yaml","reason":"the file is unreadable' "${fixture_root}/out"; then
  fail "an absent workspace file was not reported as unreadable: $(cat "${fixture_root}/out")"
fi
if grep -qF "${fixture_root}" "${workspace_response}"; then
  fail "a workspace result leaked a host path: $(cat "${workspace_response}")"
fi

# This operation reads pnpm settings and nothing else, so it never becomes a way
# to hand arbitrary target YAML to the policy engine.
printf 'value: 1\n' >"${workspace_target}/other.yaml"
expect_runner_rejected "a workspace request for another file" \
  "$(workspace_request "${workspace_target}" other.yaml)"
expect_runner_rejected "a workspace request without a selection" \
  '{"protocolVersion":2,"operation":"workspace","root":"'"${workspace_target}"'"}'
expect_runner_rejected "an uncontained workspace selection" \
  "$(workspace_request "${workspace_target}" ../pnpm-workspace.yaml)"

# A lockfile records what a target installs, never what those packages are
# licensed under, so license facts are read from the installed tree: pnpm keeps
# one real directory per resolved release there, and every dependency of a
# release is a link into the directory that really holds it.
licenses_target="${fixture_root}/licenses"
licenses_store="${licenses_target}/node_modules/.pnpm"
write_installed_package() {
  local entry="$1" name="$2" manifest="$3"
  mkdir -p "${licenses_store}/${entry}/node_modules/${name}"
  printf '%s\n' "${manifest}" \
    >"${licenses_store}/${entry}/node_modules/${name}/package.json"
}
write_installed_package example@1.2.3 example \
  '{"name":"example","version":"1.2.3","license":"MIT"}'
# The same release is stored once per peer resolution, and every copy declares
# the same license.
write_installed_package 'example@1.2.3_react@19.0.0' example \
  '{"name":"example","version":"1.2.3","license":"MIT"}'
write_installed_package '@scope+ui@0.1.0' '@scope/ui' \
  '{"name":"@scope/ui","version":"0.1.0","license":"Apache-2.0 WITH LLVM-exception"}'
write_installed_package quiet@2.0.0 quiet '{"name":"quiet","version":"2.0.0"}'
write_installed_package legacy@4.0.0 legacy \
  '{"name":"legacy","version":"4.0.0","licenses":[{"type":"MIT"}]}'
write_installed_package broken@3.0.0 broken '{'
# Every dependency of a release is a link, including the hoisted ones, so no
# release is read through the entry of something that merely depends on it.
ln -s ../../quiet@2.0.0/node_modules/quiet "${licenses_store}/example@1.2.3/node_modules/quiet"
mkdir -p "${licenses_store}/node_modules"
ln -s ../quiet@2.0.0/node_modules/quiet "${licenses_store}/node_modules/quiet"
ln -s .pnpm/example@1.2.3/node_modules/example "${licenses_target}/node_modules/example"
licenses_request() {
  printf '{"protocolVersion":2,"operation":"licenses","root":"%s","directory":"%s"}' \
    "$1" "${2:-.}"
}
licenses_response="${fixture_root}/licenses.json"
licenses_request "${licenses_target}" | run_runner >"${licenses_response}" ||
  fail "the runner rejected a well-formed licenses request"
expect_license() {
  if ! grep -qF "$1" "${licenses_response}"; then
    fail "licenses did not report $2: $(cat "${licenses_response}")"
  fi
}
expect_license '{"name":"example","version":"1.2.3","license":"MIT"}' "a declared license"
expect_license '{"name":"@scope/ui","version":"0.1.0","license":"Apache-2.0 WITH LLVM-exception"}' \
  "a scoped package"
# A license written as anything but one string is not an SPDX expression, so it
# is reported as declaring none rather than guessed at.
expect_license '{"name":"quiet","version":"2.0.0","license":""}' "a package declaring no license"
expect_license '{"name":"legacy","version":"4.0.0","license":""}' "a legacy license field"
if [[ "$(grep -o '"name":"example"' "${licenses_response}" | wc -l)" -ne 1 ]]; then
  fail "licenses reported one release more than once: $(cat "${licenses_response}")"
fi
if [[ "$(grep -o '"name":"quiet"' "${licenses_response}" | wc -l)" -ne 1 ]]; then
  fail "licenses read a release through a link to it: $(cat "${licenses_response}")"
fi
# A manifest this reader cannot use is missing coverage, named the way the
# request named the tree, rather than a release with no license against it.
expect_license '"path":"node_modules/.pnpm/broken@3.0.0/node_modules/broken/package.json","reason":"the manifest is not readable JSON' \
  "an unreadable manifest"
if grep -qF "${fixture_root}" "${licenses_response}"; then
  fail "a licenses result leaked a host path: $(cat "${licenses_response}")"
fi

# A tree this operation cannot read at all is missing coverage too, and the
# reason says which of the two it was.
mkdir -p "${licenses_target}/hoisted/node_modules/example"
licenses_request "${licenses_target}" hoisted | run_runner >"${fixture_root}/out" ||
  fail "an installed tree without a store was not reported"
if ! grep -qF '"path":"hoisted/node_modules/.pnpm","reason":"the installed tree has no isolated pnpm store' "${fixture_root}/out"; then
  fail "a tree without a store was not reported as one: $(cat "${fixture_root}/out")"
fi
licenses_request "${licenses_target}" absent | run_runner >"${fixture_root}/out" ||
  fail "an uninstalled project was not reported"
if ! grep -qF '"reason":"the dependencies of this project are not installed' "${fixture_root}/out"; then
  fail "an uninstalled project was not reported as one: $(cat "${fixture_root}/out")"
fi

expect_runner_rejected "a licenses request without a directory" \
  '{"protocolVersion":2,"operation":"licenses","root":"'"${licenses_target}"'"}'
expect_runner_rejected "an uncontained licenses directory" \
  "$(licenses_request "${licenses_target}" ../elsewhere)"
expect_runner_rejected "a licenses request with a selection" \
  '{"protocolVersion":2,"operation":"licenses","root":"'"${licenses_target}"'","directory":".","paths":[]}'

# The native audit is the one operation that runs another executable, and the
# executable is the pinned pnpm installed beside the bundle. The fixture stands
# up a bundle whose sibling runtime holds a stand-in pnpm, so what answers the
# audit proves where the runner looked for it: nothing here is on PATH, and the
# real pinned pnpm is installed somewhere else entirely.
audit_bundle="${fixture_root}/audit/bundle"
audit_pnpm="${fixture_root}/audit/${javascript_platform_tag}/pnpm/bin"
mkdir -p "${audit_bundle}" "${audit_pnpm}" "${fixture_root}/audit/project" \
  "${fixture_root}/audit/refused"
cp "${runner_sources[@]}" "${audit_bundle}/"
ln -s "${javascript_bundle_dir}/node_modules" "${audit_bundle}/node_modules"
printf '{"engines":{"node":"%s"}}\n' "${node_version}" >"${audit_bundle}/package.json"
# The governed lock, and the target-owned files pnpm would read if it were ever
# pointed at the project: settings that would redirect the audit, a hook file
# that is target code, a manifest, and an installed package.
audit_lock_source="${fixture_root}/audit/project/pnpm-lock.yaml"
printf "lockfileVersion: '9.0'\n\nimporters:\n  .: {}\n" >"${audit_lock_source}"
printf 'registry=https://registry.invalid/\n' >"${fixture_root}/audit/project/.npmrc"
printf 'module.exports = { hooks: {} };\n' >"${fixture_root}/audit/project/.pnpmfile.cjs"
printf '{"name":"audited","version":"0.0.0"}\n' >"${fixture_root}/audit/project/package.json"
mkdir -p "${fixture_root}/audit/project/node_modules/installed"
printf '{"name":"installed","version":"1.0.0"}\n' \
  >"${fixture_root}/audit/project/node_modules/installed/package.json"
printf "lockfileVersion: '9.0'\n\n# refuse-this-audit\n" \
  >"${fixture_root}/audit/refused/pnpm-lock.yaml"
audit_arguments="${fixture_root}/audit/arguments.txt"
audit_entries="${fixture_root}/audit/entries.txt"
audit_lock_seen="${fixture_root}/audit/lock-seen.yaml"
cat >"${audit_pnpm}/pnpm.cjs" <<CJS
// A stand-in for the pinned pnpm: it records how it was invoked, everything the
// directory it was pointed at contains, and the lock it read there, then
// answers with the report shape pnpm's own native audit writes.
const { readdirSync, readFileSync, writeFileSync } = require("node:fs");
const { join } = require("node:path");
const argv = process.argv.slice(2);
writeFileSync("${audit_arguments}", argv.join("\n"));
const audited = argv[argv.indexOf("--dir") + 1];
writeFileSync("${audit_entries}", readdirSync(audited).sort().join("\n"));
const lock = readFileSync(join(audited, "pnpm-lock.yaml"), "utf8");
writeFileSync("${audit_lock_seen}", lock);
if (lock.includes("refuse-this-audit")) {
  process.stdout.write(
    JSON.stringify({
      error: { code: "ERR_PNPM_AUDIT_NO_LOCKFILE", message: "Cannot audit a project without a lockfile" },
    }),
  );
  process.exit(1);
}
process.stdout.write(
  JSON.stringify({
    advisories: {
      1100: {
        id: 1100,
        module_name: "example",
        severity: "high",
        title: "unsafe example path",
        github_advisory_id: "GHSA-abcd-1234-5678",
        vulnerable_versions: "<1.2.4",
        findings: [
          { version: "1.2.3", paths: ["example"] },
          { version: "1.2.3", paths: ["other>example"] },
        ],
      },
      1200: {
        id: 1200,
        module_name: "registry-only",
        severity: "moderate",
        title: "no GitHub identity",
        github_advisory_id: "",
        findings: [{ version: "2.0.0", paths: ["registry-only"] }],
      },
      1300: { id: 1300, severity: "low", title: "no package", findings: [] },
    },
    metadata: { vulnerabilities: { high: 1, moderate: 1, low: 1 } },
  }),
);
// pnpm exits non-zero exactly when it found something, which is the ordinary
// result rather than a failure.
process.exit(1);
CJS
audit_response="${fixture_root}/audit.json"
printf '{"protocolVersion":2,"operation":"audit","root":"%s","directory":"%s"}' \
  "${fixture_root}/audit" project | javascript_sealed_run "${javascript_node}" "${audit_bundle}/runner.mjs" \
  >"${audit_response}" || fail "the runner rejected a well-formed audit request"
# The advisory Go decides is an identity, a package, a severity, and the exact
# releases it was reported against. The registry's own advisory object, its
# ranges, and its dependency paths never cross the protocol.
for reported in '"id":"GHSA-abcd-1234-5678"' '"aliases":["npm:1100"]' \
  '"package":"example"' '"severity":"high"' '"versions":["1.2.3"]' \
  '"id":"npm:1200"' '"aliases":[]' '"versions":["2.0.0"]'; do
  if ! grep -qF "${reported}" "${audit_response}"; then
    fail "audit did not report ${reported}: $(cat "${audit_response}")"
  fi
done
for native in 'vulnerable_versions' 'paths' 'metadata' '1.2.4'; do
  if grep -qF "${native}" "${audit_response}"; then
    fail "audit leaked the native report field ${native}: $(cat "${audit_response}")"
  fi
done
# An advisory this reader cannot use is missing coverage, named against the lock
# it was audited from, rather than a package with nothing reported against it.
if ! grep -qF '{"path":"project/pnpm-lock.yaml","reason":"advisory '"'"'1300'"'"' omitted' "${audit_response}"; then
  fail "audit did not report an unusable advisory: $(cat "${audit_response}")"
fi
# The audit asks one policy-owned registry, and the threshold stays Go's: no
# severity filter is ever passed to the tool.
expect_audit_argument() {
  if ! grep -qxF -- "$1" "${audit_arguments}"; then
    fail "the audit did not invoke the pinned pnpm with $1: $(cat "${audit_arguments}")"
  fi
}
expect_audit_argument audit
expect_audit_argument --json
expect_audit_argument --dir
expect_audit_argument --registry
expect_audit_argument https://registry.npmjs.org/
if grep -qF -- "--audit-level" "${audit_arguments}"; then
  fail "the audit let the tool decide the severity threshold: $(cat "${audit_arguments}")"
fi
# The audited directory is policy owned and holds exactly the governed lock. The
# target directory is never handed to the tool, so the settings, hooks,
# manifest, and installed packages beside its lock are not there to be read and
# no target code can run: pnpm reads all of those from the directory it audits.
if grep -qxF -- "${fixture_root}/audit/project" "${audit_arguments}"; then
  fail "the audit pointed the tool at the target directory: $(cat "${audit_arguments}")"
fi
if [[ "$(cat "${audit_entries}")" != "pnpm-lock.yaml" ]]; then
  fail "the audited directory held more than the governed lock: $(cat "${audit_entries}")"
fi
if ! cmp -s "${audit_lock_seen}" "${audit_lock_source}"; then
  fail "the audit read a lock other than the governed one: $(cat "${audit_lock_seen}")"
fi
audited_directory="$(awk '/^--dir$/ { getline; print; exit }' "${audit_arguments}")"
if [[ -e "${audited_directory}" ]]; then
  fail "the audit left its scratch directory behind at ${audited_directory}"
fi

# A lock the operation cannot read is missing coverage, and the tool is never
# run at all: there is nothing governed to audit.
rm -f "${audit_arguments}"
mkdir -p "${fixture_root}/audit/unlocked"
if ! printf '{"protocolVersion":2,"operation":"audit","root":"%s","directory":"%s"}' \
  "${fixture_root}/audit" unlocked | javascript_sealed_run "${javascript_node}" \
  "${audit_bundle}/runner.mjs" >"${fixture_root}/out"; then
  fail "an audit of a project without a lock failed instead of reporting coverage"
fi
if ! grep -qF '{"path":"unlocked/pnpm-lock.yaml","reason":"the file is unreadable' "${fixture_root}/out"; then
  fail "an audit without a governed lock did not report it: $(cat "${fixture_root}/out")"
fi
if [[ -e "${audit_arguments}" ]]; then
  fail "an audit without a governed lock still ran the tool"
fi

# A refusal reported by the tool is a failed operation, never a project with no
# advisories against it.
if printf '{"protocolVersion":2,"operation":"audit","root":"%s","directory":"%s"}' \
  "${fixture_root}/audit" refused | javascript_sealed_run "${javascript_node}" \
  "${audit_bundle}/runner.mjs" >"${fixture_root}/out" 2>&1; then
  fail "an audit the tool refused was accepted"
fi
if ! grep -qF 'ERR_PNPM_AUDIT_NO_LOCKFILE' "${fixture_root}/out"; then
  fail "a refused audit did not report why: $(cat "${fixture_root}/out")"
fi

expect_runner_rejected "an audit request without a directory" \
  '{"protocolVersion":2,"operation":"audit","root":"'"${fixture_root}/audit"'"}'
expect_runner_rejected "an uncontained audit directory" \
  '{"protocolVersion":2,"operation":"audit","root":"'"${fixture_root}/audit"'","directory":"../elsewhere"}'

expect_runner_rejected "a packages request without a directory" \
  '{"protocolVersion":2,"operation":"packages","root":"'"${packages_target}"'"}'
expect_runner_rejected "an uncontained packages directory" \
  "$(packages_request "${packages_target}" ../elsewhere)"
expect_runner_rejected "a packages request with a selection" \
  '{"protocolVersion":2,"operation":"packages","root":"'"${packages_target}"'","directory":".","paths":[]}'

echo "test-javascript-project: all checks passed"
