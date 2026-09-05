#!/usr/bin/env bash

exercise_scoped_analysis_fixture() {
  local fixture_root="$1" release="$2" output="$3"
  local target="${fixture_root}/scoped-analysis" python status scenario
  python="$(installed_fixture_python "${release}")"
  mkdir -p "${target}"
  write_target_commands "${target}"
  "${python}" -I -B - "${target}" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
files = {
    "frontend/package.json": json.dumps({"name": "frontend", "private": True, "type": "module", "packageManager": "pnpm@11.13.0"}),
    "frontend/src/index.js": 'import { used } from "../../python_pkg/generated/client.js";\nexport const boot = () => used();\n',
    "frontend/schema.json": '{"version":1}\n',
    "python_pkg/generated/client.js": 'export const used=()=>7;\nexport const unusedGenerated=()=>8;\n',
    "python_pkg/generated/orphan.js": 'export const orphan=()=>9;\n',
    "python_pkg/backend.py": 'def unused_backend():\n    return 1\n',
    "pyproject.toml": '[project]\nname = "python-package"\nversion = "1.0.0"\nrequires-python = "==3.12.*"\n',
    "scripts/generate.sh": '#!/usr/bin/env bash\nprintf invoked > producer-ran\nexit 19\n',
}
command = {"argv": ["bash", "scripts/generate.sh"], "cwd": ".", "timeoutSeconds": 900}
config = {
    "version": 4,
    "project": {"kind": "application", "capabilities": []},
    "scope": {"entryPoints": ["frontend/src/index.js"], "generated": ["python_pkg/generated/**"],
              "generatedJavaScript": [{"paths": ["python_pkg/generated/**"], "sourcePackage": "frontend/package.json"}]},
    "modules": [{"name": "frontend", "paths": ["frontend/**"]},
                {"name": "backend", "paths": ["python_pkg/**"]},
                {"name": "tooling", "paths": ["scripts/**"]}],
    "generation": {"producers": [{"name": "client", "inputs": ["frontend/schema.json"], "outputs": ["python_pkg/generated/**"], "generate": command, "verify": command}]},
    "checks": [{"name": "build", "provides": ["build"], "argv": ["./scripts/build.sh"], "modules": ["frontend", "backend", "tooling"], "runOn": ["build"]},
               {"name": "lock-sync", "provides": ["lock-sync"], "argv": ["./scripts/test.sh", "lock"], "modules": ["frontend", "backend"], "runOn": ["supply-chain"]}],
    "tests": {"ownership": [], "suites": [
        {"name": name + "-unit", "kind": "unit", "scope": "module", "modules": [name], "argv": ["./scripts/test.sh", name]}
        for name in ["frontend", "backend", "tooling"]
    ] + [{"name": "full", "kind": "integration", "scope": "repository", "argv": ["./scripts/test.sh", "all"], "runOn": ["full"]}]},
    "exceptions": [],
}
files[".code-polishy.json"] = json.dumps(config, indent=2) + "\n"
for relative, content in files.items():
    path = root / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")
PY
  seal_target "${target}"
  status=0
  run_policy "${target}" check --files AGENTS.md --format json || status=$?
  [[ "${status}" -le 1 ]] || fail "scoped guidance could not run: $(excerpt)"
  "${python}" -I -B - "${output}" <<'PY'
import json
import pathlib
import sys

report = json.loads(pathlib.Path(sys.argv[1]).read_text())
assert report["summary"]["errors"] == 0, report["findings"]
assert report["requestedSelection"]["expanded"] == ["AGENTS.md"], report
assert not any("python" in entry["analyzer"].lower() for entry in report["analysisContext"]), report
assert not any(finding["ruleId"].startswith(("quality.deadCode", "architecture.python", "quality.python")) for finding in report["findings"]), report
PY
  status=0
  run_policy "${target}" check --files python_pkg/generated/client.js --format json || status=$?
  [[ "${status}" == 1 ]] || fail "scoped JavaScript expected findings, received status ${status}: $(excerpt)"
  "${python}" -I -B - "${output}" "${target}" <<'PY'
import json
import pathlib
import sys

report = json.loads(pathlib.Path(sys.argv[1]).read_text())
root = pathlib.Path(sys.argv[2])
findings = report["findings"]
dead = [item for item in findings if item["ruleId"] == "quality.deadCode"]
assert any(item["path"] == "python_pkg/generated/client.js" and "unusedGenerated" in item["message"] for item in dead), findings
assert any(item["path"] == "python_pkg/generated/orphan.js" for item in dead), findings
assert not any(item["ruleId"] in {"policy.generatedJavaScriptOwnership", "policy.generationOwnership", "quality.format"} or "coverage" in item["ruleId"].lower() for item in findings), findings
assert not (root / "package.json").exists()
assert not (root / "node_modules").exists()
assert not (root / "producer-ran").exists()
assert (root / "python_pkg/generated/client.js").read_text() == 'export const used=()=>7;\nexport const unusedGenerated=()=>8;\n'
PY
  cp "${output}" "${fixture_root}/scoped-baseline.json"
  for scenario in caller-directory unrelated-root; do
    if [[ "${scenario}" == unrelated-root ]]; then
      printf '{"name":"unrelated-root","private":true}\n' >"${target}/package.json"
    fi
    status=0
    (cd "${target}/frontend" && run_policy "${target}" check --files python_pkg/generated/client.js --format json) || status=$?
    [[ "${status}" == 1 ]] || fail "${scenario}: expected scoped findings, received status ${status}: $(excerpt)"
    "${python}" -I -B - "${fixture_root}/scoped-baseline.json" "${output}" <<'PYTHON'
import json
import pathlib
import sys

before, after = [json.loads(pathlib.Path(path).read_text()) for path in sys.argv[1:]]
def dead_code(report):
    return sorted((item["path"], item.get("subject", ""), item["message"]) for item in report["findings"] if item["ruleId"] == "quality.deadCode")
assert dead_code(before) == dead_code(after), after["findings"]
assert not any("coverage" in item["ruleId"].lower() and "javascript" in item["ruleId"].lower() for item in after["findings"]), after["findings"]
PYTHON
  done
  expect_no_target_commands "${target}" "scoped installed checks"
}
