#!/usr/bin/env bash

declare output

python_adoption_host_python() {
  local candidate
  for candidate in python3.12 python3 python python.exe; do
    if ! command -v "${candidate}" >/dev/null 2>&1; then
      continue
    fi
    if "${candidate}" -c 'import sys; raise SystemExit(0 if sys.version_info >= (3, 12) else 1)' >/dev/null 2>&1; then
      command -v "${candidate}"
      return 0
    fi
  done
  return 1
}

python_adoption_create_venv() {
  local host_python="$1" environment="$2"
  rm -rf -- "${environment}"
  "${host_python}" -m venv --without-pip "${environment}" ||
    fail "python-adoption: could not create ${environment} with the host Python"
  if [[ ! -f "${environment}/pyvenv.cfg" ]]; then
    fail "python-adoption: ${environment} has no pyvenv.cfg"
  fi
  if [[ ! -x "${environment}/bin/python" && ! -x "${environment}/bin/python3" &&
    ! -f "${environment}/Scripts/python.exe" && ! -x "${environment}/Scripts/python" ]]; then
    fail "python-adoption: ${environment} has no runnable platform Python"
  fi
}

python_adoption_write_root_project() {
  local target="$1" alpha_reference="$2" build_requirement="$3"
  local beta_commit="89abcdef0123456789abcdef0123456789abcdef"
  local gamma_commit="fedcba9876543210fedcba9876543210fedcba98"
  local delta_commit="00112233445566778899aabbccddeeff00112233"
  write_file "${target}/pyproject.toml" <<EOF
[project]
name = "python-adoption"
version = "0.1.0"
requires-python = ">=3.12"
dependencies = [
  "private-alpha @ git+https://git.example.test/fixture/private-alpha.git@${alpha_reference}",
  "private-beta @ git+https://git.example.test/fixture/private-beta.git@${beta_commit}#subdirectory=python/beta",
  "private-gamma @ git+ssh://git@git.example.test/fixture/private-gamma.git@${gamma_commit}",
  "private-delta @ git+ssh://builder@git.example.test/fixture/private-delta.git@${delta_commit}#subdirectory=packages/delta",
]

[project.scripts]
adoption-api = "adoption_api.endpoint:endpoint"

[build-system]
requires = ["${build_requirement}"]
build-backend = "hatchling.build"
EOF
}

python_adoption_write_root_lock() {
  local target="$1" alpha_commit="$2"
  local beta_commit="89abcdef0123456789abcdef0123456789abcdef"
  local gamma_commit="fedcba9876543210fedcba9876543210fedcba98"
  local delta_commit="00112233445566778899aabbccddeeff00112233"
  write_file "${target}/uv.lock" <<EOF
version = 1
revision = 1
requires-python = ">=3.12"

[[package]]
name = "private-alpha"
version = "0.0.0"
source = { git = "https://git.example.test/fixture/private-alpha.git?rev=${alpha_commit}#${alpha_commit}" }

[[package]]
name = "private-beta"
version = "0.0.0"
source = { git = "https://git.example.test/fixture/private-beta.git?subdirectory=python%2Fbeta&rev=${beta_commit}#${beta_commit}" }

[[package]]
name = "private-gamma"
version = "0.0.0"
source = { git = "ssh://git@git.example.test/fixture/private-gamma.git?rev=${gamma_commit}#${gamma_commit}" }

[[package]]
name = "private-delta"
version = "0.0.0"
source = { git = "ssh://builder@git.example.test/fixture/private-delta.git?subdirectory=packages%2Fdelta&rev=${delta_commit}#${delta_commit}" }
EOF
}

python_adoption_write_nested_project() {
  local target="$1"
  write_file "${target}/apps/worker/pyproject.toml" <<'EOF'
[project]
name = "python-adoption-worker"
version = "0.1.0"
requires-python = ">=3.12"
dependencies = []

[project.scripts]
adoption-worker = "adoption_worker.worker:run"
EOF
  write_file "${target}/apps/worker/uv.lock" <<'EOF'
version = 1
revision = 1
requires-python = ">=3.12"
EOF
}

python_adoption_write_sources() {
  local target="$1"
  write_file "${target}/src/adoption_foundation/__init__.py" <<'EOF'
EOF
  write_file "${target}/src/adoption_foundation/greeting.py" <<'EOF'
def greeting() -> str:
    return "hello"
EOF
  write_file "${target}/src/adoption_service/__init__.py" <<'EOF'
EOF
  write_file "${target}/src/adoption_service/render.py" <<'EOF'
from adoption_foundation.greeting import greeting


def render() -> str:
    return greeting()
EOF
  write_file "${target}/src/adoption_api/__init__.py" <<'EOF'
EOF
  write_file "${target}/src/adoption_api/endpoint.py" <<'EOF'
from adoption_service.render import render


def endpoint() -> str:
    return render()
EOF
  write_file "${target}/apps/worker/src/adoption_worker/__init__.py" <<'EOF'
EOF
  write_file "${target}/apps/worker/src/adoption_worker/worker.py" <<'EOF'
def run() -> str:
    return "worker"
EOF
}

