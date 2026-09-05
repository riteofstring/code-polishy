#!/usr/bin/env bash
set -euo pipefail












javascript_test_name="test-javascript-runner"
# shellcheck source=scripts/javascript-runner-test-env.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/javascript-runner-test-env.sh"

runner_sources=("${bundle_source}"/*.mjs)
node_version="$(javascript_test_node_version)"
pnpm_version="$(javascript_test_pnpm_version)"
manifest="${bundle_source}/package.json"
installed_manifest="${javascript_bundle_dir}/${javascript_bundle_manifest_name}"




response="${fixture_root}/response.json"
printf '{"protocolVersion":3,"operation":"provenance"}' | run_runner >"${response}" ||
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



decoy="${fixture_root}/decoy/node_modules/prettier"
mkdir -p "${decoy}"
printf '{"name":"prettier","version":"0.0.0-decoy"}\n' >"${decoy}/package.json"
decoy_response="$(cd "${fixture_root}/decoy" && printf '{"protocolVersion":3,"operation":"provenance"}' | run_runner)"
if [[ "${decoy_response}" != "$(cat "${response}")" ]]; then
  fail "the runner resolved a tool from the working directory"
fi



target="${fixture_root}/target"
mkdir -p "${target}/src"
printf 'const value   =  1\n' >"${target}/src/unformatted.ts"
printf 'const value = 1;\n' >"${target}/src/formatted.ts"
printf 'x\n' >"${target}/src/opaque.bin"
printf '\377\376 not text\n' >"${target}/src/binary.ts"
ln -s formatted.ts "${target}/src/linked.ts"


printf '{"semi":false,"printWidth":20}\n' >"${target}/.prettierrc"
printf 'src\n' >"${target}/.prettierignore"

format_request() {
  printf '{"protocolVersion":3,"operation":"%s","root":"%s","paths":%s}' "$1" "${target}" "$2"
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



gitlab_target="${fixture_root}/gitlab"
mkdir -p "${gitlab_target}/ci" "${gitlab_target}/.git"
gitlab_digest="sha256:$(printf 'a%.0s' {1..64})"
gitlab_commit="0123456789abcdef0123456789abcdef01234567"
gitlab_integrity="sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
cat >"${gitlab_target}/.gitlab-ci.yml" <<SOURCE
include:
  - local: /ci/common.yml
  - local: .git/config
  - project: group/templates
    file:
      - release.yml
      - security.yml
    ref: ${gitlab_commit}
  - remote: https://ci.example/templates/release.yml
    integrity: ${gitlab_integrity}
  - component: gitlab.example/group/project/component@${gitlab_commit}
  - template: Jobs/SAST.gitlab-ci.yml
image: registry.example/root@${gitlab_digest}
services:
  - registry.example/root-service@${gitlab_digest}
default:
  image:
    name: registry.example/default@${gitlab_digest}
  services:
    - name: registry.example/default-service@${gitlab_digest}
release:
  image: registry.example/job@${gitlab_digest}
  services:
    - registry.example/job-service@${gitlab_digest}
SOURCE
printf 'image: registry.example/common@%s\n' "${gitlab_digest}" >"${gitlab_target}/ci/common.yml"
printf 'image: registry.example/secret@%s\n' "${gitlab_digest}" >"${gitlab_target}/.git/config"
gitlab_request() {
  printf '{"protocolVersion":3,"operation":"gitlab","root":"%s","paths":[".gitlab-ci.yml"],"governedPaths":[".gitlab-ci.yml","ci/common.yml"]}' "$1"
}
gitlab_response="${fixture_root}/gitlab.json"
gitlab_request "${gitlab_target}" | run_runner >"${gitlab_response}" ||
  fail "the gitlab operation rejected a governed configuration"
for governed in '.gitlab-ci.yml' 'ci/common.yml'; do
  if ! grep -qF "\"${governed}\"" "${gitlab_response}"; then
    fail "the gitlab operation did not report governed control ${governed}: $(cat "${gitlab_response}")"
  fi
done
for include_kind in project remote component template; do
  if ! grep -qF "\"kind\":\"${include_kind}\"" "${gitlab_response}"; then
    fail "the gitlab operation did not report ${include_kind} include facts: $(cat "${gitlab_response}")"
  fi
done
if ! grep -qF 'is not a governed repository input' "${gitlab_response}"; then
  fail "the gitlab operation did not reject an ignored include: $(cat "${gitlab_response}")"
fi
if grep -qF 'registry.example/secret' "${gitlab_response}"; then
  fail "the gitlab operation read an ignored include: $(cat "${gitlab_response}")"
fi
for image_scope in 'global:image' 'global:service' 'default:image' 'default:service' 'job:release:image' 'job:release:service'; do
  if ! grep -qF "\"scope\":\"${image_scope}\"" "${gitlab_response}"; then
    fail "the gitlab operation did not report ${image_scope}: $(cat "${gitlab_response}")"
  fi
done




gitlab_invalid_target="${fixture_root}/gitlab-invalid"
mkdir -p "${gitlab_invalid_target}/ci"
cat >"${gitlab_invalid_target}/.gitlab-ci.yml" <<'SOURCE'
include:
  - local: ci/cycle.yml
  - local: ci/cycle.yml
  - local: ci/missing.yml
  - local: ../outside.yml
  - local: $CI_CONFIG_PATH
  - unsupported: Security/SAST.gitlab-ci.yml
SOURCE
cat >"${gitlab_invalid_target}/ci/cycle.yml" <<'SOURCE'
include:
  - local: /.gitlab-ci.yml
SOURCE
gitlab_invalid_request() {
  printf '{"protocolVersion":3,"operation":"gitlab","root":"%s","paths":[".gitlab-ci.yml"],"governedPaths":[".gitlab-ci.yml","ci/cycle.yml","ci/missing.yml"]}' "$1"
}
gitlab_invalid_request "${gitlab_invalid_target}" | run_runner >"${gitlab_response}" ||
  fail "the gitlab operation rejected a malformed GitLab configuration request"
for reason in 'include cycle' 'included more than once' 'the file is unreadable' 'literal contained path' 'exactly one supported source kind'; do
  if ! grep -qF "${reason}" "${gitlab_response}"; then
    fail "the gitlab operation did not report ${reason}: $(cat "${gitlab_response}")"
  fi
done

gitlab_windows_path_target="${fixture_root}/gitlab-windows-path"
mkdir -p "${gitlab_windows_path_target}"
cat >"${gitlab_windows_path_target}/.gitlab-ci.yml" <<'SOURCE'
include:
  - local: C:/outside.yml
SOURCE
gitlab_windows_path_request() {
  printf '{"protocolVersion":3,"operation":"gitlab","root":"%s","paths":[".gitlab-ci.yml"],"governedPaths":[".gitlab-ci.yml"]}' "$1"
}
gitlab_windows_path_request "${gitlab_windows_path_target}" | run_runner >"${gitlab_response}" ||
  fail "the gitlab operation rejected a Windows-path coverage request"
if ! grep -qF 'literal contained path' "${gitlab_response}"; then
  fail "the gitlab operation accepted a Windows-absolute local path: $(cat "${gitlab_response}")"
fi

data_target="${fixture_root}/structured-data"
mkdir -p "${data_target}"
printf '{  "identity": "preserve"  }\n' >"${data_target}/identity.json"
cat >"${data_target}/identity.jsonc" <<'SOURCE'
{
  // JSONC comments and trailing commas are valid.
  "identity": "preserve",
}
SOURCE
cat >"${data_target}/identity.yaml" <<'SOURCE'
identity: preserve
items:
  - one
SOURCE
cat >"${data_target}/identity.yml" <<'SOURCE'
identity: preserve
items:
  - one
SOURCE
cp -R "${data_target}" "${fixture_root}/structured-data-before"
parse_request() {
  printf '{"protocolVersion":3,"operation":"parse","root":"%s","paths":%s}' "$1" "$2"
}
data_selection='["identity.json","identity.jsonc","identity.yaml","identity.yml"]'
parse_response="${fixture_root}/parse.json"
parse_request "${data_target}" "${data_selection}" | run_runner >"${parse_response}" ||
  fail "the runner rejected valid structured data"
if ! grep -qF '"unsupported":[]' "${parse_response}"; then
  fail "the parse-only operation rejected valid structured data: $(cat "${parse_response}")"
fi
if ! diff -ru "${fixture_root}/structured-data-before" "${data_target}" >/dev/null; then
  fail "the parse-only operation rewrote structured data"
fi
printf '{"identity":\n' >"${data_target}/broken.json"
printf '{"identity": }\n' >"${data_target}/broken.jsonc"
printf 'identity: [\n' >"${data_target}/broken.yaml"
printf 'identity: [\n' >"${data_target}/broken.yml"
parse_request "${data_target}" '["broken.json","broken.jsonc","broken.yaml","broken.yml"]' |
  run_runner >"${parse_response}" || fail "the runner rejected a malformed-data parse request"
for malformed in broken.json broken.jsonc broken.yaml broken.yml; do
  if ! grep -qF "\"path\":\"${malformed}\"" "${parse_response}"; then
    fail "the parse-only operation did not report ${malformed}: $(cat "${parse_response}")"
  fi
done





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
cat >"${lint_source}/prose.ts" <<'SOURCE'
// This prose must live in documentation.
export const prose = true;
SOURCE
cat >"${lint_source}/reference-near-miss.ts" <<'SOURCE'
/// <reference path='./declaration.ts' />
export const nearMiss = true;
SOURCE
cat >"${lint_source}/reference-whitespace-near-miss.ts" <<'SOURCE'
/// <reference lib="es2022"/>
export const whitespaceNearMiss = true;
SOURCE
cat >"${lint_source}/reference-trailing-near-miss.ts" <<'SOURCE'
/// <reference types="vitest" /> prose
export const trailingNearMiss = true;
SOURCE
cat >"${lint_source}/reference-suppression-near-miss.ts" <<'SOURCE'
/// <reference types="vitest" /> eslint-disable
export const suppressionNearMiss = true;
SOURCE
cat >"${lint_source}/reference-after-code-near-miss.ts" <<'SOURCE'
export const referenceAfterCode = true;
/// <reference types="vitest" />
SOURCE
cat >"${lint_source}/environment-after-code.test.ts" <<'SOURCE'
export const afterCode = true;
// @vitest-environment jsdom
SOURCE
cat >"${lint_source}/environment-production.ts" <<'SOURCE'
// @vitest-environment jsdom
export const productionEnvironment = true;
SOURCE
cat >"${lint_source}/environment-happy.test.ts" <<'SOURCE'
/* @vitest-environment happy-dom */
export const happyEnvironment = true;
SOURCE
cat >"${lint_source}/environment-line-whitespace.test.ts" <<'SOURCE'
//  @vitest-environment jsdom
export const lineWhitespace = true;
SOURCE
cat >"${lint_source}/environment-block-whitespace.test.ts" <<'SOURCE'
/* @vitest-environment jsdom  */
export const blockWhitespace = true;
SOURCE
cat >"${lint_source}/environment-trailing.test.ts" <<'SOURCE'
// @vitest-environment jsdom prose
export const trailingEnvironment = true;
SOURCE
cat >"${lint_source}/environment-suppression.test.ts" <<'SOURCE'
// @vitest-environment jsdom eslint-disable
export const suppressionEnvironment = true;
SOURCE
cat >"${lint_source}/environment-after-comment.test.ts" <<'SOURCE'
// A prior parser comment.
// @vitest-environment jsdom
export const afterCommentEnvironment = true;
SOURCE
{
  printf '// @vitest-environment jsdom'
  head -c 201 /dev/zero | tr '\0' 'x'
  printf '\nexport const overBoundaryEnvironment = true;\n'
} >"${lint_source}/environment-over-boundary.test.ts"
cat >"${lint_source}/jsx-comment.tsx" <<'SOURCE'
export function Comment() {
  return <>{/* JSX prose */}</>;
}
SOURCE
cat >"${lint_source}/allowed-reference.ts" <<'SOURCE'
/// <reference lib="es2022" />
export const reference = true;
SOURCE
printf '/// <reference types="vitest" />\n' >"${lint_source}/allowed-reference-only.d.ts"
cat >"${lint_source}/allowed-environment-line.test.ts" <<'SOURCE'
// @vitest-environment jsdom
export const lineEnvironment = true;
SOURCE
cat >"${lint_source}/allowed-environment-block.test.ts" <<'SOURCE'
/* @vitest-environment jsdom */
export const blockEnvironment = true;
SOURCE
cat >"${lint_source}/allowed-shebang.ts" <<'SOURCE'
#!/usr/bin/env node
export const shebang = true;
SOURCE
cat >"${lint_source}/empty-shebang.ts" <<'SOURCE'
#!
export const emptyShebang = true;
SOURCE
cat >"${lint_source}/leading-space-shebang.ts" <<'SOURCE'
 #!/usr/bin/env node
export const leadingSpaceShebang = true;
SOURCE
cat >"${lint_source}/literals.ts" <<'SOURCE'
const url = "https://example.test";
const matcher = /https?:\/\//;
const template = `// not a comment`;
const block = "/* not a comment */";
export { block, matcher, template, url };
SOURCE
cat >"${lint_source}/jsx-literal.tsx" <<'SOURCE'
export function Literal() {
  return <span data-note="/* not a comment */">{"// not a comment"}</span>;
}
SOURCE
printf 'export function broken( {\n' >"${lint_source}/broken.ts"
cat >"${lint_source}/broken-comment.ts" <<'SOURCE'
// The parser cannot decide this comment.
export function broken( {
SOURCE
cat >"${lint_source}/panel.tsx" <<'SOURCE'
export function Panel({ ready }: { ready: boolean }) {
  if (ready) {
    const [value] = useState(0);
    return <img src={value} />;
  }
  return null;
}
SOURCE


printf 'export default [{rules:{"max-depth":"off","max-params":"off"}}];\n' >"${target}/eslint.config.mjs"

lint_request() {
  printf '{"protocolVersion":3,"operation":"lint","root":"%s","paths":%s,"limits":%s,"activation":%s}' \
    "${target}" "$1" "$2" "$3"
}
lint_response="${fixture_root}/lint.json"
lint_request '["lint/deep.ts","lint/directive.ts","lint/prose.ts","lint/reference-near-miss.ts","lint/reference-whitespace-near-miss.ts","lint/reference-trailing-near-miss.ts","lint/reference-suppression-near-miss.ts","lint/reference-after-code-near-miss.ts","lint/environment-after-code.test.ts","lint/environment-production.ts","lint/environment-happy.test.ts","lint/environment-line-whitespace.test.ts","lint/environment-block-whitespace.test.ts","lint/environment-trailing.test.ts","lint/environment-suppression.test.ts","lint/environment-after-comment.test.ts","lint/environment-over-boundary.test.ts","lint/jsx-comment.tsx","lint/allowed-reference.ts","lint/allowed-reference-only.d.ts","lint/allowed-environment-line.test.ts","lint/allowed-environment-block.test.ts","lint/allowed-shebang.ts","lint/empty-shebang.ts","lint/leading-space-shebang.ts","lint/literals.ts","lint/jsx-literal.tsx","lint/broken.ts","lint/broken-comment.ts","src/opaque.bin"]' \
  '{"complexity":9,"depth":2,"parameters":2}' '{"reactHooks":false,"jsxAccessibility":false}' |
  run_runner >"${lint_response}" || fail "the runner rejected a well-formed lint request"
for expected in '"rule":"max-depth"' '"rule":"max-params"' '"path":"lint/deep.ts"' '"path":"lint/directive.ts"'; do
  if ! grep -qF "${expected}" "${lint_response}"; then
    fail "lint did not report ${expected}: $(cat "${lint_response}")"
  fi
done
if grep -qF '"path":"lint/directive.ts","reason"' "${lint_response}"; then
  fail "lint treated a parser-attributed no-rule diagnostic as uncovered: $(cat "${lint_response}")"
fi
if grep -qF '"rule":"policy/source-comment"' "${lint_response}"; then
  fail "lint made a source-comment policy decision: $(cat "${lint_response}")"
fi
for expected in \
  '"path":"lint/directive.ts","kind":"Line","raw":"// eslint-disable-next-line max-params","complete":true,"line":1,"column":1,"beforeCode":true,"preamble":true,"byteZero":true' \
  '"path":"lint/prose.ts","kind":"Line","raw":"// This prose must live in documentation.","complete":true,"line":1,"column":1,"beforeCode":true,"preamble":true,"byteZero":true' \
  '"path":"lint/allowed-reference.ts","kind":"Line","raw":"/// <reference lib=\"es2022\" />","complete":true,"line":1,"column":1,"beforeCode":true,"preamble":true,"byteZero":true' \
  '"path":"lint/allowed-reference-only.d.ts","kind":"Line","raw":"/// <reference types=\"vitest\" />","complete":true,"line":1,"column":1,"beforeCode":true,"preamble":true,"byteZero":true' \
  '"path":"lint/allowed-environment-line.test.ts","kind":"Line","raw":"// @vitest-environment jsdom","complete":true,"line":1,"column":1,"beforeCode":true,"preamble":true,"byteZero":true' \
  '"path":"lint/allowed-environment-block.test.ts","kind":"Block","raw":"/* @vitest-environment jsdom */","complete":true,"line":1,"column":1,"beforeCode":true,"preamble":true,"byteZero":true' \
  '"path":"lint/allowed-shebang.ts","kind":"Shebang","raw":"#!/usr/bin/env node","complete":true,"line":1,"column":1,"beforeCode":true,"preamble":true,"byteZero":true' \
  '"path":"lint/empty-shebang.ts","kind":"Shebang","raw":"#!","complete":true,"line":1,"column":1,"beforeCode":true,"preamble":true,"byteZero":true' \
  '"path":"lint/jsx-comment.tsx","kind":"Block","raw":"/* JSX prose */","complete":true,"line":2,"column":13,"beforeCode":false,"preamble":false,"byteZero":false' \
  '"path":"lint/prose.ts"' \
  '"path":"lint/reference-near-miss.ts"' \
  '"path":"lint/reference-whitespace-near-miss.ts"' \
  '"path":"lint/reference-trailing-near-miss.ts"' \
  '"path":"lint/reference-suppression-near-miss.ts"' \
  '"path":"lint/reference-after-code-near-miss.ts"' \
  '"path":"lint/environment-after-code.test.ts"' \
  '"path":"lint/environment-production.ts"' \
  '"path":"lint/environment-happy.test.ts"' \
  '"path":"lint/environment-line-whitespace.test.ts"' \
  '"path":"lint/environment-block-whitespace.test.ts"' \
  '"path":"lint/environment-trailing.test.ts"' \
  '"path":"lint/environment-suppression.test.ts"' \
  '"path":"lint/environment-after-comment.test.ts","kind":"Line","raw":"// @vitest-environment jsdom","complete":true,"line":2,"column":1,"beforeCode":true,"preamble":false,"byteZero":false' \
  '"path":"lint/environment-over-boundary.test.ts"' \
  '"path":"lint/leading-space-shebang.ts"' \
  '"path":"lint/jsx-comment.tsx"'; do
  if ! grep -qF "${expected}" "${lint_response}"; then
    fail "lint did not report parser fact ${expected}: $(cat "${lint_response}")"
  fi
done
if ! grep -qF '"complete":false' "${lint_response}"; then
  fail "lint did not mark an over-boundary parser fact incomplete: $(cat "${lint_response}")"
fi
for clean in \
  lint/literals.ts \
  lint/jsx-literal.tsx; do
  if grep -qF "\"path\":\"${clean}\"" "${lint_response}"; then
    fail "lint misread ${clean} as a parser comment: $(cat "${lint_response}")"
  fi
done


for undecided in lint/broken.ts lint/broken-comment.ts src/opaque.bin; do
  if ! grep -qF "\"path\":\"${undecided}\"" "${lint_response}"; then
    fail "lint did not report ${undecided} as undecided: $(cat "${lint_response}")"
  fi
done



generous='{"complexity":20,"depth":10,"parameters":10}'
lint_request '["lint/panel.tsx"]' "${generous}" '{"reactHooks":false,"jsxAccessibility":false}' |
  run_runner >"${lint_response}" || fail "the runner rejected an unactivated lint request"
if ! grep -qF '"findings":[]' "${lint_response}" || ! grep -qF '"comments":[]' "${lint_response}" || ! grep -qF '"unsupported":[]' "${lint_response}"; then
  fail "unactivated lint reported framework findings: $(cat "${lint_response}")"
fi
lint_request '["lint/panel.tsx"]' "${generous}" '{"reactHooks":true,"jsxAccessibility":true}' |
  run_runner >"${lint_response}" || fail "the runner rejected an activated lint request"
for expected in '"rule":"react-hooks/rules-of-hooks"' '"rule":"jsx-a11y/alt-text"'; do
  if ! grep -qF "${expected}" "${lint_response}"; then
    fail "activated lint did not report ${expected}: $(cat "${lint_response}")"
  fi
done



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
  printf '{"protocolVersion":3,"operation":"typecheck","root":"%s","paths":%s,"project":"%s"}' \
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


if ! grep -qF '"covered":["project/src/wrong.ts","project/src/ok.ts"]' "${typecheck_response}"; then
  fail "typecheck did not report exactly the covered files: $(cat "${typecheck_response}")"
fi
if [[ -e "${project_source}/tsconfig.tsbuildinfo" ]] || [[ -n "$(find "${project_source}" -name '*.js' -print -quit)" ]]; then
  fail "typecheck emitted into the target tree"
fi

mkdir -p "${target}/generated"
printf 'export const external: number = "text";\n' >"${target}/generated/external.ts"
printf '{"protocolVersion":3,"operation":"typecheck","root":"%s","paths":["generated/external.ts"],"project":"project/tsconfig.json","inheritedPaths":["generated/external.ts"]}' "${target}" |
  run_runner >"${typecheck_response}" || fail "the runner rejected an inherited external project root"
if ! grep -qF '"path":"generated/external.ts"' "${typecheck_response}" ||
  ! grep -qF '"code":2322' "${typecheck_response}" ||
  ! grep -qF '"covered":["generated/external.ts"]' "${typecheck_response}"; then
  fail "typecheck did not apply the project context to an external generated root: $(cat "${typecheck_response}")"
fi



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





printf '{"compilerOptions":{"noCheck":true,"target":"ES2022","module":"Preserve"},"include":["src"]}\n' \
  >"${project_source}/tsconfig.nocheck.json"
typecheck_request '["project/src/wrong.ts"]' project/tsconfig.nocheck.json |
  run_runner >"${typecheck_response}" ||
  fail "the runner rejected a project declaring noCheck"
if ! grep -qF '"code":2322' "${typecheck_response}"; then
  fail "a project declaring noCheck reported no diagnostics: $(cat "${typecheck_response}")"
fi





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



printf '{"compilerOptions":{"strict":true}}\n' >"${outside}/linked-tsconfig.json"
ln -s "${outside}" "${project_source}/linked-base"
printf '{"extends":"./linked-base/linked-tsconfig.json","include":["src"]}\n' \
  >"${project_source}/tsconfig.linked.json"
expect_project_refused "an extension chain leaving the repository through a link" \
  project/tsconfig.linked.json "the project extends configuration outside the repository"
printf '{"include":["src",\n' >"${project_source}/tsconfig.broken.json"
expect_project_refused "a malformed project" project/tsconfig.broken.json "expected"
expect_project_refused "an absent project" project/tsconfig.absent.json "the file is unreadable"

if grep -qF "${fixture_root}" "${typecheck_response}"; then
  fail "a typecheck result leaked a host path: $(cat "${typecheck_response}")"
fi



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


printf '{"entry":["src/orphan.ts"],"project":["src/**"]}\n' >"${deadcode_target}/knip.json"
printf '{"name":"web","private":true,"version":"0.0.0","dependencies":{"vite":"7.0.0"}}\n' \
  >"${deadcode_target}/packages/web/package.json"
printf 'export const shared = () => 4;\n' >"${deadcode_target}/packages/web/src/lib.ts"
printf 'export const stranded = () => 5;\n' >"${deadcode_target}/packages/web/src/spare.ts"



executed_marker="${fixture_root}/vite-config-executed"
cat >"${deadcode_target}/packages/web/vite.config.mts" <<SOURCE
import { writeFileSync } from "node:fs";
writeFileSync("${executed_marker}", "executed");
export default {};
SOURCE

deadcode_request() {
  printf '{"protocolVersion":3,"operation":"deadcode","root":"%s","directory":"%s","workspaces":%s}' \
    "${deadcode_target}" "$1" "$2"
}
deadcode_workspaces='[{"root":".","entry":["src/index.ts"],"project":["src/index.ts","src/helper.ts","src/orphan.ts"]},
  {"root":"packages/web","entry":[],"project":["packages/web/src/lib.ts","packages/web/src/spare.ts"]}]'
deadcode_response="${fixture_root}/deadcode.json"
deadcode_request . "${deadcode_workspaces}" |
  run_runner >"${deadcode_response}" || fail "the runner rejected a well-formed deadcode request"


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

if [[ -e "${deadcode_target}/node_modules" ]] ||
  [[ -n "$(find "${deadcode_target}" -name 'policy-knip.json' -print -quit)" ]]; then
  fail "the dead-code analysis wrote into the target tree"
fi

generated_deadcode_target="${fixture_root}/generated-deadcode"
mkdir -p "${generated_deadcode_target}/frontend/src" "${generated_deadcode_target}/python_pkg/generated"
printf '{"name":"frontend","private":true,"type":"module"}\n' >"${generated_deadcode_target}/frontend/package.json"
printf '[project]\nname = "python-package"\nversion = "1.0.0"\n' >"${generated_deadcode_target}/pyproject.toml"
printf 'import { used } from "../../python_pkg/generated/client.js";\nexport const boot = () => used();\n' \
  >"${generated_deadcode_target}/frontend/src/index.ts"
printf 'export const used = () => 7;\nexport const unusedGenerated = () => 8;\n' \
  >"${generated_deadcode_target}/python_pkg/generated/client.ts"
printf 'export const orphan = () => 9;\n' >"${generated_deadcode_target}/python_pkg/generated/orphan.ts"
printf '{"protocolVersion":3,"operation":"deadcode","root":"%s","directory":"frontend","workspaces":[{"root":"frontend","entry":["frontend/src/index.ts"],"project":["frontend/src/index.ts","python_pkg/generated/client.ts","python_pkg/generated/orphan.ts"],"inherited":["python_pkg/generated/client.ts","python_pkg/generated/orphan.ts"]}]}' \
  "${generated_deadcode_target}" |
  run_runner >"${deadcode_response}" || fail "the runner rejected generated JavaScript inside a Python package"
if ! grep -qF '"unusedFiles":["python_pkg/generated/orphan.ts"]' "${deadcode_response}" ||
  ! grep -qF '"symbol":"unusedGenerated"' "${deadcode_response}" ||
  grep -qF '"symbol":"used"' "${deadcode_response}"; then
  fail "deadcode lost source-package reachability for generated output: $(cat "${deadcode_response}")"
fi
if [[ -e "${generated_deadcode_target}/package.json" || -e "${generated_deadcode_target}/node_modules" ]]; then
  fail "deadcode fabricated a root JavaScript package"
fi

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




reachability_target="${fixture_root}/reachability"
mkdir -p "${reachability_target}/src"
printf '{"name":"reachability","version":"0.0.0"}\n' >"${reachability_target}/package.json"
printf 'export { helper } from "%s/src/helper.js";\n' "${reachability_target}" \
  >"${outside}/bridge.ts"
printf 'import { helper } from "../../outside/bridge.js";\nexport const used = helper;\n' \
  >"${reachability_target}/src/index.ts"
printf 'export const helper = 1;\n' >"${reachability_target}/src/helper.ts"
printf '{"protocolVersion":3,"operation":"deadcode","root":"%s","directory":".","workspaces":%s}' \
  "${reachability_target}" \
  '[{"root":".","entry":["src/index.ts"],"project":["src/index.ts","src/helper.ts"]}]' |
  run_runner >"${deadcode_response}" ||
  fail "the runner rejected a selection importing outside the repository"
if ! grep -qF '"unusedFiles":["src/helper.ts"]' "${deadcode_response}"; then
  fail "a module outside the target tree kept target source alive: $(cat "${deadcode_response}")"
fi



mkdir -p "${deadcode_target}/detached"
printf 'export const stray = 1;\n' >"${deadcode_target}/detached/stray.ts"
deadcode_request detached '[{"root":"detached","entry":[],"project":["detached/stray.ts"]}]' |
  run_runner >"${deadcode_response}" && fail "a directory without a package was analyzed"
if ! grep -qF 'Unable to find package.json' "${deadcode_response}"; then
  fail "the refusal did not name the missing package: $(cat "${deadcode_response}")"
fi



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


ln -s ../../packages/ui "${imports_target}/node_modules/@scope/ui"


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
printf '{"protocolVersion":3,"operation":"imports","root":"%s","paths":["web/app.ts","web/legacy.cjs","web/widget.vue"]}' \
  "${imports_target}" | run_runner >"${imports_response}" ||
  fail "the runner rejected a well-formed imports request"
expect_import() {
  if ! grep -qF "\"specifier\":\"$1\",\"resolved\":\"$2\",\"package\":\"$3\"" "${imports_response}"; then
    fail "imports did not report $1 as $2 from package '$3': $(cat "${imports_response}")"
  fi
}



expect_import "../domain/model.js" "domain/model.ts" ""
expect_import "@scope/ui" "packages/ui/src/index.ts" "@scope/ui"




expect_import "left-pad" "node_modules/left-pad/index.js" "left-pad"
expect_import "@scope/absent" "" "@scope/absent"


expect_import "left-pad/index.js" "node_modules/left-pad/index.js" "left-pad"


expect_import "node:path" "" ""
expect_import "crypto" "" ""
expect_import "@/lib/thing" "" ""
expect_import "#internal" "" ""



expect_import "../../outside/thing.js" "" ""
expect_import "@scope/linked" "" "@scope/linked"


expect_import "./lazy.ts" "web/lazy.ts" ""
if [[ "$(grep -oF '"specifier":"./lazy.ts"' "${imports_response}" | wc -l | tr -d ' ')" != "2" ]]; then
  fail "imports did not report both the dynamic and the type import: $(cat "${imports_response}")"
fi
if ! grep -qF '"path":"web/legacy.cjs","line":1,"column":27,"specifier":"../domain/model.js","resolved":"domain/model.ts","package":"","kind":"runtime"' \
  "${imports_response}"; then
  fail "imports did not resolve a literal require: $(cat "${imports_response}")"
fi



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



expect_runner_rejected "a lint request without budgets" \
  '{"protocolVersion":3,"operation":"lint","root":"'"${target}"'","paths":["lint/deep.ts"],"activation":{"reactHooks":false,"jsxAccessibility":false}}'
expect_runner_rejected "an unusable budget" \
  "$(lint_request '["lint/deep.ts"]' '{"complexity":0,"depth":2,"parameters":2}' '{"reactHooks":false,"jsxAccessibility":false}')"
expect_runner_rejected "an unknown budget" \
  "$(lint_request '["lint/deep.ts"]' '{"complexity":9,"depth":2,"parameters":2,"statements":4}' '{"reactHooks":false,"jsxAccessibility":false}')"
expect_runner_rejected "an unknown activation" \
  "$(lint_request '["lint/deep.ts"]' '{"complexity":9,"depth":2,"parameters":2}' '{"reactHooks":false,"jsxAccessibility":false,"vue":true}')"
expect_runner_rejected "a non-boolean activation" \
  "$(lint_request '["lint/deep.ts"]' '{"complexity":9,"depth":2,"parameters":2}' '{"reactHooks":"on","jsxAccessibility":false}')"



for uncontained in '["../escape.ts"]' '["/etc/passwd"]' '["src/./a.ts"]' '["src\\a.ts"]' '[""]'; do
  expect_runner_rejected "an uncontained selection ${uncontained}" "$(format_request format "${uncontained}")"
done
expect_runner_rejected "a relative root" \
  '{"protocolVersion":3,"operation":"format","root":"target","paths":["src/formatted.ts"]}'
expect_runner_rejected "an unnormal root" \
  '{"protocolVersion":3,"operation":"format","root":"'"${target}"'/../target","paths":["src/formatted.ts"]}'
expect_runner_rejected "an unsupported operation" '{"protocolVersion":3,"operation":"install"}'
expect_runner_rejected "a typecheck request without a project" \
  '{"protocolVersion":3,"operation":"typecheck","root":"'"${target}"'","paths":["project/src/ok.ts"]}'
expect_runner_rejected "an uncontained project" \
  "$(typecheck_request '["project/src/ok.ts"]' '../tsconfig.json')"
expect_runner_rejected "an unknown request field" \
  '{"protocolVersion":3,"operation":"provenance","paths":[]}'
expect_runner_rejected "a file operation without a selection" \
  '{"protocolVersion":3,"operation":"format","root":"'"${fixture_root}"'"}'
expect_runner_rejected "a missing request field" '{"protocolVersion":3}'
expect_runner_rejected "another protocol version" '{"protocolVersion":1,"operation":"provenance"}'
expect_runner_rejected "a malformed request" 'not json'
expect_runner_rejected "a request that is not an object" '[]'
oversized="${fixture_root}/oversized.json"
{
  printf '{"protocolVersion":3,"operation":"provenance","pad":"'
  head -c 1100000 /dev/zero | tr '\0' 'a'
  printf '"}'
} >"${oversized}"
if run_runner <"${oversized}" >"${fixture_root}/out" 2>&1; then
  fail "an oversized request was accepted"
fi
if ! grep -q 'byte limit' "${fixture_root}/out"; then
  fail "an oversized request was not rejected for its size"
fi
expect_runner_rejected "an injected Node option" \
  '{"protocolVersion":3,"operation":"provenance"}' env NODE_OPTIONS=--no-warnings
expect_runner_rejected "an injected module path" \
  '{"protocolVersion":3,"operation":"provenance"}' env NODE_PATH="${fixture_root}/decoy/node_modules"
if printf '{"protocolVersion":3,"operation":"provenance"}' |
  javascript_sealed_run "${javascript_node}" --no-warnings "${javascript_runner}" \
    >"${fixture_root}/out" 2>&1; then
  fail "a runner launched with extra Node options was accepted"
fi
unpinned="${fixture_root}/unpinned"
mkdir -p "${unpinned}"
cp "${runner_sources[@]}" "${unpinned}/"
ln -s "${javascript_bundle_dir}/node_modules" "${unpinned}/node_modules"
printf '{"engines":{"node":"0.0.0"}}\n' >"${unpinned}/package.json"
if printf '{"protocolVersion":3,"operation":"provenance"}' |
  javascript_sealed_run "${javascript_node}" "${unpinned}/runner.mjs" \
    >"${fixture_root}/out" 2>&1; then
  fail "a runner requiring another Node version was accepted"
fi
echo "test-javascript-runner: all checks passed"
