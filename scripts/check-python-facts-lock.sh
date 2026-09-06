#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

case "$(uname -s)" in
  Darwin) os_tag="darwin" ;;
  Linux) os_tag="linux" ;;
  *) echo "Unsupported OS for Python facts lock verification: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch_tag="arm64" ;;
  x86_64|amd64) arch_tag="x64" ;;
  *) echo "Unsupported architecture for Python facts lock verification: $(uname -m)" >&2; exit 1 ;;
esac

python_bin="${policy_root}/.tools/python/${os_tag}-${arch_tag}/python"
if [[ ! -x "${python_bin}" ]]; then
  echo "The policy-owned CPython runtime is unavailable at ${python_bin}." >&2
  exit 1
fi

"${python_bin}" -I -B - "${policy_root}" <<'PY'
import pathlib
import re
import sys
import tomllib

root = pathlib.Path(sys.argv[1])

def read(relative, maximum):
    data = root.joinpath(relative).read_bytes()
    if not data or len(data) > maximum:
        raise SystemExit(f"{relative} has an invalid size")
    return data

manifest = tomllib.loads(read("internal/pythonfacts/pyproject.toml", 1024 * 1024).decode("utf-8"))
lock = tomllib.loads(read("internal/pythonfacts/uv.lock", 8 * 1024 * 1024).decode("utf-8"))
versions = {name: read(f"tools/{name}-version.txt", 128).decode("ascii").strip() for name in ("astroid", "packaging")}
project = manifest["project"]
if set(manifest) != {"project", "tool"} or set(project) != {"name", "version", "requires-python", "dependencies"}:
    raise SystemExit("the Python facts manifest has an unexpected shape")
if project["name"] != "code-polishy-python-facts" or project["requires-python"] != "==3.12.*" or project["dependencies"] != [f"{name}=={version}" for name, version in versions.items()]:
    raise SystemExit("the Python facts manifest does not declare the exact carried dependencies")
if manifest["tool"] != {"uv": {"package": False}}:
    raise SystemExit("the Python facts manifest has unexpected uv settings")
if set(lock) != {"version", "revision", "requires-python", "package"} or lock["version"] != 1 or lock["revision"] != 3 or lock["requires-python"] != "==3.12.*":
    raise SystemExit("the Python facts lock has an unexpected identity")
packages = lock["package"]
if not isinstance(packages, list) or len(packages) != 3 or {item.get("name") for item in packages} != {project["name"], *versions}:
    raise SystemExit("the Python facts lock does not contain exactly the complete graph")
by_name = {item["name"]: item for item in packages}
owner = by_name[project["name"]]
if owner.get("version") != project["version"] or owner.get("source") != {"virtual": "."} or owner.get("dependencies") != [{"name": name} for name in versions]:
    raise SystemExit("the Python facts root lock entry does not match its manifest")
if owner.get("metadata") != {"requires-dist": [{"name": name, "specifier": f"=={version}"} for name, version in versions.items()]}:
    raise SystemExit("the Python facts lock metadata does not match its manifest")
for name, version in versions.items():
    distribution = by_name[name]
    if distribution.get("version") != version or distribution.get("source") != {"registry": "https://pypi.org/simple"} or distribution.get("dependencies"):
        raise SystemExit(f"the {name} lock entry has an invalid registry identity or graph")
    suffix = "checksums" if name == "astroid" else "wheel_checksums"
    inventory = {}
    for raw in read(f"tools/{name}_{suffix}.txt", 65536).decode().splitlines():
        if not raw.strip() or raw.lstrip().startswith("#"):
            continue
        parts = raw.split()
        if len(parts) != 2 or parts[0] in inventory or not re.fullmatch(r"[0-9a-f]{64}", parts[1]):
            raise SystemExit(f"the {name} checksum inventory is malformed")
        inventory[parts[0]] = parts[1]
    wheel_name = f"{name}-{version}-py3-none-any.whl"
    expected = {wheel_name, f"{name}-{version}.tar.gz"} if name == "astroid" else {wheel_name}
    if set(inventory) != expected:
        raise SystemExit(f"the {name} checksum inventory is incomplete")
    wheels = distribution.get("wheels")
    if not isinstance(wheels, list) or len(wheels) != 1:
        raise SystemExit(f"the {name} lock must identify exactly one wheel")
    wheel = wheels[0]
    if not wheel.get("url", "").endswith("/" + wheel_name) or wheel.get("hash") != "sha256:" + inventory.get(wheel_name, ""):
        raise SystemExit(f"the {name} wheel lock identity does not match its checksum inventory")
    sdist = distribution.get("sdist", {})
    if not sdist.get("url", "").endswith(f"/{name}-{version}.tar.gz") or not re.fullmatch(r"sha256:[0-9a-f]{64}", sdist.get("hash", "")):
        raise SystemExit(f"the {name} source-distribution lock identity is malformed")
    for artifact in (wheel, sdist):
        if not isinstance(artifact.get("size"), int) or artifact["size"] <= 0 or not artifact.get("upload-time"):
            raise SystemExit(f"the {name} lock artifact metadata is incomplete")
    if name == "astroid" and sdist["hash"] != "sha256:" + inventory.get(f"{name}-{version}.tar.gz", ""):
        raise SystemExit("the Astroid source archive does not match its checksum inventory")
PY
