#!/usr/bin/env bash
set -euo pipefail











policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=tools/javascript-env.sh
source "${policy_root}/tools/javascript-env.sh"

bundle_source="${policy_root}/tools/javascript"
verify_lock="${policy_root}/tools/verify-javascript-bundle-lock.sh"
verify_tree="${policy_root}/tools/verify-javascript-bundle-tree.sh"
bundle_manifest="${policy_root}/tools/javascript-bundle-manifest.sh"
inventory="${policy_root}/tools/javascript_bundle_inventory.txt"
manifest="${bundle_source}/package.json"
settings="${bundle_source}/pnpm-workspace.yaml"
lockfile="${bundle_source}/pnpm-lock.yaml"


runner_sources=("${bundle_source}"/*.mjs)



fixture_root="$(cd "$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-javascript-bundle-test.XXXXXX")" && pwd -P)"
cleanup() {
  rm -rf "${fixture_root}"
}
trap cleanup EXIT
javascript_scratch_home="${fixture_root}/home"

fail() {
  echo "test-javascript-bundle: $1" >&2
  exit 1
}

expect_rejected() {
  local description="$1"
  shift
  if "$@" >"${fixture_root}/out" 2>&1; then
    fail "${description} was accepted"
  fi
  if [[ ! -s "${fixture_root}/out" ]]; then
    fail "${description} failed without an explanation"
  fi
}



node_version="$(tr -d '[:space:]' <"${policy_root}/tools/node-version.txt")"
pnpm_version="$(tr -d '[:space:]' <"${policy_root}/tools/pnpm-version.txt")"
if ! grep -q "\"packageManager\": \"pnpm@${pnpm_version}\"" "${manifest}"; then
  fail "manifest does not pin the policy-owned pnpm ${pnpm_version}"
fi
if ! grep -q "\"node\": \"${node_version}\"" "${manifest}"; then
  fail "manifest does not pin the policy-owned Node ${node_version}"
fi
while IFS= read -r source_file; do
  if [[ ! -f "${bundle_source}/${source_file}" ]]; then
    fail "bundle source inventory names missing ${source_file}"
  fi
done <"${bundle_source}/source-files.txt"
{
  for source_path in "${bundle_source}"/* "${bundle_source}"/.[!.]*; do
    [[ -f "${source_path}" ]] && basename "${source_path}"
  done
} | LC_ALL=C sort >"${fixture_root}/bundle-source-files"
LC_ALL=C sort "${bundle_source}/source-files.txt" >"${fixture_root}/declared-source-files"
if ! diff -u "${fixture_root}/declared-source-files" \
  "${fixture_root}/bundle-source-files" >"${fixture_root}/source-files-diff"; then
  cat "${fixture_root}/source-files-diff" >&2
  fail "bundle source inventory is incomplete"
fi
while read -r specifier; do
  [[ -n "${specifier}" ]] || continue
  if [[ ! "${specifier}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    fail "manifest declares non-exact dependency version '${specifier}'"
  fi
done < <(awk '/"dependencies"/ { in_dependencies = 1; next }
  /^  }/ { in_dependencies = 0 }
  in_dependencies { gsub(/[",]/, ""); print $2 }' "${manifest}")



if grep -q '"scripts"' "${manifest}"; then
  fail "manifest declares scripts"
fi



for required_setting in \
  "onlyBuiltDependencies: []" \
  "strictDepBuilds: true" \
  "ignoreScripts: true" \
  "enablePrePostScripts: false" \
  "frozenLockfile: true" \
  "autoInstallPeers: false" \
  "verifyStoreIntegrity: true" \
  "minimumReleaseAge: 43200" \
  "minimumReleaseAgeStrict: true" \
  "minimumReleaseAgeIgnoreMissingTime: false" \
  "blockExoticSubdeps: true" \
  "trustLockfile: false" \
  "trustPolicy: no-downgrade"; do
  if ! grep -qF "${required_setting}" "${settings}"; then
    fail "bundle settings do not declare '${required_setting}'"
  fi
done


if ! grep -q '^registry=https://registry.npmjs.org/$' "${bundle_source}/.npmrc"; then
  fail "bundle npmrc does not pin the npm registry"
fi


for script in "${policy_root}/tools/javascript-env.sh" \
  "${policy_root}/tools/install-javascript-bundle.sh" \
  "${bundle_manifest}" \
  "${policy_root}/scripts/check-javascript-bundle.sh"; do
  if grep -nE 'command -v (node|npm|npx|pnpm|corepack)\b' "${script}"; then
    fail "$(basename "${script}") looks up a JavaScript tool on PATH"
  fi
  if grep -nE '^[[:space:]]*(node|npm|npx|pnpm|corepack)[[:space:]]' "${script}"; then
    fail "$(basename "${script}") invokes a JavaScript tool as a bare command"
  fi
done



"${verify_lock}" "${lockfile}" || fail "the checked-in lock was rejected"
locked="${fixture_root}/locked.txt"
awk '/^packages:/ { in_packages = 1; next }
  /^[^[:space:]]/ { in_packages = 0 }
  in_packages && /^  [^[:space:]]/ { gsub(/^  '"'"'?|'"'"'?:$/, ""); print }' \
  "${lockfile}" | LC_ALL=C sort >"${locked}"
inventoried="${fixture_root}/inventoried.txt"
grep -v '^#' "${inventory}" | awk -F'\t' '{ print $1 }' | LC_ALL=C sort >"${inventoried}"
if ! diff -u "${locked}" "${inventoried}" >"${fixture_root}/inventory-diff"; then
  cat "${fixture_root}/inventory-diff" >&2
  fail "the checked-in inventory does not account for exactly the locked packages"
fi
if grep -v '^#' "${inventory}" | awk -F'\t' 'NF != 2 || $2 == ""' | grep -q .; then
  fail "the checked-in inventory has entries without a license"
fi


tampered="${fixture_root}/pnpm-lock.yaml"
expect_rejected "a missing lock" "${verify_lock}" "${fixture_root}/absent.yaml"
expect_rejected "wrong usage" "${verify_lock}"

sed 's/^lockfileVersion: .*/lockfileVersion: '"'"'8.0'"'"'/' "${lockfile}" >"${tampered}"
expect_rejected "an unsupported lock version" "${verify_lock}" "${tampered}"

sed 's/^  autoInstallPeers: false/  autoInstallPeers: true/' "${lockfile}" >"${tampered}"
expect_rejected "an auto-installed peer graph" "${verify_lock}" "${tampered}"

sed 's|^    resolution: {integrity: sha512-|    resolution: {tarball: https://example.invalid/pkg.tgz, integrity: sha1-|' \
  "${lockfile}" >"${tampered}"
expect_rejected "a non-registry resolution" "${verify_lock}" "${tampered}"

{
  printf 'overrides:\n  example: 1.0.0\n'
  cat "${lockfile}"
} >"${tampered}"
expect_rejected "a resolution override" "${verify_lock}" "${tampered}"

sed 's/^        specifier: 3\.9\.5$/        specifier: ^3.9.5/' "${lockfile}" >"${tampered}"
expect_rejected "a non-exact direct specifier" "${verify_lock}" "${tampered}"


expect_rejected "an unmaterialized bundle" "${verify_tree}" "${fixture_root}"
expect_rejected "wrong tree-verifier usage" "${verify_tree}"

tree_fixture="${fixture_root}/tree"
mkdir -p "${tree_fixture}/node_modules"
expect_rejected "a tree with no installation metadata" "${verify_tree}" "${tree_fixture}"

metadata="${tree_fixture}/node_modules/.modules.yaml"
write_metadata() {
  cat >"${metadata}" <<METADATA
{
  "hoistedDependencies": {},
  "packageManager": "pnpm@$1",
  "pendingBuilds": $2,
  "registries": {
    "default": "$3"
  },
  "skipped": []
}
METADATA
}

write_metadata "${pnpm_version}" "[]" "https://registry.npmjs.org/"
"${verify_tree}" "${tree_fixture}" || fail "a clean materialized tree was rejected"

write_metadata "0.0.0" "[]" "https://registry.npmjs.org/"
expect_rejected "a tree installed by an unpinned pnpm" "${verify_tree}" "${tree_fixture}"

write_metadata "${pnpm_version}" '["example"]' "https://registry.npmjs.org/"
expect_rejected "a tree with a pending build" "${verify_tree}" "${tree_fixture}"

write_metadata "${pnpm_version}" "[]" "https://registry.example.invalid/"
expect_rejected "a tree installed from another registry" "${verify_tree}" "${tree_fixture}"

write_metadata "${pnpm_version}" "[]" "https://registry.npmjs.org/"
touch "${tree_fixture}/node_modules/native.node"
expect_rejected "a tree carrying a prebuilt binary" "${verify_tree}" "${tree_fixture}"
rm "${tree_fixture}/node_modules/native.node"



ln -s .pnpm/example/native.node "${tree_fixture}/node_modules/linked.node"
expect_rejected "a tree linking to a prebuilt binary" "${verify_tree}" "${tree_fixture}"
rm "${tree_fixture}/node_modules/linked.node"

ln -s ../../outside "${tree_fixture}/node_modules/escaping"
expect_rejected "a tree with a symlink that climbs out of the bundle" "${verify_tree}" "${tree_fixture}"
rm "${tree_fixture}/node_modules/escaping"

ln -s /usr/bin "${tree_fixture}/node_modules/absolute"
expect_rejected "a tree with an absolute symlink" "${verify_tree}" "${tree_fixture}"
rm "${tree_fixture}/node_modules/absolute"

ln -s .pnpm/example/node_modules/example "${tree_fixture}/node_modules/example"
"${verify_tree}" "${tree_fixture}" || fail "a contained package symlink was rejected"
rm "${tree_fixture}/node_modules/example"



for prohibited_construct in process.cwd 'require('; do
  if grep -nF "${prohibited_construct}" "${runner_sources[@]}"; then
    fail "the runner uses '${prohibited_construct}'"
  fi
done




audit_source="${bundle_source}/audit.mjs"
for module_source in "${runner_sources[@]}"; do
  [[ "${module_source}" == "${audit_source}" ]] && continue
  if grep -nF child_process "${module_source}"; then
    fail "$(basename "${module_source}") starts a process"
  fi
done
if ! grep -qxF 'import { spawnSync } from "node:child_process";' "${audit_source}"; then
  fail "the native audit imports more of child_process than one bounded spawn"
fi
audit_spawn="$(tr -d '\n' <"${audit_source}" | grep -o 'spawnSync([^,]*,' | tr -s ' ')"
if [[ "${audit_spawn}" != "spawnSync( process.execPath," ]]; then
  fail "the native audit starts something other than the pinned runtime: ${audit_spawn}"
fi


if ! grep -qF '"pnpm.cjs",' "${audit_source}" || ! grep -qF 'bundleDirectory,' "${audit_source}"; then
  fail "the native audit does not resolve the pinned pnpm beside the installed bundle"
fi





while read -r deferred_specifier; do
  case "${deferred_specifier}" in
    '"./node_modules/'*) ;;
    *) fail "the runner imports ${deferred_specifier}" ;;
  esac