python_adoption_write_data() {
  local target="$1"
  write_file "${target}/data/catalog-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.json" <<'EOF'
{  "catalog": "identity-sensitive",  "items": ["one", "two"] }
EOF
  write_file "${target}/data/catalog-fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210.yaml" <<'EOF'
catalog: identity-sensitive
items: [one, two]
EOF
}

python_adoption_write_gitlab_root() {
  local target="$1"
  local digest="sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  write_file "${target}/.gitlab-ci.yml" <<EOF
include:
  - local: ci/includes/defaults.yml
image: registry.example.test/fixture/root@${digest}
services:
  - name: registry.example.test/fixture/root-service@${digest}
default:
  image:
    name: registry.example.test/fixture/default@${digest}
  services:
    - name: registry.example.test/fixture/default-service@${digest}
root-validation:
  image: registry.example.test/fixture/root-job@${digest}
  services:
    - registry.example.test/fixture/root-job-service@${digest}
  script:
    - printf "root validation\\n"
EOF
}

python_adoption_write_gitlab_defaults() {
  local target="$1"
  local digest="sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  write_file "${target}/ci/includes/defaults.yml" <<EOF
include:
  - local: ci/includes/deep.yml
default:
  image: registry.example.test/fixture/include-default@${digest}
  services:
    - name: registry.example.test/fixture/include-default-service@${digest}
EOF
}

python_adoption_write_gitlab_deep() {
  local target="$1" image="$2" include_ref="$3"
  local digest="sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  write_file "${target}/ci/includes/deep.yml" <<EOF
include:
  - project: fixture/secure-templates
    ref: ${include_ref}
    file: /python/release.yml
deep-validation:
  image: ${image}
  services:
    - name: registry.example.test/fixture/deep-service@${digest}
  script:
    - printf "deep validation\\n"
EOF
}

python_adoption_write_commands() {
  local target="$1"
  write_target_commands "${target}"
  write_file "${target}/scripts/lock-sync.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
command_log="$(git rev-parse --path-format=absolute --git-common-dir)/code-polishy-command-log"
printf '%s\n' "$(basename "$0")" >>"${command_log}"
[[ -f uv.lock && -f apps/worker/uv.lock ]]
EOF
  write_file "${target}/scripts/security-monitoring.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
command_log="$(git rev-parse --path-format=absolute --git-common-dir)/code-polishy-command-log"
printf '%s\n' "$(basename "$0")" >>"${command_log}"
EOF
  chmod +x "${target}/scripts/lock-sync.sh" "${target}/scripts/security-monitoring.sh"
}

