#!/usr/bin/env bash

exercise_first_adoption_fixture() {
  local fixture_root="$1" real_git="$2" lock="$3" output="$4"
  local target="${fixture_root}/first-adoption"
  local command_log base

  mkdir -p "${target}/src"
  write_target_commands "${target}"
  write_file "${target}/src/state.json" <<'EOF'
{
  "state": "base"
}
EOF
  "${real_git}" -C "${target}" init --quiet -b main
  "${real_git}" -C "${target}" config user.email "installed-release-test@example.invalid"
  "${real_git}" -C "${target}" config user.name "Installed Release Test"
  "${real_git}" -C "${target}" add -A
  "${real_git}" -C "${target}" commit --quiet -m "unmanaged base"
  base="$("${real_git}" -C "${target}" rev-parse HEAD)"
  "${real_git}" -C "${target}" switch --quiet -c candidate

  write_file "${target}/.code-polishy.json" <<'EOF'
{
  "version": 4,
  "project": { "kind": "application", "capabilities": [] },
  "scope": {},
  "quality": {},
  "documentation": {
    "design": [
      { "path": "docs/design/application.md", "module": "application" }
    ]
  },
  "modules": [
    { "name": "application", "paths": ["src/**"] },
    { "name": "tooling", "paths": ["scripts/**"] }
  ],
  "verification": {
    "mergeGate": { "recommendedModules": ["application"] }
  },
  "checks": [
    {
      "name": "application-build",
      "provides": ["build"],
      "argv": ["./scripts/build.sh"],
      "modules": ["application", "tooling"],
      "runOn": ["build"]
    }
  ],
  "tests": {
    "ownership": [],
    "suites": [
      {
        "name": "application-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["application"],
        "argv": ["./scripts/test.sh", "application"]
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
        "runOn": ["full"]
      }
    ]
  },
  "exceptions": []
}
EOF
  if run_policy "${target}" doctor --strict; then
    fail "first-adoption: candidate without a release lock was accepted"
  fi
  if ! grep -qi 'lock' "${output}"; then
    fail "first-adoption: missing candidate lock produced no clear diagnostic: $(excerpt)"
  fi
  cp "${lock}" "${target}/.code-polishy.lock.json"
  write_file "${target}/docs/design/application.md" <<'EOF'
# Application State

The application owns the state document. Tooling may validate and package it,
but must preserve its values. State changes remain explicit source changes so
reviewers can inspect the exact data shipped by a build.
EOF
  expect_pass "${target}" "first-adoption guidance install" agents install
  exercise_adopted_design_context "${target}" "${output}"
  write_file "${target}/src/state.json" <<'EOF'
{
  "state": "candidate"
}

exercise_adopted_design_context() {
  local target="$1" output="$2"
  local guidance_copy="${target}/.git/adopted-guidance"
  local command_log="${target}/.git/code-polishy-command-log"
  cp "${target}/AGENTS.md" "${guidance_copy}"
  : >"${command_log}"
  expect_pass "${target}" "adopted application context" design-context --module application --format json
  grep -Fq 'docs/design/application.md' "${output}" ||
    fail "first-adoption: selected application rationale is absent: $(excerpt)"
  grep -Fq 'Tooling may validate and package it' "${output}" ||
    fail "first-adoption: selected application rationale content is absent: $(excerpt)"
  expect_pass "${target}" "adopted application match explanation" design-context --module application
  grep -Fq 'SELECTED BY: module application' "${output}" ||
    fail "first-adoption: application match has no explanation: $(excerpt)"
  expect_pass "${target}" "adopted tooling coverage gap" design-context --module tooling
  grep -Fq 'selected work lacks mapped rationale in modules: tooling' "${output}" ||
    fail "first-adoption: tooling coverage gap is absent: $(excerpt)"
  if grep -Fq 'DESIGN DOCUMENT: docs/design/application.md' "${output}"; then
    fail "first-adoption: tooling context loaded unrelated application rationale"
  fi
  cmp -s "${guidance_copy}" "${target}/AGENTS.md" ||
    fail "first-adoption: context lookup changed canonical guidance"
  if [[ -s "${command_log}" ]]; then
    fail "first-adoption: context lookup executed repository commands"
  fi
}
EOF
  "${real_git}" -C "${target}" add -A
  "${real_git}" -C "${target}" commit --quiet -m "adopt Code Polishy"

  command_log="${target}/.git/code-polishy-command-log"
  : >"${command_log}"
  expect_pass "${target}" "first-adoption merge gate" --verbose merge-gate --base main
  grep -Fqx "MERGE POLICY LEVEL: FULL" "${output}" ||
    fail "first-adoption: merge gate did not select full: $(excerpt)"
  grep -Fq "first adoption: base configuration \".code-polishy.json\" is absent at ${base}" "${output}" ||
    fail "first-adoption: merge gate omitted the unmanaged-base reason: $(excerpt)"
  if ! grep -Fxq "build.sh" "${command_log}" || ! grep -Fxq "test.sh" "${command_log}"; then
    fail "first-adoption: full gate omitted build or test evidence"
  fi
}
