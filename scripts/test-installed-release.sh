#!/usr/bin/env bash
set -euo pipefail































policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
prefix="${HOME}/.local/share/code-polishy"
lock="${policy_root}/.code-polishy.lock.json"

usage() {
  echo "usage: test-installed-release.sh [--prefix DIR] [--lock FILE]" >&2
  exit 2
}

while (($#)); do
  case "$1" in
    --prefix)
      if (($# < 2)); then
        usage
      fi
      prefix="$2"
      shift 2
      ;;
    --prefix=*)
      prefix="${1#*=}"
      shift
      ;;
    --lock)
      if (($# < 2)); then
        usage
      fi
      lock="$2"
      shift 2
      ;;
    --lock=*)
      lock="${1#*=}"
      shift
      ;;
    *) usage ;;
  esac
done

fail() {
  echo "installed release test failure: $1" >&2
  exit 1
}

launcher="${prefix}/bin/code-polishy"
if [[ ! -x "${launcher}" ]]; then
  fail "no installed Code Polishy launcher at ${launcher}; run ./scripts/install.sh"
fi
if [[ ! -f "${lock}" ]]; then
  fail "the release lock is unavailable at ${lock}"
fi

lock_field() {
  awk -v key="\"$2\":" '$1 == key { value = $2; gsub(/[,\"]/, "", value); print value; exit }' "$1"
}

locked_version="$(lock_field "${lock}" codePolishyVersion)"
locked_digest="$(lock_field "${lock}" releaseDigest)"
if [[ ! "${locked_version}" =~ ^[0-9A-Za-z][-0-9A-Za-z._+]*$ || ! "${locked_digest}" =~ ^[0-9a-f]{64}$ ]]; then
  fail "${lock} does not name a usable exact release"
fi
release="${prefix}/releases/${locked_version}-${locked_digest}"
if [[ ! -d "${release}" || -L "${release}" ]]; then
  fail "the locked release directory is unavailable at ${release}"
fi



fixture_root="$(cd "$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-installed-release.XXXXXX")" && pwd -P)"
cleanup() {
  rm -rf "${fixture_root}"
}
trap cleanup EXIT
output="${fixture_root}/output.txt"
real_git="$(command -v git)" || fail "git is required to build a disposable target repository"

write_file() {
  mkdir -p "$(dirname "$1")"
  cat >"$1"
}





write_target_commands() {
  local target="$1" name
  for name in build test; do
    write_file "${target}/scripts/${name}.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ -n "${CODE_POLISHY_INSTALLED_TEST_LOG:-}" ]]; then
  printf '%s\n' "$(basename "$0")" >>"${CODE_POLISHY_INSTALLED_TEST_LOG}"
fi
exit 0
EOF
    chmod +x "${target}/scripts/${name}.sh"
  done
}



write_security_workflow() {
  write_file "$1/.github/workflows/security.yml" <<'EOF'
name: security
on:
  schedule:
    - cron: "0 3 * * 1"
jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - run: code-polishy supply-chain
EOF
}