done < <(cat "${runner_sources[@]}" | tr -d '\n' | grep -o 'import([^)]*)' |
  sed -e 's/^import( *//' -e 's/ *)$//')



"${bundle_manifest}" verify "${javascript_bundle_dir}" ||
  fail "the installed bundle does not match its manifest"
expect_rejected "wrong manifest usage" "${bundle_manifest}" verify
expect_rejected "an unknown manifest mode" "${bundle_manifest}" inspect "${javascript_bundle_dir}"
expect_rejected "a missing bundle directory" "${bundle_manifest}" verify "${fixture_root}/absent"

manifest_fixture="${fixture_root}/bundle"
mkdir -p "${manifest_fixture}/node_modules"
cp "${manifest}" "${runner_sources[@]}" "${manifest_fixture}/"
printf 'installed\n' >"${manifest_fixture}/node_modules/installed.js"
"${bundle_manifest}" write "${manifest_fixture}" || fail "writing a bundle manifest failed"
"${bundle_manifest}" verify "${manifest_fixture}" || fail "a freshly written manifest was rejected"



sed 's/"node": "[^"]*"/"node": "0.0.0"/' \
  "${manifest_fixture}/${javascript_bundle_manifest_name}" >"${fixture_root}/changed-manifest.json"
cp "${fixture_root}/changed-manifest.json" "${manifest_fixture}/${javascript_bundle_manifest_name}"
expect_rejected "a changed manifest field" "${bundle_manifest}" verify "${manifest_fixture}"
"${bundle_manifest}" write "${manifest_fixture}" || fail "restoring the fixture manifest failed"

