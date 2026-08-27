#!/usr/bin/env bash
set -euo pipefail

# Offline contract tests for the fixed runner entry point of the sealed,
# policy-owned JavaScript tool bundle.
#
# The operations that read selected target source are exercised here against the
# installed bundle, together with the protocol, environment, and launch contract
# every operation answers under: what each one reports, what it refuses, and
# what it never reads, executes, or writes. The operations that ask about a pnpm
# project as a whole are covered by scripts/test-javascript-project.sh, and the
# checked-in manifest, lock, settings, and installed inventory by
# scripts/test-javascript-bundle.sh.

javascript_test_name="test-javascript-runner"
# shellcheck source=scripts/javascript-runner-test-env.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/javascript-runner-test-env.sh"

manifest="${bundle_source}/package.json"
installed_manifest="${javascript_bundle_dir}/${javascript_bundle_manifest_name}"

# Provenance reports the installed bytes: the manifest digest, both pinned
# runtime versions, and every tool version confirmed against the package
# installed beside the runner.
response="${fixture_root}/response.json"
printf '{"protocolVersion":2,"operation":"provenance"}' | run_runner >"${response}" ||
  fail "the runner rejected a well-formed provenance request"
expect_reported() {
  if ! grep -qF "$1" "${response}"; then
    fail "the runner did not report $1"
  fi
}
expect_reported "\"bundleDigest\":\"$(awk -F'"' '$2 == "bundleDigest" { print $4 }' "${installed_manifest}")\""
expect_reported "\"node\":\"${node_version}\""
expect_reported "\"pnpm\":\"${pnpm_version}\""
while read -r name version; do
  [[ -n "${name}" ]] || continue
  expect_reported "\"${name}\":\"${version}\""
