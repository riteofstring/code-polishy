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
  "version": 3,
  "project": { "kind": "application", "capabilities": [] },
  "scope": {},
  "quality": {},
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
  expect_pass "${target}" "first-adoption guidance install" agents install
  write_file "${target}/src/state.json" <<'EOF'
{
  "state": "candidate"
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
