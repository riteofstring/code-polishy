#!/usr/bin/env bash

exercise_python_reachability_fixture() {
  local fixture_root="$1" real_git="$2" release="$3" launcher="$4" lock="$5" python
  local interpreters=("${release}"/.tools/python/*/python)
  if [[ "${#interpreters[@]}" -ne 1 || ! -x "${interpreters[0]}" ]]; then
    fail "python-reachability: the release has no unique carried Python"
  fi
  python="${interpreters[0]}"
  "${python}" -I -B - "${fixture_root}" "${launcher}" "${lock}" "${real_git}" <<'PY'
import ast
import base64
import hashlib
import json
import pathlib
import shutil
import subprocess
import sys

fixture, launcher, lock, git = sys.argv[1:]
root = pathlib.Path(fixture) / "python-reachability"
root.mkdir()


def write(path, text):
    destination = root / path
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(text, encoding="utf-8")


def command(*arguments, status=0, machine=True):
    argv = [launcher, "--repo-root", str(root), *arguments]
    if machine:
        argv.extend(("--format", "json"))
    result = subprocess.run(argv, text=True, capture_output=True, timeout=180, check=False)
    if result.returncode != status:
        raise AssertionError(
            f"{arguments}: exit {result.returncode}, expected {status}: "
            f"{result.stdout[-6000:]} {result.stderr[-2000:]}"
        )
    return json.loads(result.stdout) if machine else result.stdout


def save_config():
    write(".code-polishy.json", json.dumps(config, indent=2) + "\n")


def digest(path):
    return hashlib.sha256((root / path).read_bytes()).hexdigest()


def position(node):
    return {"line": node.lineno, "column": node.col_offset + 1}


def declare_loader(argument, *, registry=None):
    imports = "from pkgutil import resolve_name\nfrom app.contracts import Contract\n"
    if registry:
        imports += "import json\nfrom pathlib import Path\n"
    source = imports + "\ndef load(name):\n" + (
        f"    plugin = resolve_name({argument})\n"
        "    if not issubclass(plugin, Contract):\n"
        "        raise TypeError\n"
        "    return plugin\n"
    )
    write("src/app/loader.py", source)
    calls = [node for node in ast.walk(ast.parse(source)) if isinstance(node, ast.Call)]
    load = next(node for node in calls if isinstance(node.func, ast.Name) and node.func.id == "resolve_name")
    check = next(node for node in calls if isinstance(node.func, ast.Name) and node.func.id == "issubclass")
    declaration = {
        "project": "pyproject.toml", "distribution": "plug-dist",
        "namespace": "third_party.plugins", "inputGrammar": "python-module-object/v1",
        "consumer": {
            "kind": "callsite", "importer": "src/app/loader.py", "module": "app.loader",
            "callable": "load", "site": position(load), "callee": "pkgutil.resolve_name",
            "shape": "module-object-call/v1", "argument": ast.unparse(load.args[0]),
            "sourceSha256": digest("src/app/loader.py"),
        },
        "check": {"kind": "issubclass", "protocol": "app.contracts.Contract", "site": position(check)},
    }
    if registry:
        declaration["configuration"] = {
            "path": "src/app/registry.json", "jsonPointer": "/plugins",
            "sha256": digest("src/app/registry.json"),
        }
    config["scope"]["pythonExternalPluginImports"] = [declaration]
    save_config()
    return declaration


def architecture_pass(selected="src/app/loader.py"):
    report = command("architecture", "--files", selected)
    if report["findings"]:
        raise AssertionError(f"external composition has unexpected findings: {report['findings']}")
    graph = report["sourceDependencyGraph"]
    if len(graph["externalCompositions"]) != 1:
        raise AssertionError("external load did not create one composition entry")
    if any(node["path"].startswith("third_party") for node in graph["nodes"]):
        raise AssertionError("external contract created a repository graph node")
    edge = graph["externalCompositions"][0]
    if edge["dependency"]["distribution"] != "plug-dist" or edge["contract"]["runtimeType"] != "app.contracts.Contract":
        raise AssertionError(f"external composition lost dependency or runtime proof: {edge}")
    return graph


def architecture_failure(description):
    report = command("architecture", "--files", "src/app/loader.py", status=1)
    if not any(finding["ruleId"] == "policy.pythonExternalPluginImport" for finding in report["findings"]):
        raise AssertionError(f"{description} did not fail its external contract: {report['findings']}")
    if report.get("sourceDependencyGraph"):
        raise AssertionError(f"{description} retained a complete graph")


config = {
    "version": 4, "project": {"kind": "application", "capabilities": []},
    "scope": {}, "quality": {}, "modules": [{"name": "application", "paths": ["src/app/**"]}],
    "tests": {
        "ownership": [{"paths": ["src/tests/**"], "module": "application", "focusedSuite": "contracts"}],
        "suites": [{"name": "contracts", "scope": "module", "kind": "unit", "modules": ["application"],
                    "paths": ["src/tests/**"], "cwd": "src", "argv": ["python", "-m", "unittest", "discover", "-s", "tests"]}],
    },
}
save_config()
write(".gitignore", ".code-polishy-reports/\n.venv/\n")
shutil.copyfile(lock, root / ".code-polishy.lock.json")
subprocess.run([git, "-C", str(root), "init", "--quiet"], check=True)
command("agents", "install", machine=False)
write("pyproject.toml", "[project]\nname='plugin-host'\nversion='1.0'\nrequires-python='==3.12.*'\ndependencies=['plug-dist==1.0']\n")
registry_lock = "version=1\n[[package]]\nname='plug-dist'\nversion='1.0.0'\nsource={registry='https://pypi.org/simple'}\n"
write("uv.lock", registry_lock)
write("src/app/contracts.py", "from typing import Protocol, runtime_checkable\n\n@runtime_checkable\nclass Contract(Protocol):\n    def run(self):\n        ...\n")
write("src/tests/test_contract.py", '''import unittest
from app.contracts import Contract

class Runnable:
    def run(self):
        return 1

class ContractTests(unittest.TestCase):
    def test_runtime_shape(self):
        self.assertTrue(issubclass(Runnable, Contract))
        self.assertFalse(issubclass(str, Contract))
''')
declaration = declare_loader("'third_party.plugins:Plugin'")
literal = architecture_pass()
sarif_text = command("architecture", "--files", "src/app/loader.py", "--format", "sarif", machine=False)
sarif = json.loads(sarif_text)
if sarif["runs"][0]["properties"]["codePolishyReport"]["sourceDependencyGraph"] != literal:
    raise AssertionError("installed SARIF changed the canonical external graph")

declare_loader("'third_party.plugins:Plugin'")
write("src/app/loader.py", (root / "src/app/loader.py").read_text() + "\n")
architecture_failure("stale loader source")
declaration = declare_loader("'third_party.plugins:Plugin'")
source = (root / "src/app/loader.py").read_text()
source = source.replace("plugin = resolve_name", "selected = resolve_name")
source = source.replace("issubclass(plugin", "issubclass(selected")
source = source.replace("return plugin", "return selected")
write("src/app/loader.py", source)
declaration["consumer"]["sourceSha256"] = digest("src/app/loader.py")
config["scope"]["pythonExternalPluginImports"] = [declaration]
save_config()
architecture_pass()
declare_loader("'third_party.plugins:Plugin'")
write("pyproject.toml", "[project]\nname='plugin-host'\nrequires-python='==3.12.*'\ndependencies=[]\n")
architecture_failure("transitive-only distribution")

commit = "0123456789abcdef0123456789abcdef01234567"
write("pyproject.toml", f"[project]\nname='plugin-host'\nrequires-python='==3.12.*'\ndependencies=['plug-dist @ git+ssh://git@private.example.test/team/plugin.git@{commit}']\n")
write("uv.lock", f"version=1\n[[package]]\nname='plug-dist'\nversion='1.0'\nsource={{git='ssh://git@private.example.test/team/plugin.git?rev={commit}#{commit}'}}\n")
private = architecture_pass()
if private["externalCompositions"][0]["dependency"]["kind"] != "git":
    raise AssertionError("private exact Git dependency lost its source identity")

write("src/app/registry.json", '{"plugins":{"chosen":"third_party.plugins:Plugin"}}\n')
registry_argument = "json.loads(Path('src/app/registry.json').read_text(encoding='utf-8'))['plugins'][name]"
declare_loader(registry_argument, registry=True)
registry = architecture_pass("src/app/registry.json")
write("src/app/registry.json", '{"plugins":{"chosen":"third_party.plugins.other:Plugin"}}\n')
architecture_failure("stale registry bytes")
declare_loader(registry_argument, registry=True)
updated = architecture_pass("src/app/registry.json")
if updated["externalCompositions"][0]["contract"]["inputSha256"] == registry["externalCompositions"][0]["contract"]["inputSha256"]:
    raise AssertionError("registry update retained stale input proof")

print("Installed Python external composition cases passed.")

root = pathlib.Path(fixture) / "python-external-contracts"
root.mkdir()
config["scope"] = {}
config["tests"]["suites"].append({"name": "full", "scope": "repository", "kind": "integration", "paths": ["src/tests/**"], "cwd": "src", "argv": ["python", "-m", "unittest", "discover", "-s", "tests"], "runOn": ["full"]})
lock_check = "import pathlib,tomllib; project=tomllib.loads(pathlib.Path('pyproject.toml').read_text()); package=tomllib.loads(pathlib.Path('uv.lock').read_text())['package'][0]; source=package['source']; expected='framework=='+package['version'] if 'registry' in source else 'framework @ git+'+source['git'].split('?rev=')[0]+'@'+source['git'].split('#')[1]; assert project['project']['dependencies']==[expected]"
config["checks"] = [
    {"name": "build", "provides": ["build"], "modules": ["application"], "argv": ["python", "-B", "-m", "compileall", "-q", "src"], "runOn": ["build"]},
    {"name": "lock-sync", "provides": ["lock-sync"], "modules": ["application"], "argv": ["python", "-I", "-B", "-c", lock_check], "runOn": ["supply-chain"]},
]
save_config()
write("src/tests/test_factory.py", "import unittest\nfrom app.plugin import main\n\nclass FactoryTests(unittest.TestCase):\n    def test_loader_supports_independent_instances(self):\n        self.assertIsNot(main()(), main()())\n")
write(".gitignore", ".code-polishy-reports/\n.venv/\n")
shutil.copyfile(lock, root / ".code-polishy.lock.json")
subprocess.run([git, "-C", str(root), "init", "--quiet"], check=True)
command("agents", "install", machine=False)
subprocess.run([sys.executable, "-I", "-B", "-m", "venv", "--copies", "--without-pip", str(root / ".venv")], check=True)
site = next((root / ".venv/lib").glob("python*/site-packages"))
manifest = "[project]\nname='contract-host'\nversion='1.0'\nrequires-python='==3.12.*'\ndependencies=['framework==1.0']\n[project.scripts]\ncontract-host='app.plugin:main'\n"
write("pyproject.toml", manifest)
write("uv.lock", "version=1\n[[package]]\nname='framework'\nversion='1.0'\nsource={registry='https://pypi.org/simple'}\n")


def install_contract(source, origin=None):
    metadata = "framework-1.0.dist-info/"
    files = {"framework.py": source, metadata + "METADATA": "Metadata-Version: 2.4\nName: framework\nVersion: 1.0\n\n"}
    origin_path = site / metadata / "direct_url.json"
    if origin is not None:
        files[metadata + "direct_url.json"] = json.dumps(origin)
    elif origin_path.exists():
        origin_path.unlink()
    record = ""
    for name, content in files.items():
        write(str((site / name).relative_to(root)), content)
        encoded = content.encode()
        sha = base64.urlsafe_b64encode(hashlib.sha256(encoded).digest()).decode().rstrip("=")
        record += f"{name},sha256={sha},{len(encoded)}\n"
    write(str((site / metadata / "RECORD").relative_to(root)), record + metadata + "RECORD,,\n")


def check_contract(*, rejected=False):
    report = command("check", "--files", "src/app/plugin.py", status=1)
    findings = report["findings"]
    dead = {finding["line"] for finding in findings if finding["ruleId"] == "quality.deadCode"}
    expected = {methods["unused_hook"].lineno}
    if rejected:
        expected.add(methods["on_event"].lineno)
    errors = [finding for finding in findings if finding["ruleId"] == "policy.pythonReachability"]
    if dead != expected or bool(errors) != rejected or any(finding["ruleId"] not in {"quality.deadCode", "policy.pythonReachability"} for finding in findings):
        raise AssertionError(f"external contract retained wrong members or failed unrelated checks: {findings}")


for kind in ("base", "protocol", "decorator", "entry-point"):
    member = "    def on_event(self) -> int:\n        return 1\n"
    remote = "class Contract:\n" + member
    local = "from framework import Contract\n\nclass Plugin(Contract):\n"
    if kind == "protocol":
        remote = "from typing import Protocol\nclass Contract(Protocol):\n" + member
    elif kind == "decorator":
        remote = "def Contract(function):\n    return function\n"
        local = "from framework import Contract\n\nclass Plugin:\n    @Contract\n"
    elif kind == "entry-point":
        remote = "from typing import Protocol\nclass Interface(Protocol):\n" + member + "def Contract(plugin: type[Interface]) -> type[Interface]:\n    return plugin\n"
        local = "from framework import Contract\n\nclass Plugin:\n"
    local += member + "    def unused_hook(self) -> int:\n        return 2\n"
    if kind == "entry-point":
        local += "\nContract(Plugin)\n"
    local += "\ndef main() -> type[Plugin]:\n    return Plugin\n"
    write("src/app/plugin.py", local)
    install_contract(remote)
    config["scope"] = {}
    save_config()
    command("format", "--files", "src/app/plugin.py")
    tree = ast.parse((root / "src/app/plugin.py").read_text())
    implementation = next(node for node in tree.body if isinstance(node, ast.ClassDef))
    methods = {node.name: node for node in implementation.body if isinstance(node, ast.FunctionDef)}
    binding = methods["on_event"] if kind == "decorator" else implementation
    if kind == "entry-point":
        binding = next(node.value for node in tree.body if isinstance(node, ast.Expr) and isinstance(node.value, ast.Call))
    consumer = {"kind": kind, "importer": "src/app/plugin.py", "module": "app.plugin", "site": position(binding), "sourceSha256": digest("src/app/plugin.py"), "distribution": "framework", "qualified": "framework.Contract", "implementation": "Plugin", "member": "on_event"}
    target = {"module": "app.plugin", "symbol": "Plugin.on_event"}
    config["scope"]["pythonDynamicReferences"] = [{"kind": "target", "project": "pyproject.toml", "target": target, "consumer": consumer}]
    save_config()
    check_contract()
    if kind in {"base", "protocol", "entry-point"}:
        consumer["member"], target["symbol"] = "unused_hook", "Plugin.unused_hook"
        save_config()
        check_contract(rejected=True)
        consumer["member"], target["symbol"] = "on_event", "Plugin.on_event"
        save_config()
    if kind == "base":
        url = "ssh://git@private.example.test/team/framework.git"
        write("pyproject.toml", manifest.replace("framework==1.0", f"framework @ git+{url}@{commit}"))
        write("uv.lock", f"version=1\n[[package]]\nname='framework'\nversion='1.0'\nsource={{git='{url}?rev={commit}#{commit}'}}\n")
        origin = {"url": url, "vcs_info": {"vcs": "git", "commit_id": commit}}
        install_contract(remote, origin)
        check_contract()
        origin["vcs_info"]["commit_id"] = "a" * 40
        install_contract(remote, origin)
        check_contract(rejected=True)
        write("pyproject.toml", manifest)
        write("uv.lock", "version=1\n[[package]]\nname='framework'\nversion='1.0'\nsource={registry='https://pypi.org/simple'}\n")
    print(f"Installed external reachability contract passed: {kind}", flush=True)
PY
}