write_pnpm_workspace() {
  local target="$1"
  shift
  {
    if (($#)); then
      printf 'packages:\n'
      printf '  - %s\n' "$@"
    else
      printf 'packages: []\n'
    fi
    cat <<'EOF'
onlyBuiltDependencies: []
minimumReleaseAge: 43200
minimumReleaseAgeStrict: true
minimumReleaseAgeIgnoreMissingTime: false
blockExoticSubdeps: true
trustLockfile: false
trustPolicy: no-downgrade
EOF
  } >"${target}/pnpm-workspace.yaml"
}





seal_target() {
  local target="$1"
  cp "${lock}" "${target}/.code-polishy.lock.json"
  "${real_git}" -C "${target}" init --quiet
  "${real_git}" -C "${target}" config user.email "installed-release-test@example.invalid"
  "${real_git}" -C "${target}" config user.name "Installed Release Test"
  run_policy "${target}" agents install ||
    fail "$(basename "${target}"): the release installed no canonical guidance: $(excerpt)"
  if ! cmp -s "${release}/templates/AGENTS.md" "${target}/AGENTS.md"; then
    fail "$(basename "${target}"): AGENTS.md did not come from the locked release"
  fi
  if ! cmp -s "${release}/templates/CLAUDE.md" "${target}/CLAUDE.md"; then
    fail "$(basename "${target}"): CLAUDE.md did not come from the locked release"
  fi
  if ! grep -Fxq '/.code-polishy-reports/' "${target}/.gitignore"; then
    fail "$(basename "${target}"): report artifacts are not ignored"
  fi
  run_policy "${target}" agents check ||
    fail "$(basename "${target}"): installed agent guidance is not current: $(excerpt)"
  run_policy "${target}" format --all ||
    fail "$(basename "${target}"): the release could not format the target: $(excerpt)"
  "${real_git}" -C "${target}" add -A
  "${real_git}" -C "${target}" commit --quiet -m "disposable target"
}




run_policy() {
  local target="$1"
  shift
  CODE_POLISHY_INSTALLED_TEST_LOG="${target}/.git/code-polishy-command-log" \
    "${launcher}" --repo-root "${target}" "$@" >"${output}" 2>&1
}

excerpt() {
  head -20 "${output}"
}

expect_pass() {
  local target="$1" description="$2"
  shift 2
  if ! run_policy "${target}" "$@"; then
    fail "${description}: ${*}: $(excerpt)"
  fi
}

expect_findings() {
  local target="$1" description="$2"
  shift 2
  if run_policy "${target}" "$@"; then
    fail "${description}: ${*} reported nothing: $(excerpt)"
  fi
}



expect_finding() {
  local description="$1" check="$2" path="$3" subject="$4"
  if ! grep -Eq "^FAIL +${check} +${path} \[${subject}\]" "${output}"; then
    fail "${description}: no ${check} finding for ${path} [${subject}]: $(excerpt)"
  fi
}

expect_absent() {
  local description="$1" pattern="$2"
  if grep -Eq "${pattern}" "${output}"; then
    fail "${description}: ${pattern}: $(excerpt)"
  fi
}

expect_no_target_commands() {
  local target="$1" description="$2"
  local command_log="${target}/.git/code-polishy-command-log"
  if [[ -s "${command_log}" ]]; then
    fail "${description}: target commands ran: $(head -20 "${command_log}")"
  fi
}

source "${policy_root}/scripts/test-installed-release-behavior-review.sh"

exercise_documentation_lane() {
  local target="$1" description="$2"
  local command_log="${target}/.git/code-polishy-command-log"
  local previous_checkpoint
  previous_checkpoint="$("${real_git}" -C "${target}" rev-parse HEAD)"
  write_file "${target}/README.md" <<'EOF'
# Documentation lane



This change only updates ordinary Markdown.
EOF
  : >"${command_log}"
  expect_findings "${target}" "${description} unformatted documentation" merge-gate --base HEAD
  expect_finding "${description}" "quality.format" "README.md" "prettier"
  expect_no_target_commands "${target}" "${description} unformatted documentation gate"
  expect_pass "${target}" "${description} documentation format" format --git-changes
  expect_no_target_commands "${target}" "${description} documentation format"
  "${real_git}" -C "${target}" add README.md
  "${real_git}" -C "${target}" commit --quiet -m "documentation candidate"
  : >"${command_log}"
  expect_pass "${target}" "${description} documentation checkpoint" checkpoint-gate --base "${previous_checkpoint}"
  grep -q "^CHECKPOINT GATE: DOCUMENTATION against ${previous_checkpoint}$" "${output}" ||
    fail "${description}: checkpoint gate did not disclose the documentation lane: $(excerpt)"
  grep -Eq '^CHECKPOINT ACCEPTED: [0-9a-f]{40,64}$' "${output}" ||
    fail "${description}: checkpoint gate did not record accepted HEAD: $(excerpt)"
  expect_no_target_commands "${target}" "${description} documentation checkpoint"
  : >"${command_log}"
  expect_pass "${target}" "${description} documentation merge gate" merge-gate --base "${previous_checkpoint}"
  grep -q "^MERGE GATE: DOCUMENTATION against ${previous_checkpoint}$" "${output}" ||
    fail "${description}: merge gate did not disclose the documentation lane: $(excerpt)"
  expect_no_target_commands "${target}" "${description} documentation merge gate"
  expect_pass "${target}" "${description} documentation levels" test-levels --base "${previous_checkpoint}"
  grep -Eq '^\| documentation \* +\| 0 application suites +\| automatic +\| merge-gate --base REF +\|$' "${output}" ||
    fail "${description}: test-levels did not disclose zero application suites: $(excerpt)"
  expect_no_target_commands "${target}" "${description} documentation levels"
  : >"${command_log}"
  expect_pass "${target}" "${description} changed tests" test --changed --base "${previous_checkpoint}"
  expect_no_target_commands "${target}" "${description} changed tests"
}

echo "Governing disposable targets with the release named by ${lock} requires..."




go_only="${fixture_root}/go-only"
mkdir -p "${go_only}"
write_file "${go_only}/go.mod" <<'EOF'
module example.test/goonly

go 1.26
EOF
write_file "${go_only}/internal/greeting/greeting.go" <<'EOF'
package greeting

func Render(name string) string {
	return "hello, " + name
}
EOF
write_file "${go_only}/internal/greeting/greeting_test.go" <<'EOF'
package greeting

import "testing"

func TestRenderNamesTheGreeted(t *testing.T) {
	if got := Render("world"); got != "hello, world" {
		t.Fatalf("Render() = %q", got)
	}
}
EOF
write_target_commands "${go_only}"
write_file "${go_only}/scripts/test.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ -n "${CODE_POLISHY_INSTALLED_TEST_LOG:-}" ]]; then
  printf '%s\n' "$(basename "$0")" >>"${CODE_POLISHY_INSTALLED_TEST_LOG}"
fi
go test ./...
EOF
chmod +x "${go_only}/scripts/test.sh"
write_security_workflow "${go_only}"
write_file "${go_only}/.code-polishy.json" <<'EOF'
{
  "version": 3,
  "project": { "kind": "application", "capabilities": [] },
  "scope": {},
  "quality": {},
  "modules": [
    { "name": "greeting", "paths": ["internal/greeting/**"] },
    { "name": "tooling", "paths": ["scripts/**"] }
  ],
  "checks": [
    {
      "name": "go-build",
      "provides": ["build"],
      "argv": ["./scripts/build.sh"],
      "modules": ["greeting", "tooling"],
      "runOn": ["build"]
    }
  ],
  "tests": {
    "suites": [
      {
        "name": "greeting-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["greeting"],
        "argv": ["./scripts/test.sh", "greeting"]
      },
      {
        "name": "tooling-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["tooling"],
        "argv": ["./scripts/test.sh", "tooling"]
      },
      {
        "name": "repository-full",
        "kind": "integration",
        "scope": "repository",
        "cost": "standard",
        "argv": ["./scripts/test.sh", "all"],
        "runOn": ["recommended", "full"]
      }
    ]
  },
  "exceptions": []
}
EOF
seal_target "${go_only}"
exercise_documentation_lane "${go_only}" "go-only"
expect_pass "${go_only}" "go-only" check --all
expect_pass "${go_only}" "go-only" --verbose doctor --strict





if ! grep -q "conditional policy module: osv at ." "${output}"; then
  fail "go-only: the release activated no dependency scanning: $(excerpt)"
fi
expect_absent "go-only activated a JavaScript framework policy module" \
  "conditional policy module: (react|electron)"
exercise_behavior_review_lane "${go_only}" "go-only" "${real_git}" "${fixture_root}" "${output}"
write_file "${go_only}/internal/greeting/farewell.go" <<'EOF'
package greeting

func Farewell( name string ) string {
return "goodbye, " + name
}
EOF
expect_findings "${go_only}" "go-only" check --all
expect_finding "go-only" "quality.gofmt" "internal/greeting/farewell.go" "format"
expect_absent "go-only reported a JavaScript-owned check" \
  "^FAIL +quality\.(format|lint|typecheck|deadCode) "




pnpm_app="${fixture_root}/pnpm-app"
mkdir -p "${pnpm_app}"
write_file "${pnpm_app}/package.json" <<'EOF'
{
  "name": "app",
  "private": true,
  "packageManager": "pnpm@11.13.0",
  "type": "module"
}
EOF
write_file "${pnpm_app}/pnpm-lock.yaml" <<'EOF'
lockfileVersion: '9.0'

settings:
  autoInstallPeers: false

importers:

  .: {}
EOF
write_pnpm_workspace "${pnpm_app}"
write_file "${pnpm_app}/src/index.js" <<'EOF'
export function render(name) {
  return `hello, ${name}`;
}
EOF
write_target_commands "${pnpm_app}"
write_security_workflow "${pnpm_app}"
write_file "${pnpm_app}/.code-polishy.json" <<'EOF'
{
  "version": 3,
  "project": { "kind": "application", "capabilities": [] },
  "scope": { "entryPoints": ["src/**"] },
  "quality": {},
  "modules": [
    { "name": "app", "paths": ["src/**"] },
    { "name": "tooling", "paths": ["scripts/**"] }
  ],
  "checks": [
    {
      "name": "app-build",
      "provides": ["build"],
      "argv": ["./scripts/build.sh"],
      "modules": ["app", "tooling"],
      "runOn": ["build"]
    }
  ],
  "tests": {
    "suites": [
      {
        "name": "app-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["app"],
        "argv": ["./scripts/test.sh", "app"]
      },
      {
        "name": "tooling-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["tooling"],
        "argv": ["./scripts/test.sh", "tooling"]
      },
      {
        "name": "repository-full",
        "kind": "integration",
        "scope": "repository",
        "cost": "standard",
        "argv": ["./scripts/test.sh", "all"],
        "runOn": ["recommended", "full"]
      }
    ]
  },
  "exceptions": []
}
EOF
seal_target "${pnpm_app}"
exercise_documentation_lane "${pnpm_app}" "pnpm-app"
expect_pass "${pnpm_app}" "pnpm-app" check --all
expect_pass "${pnpm_app}" "pnpm-app" supply-chain --offline


write_file "${pnpm_app}/src/unformatted.js" <<'EOF'
export function shout( text ) {
    return text.toUpperCase()
}
EOF
write_file "${pnpm_app}/src/parameters.js" <<'EOF'
export function join(one, two, three, four, five, six) {
  return [one, two, three, four, five, six].join(" ");
}
EOF
expect_findings "${pnpm_app}" "pnpm-app" check --all
expect_finding "pnpm-app" "quality.format" "src/unformatted.js" "prettier"
expect_finding "pnpm-app" "quality.complexity" "src/parameters.js" "max-params"




monorepo="${fixture_root}/pnpm-monorepo"
mkdir -p "${monorepo}"
write_file "${monorepo}/package.json" <<'EOF'
{
  "name": "monorepo",
  "private": true,
  "packageManager": "pnpm@11.13.0",
  "type": "module"
}
EOF
write_file "${monorepo}/pnpm-lock.yaml" <<'EOF'
lockfileVersion: '9.0'

settings:
  autoInstallPeers: false

importers:

  .: {}

  packages/app:
    dependencies:
      '@fixture/lib':
        specifier: workspace:*
        version: link:../lib

  packages/lib: {}
EOF
write_pnpm_workspace "${monorepo}" "packages/*"
write_file "${monorepo}/packages/lib/package.json" <<'EOF'
{
  "name": "@fixture/lib",
  "private": true,
  "type": "module",
  "main": "src/index.js"
}
EOF
write_file "${monorepo}/packages/lib/src/index.js" <<'EOF'
export function greet(name) {
  return `hello, ${name}`;
}
EOF
write_file "${monorepo}/packages/app/package.json" <<'EOF'
{
  "name": "@fixture/app",
  "private": true,
  "type": "module",
  "main": "src/index.js",
  "dependencies": {
    "@fixture/lib": "workspace:*"
  }
}
EOF
write_file "${monorepo}/packages/app/src/index.js" <<'EOF'
import { greet } from "@fixture/lib";

export function main() {
  return greet("world");
}
EOF


mkdir -p "${monorepo}/packages/app/node_modules/@fixture"
ln -s ../../../lib "${monorepo}/packages/app/node_modules/@fixture/lib"
write_target_commands "${monorepo}"
write_security_workflow "${monorepo}"
write_file "${monorepo}/.code-polishy.json" <<'EOF'
{
  "version": 3,
  "project": { "kind": "application", "capabilities": [] },
  "scope": { "entryPoints": ["packages/*/src/index.js"] },
  "quality": {},
  "modules": [
    { "name": "lib", "paths": ["packages/lib/**"] },
    { "name": "app", "paths": ["packages/app/**"], "dependsOn": ["lib"] },
    { "name": "tooling", "paths": ["scripts/**"] }
  ],
  "checks": [
    {
      "name": "workspace-build",
      "provides": ["build"],
      "argv": ["./scripts/build.sh"],
      "modules": ["lib", "app", "tooling"],
      "runOn": ["build"]
    }
  ],
  "tests": {
    "suites": [
      {
        "name": "lib-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["lib"],
        "argv": ["./scripts/test.sh", "lib"]
      },
      {
        "name": "app-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["app"],
        "argv": ["./scripts/test.sh", "app"]
      },
      {
        "name": "tooling-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["tooling"],
        "argv": ["./scripts/test.sh", "tooling"]
      },
      {
        "name": "repository-full",
        "kind": "integration",
        "scope": "repository",
        "cost": "standard",
        "argv": ["./scripts/test.sh", "all"],
        "runOn": ["recommended", "full"]
      }
    ]
  },
  "exceptions": []
}
EOF
seal_target "${monorepo}"
expect_pass "${monorepo}" "pnpm-monorepo" check --all
expect_pass "${monorepo}" "pnpm-monorepo" supply-chain --offline


write_file "${monorepo}/packages/lib/src/index.js" <<'EOF'
import { main } from "../../app/src/index.js";

export function greet(name) {
  return `${main()} and ${name}`;
}
EOF
expect_findings "${monorepo}" "pnpm-monorepo" architecture --all
expect_finding "pnpm-monorepo" "architecture.moduleDependency" "packages/lib/src/index.js" "app"



typescript="${fixture_root}/typescript"
mkdir -p "${typescript}"
write_file "${typescript}/package.json" <<'EOF'
{
  "name": "library",
  "private": true,
  "packageManager": "pnpm@11.13.0",
  "type": "module"
}
EOF
write_file "${typescript}/pnpm-lock.yaml" <<'EOF'
lockfileVersion: '9.0'

settings:
  autoInstallPeers: false

importers:

  .: {}
EOF
write_pnpm_workspace "${typescript}"
write_file "${typescript}/tsconfig.json" <<'EOF'
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "noEmit": true
  },
  "include": ["src/**/*.ts"]
}
EOF
write_file "${typescript}/src/index.ts" <<'EOF'
export function render(name: string): string {
  return `hello, ${name}`;
}
EOF
write_target_commands "${typescript}"
write_security_workflow "${typescript}"
write_file "${typescript}/.code-polishy.json" <<'EOF'
{
  "version": 3,
  "project": { "kind": "library", "capabilities": [] },
  "scope": { "entryPoints": ["src/index.ts"] },
  "quality": {},
  "modules": [
    { "name": "library", "paths": ["src/**"] },
    { "name": "tooling", "paths": ["scripts/**"] }
  ],
  "checks": [
    {
      "name": "library-build",
      "provides": ["build"],
      "argv": ["./scripts/build.sh"],
      "modules": ["library", "tooling"],
      "runOn": ["build"]
    }
  ],
  "tests": {
    "suites": [
      {
        "name": "library-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["library"],
        "argv": ["./scripts/test.sh", "library"]
      },
      {
        "name": "tooling-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["tooling"],
        "argv": ["./scripts/test.sh", "tooling"]
      },
      {
        "name": "repository-full",
        "kind": "integration",
        "scope": "repository",
        "cost": "standard",
        "argv": ["./scripts/test.sh", "all"],
        "runOn": ["recommended", "full"]
      }
    ]
  },
  "exceptions": []
}
EOF
seal_target "${typescript}"
expect_pass "${typescript}" "typescript" check --all