printf 'changed\n' >"${manifest_fixture}/node_modules/installed.js"
expect_rejected "a changed installed file" "${bundle_manifest}" verify "${manifest_fixture}"
printf 'installed\n' >"${manifest_fixture}/node_modules/installed.js"

printf 'added\n' >"${manifest_fixture}/node_modules/added.js"
expect_rejected "an added installed file" "${bundle_manifest}" verify "${manifest_fixture}"
rm "${manifest_fixture}/node_modules/added.js"

rm "${manifest_fixture}/node_modules/installed.js"
expect_rejected "a removed installed file" "${bundle_manifest}" verify "${manifest_fixture}"
printf 'installed\n' >"${manifest_fixture}/node_modules/installed.js"



printf 'other\n' >"${manifest_fixture}/node_modules/other.js"
ln -s installed.js "${manifest_fixture}/node_modules/tool.js"
expect_rejected "an added symlink" "${bundle_manifest}" verify "${manifest_fixture}"
"${bundle_manifest}" write "${manifest_fixture}" || fail "writing a manifest over a symlink failed"
"${bundle_manifest}" verify "${manifest_fixture}" || fail "a symlinked bundle was rejected"
rm "${manifest_fixture}/node_modules/tool.js"
ln -s other.js "${manifest_fixture}/node_modules/tool.js"
expect_rejected "a retargeted symlink" "${bundle_manifest}" verify "${manifest_fixture}"
rm "${manifest_fixture}/node_modules/tool.js" "${manifest_fixture}/node_modules/other.js"
"${bundle_manifest}" write "${manifest_fixture}" || fail "rewriting the fixture manifest failed"

