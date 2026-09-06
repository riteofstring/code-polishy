#!/usr/bin/env bash

exercise_python_pydantic_fixture() {
  local fixture_root="$1" real_git="$2" release="$3" launcher="$4" lock="$5" policy_root="$6"
  local interpreters=("${release}"/.tools/python/*/python)
  if [[ "${#interpreters[@]}" -ne 1 || ! -x "${interpreters[0]}" ]]; then
    fail "python-pydantic: the release has no unique carried Python"
  fi
  "${interpreters[0]}" -I -B - "${fixture_root}" "${real_git}" "${launcher}" "${lock}" "${policy_root}" <<'PY'
import hashlib
import io
import json
import os
import pathlib
import shutil
import stat
import subprocess
import sys
import tomllib
import urllib.parse
import urllib.request
import zipfile

from packaging.tags import sys_tags
from packaging.utils import parse_wheel_filename

fixture, git, launcher, lock, policy_root = sys.argv[1:]
root = pathlib.Path(fixture) / "python-pydantic"
root.mkdir()
inputs = pathlib.Path(policy_root) / "tools/fixtures/python-pydantic"


def write(path, content):
    target = root / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content, encoding="utf-8")


def policy(*arguments, status=0):
    result = subprocess.run(
        [launcher, "--repo-root", str(root), *arguments, "--format", "json"],
        text=True, capture_output=True, timeout=300, check=False,
    )
    if result.returncode != status:
        raise AssertionError(f"{arguments}: exit {result.returncode}, expected {status}: {result.stdout[-8000:]} {result.stderr[-2000:]}")
    return json.loads(result.stdout)


def install_wheels(environment):
    site = pathlib.Path(subprocess.check_output(
        [str(environment), "-I", "-B", "-c", "import sysconfig; print(sysconfig.get_path('purelib'))"], text=True,
    ).strip())
    site.resolve().relative_to((root / ".venv").resolve())
    ranks = {tag: index for index, tag in enumerate(sys_tags())}
    packages = tomllib.loads((root / "uv.lock").read_text())["package"]
    for package in packages:
        if "virtual" in package["source"]:
            continue
        candidates = []
        for wheel in package["wheels"]:
            filename = urllib.parse.urlsplit(wheel["url"]).path.rsplit("/", 1)[-1]
            tags = parse_wheel_filename(filename)[3]
            scores = [ranks[tag] for tag in tags if tag in ranks]
            if scores:
                candidates.append((min(scores), filename, wheel))
        if not candidates:
            raise AssertionError(f"no frozen wheel matches carried CPython: {package['name']}")
        _, _, wheel = min(candidates)
        with urllib.request.urlopen(wheel["url"], timeout=60) as response:
            data = response.read(32 * 1024 * 1024 + 1)
        if len(data) != wheel["size"] or "sha256:" + hashlib.sha256(data).hexdigest() != wheel["hash"]:
            raise AssertionError(f"wheel does not match frozen content: {package['name']}")
        with zipfile.ZipFile(io.BytesIO(data)) as archive:
            for item in archive.infolist():
                path = pathlib.PurePosixPath(item.filename)
                if path.is_absolute() or ".." in path.parts or stat.S_ISLNK(item.external_attr >> 16):
                    raise AssertionError("wheel has an uncontained member")
                target = site.joinpath(*path.parts)
                target.resolve().relative_to(site.resolve())
                if item.is_dir():
                    target.mkdir(parents=True, exist_ok=True)
                else:
                    target.parent.mkdir(parents=True, exist_ok=True)
                    target.write_bytes(archive.read(item))


subprocess.run([git, "init", "--quiet", str(root)], check=True)
subprocess.run([git, "-C", str(root), "config", "user.name", "Installed release fixture"], check=True)
subprocess.run([git, "-C", str(root), "config", "user.email", "fixture@example.test"], check=True)
shutil.copy2(lock, root / ".code-polishy.lock.json")
subprocess.run([launcher, "--repo-root", str(root), "agents", "install"], check=True, capture_output=True)
subprocess.run([sys.executable, "-I", "-B", "-m", "venv", "--copies", "--without-pip", str(root / ".venv")], check=True)
executable = "Scripts/python.exe" if os.name == "nt" else "bin/python"
environment = root / ".venv" / executable
python = ".venv/" + executable
test_command = [python, "-B", "-m", "unittest", "discover", "-s", "src/tests", "-t", "src"]
config = {
    "version": 4,
    "project": {"kind": "application", "capabilities": []},
    "scope": {"entryPoints": ["src/main.py"], "pythonContracts": [{"project": "pyproject.toml", "kind": "type", "target": "pydantic.BaseModel", "attributes": ["model_config"], "annotatedFields": True, "reason": "Model validation consumes declared fields and configuration."}]},
    "quality": {},
    "modules": [{"name": "application", "paths": ["src/reported.py", "src/models/**", "src/main.py", "scripts/**"]}],
    "checks": [
        {"name": "application-build", "provides": ["build"], "argv": [python, "-B", "-m", "compileall", "-q", "src", "scripts"], "modules": ["application"], "runOn": ["build"]},
        {"name": "python-lock-sync", "provides": ["lock-sync"], "argv": [python, "-B", "scripts/lock_sync.py"], "runOn": ["supply-chain"]},
    ],
    "tests": {
        "paths": ["src/tests/**"],
        "ownership": [{"paths": ["src/tests/**"], "module": "application", "focusedSuite": "application-unit"}],
        "suites": [
            {"name": "application-unit", "kind": "unit", "scope": "module", "modules": ["application"], "paths": ["src/tests/**"], "argv": test_command},
            {"name": "repository-full", "kind": "integration", "scope": "repository", "cost": "standard", "paths": ["src/tests/**"], "argv": test_command, "runOn": ["full"]},
        ],
    },
    "exceptions": [],
}
write(".code-polishy.json", json.dumps(config, indent=2) + "\n")
write("scripts/lock_sync.py", '''import pathlib
import tomllib

root = pathlib.Path(__file__).resolve().parents[1]
project = tomllib.loads((root / "pyproject.toml").read_text())["project"]
locked = tomllib.loads((root / "uv.lock").read_text())["package"]
local = next(package for package in locked if package["source"] == {"virtual": "."})
requirements = local.get("metadata", {}).get("requires-dist", [])
assert sorted(project["dependencies"]) == sorted(requirement["name"] + requirement["specifier"] for requirement in requirements)
''')
write("pyproject.toml", '[project]\nname = "installed-pydantic-regression"\nversion = "1.0.0"\nrequires-python = "==3.12.*"\ndependencies = []\n\n[tool.uv]\npackage = false\nrequired-version = "==0.8.19"\n')
write("uv.lock", 'version = 1\nrevision = 3\nrequires-python = "==3.12.*"\n\n[[package]]\nname = "installed-pydantic-regression"\nversion = "1.0.0"\nsource = { virtual = "." }\n')
subprocess.run([git, "-C", str(root), "add", "-A"], check=True)
subprocess.run([git, "-C", str(root), "commit", "--quiet", "-m", "Empty dependency baseline"], check=True)
shutil.copy2(inputs / "pyproject.fixture.toml", root / "pyproject.toml")
shutil.copy2(inputs / "uv.fixture.lock", root / "uv.lock")
policy("dependency-review", "--base", "HEAD")
install_wheels(environment)

cases = [
    ("direct BaseModel", "DirectModel", {"direct_field": "value"}, '''from pydantic import BaseModel


class DirectModel(BaseModel):
    direct_field: str
'''),
    ("inherited model re-export", "InheritedModel", {"inherited_field": 7}, '''from models import ModelBase


class InheritedModel(ModelBase):
    inherited_field: int
'''),
    ("multiline annotated field", "AnnotatedModel", {"annotated_field": "value"}, '''from typing import Annotated

from pydantic import BaseModel, Field


class AnnotatedModel(BaseModel):
    annotated_field: Annotated[
        str,
        Field(min_length=1),
    ]
'''),
    ("multiline model config", "ConfiguredModel", {}, '''from pydantic import BaseModel, ConfigDict


class ConfiguredModel(BaseModel):
    model_config = ConfigDict(
        extra="forbid",
    )
'''),
]
for label, model, payload, source in cases:
    shutil.rmtree(root / "src", ignore_errors=True)
    if model == "InheritedModel":
        write("src/models/base.py", "from pydantic import BaseModel\n\n\nclass ProjectModel(BaseModel):\n    pass\n")
        write("src/models/__init__.py", "from .base import ProjectModel as ModelBase\n\n__all__ = [\"ModelBase\"]\n")
    write("src/reported.py", source)
    write("src/tests/__init__.py", "")
    write("src/main.py", f'''import json

from reported import {model}


def render() -> dict[str, object]:
    return {model}.model_validate({payload!r}).model_dump()


def main() -> None:
    print(json.dumps(render()))
''')
    write("src/tests/test_models.py", f'''import unittest

from main import render


class TestModels(unittest.TestCase):
    def test_model_serializes_declared_fields(self) -> None:
        self.assertEqual(render(), {payload!r})
''')
    project = (root / "pyproject.toml").read_text()
    if "[project.scripts]" not in project:
        write("pyproject.toml", project + '\n[project.scripts]\ninstalled-pydantic = "main:main"\n')
    policy("format", "--all")
    report = policy("check", "--files", "src/reported.py")
    if report["findings"]:
        raise AssertionError(f"{label}: expected a clean installed check: {report['findings']}")
    policy("test", "--suite", "application-unit")
    original = (root / "src/reported.py").read_text()
    write("src/reported.py", original + "\n\nclass Plain:\n    unrelated_field: str\n\n    def unused_method(self) -> int:\n        return 7\n")
    focused = policy("check", "--files", "src/reported.py")
    if any(finding["ruleId"] == "quality.deadCode" for finding in focused["findings"]):
        raise AssertionError(f"{label}: focused check unexpectedly ran dead-code analysis: {focused['findings']}")
    report = policy("check", "--all", status=1)
    first_line = original.count("\n") + 3
    locations = {(finding["path"], finding["line"]) for finding in report["findings"]}
    expected = {("src/reported.py", first_line + offset) for offset in (0, 1, 3)}
    if locations != expected or any(finding["ruleId"] != "quality.deadCode" for finding in report["findings"]):
        raise AssertionError(f"{label}: unrelated members were hidden or other checks failed: {report['findings']}")
    print(f"Installed Pydantic case passed: {label}", flush=True)
PY
}