write_file "${typescript}/src/widen.ts" <<'EOF'
export function widen(count: number): string {
  return count;
}
EOF
expect_findings "${typescript}" "typescript" check --all
expect_finding "typescript" "quality.typecheck" "src/widen.ts" "TS2322"
expect_finding "typescript" "quality.deadCode" "src/widen.ts" "knip"




react="${fixture_root}/react"
mkdir -p "${react}"
write_file "${react}/package.json" <<'EOF'
{
  "name": "interface",
  "private": true,
  "packageManager": "pnpm@11.13.0",
  "type": "module",
  "dependencies": {
    "react": "19.0.0",
    "react-dom": "19.0.0"
  }
}
EOF
write_file "${react}/pnpm-lock.yaml" <<'EOF'
lockfileVersion: '9.0'

settings:
  autoInstallPeers: false

importers:

  .:
    dependencies:
      react:
        specifier: 19.0.0
        version: 19.0.0
      react-dom:
        specifier: 19.0.0
        version: 19.0.0(react@19.0.0)

packages:

  react@19.0.0:
    resolution: {integrity: sha512-aaa==}

  react-dom@19.0.0:
    resolution: {integrity: sha512-bbb==}
EOF
write_pnpm_workspace "${react}"



write_file "${react}/node_modules/react/package.json" <<'EOF'
{
  "name": "react",
  "version": "19.0.0",
  "license": "MIT",
  "types": "index.d.ts",
  "exports": {
    ".": { "types": "./index.d.ts" },
    "./jsx-runtime": { "types": "./jsx-runtime.d.ts" }
  }
}
EOF
write_file "${react}/node_modules/react/index.d.ts" <<'EOF'
export declare function useState<S>(initial: S): [S, (next: S) => void];
export declare function useEffect(effect: () => void, deps?: unknown[]): void;
EOF
write_file "${react}/node_modules/react/jsx-runtime.d.ts" <<'EOF'
export declare namespace JSX {
  interface Element {
    readonly type: string;
  }
  interface IntrinsicElements {
    [element: string]: Record<string, unknown>;
  }
}
export declare function jsx(type: unknown, props: unknown): JSX.Element;
export declare function jsxs(type: unknown, props: unknown): JSX.Element;
EOF
write_file "${react}/node_modules/react-dom/package.json" <<'EOF'
{ "name": "react-dom", "version": "19.0.0", "license": "MIT" }
EOF
write_file "${react}/tsconfig.json" <<'EOF'
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "noEmit": true,
    "jsx": "react-jsx",
    "jsxImportSource": "react"
  },
  "include": ["src/**/*.ts", "src/**/*.tsx"]
}
EOF
write_file "${react}/src/counter.tsx" <<'EOF'
import { useState } from "react";