python_adoption_write_config() {
  local target="$1"
  write_file "${target}/.code-polishy.json" <<'EOF'
{
  "version": 4,
  "project": { "kind": "application", "capabilities": [] },
  "scope": {
    "entryPoints": ["src/adoption_api/endpoint.py"],
    "data": [
      "data/catalog-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.json",
      "data/catalog-fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210.yaml"
    ]
  },
  "quality": {},
  "modules": [
    { "name": "foundation", "paths": ["src/adoption_foundation/**"] },
    { "name": "service", "paths": ["src/adoption_service/**"], "dependsOn": ["foundation"] },
    { "name": "application", "paths": ["src/adoption_api/**"], "dependsOn": ["service"] },
    { "name": "worker", "paths": ["apps/worker/src/adoption_worker/**"] },
    { "name": "catalog", "paths": ["data/**"] },
    { "name": "tooling", "paths": ["scripts/**"] }
  ],
  "verification": {
    "mergeGate": { "recommendedModules": ["application"] }
  },
  "checks": [
    {
      "name": "adoption-build",
      "provides": ["build"],
      "argv": ["./scripts/build.sh"],
      "modules": ["foundation", "service", "application", "worker", "tooling"],
      "runOn": ["build"]
    },
    {
      "name": "python-lock-sync",
      "provides": ["lock-sync"],
      "argv": ["./scripts/lock-sync.sh"],
      "runOn": ["supply-chain"]
    },
    {
      "name": "gitlab-security-monitoring",
      "provides": ["security-monitoring"],
      "argv": ["./scripts/security-monitoring.sh"],
      "runOn": ["security"]
    }
  ],
  "tests": {
    "ownership": [],
    "suites": [
      {
        "name": "foundation-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["foundation"],
        "argv": ["./scripts/test.sh", "foundation"]
      },
      {
        "name": "service-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["service"],
        "argv": ["./scripts/test.sh", "service"]
      },
      {
        "name": "application-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["application"],
        "argv": ["./scripts/test.sh", "application"]
      },
      {
        "name": "worker-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["worker"],
        "argv": ["./scripts/test.sh", "worker"]
      },
      {
        "name": "catalog-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["catalog"],
        "argv": ["./scripts/test.sh", "catalog"]
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
}

python_adoption_expect_data_preserved() {
  local target="$1" json_snapshot="$2" yaml_snapshot="$3"
  local json_path="data/catalog-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.json"
  local yaml_path="data/catalog-fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210.yaml"
  if ! cmp -s "${target}/${json_path}" "${json_snapshot}"; then
    fail "python-adoption: format changed hand-written ${json_path}"
  fi
  if ! cmp -s "${target}/${yaml_path}" "${yaml_snapshot}"; then
    fail "python-adoption: format changed hand-written ${yaml_path}"
  fi
}


exercise_python_adoption_fixture() {
  local fixture_root="$1" real_git="$2" release="$3"
  local target="${fixture_root}/python-adoption"
  local host_python alpha_commit mismatch_commit gitlab_digest command_log base missing_venv_findings
  local json_path yaml_path json_snapshot yaml_snapshot
  alpha_commit="0123456789abcdef0123456789abcdef01234567"
  mismatch_commit="abcdef0123456789abcdef0123456789abcdef01"
  gitlab_digest="sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  json_path="data/catalog-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.json"
  yaml_path="data/catalog-fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210.yaml"
  host_python="$(python_adoption_host_python)" ||
    fail "python-adoption: a host Python >= 3.12 is required to create disposable environments"

  mkdir -p "${target}"
  write_file "${target}/.gitignore" <<'EOF'
.venv/
EOF
  python_adoption_write_root_project "${target}" "${alpha_commit}" "hatchling==1.25.0"
  python_adoption_write_root_lock "${target}" "${alpha_commit}"
  python_adoption_write_nested_project "${target}"
  python_adoption_write_sources "${target}"
  python_adoption_write_data "${target}"
  python_adoption_write_gitlab_root "${target}"
  python_adoption_write_gitlab_defaults "${target}"
  python_adoption_write_gitlab_deep \
    "${target}" "registry.example.test/fixture/deep@${gitlab_digest}" "${alpha_commit}"
  python_adoption_write_commands "${target}"
  python_adoption_write_config "${target}"
  python_adoption_create_venv "${host_python}" "${target}/.venv"
  python_adoption_create_venv "${host_python}" "${target}/apps/worker/.venv"

  seal_target "${target}"
  base="$("${real_git}" -C "${target}" rev-parse HEAD)"
  command_log="${target}/.git/code-polishy-command-log"
  json_snapshot="${fixture_root}/python-adoption-catalog.json"
  yaml_snapshot="${fixture_root}/python-adoption-catalog.yaml"
  cp "${target}/${json_path}" "${json_snapshot}"
  cp "${target}/${yaml_path}" "${yaml_snapshot}"

  expect_pass "${target}" "python-adoption doctor" --verbose doctor --strict
  expect_pass "${target}" "python-adoption check" check --all
  expect_pass "${target}" "python-adoption architecture" architecture --all
  expect_pass "${target}" "python-adoption offline supply chain" supply-chain --offline
  expect_pass "${target}" "python-adoption data-preserving format" format --all
  python_adoption_expect_data_preserved "${target}" "${json_snapshot}" "${yaml_snapshot}"

  write_file "${target}/${yaml_path}" <<'EOF'
catalog: [
EOF
  expect_findings "${target}" "python-adoption invalid data syntax" check --all
  expect_finding "python-adoption invalid data syntax" "quality.dataSyntax" "${yaml_path}" "yaml"
  python_adoption_write_data "${target}"
  write_file "${target}/${json_path}" <<'EOF'
{
EOF
  expect_findings "${target}" "python-adoption invalid JSON syntax" check --all
  expect_finding "python-adoption invalid JSON syntax" "quality.dataSyntax" "${json_path}" "json"
  python_adoption_write_data "${target}"

  python_adoption_write_root_lock "${target}" "${mismatch_commit}"
  expect_findings "${target}" "python-adoption mismatched Git lock" supply-chain --offline
  expect_finding "python-adoption mismatched Git lock" "supplyChain.lockConsistency" "uv.lock" "private-alpha"
  python_adoption_write_root_lock "${target}" "${alpha_commit}"

  python_adoption_write_root_project "${target}" "v1.2.3" "hatchling==1.25.0"
  expect_findings "${target}" "python-adoption tag-pinned dependency" supply-chain --offline
  expect_finding "python-adoption tag-pinned dependency" "supplyChain.pythonManifest" "pyproject.toml" "pyproject.toml"
  python_adoption_write_root_project "${target}" "${alpha_commit}" "hatchling==1.25.0"

  python_adoption_write_root_project "${target}" "${alpha_commit}" "hatchling>=1.25"
  expect_findings "${target}" "python-adoption unpinned build requirement" supply-chain --offline
  expect_finding "python-adoption unpinned build requirement" "supplyChain.pythonExactVersion" "pyproject.toml" "hatchling"
  python_adoption_write_root_project "${target}" "${alpha_commit}" "hatchling==1.25.0"

  write_file "${target}/src/adoption_foundation/forbidden.py" <<'EOF'
from adoption_service.render import render


def forbidden() -> str:
    return render()
EOF
  expect_findings "${target}" "python-adoption forbidden import" architecture --all
  expect_finding \
    "python-adoption forbidden import" "architecture.moduleDependency" \
    "src/adoption_foundation/forbidden.py" "service"
  rm -f -- "${target}/src/adoption_foundation/forbidden.py"

  write_file "${target}/src/adoption_api/type_error.py" <<'EOF'
value: int = "wrong"
EOF
  expect_findings "${target}" "python-adoption ty diagnostic" check --all
  expect_finding \
    "python-adoption ty diagnostic" "quality.typecheck" \
    "src/adoption_api/type_error.py:[0-9]+:[0-9]+" "invalid-assignment:[0-9a-f]{64}"
  rm -f -- "${target}/src/adoption_api/type_error.py"

  rm -rf -- "${target}/.venv"
  expect_findings "${target}" "python-adoption missing root environment" check --all
  expect_finding \
    "python-adoption missing root environment" "quality.typecheckCoverage" "pyproject.toml" "ty"
  missing_venv_findings="$(grep -Ec '^FAIL +quality\.typecheckCoverage +pyproject\.toml \[ty\]' "${output}" || true)"
  if [[ "${missing_venv_findings}" != 1 ]]; then
    fail "python-adoption: missing root environment reported ${missing_venv_findings} ty coverage findings"
  fi
  expect_absent "python-adoption missing root environment" "^FAIL +quality.typecheck "
  python_adoption_create_venv "${host_python}" "${target}/.venv"

  write_file "${target}/src/adoption_api/endpoint.py" <<'EOF'
from adoption_service.render import render


def endpoint() -> str:
    return render() + " changed"
EOF
  "${real_git}" -C "${target}" add src/adoption_api/endpoint.py
  "${real_git}" -C "${target}" commit --quiet -m "application candidate"
  : >"${command_log}"
  expect_pass "${target}" "python-adoption changed tests" test --changed --base "${base}"
  if ! grep -Fxq "test.sh" "${command_log}"; then
    fail "python-adoption: changed tests ran no selected target suite"
  fi
  : >"${command_log}"
  expect_pass "${target}" "python-adoption selected merge gate" merge-gate --base "${base}"
  grep -Fqx "MERGE GATE: RECOMMENDED against ${base}" "${output}" ||
    fail "python-adoption: merge gate did not select the ordinary recommended profile: $(excerpt)"
  if ! grep -Fxq "build.sh" "${command_log}" || ! grep -Fxq "test.sh" "${command_log}"; then
    fail "python-adoption: selected merge gate omitted its build or test evidence"
  fi

  python_adoption_write_gitlab_deep \
    "${target}" "registry.example.test/fixture/deep:latest" "${alpha_commit}"
  expect_findings "${target}" "python-adoption mutable GitLab image" --verbose doctor --strict
  expect_finding \
    "python-adoption mutable GitLab image" "supplyChain.gitLabImagePin" \
    "ci/includes/deep.yml" "registry.example.test/fixture/deep:latest"
  python_adoption_write_gitlab_deep \
    "${target}" "registry.example.test/fixture/deep@${gitlab_digest}" "main"
  expect_findings "${target}" "python-adoption mutable GitLab include" --verbose doctor --strict
  expect_finding \
    "python-adoption mutable GitLab include" "supplyChain.gitLabIncludePin" \
    "ci/includes/deep.yml" "fixture/secure-templates"

  if ! "${release}/scripts/release-manifest.sh" verify "${release}" >"${output}" 2>&1; then
    fail "python-adoption: managed Python commands changed the installed release: $(excerpt)"
  fi
}