sed 's/"sourceDigest": "[0-9a-f]*"/"sourceDigest": "0000000000000000000000000000000000000000000000000000000000000000"/' \
  "${manifest_fixture}/${javascript_bundle_manifest_name}" >"${fixture_root}/foreign-manifest.json"
cp "${fixture_root}/foreign-manifest.json" "${manifest_fixture}/${javascript_bundle_manifest_name}"
expect_rejected "a bundle installed from other checked-in source" \
  "${bundle_manifest}" verify "${manifest_fixture}"

rm "${manifest_fixture}/${javascript_bundle_manifest_name}"
expect_rejected "a bundle with no manifest" "${bundle_manifest}" verify "${manifest_fixture}"

portable_source="${fixture_root}/portable-source"
portable_bundle="${fixture_root}/portable-bundle"
javascript_copy_bundle_source "${portable_source}"
javascript_sealed_pnpm "${portable_source}" install --offline --frozen-lockfile \
  --ignore-scripts --config.nodeLinker=hoisted >/dev/null
"${policy_root}/.tools/bin/code-polishy" --policy-root "${policy_root}" \
  release-manifest materialize --source "${portable_source}" \
  --destination "${portable_bundle}" || fail "portable release materialization failed"
if find "${portable_bundle}" -type l | grep -q .; then
  fail "portable release materialization retained links"
fi
"${bundle_manifest}" write "${portable_bundle}" ||
  fail "portable release manifest creation failed"
printf '%s\n' '{"protocolVersion":3,"operation":"provenance"}' |
  javascript_sealed_run "${javascript_node}" "${portable_bundle}/runner.mjs" \
    >"${fixture_root}/portable-provenance" ||
  fail "portable release bundle did not answer provenance"
if ! grep -q '"operation":"provenance"' "${fixture_root}/portable-provenance"; then
  fail "portable release bundle returned incomplete provenance"
fi

echo "test-javascript-bundle: all checks passed"