export function Counter({ start }: { start: number }) {
  const [count, setCount] = useState(start);
  return <button onClick={() => setCount(count + 1)}>{count}</button>;
}
EOF
write_target_commands "${react}"
write_security_workflow "${react}"
write_file "${react}/.code-polishy.json" <<'EOF'
{
  "version": 3,
  "project": { "kind": "application", "capabilities": ["frontend", "ui"] },
  "scope": { "entryPoints": ["src/**"] },
  "quality": {},
  "modules": [
    { "name": "interface", "paths": ["src/**"] },
    { "name": "tooling", "paths": ["scripts/**"] }
  ],
  "checks": [
    {
      "name": "interface-build",
      "provides": ["build"],
      "argv": ["./scripts/build.sh"],
      "modules": ["interface", "tooling"],
      "runOn": ["build"]
    }
  ],
  "tests": {
    "suites": [
      {
        "name": "interface-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["interface"],
        "argv": ["./scripts/test.sh", "interface"]
      },
      {
        "name": "tooling-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["tooling"],
        "argv": ["./scripts/test.sh", "tooling"]
      },
      {
        "name": "interface-browser",
        "kind": "browser",
        "scope": "repository",
        "cost": "standard",
        "argv": ["./scripts/test.sh", "browser"],
        "runOn": ["recommended", "full"]
      }
    ]
  },
  "exceptions": []
}
EOF
seal_target "${react}"
expect_pass "${react}" "react" check --all
expect_pass "${react}" "react" --verbose doctor --strict
if ! grep -q "conditional policy module: react at ." "${output}"; then
  fail "react: the release activated no React policy: $(excerpt)"
fi
write_file "${react}/src/conditional.tsx" <<'EOF'
import { useState } from "react";

export function Conditional({ enabled }: { enabled: boolean }) {
  if (enabled) {
    const [value] = useState(0);
    return <span>{value}</span>;
  }
  return <span>off</span>;
}
EOF
expect_findings "${react}" "react" check --all
expect_finding "react" "quality.lint" "src/conditional.tsx" "react-hooks/rules-of-hooks"

echo "The installed release governed every disposable target shape."