done < <(awk '/"dependencies"/ { in_dependencies = 1; next }
  /^  }/ { in_dependencies = 0 }
  in_dependencies { gsub(/[",]/, ""); sub(/:$/, "", $1); print $1, $2 }' "${manifest}")

# A target tree that shadows a policy tool changes nothing: the runner reports
# the version installed beside it, not the one in the working directory.
decoy="${fixture_root}/decoy/node_modules/prettier"
mkdir -p "${decoy}"
printf '{"name":"prettier","version":"0.0.0-decoy"}\n' >"${decoy}/package.json"
decoy_response="$(cd "${fixture_root}/decoy" && printf '{"protocolVersion":2,"operation":"provenance"}' | run_runner)"
if [[ "${decoy_response}" != "$(cat "${response}")" ]]; then
  fail "the runner resolved a tool from the working directory"
fi

# Formatting is decided by the sealed central configuration over the selected
# files of a target tree, and by nothing the target ships.
target="${fixture_root}/target"
mkdir -p "${target}/src"
printf 'const value   =  1\n' >"${target}/src/unformatted.ts"
printf 'const value = 1;\n' >"${target}/src/formatted.ts"
printf 'x\n' >"${target}/src/opaque.bin"
printf '\377\376 not text\n' >"${target}/src/binary.ts"
ln -s formatted.ts "${target}/src/linked.ts"
# A target that ships its own Prettier configuration changes nothing: the
# runner never resolves configuration, so these are inert bytes.
printf '{"semi":false,"printWidth":20}\n' >"${target}/.prettierrc"
printf 'src\n' >"${target}/.prettierignore"

format_request() {
  printf '{"protocolVersion":2,"operation":"%s","root":"%s","paths":%s}' "$1" "${target}" "$2"
}
selection='["src/unformatted.ts","src/formatted.ts","src/opaque.bin","src/binary.ts","src/linked.ts"]'
format_response="${fixture_root}/format.json"
format_request format "${selection}" | run_runner >"${format_response}" ||
  fail "the runner rejected a well-formed format request"
if ! grep -qF '"changed":["src/unformatted.ts"]' "${format_response}"; then
  fail "format did not report exactly the file the sealed configuration rewrites: $(cat "${format_response}")"
fi
for undecided in src/opaque.bin src/binary.ts src/linked.ts; do
  if ! grep -qF "\"path\":\"${undecided}\"" "${format_response}"; then
    fail "format did not report ${undecided} as undecided: $(cat "${format_response}")"
  fi
done
if [[ "$(cat "${target}/src/unformatted.ts")" != 'const value   =  1' ]]; then
  fail "format rewrote a file it was only asked to check"
fi

# Writing is a separate operation, and it rewrites exactly the reported files.
format_request format-write "${selection}" | run_runner >"${format_response}" ||
  fail "the runner rejected a well-formed format-write request"
if ! grep -qF '"changed":["src/unformatted.ts"]' "${format_response}"; then
  fail "format-write did not report the file it rewrote: $(cat "${format_response}")"
fi
if [[ "$(cat "${target}/src/unformatted.ts")" != 'const value = 1;' ]]; then
  fail "format-write did not apply the sealed configuration: $(cat "${target}/src/unformatted.ts")"
fi
if [[ "$(cat "${target}/src/formatted.ts")" != 'const value = 1;' ]]; then
  fail "format-write rewrote an already formatted file"
fi

# A selected path is contained by what it really names, not by how it is
# spelled: a directory component that is a link out of the repository names a
# file in another tree, so it is undecidable rather than read, and the write
# operation never reaches it.
outside="${fixture_root}/outside"
mkdir -p "${outside}"
printf 'const value   =  1\n' >"${outside}/loose.ts"
ln -s "${outside}" "${target}/linked-directory"
escape_response="${fixture_root}/escape.json"
for operation in format format-write; do
  format_request "${operation}" '["linked-directory/loose.ts"]' |
    run_runner >"${escape_response}" ||
    fail "the runner failed the ${operation} request for a linked directory"
  if ! grep -qF '{"path":"linked-directory/loose.ts","reason":"the path resolves outside the target tree"}' \
    "${escape_response}"; then
    fail "${operation} read through a linked directory: $(cat "${escape_response}")"
  fi
done
if [[ "$(cat "${outside}/loose.ts")" != 'const value   =  1' ]]; then
  fail "format-write rewrote a file outside the target tree"
fi

# Linting is decided by the sealed central configuration under exactly the
# budgets and activation the request carries.
lint_source="${target}/lint"
mkdir -p "${lint_source}"
cat >"${lint_source}/deep.ts" <<'SOURCE'
export function process(first: number, second: number, third: number) {
  if (first > 0) {
    if (second > 0) {
      if (third > 0) {
        return third;
      }
    }
  }
  return first + second;
}
SOURCE
cat >"${lint_source}/directive.ts" <<'SOURCE'
// eslint-disable-next-line max-params
export function wide(first: number, second: number, third: number) {
  return first + second + third;
}
SOURCE
printf 'export function broken( {\n' >"${lint_source}/broken.ts"
cat >"${lint_source}/panel.tsx" <<'SOURCE'
export function Panel({ ready }: { ready: boolean }) {
  if (ready) {
    const [value] = useState(0);
    return <img src={value} />;
  }
  return null;
}
SOURCE
# A target that ships its own ESLint configuration changes nothing either: the
# runner resolves no configuration, so these are inert bytes.
printf 'export default [{rules:{"max-depth":"off","max-params":"off"}}];\n' >"${target}/eslint.config.mjs"

lint_request() {
  printf '{"protocolVersion":2,"operation":"lint","root":"%s","paths":%s,"limits":%s,"activation":%s}' \
    "${target}" "$1" "$2" "$3"
}
lint_response="${fixture_root}/lint.json"
lint_request '["lint/deep.ts","lint/directive.ts","lint/broken.ts","src/opaque.bin"]' \
  '{"complexity":9,"depth":2,"parameters":2}' '{"reactHooks":false,"jsxAccessibility":false}' |
  run_runner >"${lint_response}" || fail "the runner rejected a well-formed lint request"
for expected in '"rule":"max-depth"' '"rule":"max-params"' '"path":"lint/deep.ts"' '"path":"lint/directive.ts"'; do
  if ! grep -qF "${expected}" "${lint_response}"; then
    fail "lint did not report ${expected}: $(cat "${lint_response}")"
  fi
done
# The inline directive is reported and never honored: the rule it names still
# failed on the line below it.
if ! grep -qF '"directives":[{"path":"lint/directive.ts","line":1}]' "${lint_response}"; then
  fail "lint did not report the inline directive: $(cat "${lint_response}")"
fi
# A file the linter could not parse and a file type it does not analyze are
# missing coverage, not clean files.
for undecided in lint/broken.ts src/opaque.bin; do
  if ! grep -qF "\"path\":\"${undecided}\"" "${lint_response}"; then
    fail "lint did not report ${undecided} as undecided: $(cat "${lint_response}")"
  fi
done

# The framework rules run only when the request activates them, and the file is
# otherwise linted exactly the same way.
generous='{"complexity":20,"depth":10,"parameters":10}'
lint_request '["lint/panel.tsx"]' "${generous}" '{"reactHooks":false,"jsxAccessibility":false}' |
  run_runner >"${lint_response}" || fail "the runner rejected an unactivated lint request"
if ! grep -qF '"findings":[]' "${lint_response}" || ! grep -qF '"unsupported":[]' "${lint_response}"; then
  fail "unactivated lint reported framework findings: $(cat "${lint_response}")"
fi
lint_request '["lint/panel.tsx"]' "${generous}" '{"reactHooks":true,"jsxAccessibility":true}' |
  run_runner >"${lint_response}" || fail "the runner rejected an activated lint request"
for expected in '"rule":"react-hooks/rules-of-hooks"' '"rule":"jsx-a11y/alt-text"'; do
  if ! grep -qF "${expected}" "${lint_response}"; then
    fail "activated lint did not report ${expected}: $(cat "${lint_response}")"
  fi
done

# Type and syntax checking is decided by the contained project the request
# names, read as JSON/JSONC data and never executed.
project_source="${target}/project"
mkdir -p "${project_source}/src"
cat >"${project_source}/tsconfig.json" <<'SOURCE'
{
  // A comment: a project is contained JSONC data, not code.
  "compilerOptions": { "strict": true, "target": "ES2022", "module": "Preserve" },
  "include": ["src"]
}
SOURCE
printf 'export const value: number = "text";\n' >"${project_source}/src/wrong.ts"
printf 'export const ok: number = 1;\n' >"${project_source}/src/ok.ts"
printf 'export const stray = 1;\n' >"${project_source}/stray.ts"

typecheck_request() {
  printf '{"protocolVersion":2,"operation":"typecheck","root":"%s","paths":%s,"project":"%s"}' \
    "${target}" "$1" "$2"
}
typecheck_response="${fixture_root}/typecheck.json"
typecheck_request \
  '["project/src/wrong.ts","project/src/ok.ts","project/stray.ts"]' project/tsconfig.json |
  run_runner >"${typecheck_response}" || fail "the runner rejected a well-formed typecheck request"
for expected in '"path":"project/src/wrong.ts"' '"line":1,"column":14' '"code":2322'; do
  if ! grep -qF "${expected}" "${typecheck_response}"; then
    fail "typecheck did not report ${expected}: $(cat "${typecheck_response}")"
  fi
done
# Coverage is reported, never assumed: the file the project does not include was
# not checked, so it is absent from what the program covered.
if ! grep -qF '"covered":["project/src/wrong.ts","project/src/ok.ts"]' "${typecheck_response}"; then
  fail "typecheck did not report exactly the covered files: $(cat "${typecheck_response}")"
fi
if [[ -e "${project_source}/tsconfig.tsbuildinfo" ]] || [[ -n "$(find "${project_source}" -name '*.js' -print -quit)" ]]; then
  fail "typecheck emitted into the target tree"
fi

# An extension chain that stays inside the repository is contained data, so it
# is followed: the strictness the base declares decides the extending project.
mkdir -p "${project_source}/strict"
printf '{"compilerOptions":{"strict":true,"target":"ES2022","module":"Preserve"}}\n' \
  >"${project_source}/tsconfig.base.json"
printf '{"extends":"./tsconfig.base.json","include":["strict"]}\n' \
  >"${project_source}/tsconfig.extended.json"
printf 'export function identity(value) {\n  return value;\n}\n' \
  >"${project_source}/strict/implicit.ts"
typecheck_request '["project/strict/implicit.ts"]' project/tsconfig.extended.json |
  run_runner >"${typecheck_response}" || fail "the runner rejected a contained extension chain"
if ! grep -qF '"code":7006' "${typecheck_response}"; then
  fail "the extended project did not inherit its base strictness: $(cat "${typecheck_response}")"
fi

# noCheck asks the compiler to parse a project and report nothing about its
# types. A project declaring it would be reported as covered and clean while
# nothing about it was checked, so whether a policy run checks is decided here
# rather than by the project.
printf '{"compilerOptions":{"noCheck":true,"target":"ES2022","module":"Preserve"},"include":["src"]}\n' \
  >"${project_source}/tsconfig.nocheck.json"
typecheck_request '["project/src/wrong.ts"]' project/tsconfig.nocheck.json |
  run_runner >"${typecheck_response}" ||
  fail "the runner rejected a project declaring noCheck"
if ! grep -qF '"code":2322' "${typecheck_response}"; then
  fail "a project declaring noCheck reported no diagnostics: $(cat "${typecheck_response}")"
fi

# A program contains the target tree and the policy-owned library declarations
# installed beside the runner, and nothing else. A specifier that climbs out of
# the repository names no module rather than pulling another tree into the
# checked program, and no diagnostic is ever reported from outside the target.
printf 'export const outside: number = "text";\n' >"${outside}/escaped.ts"
printf 'import { outside } from "../../../outside/escaped.js";\nexport const used = outside;\n' \
  >"${project_source}/src/escape.ts"
typecheck_request '["project/src/escape.ts"]' project/tsconfig.json |
  run_runner >"${typecheck_response}" ||
  fail "the runner rejected a project importing outside the repository"
if ! grep -qF '"code":2307' "${typecheck_response}"; then
  fail "a specifier leaving the repository resolved: $(cat "${typecheck_response}")"
fi
if grep -qF 'escaped.ts' "${typecheck_response}"; then
  fail "the checked program read outside the target tree: $(cat "${typecheck_response}")"
fi
rm "${project_source}/src/escape.ts"

# A project this operation cannot analyze is refused with its own reason rather
# than read under a guess, and never by loading anything executable.
expect_project_refused() {
  local description="$1"
  local project="$2"
  local reason="$3"
  typecheck_request '["project/src/ok.ts"]' "${project}" |
    run_runner >"${typecheck_response}" || fail "the runner failed the ${description} request"
  if ! grep -qF "${reason}" "${typecheck_response}"; then
    fail "${description} was not refused: $(cat "${typecheck_response}")"
  fi
  if ! grep -qF '"diagnostics":[]' "${typecheck_response}"; then
    fail "${description} still reported diagnostics: $(cat "${typecheck_response}")"
  fi
}
printf '{"references":[{"path":"./other"}],"include":["src"]}\n' \
  >"${project_source}/tsconfig.references.json"
expect_project_refused "a project reference" project/tsconfig.references.json \
  "the project declares references"
printf '{"compilerOptions":{"plugins":[{"name":"a-plugin"}]},"include":["src"]}\n' \
  >"${project_source}/tsconfig.plugins.json"
expect_project_refused "a compiler plug-in" project/tsconfig.plugins.json \
  "the project declares compiler plug-ins"
printf '{"compilerOptions":{"strict":true}}\n' >"${fixture_root}/outside-tsconfig.json"
printf '{"extends":"../../outside-tsconfig.json","include":["src"]}\n' \
  >"${project_source}/tsconfig.outside.json"
expect_project_refused "an extension chain leaving the repository" project/tsconfig.outside.json \
  "the project extends configuration outside the repository"
# The same refusal by what the chain really names rather than by how it is
# spelled: every segment of this one is inside the project, and the directory
# one of them names is not.
printf '{"compilerOptions":{"strict":true}}\n' >"${outside}/linked-tsconfig.json"
ln -s "${outside}" "${project_source}/linked-base"
printf '{"extends":"./linked-base/linked-tsconfig.json","include":["src"]}\n' \
  >"${project_source}/tsconfig.linked.json"
expect_project_refused "an extension chain leaving the repository through a link" \
  project/tsconfig.linked.json "the project extends configuration outside the repository"
printf '{"include":["src",\n' >"${project_source}/tsconfig.broken.json"
expect_project_refused "a malformed project" project/tsconfig.broken.json "expected"
expect_project_refused "an absent project" project/tsconfig.absent.json "the file is unreadable"
# No refusal reason names a host path, whatever the compiler called the file.
if grep -qF "${fixture_root}" "${typecheck_response}"; then
  fail "a typecheck result leaked a host path: $(cat "${typecheck_response}")"
fi

# Dead code is decided over a whole tree of packages, under a configuration
# Code Polishy generates from the request and the target cannot influence.
deadcode_target="${fixture_root}/deadcode"
mkdir -p "${deadcode_target}/src" "${deadcode_target}/packages/web/src"
printf '{"name":"fixture","private":true,"version":"0.0.0","type":"module"}\n' \
  >"${deadcode_target}/package.json"
cat >"${deadcode_target}/src/index.ts" <<'SOURCE'
import { used } from "./helper.js";
import { shared } from "../packages/web/src/lib.js";
export const boot = () => used() + shared();
SOURCE
cat >"${deadcode_target}/src/helper.ts" <<'SOURCE'
export const used = () => 1;
export const unused = () => 2;
export type Spare = string;
SOURCE
printf 'export const orphan = 3;\n' >"${deadcode_target}/src/orphan.ts"
# A target that ships its own dead-code configuration changes nothing: this one
# declares the orphan an entry point, and the runner never reads it.
printf '{"entry":["src/orphan.ts"],"project":["src/**"]}\n' >"${deadcode_target}/knip.json"
printf '{"name":"web","private":true,"version":"0.0.0","dependencies":{"vite":"7.0.0"}}\n' \
  >"${deadcode_target}/packages/web/package.json"
printf 'export const shared = () => 4;\n' >"${deadcode_target}/packages/web/src/lib.ts"
printf 'export const stranded = () => 5;\n' >"${deadcode_target}/packages/web/src/spare.ts"
# A framework configuration file is target code. An analyzer plug-in would load
# this one to learn the framework's entry points; every plug-in is disabled, so
# nothing here ever runs.
executed_marker="${fixture_root}/vite-config-executed"
cat >"${deadcode_target}/packages/web/vite.config.mts" <<SOURCE
import { writeFileSync } from "node:fs";
writeFileSync("${executed_marker}", "executed");
export default {};
SOURCE

deadcode_request() {
  printf '{"protocolVersion":2,"operation":"deadcode","root":"%s","directory":"%s","workspaces":%s}' \
    "${deadcode_target}" "$1" "$2"
}
deadcode_workspaces='[{"root":".","entry":["src/index.ts"],"project":["src/index.ts","src/helper.ts","src/orphan.ts"]},
  {"root":"packages/web","entry":[],"project":["packages/web/src/lib.ts","packages/web/src/spare.ts"]}]'
deadcode_response="${fixture_root}/deadcode.json"
deadcode_request . "${deadcode_workspaces}" |
  run_runner >"${deadcode_response}" || fail "the runner rejected a well-formed deadcode request"
# The file no entry point reaches is dead, and so is every export nothing uses.
# The sibling package's export is used, because the tree is analyzed at once.
if ! grep -qF '"unusedFiles":["packages/web/src/spare.ts","src/orphan.ts"]' "${deadcode_response}"; then
  fail "deadcode did not report exactly the unreachable files: $(cat "${deadcode_response}")"
fi
for expected in '"symbol":"unused","kind":"exports"' '"symbol":"Spare","kind":"types"'; do
  if ! grep -qF "${expected}" "${deadcode_response}"; then
    fail "deadcode did not report ${expected}: $(cat "${deadcode_response}")"
  fi
done
if grep -qF '"symbol":"used"' "${deadcode_response}" || grep -qF '"symbol":"shared"' "${deadcode_response}"; then
  fail "deadcode reported a used export: $(cat "${deadcode_response}")"
fi
if [[ -e "${executed_marker}" ]]; then
  fail "the dead-code analysis executed target configuration"
fi
# The analysis leaves nothing behind in the tree it read.
if [[ -e "${deadcode_target}/node_modules" ]] ||
  [[ -n "$(find "${deadcode_target}" -name 'policy-knip.json' -print -quit)" ]]; then
  fail "the dead-code analysis wrote into the target tree"
fi

# A tree rooted below the repository is analyzed under its own package, so the
# packages and files the request names reach the analyzer named the way that
# tree sees them rather than the way the repository does.
mkdir -p "${deadcode_target}/tools/kit"
printf '{"name":"kit","private":true,"version":"0.0.0","type":"module"}\n' \
  >"${deadcode_target}/tools/kit/package.json"
printf 'import { helper } from "./helper.mjs";\nexport const run = () => helper();\n' \
  >"${deadcode_target}/tools/kit/entry.mjs"
printf 'export const helper = () => 1;\nexport const idle = () => 2;\n' \
  >"${deadcode_target}/tools/kit/helper.mjs"
printf 'export const detached = () => 3;\n' >"${deadcode_target}/tools/kit/detached.mjs"
deadcode_request tools/kit \
  '[{"root":"tools/kit","entry":["tools/kit/entry.mjs"],"project":["tools/kit/entry.mjs","tools/kit/helper.mjs","tools/kit/detached.mjs"]}]' |
  run_runner >"${deadcode_response}" || fail "the runner rejected a nested tree"
if ! grep -qF '"unusedFiles":["tools/kit/detached.mjs"]' "${deadcode_response}"; then
  fail "a nested tree was not analyzed under its own package: $(cat "${deadcode_response}")"
fi
if ! grep -qF '"symbol":"idle"' "${deadcode_response}"; then
  fail "a nested tree reported no unused export: $(cat "${deadcode_response}")"
fi

# Coverage is reported, never assumed: a selected file the analyzer cannot
# address is named with its reason instead of counting as reachable.
deadcode_request . '[{"root":".","entry":[],"project":["src/index.ts","src/absent.ts","src/notes.md"]}]' |
  run_runner >"${deadcode_response}" || fail "the runner rejected an undecidable deadcode selection"
if ! grep -qF '"covered":["src/index.ts"]' "${deadcode_response}"; then
  fail "deadcode did not report exactly what it analyzed: $(cat "${deadcode_response}")"
fi
for undecided in '"path":"src/absent.ts"' '"path":"src/notes.md"'; do
  if ! grep -qF "${undecided}" "${deadcode_response}"; then
    fail "deadcode did not report ${undecided} as undecided: $(cat "${deadcode_response}")"
  fi
done
# A selected file reached through a directory that links out of the repository
# is source another tree owns, so it is named as undecidable rather than
# analyzed as if the package contained it.
printf 'export const elsewhere = 1;\n' >"${outside}/elsewhere.ts"
ln -s "${outside}" "${deadcode_target}/src/linked-directory"
deadcode_request . \
  '[{"root":".","entry":[],"project":["src/index.ts","src/linked-directory/elsewhere.ts"]}]' |
  run_runner >"${deadcode_response}" ||
  fail "the runner rejected a selection reaching through a linked directory"
if ! grep -qF '{"path":"src/linked-directory/elsewhere.ts","reason":"the path resolves outside the target tree"}' \
  "${deadcode_response}"; then
  fail "deadcode analyzed a file outside the target tree: $(cat "${deadcode_response}")"
fi
rm "${deadcode_target}/src/linked-directory"
if grep -qF "${fixture_root}" "${deadcode_response}"; then
  fail "a deadcode result leaked a host path: $(cat "${deadcode_response}")"
fi
# The analyzer resolves the imports of the files it was given, and each of those
# is a path it would otherwise read wherever it landed. A module outside the
# repository reads as absent, so what is written there cannot decide whether
# target source is reachable: the file only that module reaches is unreachable.
reachability_target="${fixture_root}/reachability"
mkdir -p "${reachability_target}/src"
printf '{"name":"reachability","version":"0.0.0"}\n' >"${reachability_target}/package.json"
printf 'export { helper } from "%s/src/helper.js";\n' "${reachability_target}" \
  >"${outside}/bridge.ts"
printf 'import { helper } from "../../outside/bridge.js";\nexport const used = helper;\n' \
  >"${reachability_target}/src/index.ts"
printf 'export const helper = 1;\n' >"${reachability_target}/src/helper.ts"
printf '{"protocolVersion":2,"operation":"deadcode","root":"%s","directory":".","workspaces":%s}' \
  "${reachability_target}" \
  '[{"root":".","entry":["src/index.ts"],"project":["src/index.ts","src/helper.ts"]}]' |
  run_runner >"${deadcode_response}" ||
  fail "the runner rejected a selection importing outside the repository"
if ! grep -qF '"unusedFiles":["src/helper.ts"]' "${deadcode_response}"; then
  fail "a module outside the target tree kept target source alive: $(cat "${deadcode_response}")"
fi

# A directory that declares no package is refused with an explanation rather
# than reported as having no dead code.
mkdir -p "${deadcode_target}/detached"
printf 'export const stray = 1;\n' >"${deadcode_target}/detached/stray.ts"
deadcode_request detached '[{"root":"detached","entry":[],"project":["detached/stray.ts"]}]' |
  run_runner >"${deadcode_response}" && fail "a directory without a package was analyzed"
if ! grep -qF 'Unable to find package.json' "${deadcode_response}"; then
  fail "the refusal did not name the missing package: $(cat "${deadcode_response}")"
fi

# Nothing about the analyzed tree may be left to the runner, so a request that
# does not decide it completely is refused.
expect_runner_rejected "a deadcode request without packages" \
  "$(deadcode_request . '[]')"
expect_runner_rejected "a package outside the analyzed directory" \
  "$(deadcode_request packages/web '[{"root":".","entry":[],"project":["src/index.ts"]}]')"
expect_runner_rejected "a package that selects nothing" \
  "$(deadcode_request . '[{"root":".","entry":[],"project":[]}]')"
expect_runner_rejected "a duplicate package" \
  "$(deadcode_request . '[{"root":".","entry":[],"project":["src/index.ts"]},{"root":".","entry":[],"project":["src/helper.ts"]}]')"
expect_runner_rejected "a file the package does not contain" \
  "$(deadcode_request . '[{"root":"packages/web","entry":[],"project":["src/index.ts"]}]')"
expect_runner_rejected "an unanalyzed entry point" \
  "$(deadcode_request . '[{"root":".","entry":["src/helper.ts"],"project":["src/index.ts"]}]')"
expect_runner_rejected "an unknown package field" \
  "$(deadcode_request . '[{"root":".","entry":[],"project":["src/index.ts"],"ignore":["x"]}]')"

# Import facts are read from the selected source and resolved against the target
# tree. The operation reports which file a specifier names; whether that edge is
# allowed is a Go decision this runner knows nothing about.
imports_target="${fixture_root}/imports"
mkdir -p "${imports_target}/web" "${imports_target}/domain" "${imports_target}/packages/ui/src" \
  "${imports_target}/node_modules/left-pad" "${imports_target}/node_modules/@scope" \
  "${fixture_root}/outside"
printf 'export const model = "m";\n' >"${imports_target}/domain/model.ts"
printf 'export type Kind = string;\n' >"${imports_target}/web/lazy.ts"
printf '<template />\n' >"${imports_target}/web/widget.vue"
printf 'export const outside = 1;\n' >"${fixture_root}/outside/thing.ts"
printf '{"name":"left-pad","version":"1.0.0","main":"index.js"}\n' \
  >"${imports_target}/node_modules/left-pad/package.json"
printf 'module.exports = 1;\n' >"${imports_target}/node_modules/left-pad/index.js"
printf '{"name":"@scope/ui","version":"0.0.0","exports":{".":"./src/index.ts"}}\n' \
  >"${imports_target}/packages/ui/package.json"
printf 'export const ui = 1;\n' >"${imports_target}/packages/ui/src/index.ts"
# A workspace package is reached through a link, exactly as a package manager
# installs one, so the file the specifier names is the sibling package's source.
ln -s ../../packages/ui "${imports_target}/node_modules/@scope/ui"
# A link is followed to whatever it really names, so one installed against
# another checkout reaches nothing inside this target.
mkdir -p "${fixture_root}/outside/linked"
printf '{"name":"@scope/linked","version":"1.0.0","main":"index.js"}\n' \
  >"${fixture_root}/outside/linked/package.json"
printf 'module.exports = 1;\n' >"${fixture_root}/outside/linked/index.js"
ln -s "${fixture_root}/outside/linked" "${imports_target}/node_modules/@scope/linked"
cat >"${imports_target}/web/app.ts" <<'SOURCE'
import { model } from "../domain/model.js";
import left from "left-pad";
import { ui } from "@scope/ui";
import absent from "@scope/absent";
import linked from "@scope/linked";
import outside from "../../outside/thing.js";
const lazy = await import("./lazy.ts");
const computed = await import(`./${model}.ts`);
export type Kind = import("./lazy.ts").Kind;
import subpath from "left-pad/index.js";
import { join } from "node:path";
import { createHash } from "crypto";
import aliased from "@/lib/thing";
import internal from "#internal";
export { model, left, ui, absent, linked, outside, lazy, computed };
export { subpath, join, createHash, aliased, internal };
SOURCE
printf 'const { model } = require("../domain/model.js");\nmodule.exports = model;\n' \
  >"${imports_target}/web/legacy.cjs"

imports_response="${fixture_root}/imports.json"
printf '{"protocolVersion":2,"operation":"imports","root":"%s","paths":["web/app.ts","web/legacy.cjs","web/widget.vue"]}' \
  "${imports_target}" | run_runner >"${imports_response}" ||
  fail "the runner rejected a well-formed imports request"
expect_import() {
  if ! grep -qF "\"specifier\":\"$1\",\"resolved\":\"$2\",\"package\":\"$3\"" "${imports_response}"; then
    fail "imports did not report $1 as $2 from package '$3': $(cat "${imports_response}")"
  fi
}
# The extension a TypeScript package writes is not the extension it ships, and
# a package's exports map decides which of its files an import names. Both are
# the compiler's answers rather than a path rewrite. A path names no package.
expect_import "../domain/model.js" "domain/model.ts" ""
expect_import "@scope/ui" "packages/ui/src/index.ts" "@scope/ui"
# An installed package resolves inside the tree without being governed by it,
# and a package the target never installed resolves to nothing at all. Both
# name the package they reach, because which package a specifier names is not
# the same question as which file it resolves to.
expect_import "left-pad" "node_modules/left-pad/index.js" "left-pad"
expect_import "@scope/absent" "" "@scope/absent"
# A subpath of a package is that package: which of its files the subpath selects
# is resolution, and the reported name is only which package was reached.
expect_import "left-pad/index.js" "node_modules/left-pad/index.js" "left-pad"
# A module the runtime itself provides is no installed package, written either
# way, and neither is a specifier that is no package name at all.
expect_import "node:path" "" ""
expect_import "crypto" "" ""
expect_import "@/lib/thing" "" ""
expect_import "#internal" "" ""
# A specifier that climbs out of the repository names nothing, and neither does
# one installed against another checkout: the resolution host reads only what is
# really inside the declared root, however a path arrives there.
expect_import "../../outside/thing.js" "" ""
expect_import "@scope/linked" "" "@scope/linked"
# A dynamic import with a written specifier is an import like any other, and so
# is a literal require. A type-only import is one too.
expect_import "./lazy.ts" "web/lazy.ts" ""
if [[ "$(grep -oF '"specifier":"./lazy.ts"' "${imports_response}" | wc -l | tr -d ' ')" != "2" ]]; then
  fail "imports did not report both the dynamic and the type import: $(cat "${imports_response}")"
fi
if ! grep -qF '"path":"web/legacy.cjs","line":1,"specifier":"../domain/model.js","resolved":"domain/model.ts","package":""' \
  "${imports_response}"; then
  fail "imports did not resolve a literal require: $(cat "${imports_response}")"
fi
# A dynamic import whose specifier is computed names no file, and a file type
# the reader cannot parse was never read: both are missing coverage rather than
# source that declared no imports.
if ! grep -qF 'line 8: a dynamic import whose specifier is computed' "${imports_response}"; then
  fail "imports did not report the computed dynamic import: $(cat "${imports_response}")"
fi
if ! grep -qF '"path":"web/widget.vue","reason":"the policy-owned import reader does not read this file"' \
  "${imports_response}"; then
  fail "imports did not report the unreadable file type: $(cat "${imports_response}")"
fi
if grep -qF "${fixture_root}" "${imports_response}"; then
  fail "an imports result leaked a host path: $(cat "${imports_response}")"
fi
if [[ -e "${imports_target}/tsconfig.json" ]] || [[ -e "${imports_target}/web/app.js" ]]; then
  fail "reading imports wrote into the target tree"
fi

# Every budget and activation the protocol does not admit is refused, so no
# operation can run under a rule setting Go did not decide.
expect_runner_rejected "a lint request without budgets" \
  '{"protocolVersion":2,"operation":"lint","root":"'"${target}"'","paths":["lint/deep.ts"],"activation":{"reactHooks":false,"jsxAccessibility":false}}'
expect_runner_rejected "an unusable budget" \
  "$(lint_request '["lint/deep.ts"]' '{"complexity":0,"depth":2,"parameters":2}' '{"reactHooks":false,"jsxAccessibility":false}')"
expect_runner_rejected "an unknown budget" \
  "$(lint_request '["lint/deep.ts"]' '{"complexity":9,"depth":2,"parameters":2,"statements":4}' '{"reactHooks":false,"jsxAccessibility":false}')"
expect_runner_rejected "an unknown activation" \
  "$(lint_request '["lint/deep.ts"]' '{"complexity":9,"depth":2,"parameters":2}' '{"reactHooks":false,"jsxAccessibility":false,"vue":true}')"
expect_runner_rejected "a non-boolean activation" \
  "$(lint_request '["lint/deep.ts"]' '{"complexity":9,"depth":2,"parameters":2}' '{"reactHooks":"on","jsxAccessibility":false}')"

# No selection may name a tree other than the declared root, and the root itself
# must be a normal absolute path.
for uncontained in '["../escape.ts"]' '["/etc/passwd"]' '["src/./a.ts"]' '["src\\a.ts"]' '[""]'; do
  expect_runner_rejected "an uncontained selection ${uncontained}" "$(format_request format "${uncontained}")"
done
expect_runner_rejected "a relative root" \
  '{"protocolVersion":2,"operation":"format","root":"target","paths":["src/formatted.ts"]}'
expect_runner_rejected "an unnormal root" \
  '{"protocolVersion":2,"operation":"format","root":"'"${target}"'/../target","paths":["src/formatted.ts"]}'

# Every request the protocol does not admit is refused. Each operation admits
# exactly its own fields, so provenance cannot be handed a selection and a file
# operation cannot leave one out.
expect_runner_rejected "an unsupported operation" '{"protocolVersion":2,"operation":"install"}'
expect_runner_rejected "a typecheck request without a project" \
  '{"protocolVersion":2,"operation":"typecheck","root":"'"${target}"'","paths":["project/src/ok.ts"]}'
expect_runner_rejected "an uncontained project" \
  "$(typecheck_request '["project/src/ok.ts"]' '../tsconfig.json')"
expect_runner_rejected "an unknown request field" \
  '{"protocolVersion":2,"operation":"provenance","paths":[]}'
expect_runner_rejected "a file operation without a selection" \
  '{"protocolVersion":2,"operation":"format","root":"'"${fixture_root}"'"}'
expect_runner_rejected "a missing request field" '{"protocolVersion":2}'
expect_runner_rejected "another protocol version" '{"protocolVersion":1,"operation":"provenance"}'
expect_runner_rejected "a malformed request" 'not json'
expect_runner_rejected "a request that is not an object" '[]'
oversized="${fixture_root}/oversized.json"
{
  printf '{"protocolVersion":2,"operation":"provenance","pad":"'
  head -c 1100000 /dev/zero | tr '\0' 'a'
  printf '"}'
} >"${oversized}"
if run_runner <"${oversized}" >"${fixture_root}/out" 2>&1; then
  fail "an oversized request was accepted"
fi
if ! grep -q 'byte limit' "${fixture_root}/out"; then
  fail "an oversized request was not rejected for its size"
fi

# A launch that could inject code, a module path, or a debugger is refused even
# though Go already scrubs the environment.
expect_runner_rejected "an injected Node option" \
  '{"protocolVersion":2,"operation":"provenance"}' env NODE_OPTIONS=--no-warnings
expect_runner_rejected "an injected module path" \
  '{"protocolVersion":2,"operation":"provenance"}' env NODE_PATH="${fixture_root}/decoy/node_modules"
if printf '{"protocolVersion":2,"operation":"provenance"}' |
  javascript_sealed_run "${javascript_node}" --no-warnings "${javascript_runner}" \
    >"${fixture_root}/out" 2>&1; then
  fail "a runner launched with extra Node options was accepted"
fi

# A runtime other than the pinned one is refused before anything else is read.
# The fixture links the installed analyzers so the runner reaches its version
# check rather than failing to resolve a tool.
unpinned="${fixture_root}/unpinned"
mkdir -p "${unpinned}"
cp "${runner_sources[@]}" "${unpinned}/"
ln -s "${javascript_bundle_dir}/node_modules" "${unpinned}/node_modules"
printf '{"engines":{"node":"0.0.0"}}\n' >"${unpinned}/package.json"
if printf '{"protocolVersion":2,"operation":"provenance"}' |
  javascript_sealed_run "${javascript_node}" "${unpinned}/runner.mjs" \
    >"${fixture_root}/out" 2>&1; then
  fail "a runner requiring another Node version was accepted"
fi


echo "test-javascript-runner: all checks passed"
